package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

func TestSettings_InferenceRefreshRunsWorkerForEveryMediaSpace(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-multi-space.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	defer sqlDB.Close()
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "false"); err != nil {
		t.Fatalf("预置关闭设置失败: %v", err)
	}
	libSvc := library.NewService(gdb).WithInferenceConfigProvider(func(string, int64) library.InferenceConfig {
		raw, _ := settingsSvc.Get(settings.KeyMediaInferenceEnabled)
		generation, _ := settingsSvc.Get(settings.KeyMediaInferenceGeneration)
		return library.InferenceConfig{Enabled: settings.ParseBoolSetting(raw, true), Generation: settings.ParseInt64Setting(generation)}
	})
	for _, spaceID := range []string{models.DefaultSpaceID, "space-b"} {
		dir := t.TempDir()
		lp, createErr := libSvc.CreateLibraryPathWithKindInSpace(spaceID, dir, "local", spaceID, models.LibraryKindMovie)
		if createErr != nil {
			t.Fatalf("创建 %s 媒体库失败: %v", spaceID, createErr)
		}
		if _, createErr = libSvc.CreateMediaFileInSpace(spaceID, lp.ID, filepath.Join(dir, spaceID+".Movie.2024.mkv"), 10); createErr != nil {
			t.Fatalf("创建 %s 媒体失败: %v", spaceID, createErr)
		}
	}
	taskSvc := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(tasksvc.NewWorkerRegistry(taskSvc))
	r := gin.New()
	RegisterRoutes(r, h)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"settings":{"media_inference_enabled":"TRUE"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("启用推断期望 200，实际 %d: %s", w.Code, w.Body.String())
	}
	executionWorkers := tasksvc.NewWorkerRegistry(taskSvc)
	if err := RegisterInferenceBackfillWorker(executionWorkers, libSvc); err != nil {
		t.Fatalf("注册推断 worker 失败: %v", err)
	}
	if err := executionWorkers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行真实推断 worker 失败: %v", err)
	}
	var tasks []models.Task
	if err := gdb.Where("type = ?", inferenceBackfillTaskType).Order("id").Find(&tasks).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(tasks) != 2 || tasks[0].Status != models.TaskStatusSucceeded || tasks[1].Status != models.TaskStatusSucceeded {
		t.Fatalf("两个存在媒体的 Space 应各完成一个任务: %+v", tasks)
	}
	spaces := map[string]bool{}
	for _, task := range tasks {
		if task.SpaceID != nil {
			spaces[*task.SpaceID] = true
		}
	}
	if !spaces[models.DefaultSpaceID] || !spaces["space-b"] {
		t.Fatalf("任务 Space 不完整: %+v", spaces)
	}
	var count int64
	if err := gdb.Model(&models.MediaInference{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("真实 worker 应处理两个 Space: count=%d err=%v", count, err)
	}
}

