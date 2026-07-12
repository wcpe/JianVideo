package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/settings"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// setupSettingsRouter 构建带 settings 服务的测试路由。
func setupSettingsRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Space{}, &models.LibraryPath{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := gdb.Create(&models.Space{ID: models.DefaultSpaceID, Name: "默认 Space", OwnerUserID: 1}).Error; err != nil {
		t.Fatalf("创建默认 Space 失败: %v", err)
	}

	h := NewHandler(library.NewService(gdb)).WithSettings(settings.NewService(gdb)).WithDBPath("D:/jianvideo-data/jianvideo.db")
	r := gin.New()
	RegisterRoutes(r, h)
	return r
}

func TestSettings_StorageInfoShowsSpaceAndSeparatedPaths(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/storage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("存储信息期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Space        models.Space `json:"space"`
		DataDir      string       `json:"data_dir"`
		DatabasePath string       `json:"database_path"`
		LibraryCount int          `json:"library_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析存储信息失败: %v", err)
	}
	if resp.Space.ID != models.DefaultSpaceID || resp.Space.Name != "默认 Space" {
		t.Fatalf("当前 Space 不正确: %+v", resp.Space)
	}
	if resp.DataDir != filepath.Dir("D:/jianvideo-data/jianvideo.db") || resp.DatabasePath != "D:/jianvideo-data/jianvideo.db" {
		t.Fatalf("目录与索引库路径不正确: data=%q db=%q", resp.DataDir, resp.DatabasePath)
	}
	if resp.LibraryCount != 0 {
		t.Fatalf("空库目录计数应为 0, 实际 %d", resp.LibraryCount)
	}
}

// TestSettings_PutThenGet PUT 写入后 GET 能读回（持久化往返）。
func TestSettings_PutThenGet(t *testing.T) {
	router := setupSettingsRouter(t)

	body := `{"settings":{"scan_interval":"3600","recycle_bin_paths":"{\"D\":\"D:/.recycle\"}"}}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET 期望 200, 实际 %d", w.Code)
	}

	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v, body: %s", err, w.Body.String())
	}
	if resp.Settings["scan_interval"] != "3600" {
		t.Fatalf("期望 scan_interval=3600, 实际 %q", resp.Settings["scan_interval"])
	}
	if resp.Settings["recycle_bin_paths"] != `{"D":"D:/.recycle"}` {
		t.Fatalf("回收站路径不匹配, 实际 %q", resp.Settings["recycle_bin_paths"])
	}
}

