package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

func setupInferenceRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.MediaInference{}, &models.AuditEvent{}, &models.Task{}); err != nil {
		t.Fatalf("迁移推断测试表失败: %v", err)
	}
	auditSvc := audit.NewRecorder(db)
	libSvc := library.NewService(db).WithAudit(auditSvc)
	taskSvc := tasksvc.NewService(db).WithAudit(auditSvc)
	workers := tasksvc.NewWorkerRegistry(taskSvc)
	if err := RegisterInferenceBackfillWorker(workers, libSvc); err != nil {
		t.Fatalf("注册推断回填 worker 失败: %v", err)
	}
	handler := NewHandler(libSvc).WithTasks(taskSvc).WithTaskWorkers(workers)
	r := gin.New()
	RegisterRoutes(r, handler)
	return r, libSvc, db
}

func TestMediaInferenceAPIManualCorrectionAndBackfill(t *testing.T) {
	r, libSvc, _ := setupInferenceRouter(t)
	dir := t.TempDir()
	lp, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	mf, err := libSvc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Movie.Name.2024.1080p.mkv"), 10)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/library/media/"+strconvID(mf.ID)+"/inference", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET 推断状态码 got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Movie Name") {
		t.Fatalf("GET 推断应返回自动标题: %s", resp.Body.String())
	}

	body := bytes.NewBufferString(`{"title":"人工片名","year":2025}`)
	req = httptest.NewRequest(http.MethodPut, "/api/library/media/"+strconvID(mf.ID)+"/inference", body)
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT 推断状态码 got=%d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"manual":true`) || !strings.Contains(resp.Body.String(), "人工片名") {
		t.Fatalf("PUT 推断应返回人工纠正: %s", resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/library/inference/backfill", bytes.NewBufferString(`{"library_id":`+strconvID(lp.ID)+`}`))
	req.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("backfill 状态码 got=%d body=%s", resp.Code, resp.Body.String())
	}
	var backfill struct {
		Status string `json:"status"`
		TaskID int64  `json:"task_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &backfill); err != nil {
		t.Fatalf("解析 backfill 响应失败: %v", err)
	}
	if backfill.Status != models.TaskStatusPending || backfill.TaskID == 0 {
		t.Fatalf("backfill 应返回待执行任务: %+v", backfill)
	}
	waitInferenceTaskTerminal(t, r, backfill.TaskID, models.TaskStatusSucceeded)

	got, _ := libSvc.GetMediaInferenceInSpace(models.DefaultSpaceID, mf.ID)
	if got.Title != "人工片名" || !got.Manual {
		t.Fatalf("backfill 不得覆盖人工值: %+v", got)
	}
}

func TestMediaInferenceAPIHonorsGlobalSwitch(t *testing.T) {
	r, libSvc, db := setupInferenceRouter(t)
	libSvc.WithInferenceConfigProvider(func(string, int64) library.InferenceConfig {
		return library.InferenceConfig{Enabled: false}
	})
	mf := models.MediaFile{
		SpaceID:    models.DefaultSpaceID,
		LibraryID:  1,
		FilePath:   "D:/Movies/Movie.Name.2024.mkv",
		FileName:   "Movie.Name.2024.mkv",
		AddedAt:    time.Now(),
		ModifiedAt: time.Now(),
	}
	if err := db.Create(&mf).Error; err != nil {
		t.Fatalf("预置媒体失败: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/library/inference/backfill", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("backfill 状态码 got=%d body=%s", resp.Code, resp.Body.String())
	}
	var backfill struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &backfill); err != nil {
		t.Fatalf("解析 backfill 响应失败: %v", err)
	}
	waitInferenceTaskTerminal(t, r, backfill.TaskID, models.TaskStatusSucceeded)
	if _, err := libSvc.GetMediaInferenceInSpace(models.DefaultSpaceID, mf.ID); err == nil {
		t.Fatal("全局关闭后 backfill 不应产生推断")
	}
}

func TestInferenceBackfillTaskSpacePayloadAndIdempotency(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	taskSvc := tasksvc.NewService(db)
	handler := NewHandler(library.NewService(db)).WithTasks(taskSvc)

	first, err := handler.enqueueInferenceBackfillTask(context.Background(), "space-a", 9)
	if err != nil {
		t.Fatalf("首次入队失败: %v", err)
	}
	second, err := handler.enqueueInferenceBackfillTask(context.Background(), "space-a", 9)
	if err != nil {
		t.Fatalf("重复入队失败: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("未完成的相同回填应复用任务: first=%d second=%d", first.ID, second.ID)
	}
	if first.SpaceID == nil || *first.SpaceID != "space-a" {
		t.Fatalf("任务 Space 不正确: %+v", first)
	}
	var payload inferenceBackfillPayload
	if err := json.Unmarshal([]byte(first.PayloadJSON), &payload); err != nil {
		t.Fatalf("解析任务参数失败: %v", err)
	}
	if payload.SpaceID != "space-a" || payload.LibraryID != 9 {
		t.Fatalf("任务参数不正确: %+v", payload)
	}
}

func waitInferenceTaskTerminal(t *testing.T, r http.Handler, taskID int64, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+strconvID(taskID), nil)
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("查询任务失败: status=%d body=%s", resp.Code, resp.Body.String())
		}
		var task taskResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &task); err != nil {
			t.Fatalf("解析任务详情失败: %v", err)
		}
		if task.Status == want {
			return
		}
		if task.Status == models.TaskStatusFailed || task.Status == models.TaskStatusCanceled {
			t.Fatalf("任务提前进入异常终态: %+v", task)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待任务 %d 进入终态 %s 超时", taskID, want)
}

func strconvID(id int64) string {
	return strconv.FormatInt(id, 10)
}