func TestSettings_InferenceEffectiveChangeWithoutMissingMediaDoesNotRequireQueue(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "0"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	h := NewHandler(library.NewService(gdb)).WithSettings(settingsSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"settings":{"media_inference_enabled":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("无缺失媒体时不应要求任务队列: %d %s", w.Code, w.Body.String())
	}
}

func TestSettings_InferenceNoEffectiveChangeDoesNotEnqueue(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "true"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	taskSvc := tasksvc.NewService(gdb)
	h := NewHandler(library.NewService(gdb)).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(tasksvc.NewWorkerRegistry(taskSvc))
	r := gin.New()
	RegisterRoutes(r, h)
	for _, body := range []string{
		`{"settings":{"media_inference_enabled":"1"}}`,
		`{"settings":{"scan_interval":"60"}}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("无有效推断变化保存失败: %d %s", w.Code, w.Body.String())
		}
	}
	var count int64
	if err := gdb.Model(&models.Task{}).Where("type = ?", inferenceBackfillTaskType).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("无有效推断变化不得入队: count=%d err=%v", count, err)
	}
}

func TestSettings_InferenceLibraryScopeChangeOnlyFillsNewlyEnabledLibrary(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-library-scope.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	defer sqlDB.Close()
	settingsSvc := settings.NewService(gdb)
	libSvc := library.NewService(gdb).WithInferenceConfigProvider(func(string, int64) library.InferenceConfig {
		enabled, _ := settingsSvc.Get(settings.KeyMediaInferenceEnabled)
		disabled, _ := settingsSvc.Get(settings.KeyMediaInferenceDisabledLibraries)
		generation, _ := settingsSvc.Get(settings.KeyMediaInferenceGeneration)
		return library.InferenceConfig{
			Enabled:           settings.ParseBoolSetting(enabled, true),
			DisabledLibraries: library.ParseDisabledInferenceLibraries(disabled),
			Generation:        settings.ParseInt64Setting(generation),
		}
	})
	dir := t.TempDir()
	first, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, t.TempDir(), "local", "第一库", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建第一库失败: %v", err)
	}
	second, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, t.TempDir(), "local", "第二库", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建第二库失败: %v", err)
	}
	if err := settingsSvc.Set(settings.KeyMediaInferenceDisabledLibraries, fmt.Sprintf("[%d,%d]", first.ID, second.ID)); err != nil {
		t.Fatalf("预置按库关闭设置失败: %v", err)
	}
	for _, item := range []struct {
		libraryID int64
		name      string
	}{
		{first.ID, "First.Movie.2024.mkv"},
		{second.ID, "Second.Movie.2025.mkv"},
	} {
		media := models.MediaFile{
			SpaceID: models.DefaultSpaceID, LibraryID: item.libraryID,
			FilePath: filepath.Join(dir, item.name), FileName: item.name, Format: "mkv",
			AddedAt: time.Now(), ModifiedAt: time.Now(),
		}
		if err := gdb.Create(&media).Error; err != nil {
			t.Fatalf("创建媒体失败: %v", err)
		}
	}
	taskSvc := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(tasksvc.NewWorkerRegistry(taskSvc))
	r := gin.New()
	RegisterRoutes(r, h)
	put := func() {
		body := fmt.Sprintf(`{"settings":{"media_inference_disabled_libraries":"[%d]"}}`, second.ID)
		req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("调整按库范围失败: %d %s", w.Code, w.Body.String())
		}
	}
	put()
	executionWorkers := tasksvc.NewWorkerRegistry(taskSvc)
	if err := RegisterInferenceBackfillWorker(executionWorkers, libSvc); err != nil {
		t.Fatalf("注册 worker 失败: %v", err)
	}
	if err := executionWorkers.RunPending(context.Background()); err != nil {
		t.Fatalf("执行范围补齐 worker 失败: %v", err)
	}
	var inferred []models.MediaInference
	if err := gdb.Find(&inferred).Error; err != nil || len(inferred) != 1 {
		t.Fatalf("仅新启用库应补齐推断: items=%+v err=%v", inferred, err)
	}
	var media models.MediaFile
	if err := gdb.First(&media, inferred[0].MediaID).Error; err != nil || media.LibraryID != first.ID {
		t.Fatalf("推断应属于第一库: media=%+v err=%v", media, err)
	}
	put()
	var taskCount int64
	if err := gdb.Model(&models.Task{}).Where("type = ?", inferenceBackfillTaskType).Count(&taskCount).Error; err != nil || taskCount != 1 {
		t.Fatalf("相同范围重复保存不得再次入队: count=%d err=%v", taskCount, err)
	}
}

func TestSettings_InferenceRapidDisableEnableCreatesNewGenerationTask(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-generation.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	defer sqlDB.Close()
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "0"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	dir := t.TempDir()
	lp, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID: models.DefaultSpaceID, LibraryID: lp.ID,
		FilePath: filepath.Join(dir, "Rapid.Movie.2024.mkv"), FileName: "Rapid.Movie.2024.mkv",
		Format: "mkv", AddedAt: time.Now(), ModifiedAt: time.Now(),
	}
	if err := gdb.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	taskSvc := tasksvc.NewService(gdb)
	workers := tasksvc.NewWorkerRegistry(taskSvc)
	h := NewHandler(libSvc).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(workers)
	r := gin.New()
	RegisterRoutes(r, h)
	put := func(value string) {
		req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"settings":{"media_inference_enabled":"`+value+`"}}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("保存 %s 失败: %d %s", value, w.Code, w.Body.String())
		}
	}
	put("1")
	first, err := taskSvc.ClaimNext(context.Background(), tasksvc.ClaimQuery{Type: inferenceBackfillTaskType})
	if err != nil {
		t.Fatalf("领取首个任务失败: %v", err)
	}
	put("0")
	put("1")
	var tasks []models.Task
	if err := gdb.Where("type = ?", inferenceBackfillTaskType).Order("id").Find(&tasks).Error; err != nil {
		t.Fatalf("查询任务失败: %v", err)
	}
	if len(tasks) != 2 || tasks[0].Status != models.TaskStatusRunning || tasks[1].Status != models.TaskStatusPending {
		t.Fatalf("快速关闭再开启应保留运行中任务并新增补偿任务: %+v", tasks)
	}
	var firstPayload, secondPayload inferenceBackfillPayload
	_ = json.Unmarshal([]byte(first.PayloadJSON), &firstPayload)
	_ = json.Unmarshal([]byte(tasks[1].PayloadJSON), &secondPayload)
	if firstPayload.Generation == 0 || secondPayload.Generation <= firstPayload.Generation {
		t.Fatalf("补偿任务 generation 应递增: first=%+v second=%+v", firstPayload, secondPayload)
	}
}

func TestSettings_InferenceEnqueueFailureRollsBackSettings(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "settings-rollback.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Setting{}, &models.Task{}, &models.LibraryPath{}, &models.MediaFile{}, &models.MediaInference{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	defer sqlDB.Close()
	settingsSvc := settings.NewService(gdb)
	if err := settingsSvc.Set(settings.KeyMediaInferenceEnabled, "0"); err != nil {
		t.Fatalf("预置设置失败: %v", err)
	}
	libSvc := library.NewService(gdb)
	dir := t.TempDir()
	lp, err := libSvc.CreateLibraryPathWithKindInSpace(models.DefaultSpaceID, dir, "local", "电影", models.LibraryKindMovie)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	media := models.MediaFile{
		SpaceID: models.DefaultSpaceID, LibraryID: lp.ID,
		FilePath: filepath.Join(dir, "Rollback.Movie.2024.mkv"), FileName: "Rollback.Movie.2024.mkv",
		Format: "mkv", AddedAt: time.Now(), ModifiedAt: time.Now(),
	}
	if err := gdb.Create(&media).Error; err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	taskSvc := tasksvc.NewService(gdb)
	h := NewHandler(libSvc).WithSettings(settingsSvc).WithTasks(taskSvc).WithTaskWorkers(tasksvc.NewWorkerRegistry(taskSvc))
	if err := gdb.Migrator().DropTable(&models.Task{}); err != nil {
		t.Fatalf("删除任务表失败: %v", err)
	}
	r := gin.New()
	RegisterRoutes(r, h)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", bytes.NewBufferString(`{"settings":{"media_inference_enabled":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("入队失败期望 500，实际 %d: %s", w.Code, w.Body.String())
	}
	got, err := settingsSvc.Get(settings.KeyMediaInferenceEnabled)
	if err != nil || got != "0" {
		t.Fatalf("入队失败时设置必须同事务回滚: got=%q err=%v", got, err)
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
