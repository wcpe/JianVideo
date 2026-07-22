package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
	"github.com/wcpe/JianVideo/internal/tools"
)

func setupToolsRouter(t *testing.T, sourceURL, sourceSHA string) (*gin.Engine, *tasksvc.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Task{}, &models.Setting{}); err != nil {
		t.Fatalf("迁移测试表失败: %v", err)
	}
	taskSvc := tasksvc.NewService(gdb)
	manager := tools.NewManager(tools.ManagerOptions{
		Installer: tools.NewInstaller(t.TempDir(), nil),
		Settings:  settings.NewService(gdb),
		Tasks:     taskSvc,
		Registry: []tools.Source{{
			ID:        "ffmpeg-local",
			Tool:      tools.ToolFFmpeg,
			Platform:  "windows",
			Arch:      "amd64",
			Version:   "test",
			URL:       sourceURL,
			SHA256:    sourceSHA,
			Label:     "本地测试源",
			AllowHTTP: true,
		}},
	})
	h := NewHandler(library.NewService(gdb)).
		WithSettings(settings.NewService(gdb)).
		WithTasks(taskSvc).
		WithTools(manager)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, taskSvc
}

func TestToolsSourcesAPI(t *testing.T) {
	archive := makeAPITestToolZip(t, tools.ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	r, _ := setupToolsRouter(t, "http://127.0.0.1/source.zip", hex.EncodeToString(sum[:]))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/tools/sources", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("sources 期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Sources []tools.Source `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].ID != "ffmpeg-local" {
		t.Fatalf("sources 响应不正确: %+v", resp.Sources)
	}
}

func TestToolsStatusAPIUsesItemsEnvelope(t *testing.T) {
	archive := makeAPITestToolZip(t, tools.ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	r, _ := setupToolsRouter(t, "http://127.0.0.1/source.zip", hex.EncodeToString(sum[:]))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/system/tools", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("tools 期望 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []tools.Status `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if len(resp.Items) == 0 || resp.Items[0].Tool != tools.ToolFFmpeg {
		t.Fatalf("tools 响应应使用 items 包裹工具状态，实际 %+v", resp.Items)
	}
}

func TestToolsDownloadAPICreatesSystemTask(t *testing.T) {
	archive := makeAPITestToolZip(t, tools.ToolFFmpeg, "ffmpeg version test")
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer server.Close()
	r, taskSvc := setupToolsRouter(t, server.URL, hex.EncodeToString(sum[:]))

	body := `{"tool":"ffmpeg","source_id":"ffmpeg-local"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system/tools/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("下载入队期望 202，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Status != "queued" || resp.TaskID == "" {
		t.Fatalf("入队响应不正确: %+v", resp)
	}
	page, err := taskSvc.List(context.Background(), tasksvc.Query{Scope: models.TaskScopeSystem})
	if err != nil {
		t.Fatalf("查询系统任务失败: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Type != tools.TaskTypeDownload {
		t.Fatalf("应创建 tool.download 系统任务: %+v", page.Items)
	}
}

func makeAPITestToolZip(t *testing.T, tool, versionLine string) []byte {
	t.Helper()
	name := tool
	body := "#!/bin/sh\necho '" + versionLine + "'\n"
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		name += ".cmd"
		body = "@echo " + versionLine + "\r\n"
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	header := &zip.FileHeader{Name: "bin/" + name, Method: zip.Deflate}
	header.SetMode(mode)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatalf("创建 zip 条目失败: %v", err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("写入 zip 条目失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip 失败: %v", err)
	}
	return b.Bytes()
}

func TestToolsDownloadAPIRejectsHTTPCustomURLWithoutExplicitLocalFlag(t *testing.T) {
	r, _ := setupToolsRouter(t, "http://127.0.0.1/source.zip", "0")
	body := `{"tool":"ffmpeg","custom_url":"http://example.com/ffmpeg.zip","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system/tools/download", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非本地 HTTP 自定义 URL 应返回 400，实际 %d，body=%s", w.Code, w.Body.String())
	}
}
