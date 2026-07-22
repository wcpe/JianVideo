package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// setupExportRouter 构造最小可用的路由 + 任务中心。
func setupExportRouter(t *testing.T) (*gin.Engine, *gorm.DB, *tasksvc.Service, *library.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	tasks := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithTasks(tasks)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, gdb, tasks, libSvc
}

func doExportJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestImageExportMediaRejectsBadInput(t *testing.T) {
	r, _, _, _ := setupExportRouter(t)
	// 不存在的媒体。
	w := doExportJSON(t, r, http.MethodPost, "/api/library/media/999/image-export", `{"format":"jpeg"}`)
	if w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound {
		t.Fatalf("期望 400/404，实际 %d, body=%s", w.Code, w.Body.String())
	}
}

func TestClipExportMediaEnqueueUnit(t *testing.T) {
	// 单元层覆盖 EnqueueVideoClip 幂等性即可（无需启动路由）。
	_, _, tasks, _ := setupExportRouter(t)
	p := library.VideoClipParams{StartSec: 1, EndSec: 5, Format: "mp4"}
	a, err := library.EnqueueVideoClip(t.Context(), tasks, models.DefaultSpaceID, 99, p)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	b, err := library.EnqueueVideoClip(t.Context(), tasks, models.DefaultSpaceID, 99, p)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("同参数应复用任务")
	}
}

func TestDownloadExportArtifactNotReady(t *testing.T) {
	r, _, tasks, _ := setupExportRouter(t)
	task, err := tasks.Enqueue(t.Context(), tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: models.DefaultSpaceID,
		Type: library.TaskTypeImageExport, PayloadJSON: `{"space_id":"space-default","media_id":1,"image":{"format":"jpeg"}}`,
		IdempotencyKey: "download-test",
	})
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	w := doExportJSON(t, r, http.MethodGet, "/api/library/exports/"+strconv.FormatInt(task.ID, 10)+"/download", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("期望 409，实际 %d, body=%s", w.Code, w.Body.String())
	}
}
