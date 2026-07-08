package api

import (
	"context"
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
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/transcoder"
)

func setupTaskRouter(t *testing.T) (*gin.Engine, *tasksvc.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.Space{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	spaces := []models.Space{
		{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now},
		{ID: "space-other", Name: "其他 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now},
	}
	if err := gdb.Create(&spaces).Error; err != nil {
		t.Fatalf("创建 Space 失败: %v", err)
	}
	svc := tasksvc.NewService(gdb)
	h := NewHandler(library.NewService(gdb)).WithTasks(svc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc
}

func TestTaskAPI_ListDetailStatsScopeAndLegacyStatus(t *testing.T) {
	r, svc := setupTaskRouter(t)
	ctx := context.Background()
	defaultTask := mustEnqueueAPITask(t, svc, tasksvc.EnqueueInput{
		Scope:        models.TaskScopeSpace,
		SpaceID:      models.DefaultSpaceID,
		Type:         "library.scan",
		Priority:     7,
		ResourceType: "library",
		ResourceID:   "1",
	})
	succeeded := mustEnqueueAPITask(t, svc, tasksvc.EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "transcode.hls",
	})
	claimed, err := svc.ClaimNext(ctx, tasksvc.ClaimQuery{Type: "transcode.hls"})
	if err != nil {
		t.Fatalf("领取任务失败: %v", err)
	}
	if err := svc.MarkSucceeded(ctx, claimed.ID); err != nil {
		t.Fatalf("标记任务成功失败: %v", err)
	}
	mustEnqueueAPITask(t, svc, tasksvc.EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: "space-other",
		Type:    "library.scan",
	})
	systemTask := mustEnqueueAPITask(t, svc, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSystem,
		Type:  "tool.download",
	})

	w := serveTaskRequest(r, "GET", "/api/tasks?page=1&page_size=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("查询任务列表期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var page struct {
		Items []taskResponse `json:"items"`
		Total int64          `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析任务列表失败: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("默认 Space 应有 2 条任务, 实际 %+v", page)
	}
	if page.Items[0].SpaceID == nil || *page.Items[0].SpaceID != models.DefaultSpaceID {
		t.Fatalf("任务列表应只返回默认 Space 任务: %+v", page.Items)
	}

	w = serveTaskRequest(r, "GET", "/api/tasks?scope=system", "")
	if w.Code != http.StatusOK {
		t.Fatalf("查询系统任务期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析系统任务列表失败: %v", err)
	}
	if page.Total != 1 || page.Items[0].ID != taskIDString(systemTask.ID) || page.Items[0].SpaceID != nil {
		t.Fatalf("系统任务查询应只返回 space_id 为空的系统任务: %+v", page)
	}

	w = serveTaskRequest(r, "GET", "/api/tasks/"+taskIDString(defaultTask.ID), "space-other")
	if w.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 任务详情应返回 404, 实际 %d", w.Code)
	}

	w = serveTaskRequest(r, "GET", "/api/tasks?status=completed", "")
	if w.Code != http.StatusOK {
		t.Fatalf("旧 completed 状态过滤期望 200, 实际 %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("解析 completed 过滤失败: %v", err)
	}
	if page.Total != 1 || page.Items[0].ID != taskIDString(succeeded.ID) || page.Items[0].Status != models.TaskStatusSucceeded {
		t.Fatalf("completed 应映射为 succeeded: %+v", page)
	}

	w = serveTaskRequest(r, "GET", "/api/tasks/stats", "")
	if w.Code != http.StatusOK {
		t.Fatalf("查询任务统计期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var stats struct {
		Total    int64            `json:"total"`
		ByStatus map[string]int64 `json:"by_status"`
		ByType   map[string]int64 `json:"by_type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("解析任务统计失败: %v", err)
	}
	if stats.Total != 2 || stats.ByStatus[models.TaskStatusPending] != 1 || stats.ByStatus[models.TaskStatusSucceeded] != 1 {
		t.Fatalf("任务统计状态桶异常: %+v", stats)
	}
	if stats.ByType["library.scan"] != 1 || stats.ByType["transcode.hls"] != 1 {
		t.Fatalf("任务统计类型桶异常: %+v", stats)
	}
}

func TestTaskAPI_CancelAndRetryReturnTask(t *testing.T) {
	r, svc := setupTaskRouter(t)
	task := mustEnqueueAPITask(t, svc, tasksvc.EnqueueInput{
		Scope:   models.TaskScopeSpace,
		SpaceID: models.DefaultSpaceID,
		Type:    "thumbnail.generate",
	})

	w := serveTaskRequest(r, "POST", "/api/tasks/"+taskIDString(task.ID)+"/cancel", "")
	if w.Code != http.StatusOK {
		t.Fatalf("取消任务期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var canceled taskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &canceled); err != nil {
		t.Fatalf("解析取消响应失败: %v", err)
	}
	if canceled.Status != models.TaskStatusCanceled || canceled.Error != nil {
		t.Fatalf("取消响应异常: %+v", canceled)
	}

	w = serveTaskRequest(r, "POST", "/api/tasks/"+taskIDString(task.ID)+"/retry", "")
	if w.Code != http.StatusOK {
		t.Fatalf("重试任务期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var retried taskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &retried); err != nil {
		t.Fatalf("解析重试响应失败: %v", err)
	}
	if retried.Status != models.TaskStatusPending || retried.Progress != 0 || retried.Error != nil {
		t.Fatalf("重试响应异常: %+v", retried)
	}
}

func TestTaskAPI_LegacyQueueActionsSyncMirror(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.Space{}, &models.ScanTask{}, &models.TranscodeTask{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	if err := gdb.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("创建 Space 失败: %v", err)
	}

	svc := tasksvc.NewService(gdb)
	scanQueue := library.NewTaskQueue(gdb, func(int64, string, string, string) (int, error) { return 0, nil }).WithTasks(svc)
	pregenQueue := transcoder.NewPregenQueue(gdb, func(int64, string) error { return nil }).WithTasks(svc)
	h := NewHandler(library.NewService(gdb)).WithTasks(svc).WithScanQueue(scanQueue).WithTranscodePresets(nil, pregenQueue)
	r := gin.New()
	RegisterRoutes(r, h)

	scanID, err := scanQueue.EnqueueInSpace(models.DefaultSpaceID, 1, "D:/media", "local", models.ScanTypeFull)
	if err != nil {
		t.Fatalf("扫描任务入队失败: %v", err)
	}
	scanMirror := findUnifiedTask(t, svc, "library.scan", models.TaskStatusPending)
	w := serveTaskRequest(r, "POST", "/api/tasks/"+taskIDString(scanMirror.ID)+"/cancel", "")
	if w.Code != http.StatusOK {
		t.Fatalf("任务中心取消扫描镜像期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var scanTask models.ScanTask
	if err := gdb.First(&scanTask, scanID).Error; err != nil {
		t.Fatalf("读取扫描旧任务失败: %v", err)
	}
	if scanTask.Status != models.ScanTaskStatusCanceled {
		t.Fatalf("旧扫描任务应被取消: %+v", scanTask)
	}
	scanMirror = findUnifiedTask(t, svc, "library.scan", models.TaskStatusCanceled)
	if scanMirror.Status != models.TaskStatusCanceled {
		t.Fatalf("扫描镜像应同步取消: %+v", scanMirror)
	}

	transcodeID, err := pregenQueue.EnqueueInSpace(models.DefaultSpaceID, 2, 3, "h264", 0, 0)
	if err != nil {
		t.Fatalf("预生成任务入队失败: %v", err)
	}
	if err := pregenQueue.CancelTaskInSpace(models.DefaultSpaceID, transcodeID); err != nil {
		t.Fatalf("预生成任务预置取消失败: %v", err)
	}
	transcodeMirror := findUnifiedTask(t, svc, "transcode.hls", models.TaskStatusCanceled)
	w = serveTaskRequest(r, "POST", "/api/tasks/"+taskIDString(transcodeMirror.ID)+"/retry", "")
	if w.Code != http.StatusOK {
		t.Fatalf("任务中心重试预生成镜像期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var transcodeTask models.TranscodeTask
	if err := gdb.First(&transcodeTask, transcodeID).Error; err != nil {
		t.Fatalf("读取预生成旧任务失败: %v", err)
	}
	if transcodeTask.Status != models.TranscodeTaskStatusPending {
		t.Fatalf("旧预生成任务应回到 pending: %+v", transcodeTask)
	}
	transcodeMirror = findUnifiedTask(t, svc, "transcode.hls", models.TaskStatusPending)
	if transcodeMirror.Status != models.TaskStatusPending {
		t.Fatalf("预生成镜像应同步 pending: %+v", transcodeMirror)
	}
}

func mustEnqueueAPITask(t *testing.T, svc *tasksvc.Service, input tasksvc.EnqueueInput) *models.Task {
	t.Helper()
	task, err := svc.Enqueue(context.Background(), input)
	if err != nil {
		t.Fatalf("入队任务失败: %v", err)
	}
	return task
}

func serveTaskRequest(r *gin.Engine, method, path, spaceID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if spaceID != "" {
		req.Header.Set(spaceHeader, spaceID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func taskIDString(id int64) string {
	return strconv.FormatInt(id, 10)
}

func findUnifiedTask(t *testing.T, svc *tasksvc.Service, taskType, status string) models.Task {
	t.Helper()
	page, err := svc.List(context.Background(), tasksvc.Query{
		SpaceID: models.DefaultSpaceID,
		Type:    taskType,
		Status:  status,
	})
	if err != nil {
		t.Fatalf("查询统一任务失败: %v", err)
	}
	if len(page.Items) == 0 {
		t.Fatalf("未找到 type=%s status=%s 的统一任务", taskType, status)
	}
	return page.Items[0]
}
