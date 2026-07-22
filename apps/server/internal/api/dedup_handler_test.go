package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// setupDedupRouter 创建带媒体表迁移的测试路由、服务与底层 DB（DB 用于直接写入 dhash 列）。
func setupDedupRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}, &models.MediaHashGroup{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := library.NewService(gdb)
	h := NewHandler(svc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc, gdb
}

// seedMediaWithDHash 入库一条媒体并直接写入指定 dhash（绕过缩略图，专注端点契约）。
func seedMediaWithDHash(t *testing.T, svc *library.Service, gdb *gorm.DB, name string, dhash int64) int64 {
	t.Helper()
	id := seedMedia(t, svc, 1, name)
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", id).Update("dhash", dhash).Error; err != nil {
		t.Fatalf("写入 dhash 失败: %v", err)
	}
	return id
}

// TestDuplicatesScan_API scan 端点返回本次计算条数（无缩略图时为 0、不报错）。
func TestDuplicatesScan_API(t *testing.T) {
	router, svc, _ := setupDedupRouter(t)
	seedMedia(t, svc, 1, "a.jpg")

	w := doJSON(t, router, "POST", "/api/library/duplicates/scan", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Computed int `json:"computed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	// 无缩略图、生成失败 → 跳过，computed=0（端点不因单条失败而 500）
	if resp.Computed != 0 {
		t.Fatalf("无缩略图时 computed 应为 0, 实际 %d", resp.Computed)
	}
}

// TestDuplicatesList_API groups 端点返回按汉明距离聚类的重复组。
func TestDuplicatesList_API(t *testing.T) {
	router, svc, gdb := setupDedupRouter(t)
	// 两条相同 dhash → 同组；一条迥异 → 不入组
	id1 := seedMediaWithDHash(t, svc, gdb, "a.jpg", 0x0F0F0F0F0F0F0F0F)
	id2 := seedMediaWithDHash(t, svc, gdb, "b.jpg", 0x0F0F0F0F0F0F0F0F)
	_ = seedMediaWithDHash(t, svc, gdb, "c.jpg", 0x7FFFFFFFFFFFFFFF) // 远离前两者

	w := doJSON(t, router, "GET", "/api/library/duplicates", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Groups [][]models.MediaFile `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("应返回 1 个重复组, 实际 %d", len(resp.Groups))
	}
	if len(resp.Groups[0]) != 2 {
		t.Fatalf("组内应为 2 项, 实际 %d", len(resp.Groups[0]))
	}
	if resp.Groups[0][0].ID != id1 || resp.Groups[0][1].ID != id2 {
		t.Fatalf("重复组成员应为 [%d %d], 实际 [%d %d]", id1, id2, resp.Groups[0][0].ID, resp.Groups[0][1].ID)
	}
}

// TestExactDuplicatesAPI 以 content_hash 精确分组，排除同 size 不同 hash。
func TestExactDuplicatesAPI(t *testing.T) {
	router, svc, gdb := setupDedupRouter(t)
	now := time.Now().UTC()
	id1 := seedMedia(t, svc, 1, "exact-a.mp4")
	id2 := seedMedia(t, svc, 1, "exact-b.mp4")
	id3 := seedMedia(t, svc, 1, "same-size-other-hash.mp4")
	for _, row := range []struct {
		id   int64
		hash string
	}{
		{id1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{id2, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{id3, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	} {
		if err := gdb.Model(&models.MediaFile{}).Where("id = ?", row.id).Updates(map[string]any{
			"file_size":                100,
			"content_hash":             row.hash,
			"content_hash_algo":        "sha256",
			"content_hash_computed_at": now,
			"content_hash_stale":       false,
		}).Error; err != nil {
			t.Fatalf("写入内容 hash 失败: %v", err)
		}
	}
	if err := svc.RefreshContentHashGroups(t.Context(), models.DefaultSpaceID); err != nil {
		t.Fatalf("刷新内容 hash 分组失败: %v", err)
	}

	w := doJSON(t, router, "GET", "/api/library/duplicates/exact", "")
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Groups []struct {
			ContentHash string             `json:"content_hash"`
			FileSize    int64              `json:"file_size"`
			Items       []models.MediaFile `json:"items"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Groups) != 1 || len(resp.Groups[0].Items) != 2 {
		t.Fatalf("应返回一个 2 项精确重复组, 实际 %+v", resp.Groups)
	}
	if resp.Groups[0].Items[0].ID != id1 || resp.Groups[0].Items[1].ID != id2 {
		t.Fatalf("精确重复组成员不正确: %+v", resp.Groups[0].Items)
	}
}

// TestBackfillFileHashesAPI 入队 FR2-037 通用任务而不是在请求线程内计算。
func TestBackfillFileHashesAPI(t *testing.T) {
	_, _, gdb := setupDedupRouter(t)
	if err := gdb.AutoMigrate(&models.Task{}); err != nil {
		t.Fatalf("迁移任务表失败: %v", err)
	}
	h := NewHandler(library.NewService(gdb)).WithTasks(tasksvc.NewService(gdb))
	router := gin.New()
	RegisterRoutes(router, h)

	w := doJSON(t, router, "POST", "/api/library/file-hashes/backfill", "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("期望 202, 实际 %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Status string `json:"status"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Status != "queued" || resp.TaskID == "" {
		t.Fatalf("回填响应不正确: %+v", resp)
	}
	var task models.Task
	if err := gdb.Where("type = ?", "library.file_hash_backfill").First(&task).Error; err != nil {
		t.Fatalf("应创建通用任务: %v", err)
	}
}
