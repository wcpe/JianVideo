package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/player"
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
	exec := func(_ int64, _, _, _ string) (int, error) { return 5, nil }
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

// TestScanLibrary_DoesNotCreateCostlyTranscodesAfterSuccess 验证扫描仅负责入库，不自动生成 HLS 转码产物。
func TestScanLibrary_DoesNotCreateCostlyTranscodesAfterSuccess(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(
		&models.Space{},
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.MediaTypeRule{},
		&models.ScanTask{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	if err := gdb.Create(&models.Space{ID: "space-a", Name: "Space A", OwnerUserID: 1}).Error; err != nil {
		t.Fatalf("创建测试 Space 失败: %v", err)
	}
	svc := library.NewService(gdb)
	mediaDir := t.TempDir()
	lp, err := svc.CreateLibraryPathInSpace("space-a", mediaDir, "local", "测试库")
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	newPath := filepath.Join(mediaDir, "new-after-scan.mp4")
	if err := os.WriteFile(newPath, []byte("new video"), 0o644); err != nil {
		t.Fatalf("写入新视频失败: %v", err)
	}

	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	q := library.NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) {
		close(scanStarted)
		<-releaseScan
		mf := models.MediaFile{
			SpaceID:   "space-a",
			LibraryID: lp.ID,
			FilePath:  filepath.ToSlash(newPath),
			FileName:  filepath.Base(newPath),
			Format:    "mp4",
			FileState: models.MediaFileStateAvailable,
			AddedAt:   time.Now(),
		}
		return 1, gdb.Create(&mf).Error
	})

	hlsDir := t.TempDir()
	h := NewHandler(svc).WithHLSPreSlice(hlsDir, player.NewHLSManager(hlsDir)).WithScanQueue(q)
	q.Start()
	defer q.Stop()

	r := gin.New()
	RegisterRoutes(r, h)
	req := httptest.NewRequest("POST", "/api/library/scan/"+strconv.FormatInt(lp.ID, 10), nil)
	req.Header.Set("X-JianVideo-Space-Id", "space-a")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("触发扫描期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	select {
	case <-scanStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("扫描任务未开始")
	}
	entries, err := os.ReadDir(hlsDir)
	if err != nil {
		t.Fatalf("读取 HLS 目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("扫描完成前不得自动创建高成本转码，实际生成 %d 项", len(entries))
	}
	close(releaseScan)

	deadline := time.Now().Add(5 * time.Second)
	var newMedia models.MediaFile
	completed := false
	for time.Now().Before(deadline) {
		mediaErr := gdb.Where("space_id = ? AND file_path = ?", "space-a", filepath.ToSlash(newPath)).First(&newMedia).Error
		var completedCount int64
		gdb.Model(&models.ScanTask{}).Where("status = ?", models.ScanTaskStatusCompleted).Count(&completedCount)
		if mediaErr == nil && completedCount == 1 {
			completed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if newMedia.ID == 0 {
		t.Fatal("扫描后新视频未入库")
	}
	if !completed {
		t.Fatal("扫描任务未完成")
	}
	entries, err = os.ReadDir(hlsDir)
	if err != nil {
		t.Fatalf("读取 HLS 目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("扫描完成后不得自动创建高成本转码，实际生成 %d 项", len(entries))
	}
}

func TestListScanTasks(t *testing.T) {
	// exec 阻塞，确保任务处于 running 时能观察到 current
	release := make(chan struct{})
	exec := func(_ int64, _, _, _ string) (int, error) {
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
