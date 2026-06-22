package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

// setupScanQueueRouter 构造注入了扫描任务队列的测试路由。
// exec 为扫描执行替身，避免依赖真实文件系统遍历。
func setupScanQueueRouter(t *testing.T, exec library.ScanExecFunc) (*gin.Engine, *gorm.DB, *library.TaskQueue) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.ScanTask{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	svc := library.NewService(gdb)
	q := library.NewTaskQueue(gdb, exec)
	q.Start()
	t.Cleanup(q.Stop)

	h := NewHandler(svc).WithScanQueue(q)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, q
}

// TestScanLibrary_Enqueues 触发扫描时入队任务并返回 task_id，而非直接执行。
func TestScanLibrary_Enqueues(t *testing.T) {
	exec := func(libraryID int64, path, dirType, mode string) (int, error) { return 5, nil }
	r, gdb, _ := setupScanQueueRouter(t, exec)

	svc := library.NewService(gdb)
	lp, err := svc.CreateLibraryPath(t.TempDir(), "local", "测试库")
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/library/scan/"+strconv.FormatInt(lp.ID, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "queued" {
		t.Fatalf("响应 status 期望 queued, 实际 %v", resp["status"])
	}
	if _, ok := resp["task_id"]; !ok {
		t.Fatalf("响应应含 task_id, body: %s", w.Body.String())
	}

	// 任务最终完成
	deadline := time.Now().Add(2 * time.Second)
	var completed bool
	for time.Now().Before(deadline) {
		var cnt int64
		gdb.Model(&models.ScanTask{}).Where("status = ?", models.ScanTaskStatusCompleted).Count(&cnt)
		if cnt == 1 {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !completed {
		t.Fatal("入队任务最终应完成")
	}
}

// TestListScanTasks 列任务端点返回任务列表与当前进行中任务。
func TestListScanTasks(t *testing.T) {
	// exec 阻塞，确保任务处于 running 时能观察到 current
	release := make(chan struct{})
	exec := func(libraryID int64, path, dirType, mode string) (int, error) {
		<-release
		return 3, nil
	}
	r, gdb, _ := setupScanQueueRouter(t, exec)

	svc := library.NewService(gdb)
	lp, _ := svc.CreateLibraryPath(t.TempDir(), "local", "库")

	// 触发两次扫描
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/library/scan/"+strconv.FormatInt(lp.ID, 10), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("触发扫描期望 200, 实际 %d", w.Code)
		}
	}

	// 等到有一个任务进入 running
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var cnt int64
		gdb.Model(&models.ScanTask{}).Where("status = ?", models.ScanTaskStatusRunning).Count(&cnt)
		if cnt == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	req := httptest.NewRequest("GET", "/api/library/scan/tasks", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列任务期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Tasks   []models.ScanTask `json:"tasks"`
		Current *models.ScanTask  `json:"current"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("应有 2 条任务, 实际 %d", len(resp.Tasks))
	}
	if resp.Current == nil || resp.Current.Status != models.ScanTaskStatusRunning {
		t.Fatalf("current 应为进行中任务, 实际 %+v", resp.Current)
	}

	close(release)
}
