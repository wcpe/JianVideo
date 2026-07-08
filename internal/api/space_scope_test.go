package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

func setupSpaceTestRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("获取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.Tag{}, &models.TagMapping{}, &models.ScanTask{}, &models.TranscodeTask{}); err != nil {
		t.Fatalf("迁移扩展表失败: %v", err)
	}
	ensureSpaceTestSchema(t, gdb)

	svc := library.NewService(gdb)
	h := NewHandler(svc)
	router := gin.New()
	RegisterRoutes(router, h)
	return router, svc, gdb
}

func ensureSpaceTestSchema(t *testing.T, gdb *gorm.DB) {
	t.Helper()
	if err := gdb.Exec(`
		CREATE TABLE IF NOT EXISTS spaces (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			owner_user_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`).Error; err != nil {
		t.Fatalf("创建测试 spaces 表失败: %v", err)
	}
	now := time.Now()
	for _, spaceID := range []string{"space-default", "space-alt"} {
		if err := gdb.Exec(
			"INSERT INTO spaces(id, name, owner_user_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
			spaceID, spaceID, 1, now, now,
		).Error; err != nil {
			t.Fatalf("插入测试 Space 失败: %v", err)
		}
	}
	addSpaceColumnIfMissing(t, gdb, "library_paths")
	addSpaceColumnIfMissing(t, gdb, "media_files")
}

func addSpaceColumnIfMissing(t *testing.T, gdb *gorm.DB, table string) {
	t.Helper()
	if gdb.Migrator().HasColumn(table, "space_id") {
		return
	}
	if err := gdb.Exec("ALTER TABLE " + table + " ADD COLUMN space_id TEXT NOT NULL DEFAULT 'space-default'").Error; err != nil {
		t.Fatalf("添加 %s.space_id 失败: %v", table, err)
	}
}

func createSpaceMedia(t *testing.T, svc *library.Service, spaceID, libraryPath, filePath string) (models.LibraryPath, models.MediaFile) {
	t.Helper()
	lp, err := svc.CreateLibraryPathInSpace(spaceID, libraryPath, "local", spaceID)
	if err != nil {
		t.Fatalf("创建媒体库失败: %v", err)
	}
	mf, err := svc.CreateMediaFileInSpace(spaceID, lp.ID, filePath, 100)
	if err != nil {
		t.Fatalf("创建媒体失败: %v", err)
	}
	return *lp, *mf
}

func getJSON(t *testing.T, router *gin.Engine, path, spaceID string) *httptest.ResponseRecorder {
	t.Helper()
	return requestWithSpace(t, router, http.MethodGet, path, spaceID, "")
}