func TestSettings_Definitions(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("GET", "/api/settings/definitions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("definitions 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Definitions []settings.Definition `json:"definitions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	seen := map[string]settings.Definition{}
	for _, def := range resp.Definitions {
		seen[def.Key] = def
	}
	if seen[settings.KeyNetworkProxy].Sensitive != true {
		t.Fatalf("network_proxy definitions 应标记敏感: %+v", seen[settings.KeyNetworkProxy])
	}
	if seen[settings.KeyScanInterval].ValueType != settings.ValueInt {
		t.Fatalf("scan_interval 应标记 int 类型: %+v", seen[settings.KeyScanInterval])
	}
}

func TestSettings_PutUnknownKeyRejected(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{"typo_key":"x","scan_interval":"60"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知 key 期望 400, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if resp.Settings[settings.KeyScanInterval] == "60" {
		t.Fatalf("未知 key 导致批量失败时不应写入合法 key")
	}
}

func TestSettings_GetRedactsSensitiveValues(t *testing.T) {
	router := setupSettingsRouter(t)

	body := `{"settings":{"` + settings.KeyNetworkProxy + `":"http://user:secret@example.com:8080"}}`
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) || bytes.Contains(w.Body.Bytes(), []byte("user:")) {
		t.Fatalf("PUT 响应不应包含代理凭据: %s", w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if bytes.Contains(w.Body.Bytes(), []byte("secret")) || bytes.Contains(w.Body.Bytes(), []byte("user:")) {
		t.Fatalf("GET 响应不应包含代理凭据: %s", w.Body.String())
	}
}

// TestSettings_PutUpsert 同一 key 重复 PUT 覆盖旧值。
func TestSettings_PutUpsert(t *testing.T) {
	router := setupSettingsRouter(t)

	put := func(v string) {
		req := httptest.NewRequest("PUT", "/api/settings",
			bytes.NewBufferString(`{"settings":{"scan_interval":"`+v+`"}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT %s 期望 200, 实际 %d", v, w.Code)
		}
	}
	put("60")
	put("120")

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var resp struct {
		Settings map[string]string `json:"settings"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Settings["scan_interval"] != "120" {
		t.Fatalf("期望覆盖为 120, 实际 %q", resp.Settings["scan_interval"])
	}
}

// TestSettings_PutEmpty 空 settings 返回 400。
func TestSettings_PutEmpty(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空设置期望 400, 实际 %d", w.Code)
	}
}

// TestSettings_PutInvalidJSON 非法 JSON 返回 400。
func TestSettings_PutInvalidJSON(t *testing.T) {
	router := setupSettingsRouter(t)

	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 期望 400, 实际 %d", w.Code)
	}
}

// TestSettings_PutTriggersReload 保存成功后调用设置变更回调（定时扫描周期热生效，FR-28）。
func TestSettings_PutTriggersReload(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	reloaded := 0
	h := NewHandler(library.NewService(gdb)).
		WithSettings(settings.NewService(gdb)).
		WithSettingsReload(func() { reloaded++ })
	r := gin.New()
	RegisterRoutes(r, h)

	// 成功 PUT 应触发一次回调
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{"scan_interval":"600"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT 期望 200, 实际 %d", w.Code)
	}
	if reloaded != 1 {
		t.Fatalf("成功保存应触发 1 次设置变更回调, 实际 %d", reloaded)
	}

	// 失败 PUT（空设置 400）不应触发回调
	req = httptest.NewRequest("PUT", "/api/settings", bytes.NewBufferString(`{"settings":{}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空设置期望 400, 实际 %d", w.Code)
	}
	if reloaded != 1 {
		t.Fatalf("失败保存不应触发回调, 回调次数 %d", reloaded)
	}
}

func TestSettings_InferenceChangeEnqueuesIncrementalRefresh(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-inference.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取测试数据库连接失败: %v", err)
	}
	defer sqlDB.Close()
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "0"); err != nil {
		t.Fatalf("预置关闭开关失败: %v", err)
	}
	libSvc := library.NewService(gdb).WithInferenceConfigProvider(func(string, int64) library.InferenceConfig {
		raw, _ := settingsSvc.Get(settings.KeyMediaInferenceEnabled)
		return library.InferenceConfig{Enabled: settings.ParseBoolSetting(raw, true)}
	})
	dir := t.TempDir()
	lp, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建测试媒体库失败: %v", err)
	}
	missing, err := libSvc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Missing.Movie.2024.mkv"), 10)
	if err != nil {
		t.Fatalf("创建待补齐媒体失败: %v", err)
	}
	manual, err := libSvc.CreateMediaFileInSpace(models.DefaultSpaceID, lp.ID, filepath.Join(dir, "Manual.Movie.2025.mkv"), 10)
	if err != nil {
		t.Fatalf("创建人工媒体失败: %v", err)
	}
	if _, err := libSvc.UpsertManualInferenceInSpace(models.DefaultSpaceID, manual.ID, library.InferenceManualInput{Title: "人工片名"}); err != nil {
		t.Fatalf("保存人工推断失败: %v", err)
	}

	taskSvc := tasksvc.NewService(gdb)
	workers := tasksvc.NewWorkerRegistry(taskSvc)
	h := NewHandler(libSvc).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(workers)
	r := gin.New()
	RegisterRoutes(r, h)

	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"settings":{"media_inference_enabled":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("启用推断期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	var tasks []models.Task
	if err := gdb.Where("type = ?", inferenceBackfillTaskType).Find(&tasks).Error; err != nil {
		t.Fatalf("查询增量刷新任务失败: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("推断设置变化应入队 1 个增量刷新任务，实际 %d", len(tasks))
	}
	var payload inferenceBackfillPayload
	if err := json.Unmarshal([]byte(tasks[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("解析增量刷新任务参数失败: %v", err)
	}
	if payload.Mode != inferenceBackfillModeMissing || payload.LibraryID != 0 {
		t.Fatalf("设置变化应触发全局缺失项增量刷新: %+v", payload)
	}
	if _, err := libSvc.BackfillMissingMediaInferencesWithProgressInSpace(context.Background(), models.DefaultSpaceID, 0, nil); err != nil {
		t.Fatalf("执行缺失项增量推断失败: %v", err)
	}
	missingInference, err := libSvc.GetMediaInferenceInSpace(models.DefaultSpaceID, missing.ID)
	if err != nil || missingInference == nil || missingInference.Source != library.InferenceSourceRule {
		t.Fatalf("待补齐媒体推断 = %#v, %v，期望自动推断", missingInference, err)
	}
	manualInference, err := libSvc.GetMediaInferenceInSpace(models.DefaultSpaceID, manual.ID)
	if err != nil || manualInference == nil || manualInference.Source != library.InferenceSourceManual || manualInference.Title != "人工片名" {
		t.Fatalf("人工推断 = %#v, %v，期望保持不变", manualInference, err)
	}
}

// TestSettings_Unavailable 未注入 settings 服务时返回 503。
func TestSettings_Unavailable(t *testing.T) {
	h := NewHandler(library.NewService(nil))
	r := gin.New()
	RegisterRoutes(r, h)

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("未启用设置服务期望 503, 实际 %d", w.Code)
	}
}