func requestWithSpace(t *testing.T, router *gin.Engine, method, path, spaceID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if spaceID != "" {
		req.Header.Set("X-JianVideo-Space-Id", spaceID)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestSpaceHeaderMissingUsesDefaultSpace(t *testing.T) {
	router, svc, _ := setupSpaceTestRouter(t)
	createSpaceMedia(t, svc, "space-default", t.TempDir(), "/default/a.mp4")
	createSpaceMedia(t, svc, "space-alt", t.TempDir(), "/alt/b.mp4")

	w := getJSON(t, router, "/api/library/media", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items []models.MediaFile `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].FilePath != "/default/a.mp4" {
		t.Fatalf("缺省 Space 应只返回默认 Space 媒体, 实际: %+v", resp.Items)
	}
}

func TestSpaceHeaderRejectsInvalidAndMissingSpace(t *testing.T) {
	router, _, _ := setupSpaceTestRouter(t)

	invalid := getJSON(t, router, "/api/library/media", "bad space")
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("非法 Space header 期望 400, 实际 %d", invalid.Code)
	}

	missing := getJSON(t, router, "/api/library/media", "space-missing")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("不存在 Space 期望 404, 实际 %d", missing.Code)
	}
}

func TestSpaceScopesMediaListDetailBrowseStatsAndScan(t *testing.T) {
	router, svc, _ := setupSpaceTestRouter(t)
	defaultLib, defaultMedia := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/shared/default.mp4")
	_, altMedia := createSpaceMedia(t, svc, "space-alt", t.TempDir(), "/shared/alt.mp4")

	list := getJSON(t, router, "/api/library/media", "space-alt")
	if list.Code != http.StatusOK {
		t.Fatalf("列表期望 200, 实际 %d, body: %s", list.Code, list.Body.String())
	}
	var listResp struct {
		Items []models.MediaFile `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("解析列表响应失败: %v", err)
	}
	if len(listResp.Items) != 1 || listResp.Items[0].ID != altMedia.ID {
		t.Fatalf("列表应隔离到 space-alt, 实际: %+v", listResp.Items)
	}

	detail := getJSON(t, router, "/api/library/media/"+strconv.FormatInt(defaultMedia.ID, 10), "space-alt")
	if detail.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 详情期望 404, 实际 %d", detail.Code)
	}

	browse := getJSON(t, router, "/api/library/browse?parent_path=/shared", "space-alt")
	if browse.Code != http.StatusOK {
		t.Fatalf("目录浏览期望 200, 实际 %d, body: %s", browse.Code, browse.Body.String())
	}
	var browseResp models.BrowseResponse
	if err := json.Unmarshal(browse.Body.Bytes(), &browseResp); err != nil {
		t.Fatalf("解析目录响应失败: %v", err)
	}
	if len(browseResp.Files) != 1 || browseResp.Files[0].ID != altMedia.ID {
		t.Fatalf("目录浏览应隔离到 space-alt, 实际: %+v", browseResp.Files)
	}

	stats := getJSON(t, router, "/api/library/stats", "space-alt")
	if stats.Code != http.StatusOK {
		t.Fatalf("统计期望 200, 实际 %d, body: %s", stats.Code, stats.Body.String())
	}
	var statsResp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(stats.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("解析统计响应失败: %v", err)
	}
	if statsResp.Total != 1 {
		t.Fatalf("统计应隔离到 space-alt, total=%d", statsResp.Total)
	}

	scan := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/library/scan/"+strconv.FormatInt(defaultLib.ID, 10), nil)
	req.Header.Set("X-JianVideo-Space-Id", "space-alt")
	router.ServeHTTP(scan, req)
	if scan.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 扫描入口期望 404, 实际 %d", scan.Code)
	}
}

func TestSpaceScopesAuxiliaryMediaReadEndpoints(t *testing.T) {
	router, svc, _ := setupSpaceTestRouter(t)
	_, defaultMedia := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/default/read.mp4")
	createSpaceMedia(t, svc, "space-alt", t.TempDir(), "/alt/read.mp4")
	defaultID := strconv.FormatInt(defaultMedia.ID, 10)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/library/thumbnail/" + defaultID},
		{method: http.MethodGet, path: "/api/library/media/" + defaultID + "/raw"},
		{method: http.MethodGet, path: "/api/library/media/" + defaultID + "/download"},
		{method: http.MethodGet, path: "/api/play/" + defaultID + "/subtitles"},
		{method: http.MethodPost, path: "/api/play/" + defaultID + "/negotiate", body: `{"client_caps":{}}`},
	}
	for _, tc := range cases {
		w := requestWithSpace(t, router, tc.method, tc.path, "space-alt", tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s 跨 Space 期望 404, 实际 %d, body: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSpaceScopesWatchRecentAndFavoriteEndpoints(t *testing.T) {
	router, svc, gdb := setupSpaceTestRouter(t)
	_, defaultMedia := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/default/watch.mp4")
	_, altMedia := createSpaceMedia(t, svc, "space-alt", t.TempDir(), "/alt/watch.mp4")

	if _, err := svc.UpdateWatchPositionInSpace("space-default", defaultMedia.ID, 10); err != nil {
		t.Fatalf("写入默认 Space 续播失败: %v", err)
	}
	if _, err := svc.UpdateWatchPositionInSpace("space-alt", altMedia.ID, 20); err != nil {
		t.Fatalf("写入备选 Space 续播失败: %v", err)
	}
	if err := svc.SetMediaViewedInSpace("space-default", defaultMedia.ID); err != nil {
		t.Fatalf("写入默认 Space 最近查看失败: %v", err)
	}
	if err := svc.SetMediaViewedInSpace("space-alt", altMedia.ID); err != nil {
		t.Fatalf("写入备选 Space 最近查看失败: %v", err)
	}

	memoryDay := time.Now().AddDate(-1, 0, 0)
	if err := gdb.Model(&models.MediaFile{}).Where("id IN ?", []int64{defaultMedia.ID, altMedia.ID}).Update("media_time", memoryDay).Error; err != nil {
		t.Fatalf("写入那年今日媒体时间失败: %v", err)
	}

	for _, path := range []string{"/api/library/continue-watching", "/api/library/recently-viewed", "/api/library/on-this-day"} {
		w := getJSON(t, router, path, "space-alt")
		if w.Code != http.StatusOK {
			t.Fatalf("%s 期望 200, 实际 %d, body: %s", path, w.Code, w.Body.String())
		}
		var resp struct {
			Items []models.MediaFile `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("解析 %s 响应失败: %v", path, err)
		}
		if len(resp.Items) != 1 || resp.Items[0].ID != altMedia.ID {
			t.Fatalf("%s 应只返回 space-alt 媒体, 实际: %+v", path, resp.Items)
		}
	}

	defaultID := strconv.FormatInt(defaultMedia.ID, 10)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPut, path: "/api/play/" + defaultID + "/position", body: `{"position":30}`},
		{method: http.MethodPut, path: "/api/play/" + defaultID + "/watched"},
		{method: http.MethodPut, path: "/api/library/media/" + defaultID + "/favorite", body: `{"favorite":true}`},
		{method: http.MethodPut, path: "/api/library/media/" + defaultID + "/viewed"},
	} {
		w := requestWithSpace(t, router, tc.method, tc.path, "space-alt", tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s %s 跨 Space 期望 404, 实际 %d, body: %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestSpaceScopesTagsRecycleAndScanTasks(t *testing.T) {
	router, svc, gdb := setupSpaceTestRouter(t)
	_, defaultMedia := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/default/deleted.mp4")
	_, altMedia := createSpaceMedia(t, svc, "space-alt", t.TempDir(), "/alt/deleted.mp4")

	defaultTag := requestWithSpace(t, router, http.MethodPost, "/api/library/tags", "space-default", `{"name":"默认标签"}`)
	if defaultTag.Code != http.StatusCreated {
		t.Fatalf("默认 Space 创建标签期望 201, 实际 %d, body: %s", defaultTag.Code, defaultTag.Body.String())
	}
	altTag := requestWithSpace(t, router, http.MethodPost, "/api/library/tags", "space-alt", `{"name":"备选标签"}`)
	if altTag.Code != http.StatusCreated {
		t.Fatalf("备选 Space 创建标签期望 201, 实际 %d, body: %s", altTag.Code, altTag.Body.String())
	}
	tags := getJSON(t, router, "/api/library/tags", "space-alt")
	if tags.Code != http.StatusOK {
		t.Fatalf("标签列表期望 200, 实际 %d, body: %s", tags.Code, tags.Body.String())
	}
	var tagResp struct {
		Items []models.Tag `json:"items"`
	}
	if err := json.Unmarshal(tags.Body.Bytes(), &tagResp); err != nil {
		t.Fatalf("解析标签列表失败: %v", err)
	}
	if len(tagResp.Items) != 1 || tagResp.Items[0].Name != "备选标签" {
		t.Fatalf("标签列表应隔离到 space-alt, 实际: %+v", tagResp.Items)
	}

	if err := svc.DeleteMediaFileInSpace("space-default", defaultMedia.ID); err != nil {
		t.Fatalf("默认 Space 软删失败: %v", err)
	}
	if err := svc.DeleteMediaFileInSpace("space-alt", altMedia.ID); err != nil {
		t.Fatalf("备选 Space 软删失败: %v", err)
	}
	recycle := getJSON(t, router, "/api/library/recycle", "space-alt")
	if recycle.Code != http.StatusOK {
		t.Fatalf("回收站期望 200, 实际 %d, body: %s", recycle.Code, recycle.Body.String())
	}
	var recycleResp struct {
		Items []models.MediaFile `json:"items"`
	}
	if err := json.Unmarshal(recycle.Body.Bytes(), &recycleResp); err != nil {
		t.Fatalf("解析回收站失败: %v", err)
	}
	if len(recycleResp.Items) != 1 || recycleResp.Items[0].ID != altMedia.ID {
		t.Fatalf("回收站应隔离到 space-alt, 实际: %+v", recycleResp.Items)
	}
	restore := requestWithSpace(t, router, http.MethodPost, "/api/library/media/"+strconv.FormatInt(defaultMedia.ID, 10)+"/restore", "space-alt", "")
	if restore.Code != http.StatusNotFound {
		t.Fatalf("跨 Space 恢复期望 404, 实际 %d", restore.Code)
	}

	q := library.NewTaskQueue(gdb, func(_ int64, _, _, _ string) (int, error) { return 0, nil })
	if _, err := q.EnqueueInSpace("space-default", defaultMedia.LibraryID, "/default", "local", library.ScanModeIncremental); err != nil {
		t.Fatalf("默认 Space 扫描任务入队失败: %v", err)
	}
	if _, err := q.EnqueueInSpace("space-alt", altMedia.LibraryID, "/alt", "local", library.ScanModeIncremental); err != nil {
		t.Fatalf("备选 Space 扫描任务入队失败: %v", err)
	}
	scanRouter := gin.New()
	RegisterRoutes(scanRouter, NewHandler(svc).WithScanQueue(q))
	tasks := getJSON(t, scanRouter, "/api/library/scan/tasks", "space-alt")
	if tasks.Code != http.StatusOK {
		t.Fatalf("扫描任务列表期望 200, 实际 %d, body: %s", tasks.Code, tasks.Body.String())
	}
	var tasksResp struct {
		Tasks []models.ScanTask `json:"tasks"`
	}
	if err := json.Unmarshal(tasks.Body.Bytes(), &tasksResp); err != nil {
		t.Fatalf("解析扫描任务列表失败: %v", err)
	}
	if len(tasksResp.Tasks) != 1 || tasksResp.Tasks[0].SpaceID != "space-alt" {
		t.Fatalf("扫描任务列表应隔离到 space-alt, 实际: %+v", tasksResp.Tasks)
	}
}

func TestLegacyPageResponseIncludesCursor(t *testing.T) {
	router, svc, gdb := setupSpaceTestRouter(t)
	_, first := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/cursor/old.mp4")
	_, second := createSpaceMedia(t, svc, "space-default", t.TempDir(), "/cursor/new.mp4")
	oldTime := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 7, 8, 9, 0, 0, 0, time.UTC)
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", first.ID).Update("added_at", oldTime).Error; err != nil {
		t.Fatalf("更新时间失败: %v", err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", second.ID).Update("added_at", newTime).Error; err != nil {
		t.Fatalf("更新时间失败: %v", err)
	}

	w := getJSON(t, router, "/api/library/media?page=1&page_size=1", "space-default")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Items      []models.MediaFile `json:"items"`
		NextCursor string             `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("期望返回 1 条, 实际: %+v", resp.Items)
	}
	if resp.NextCursor == "" {
		t.Fatal("旧 page 响应应携带 next_cursor")
	}
}
