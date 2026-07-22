package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
)

func setupNextEpisodeRouter(t *testing.T) (*gin.Engine, *library.Service, *gorm.DB) {
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
	if err := gdb.AutoMigrate(
		&models.LibraryPath{},
		&models.MediaFile{},
		&models.MediaExtension{},
		&models.MediaInference{},
		&models.Album{},
		&models.AlbumItem{},
	); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	svc := library.NewService(gdb)
	h := NewHandler(svc)
	r := gin.New()
	RegisterRoutes(r, h)
	return r, svc, gdb
}

func TestGetNextEpisode_API(t *testing.T) {
	router, svc, _ := setupNextEpisodeRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "剧集库")
	if err != nil {
		t.Fatal(err)
	}
	e01, err := svc.CreateMediaFile(lp.ID, filepath.ToSlash(filepath.Join(dir, "S01E01.mp4")), 100)
	if err != nil {
		t.Fatal(err)
	}
	e02, err := svc.CreateMediaFile(lp.ID, filepath.ToSlash(filepath.Join(dir, "S01E02.mp4")), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		id int64
		ep int
	}{
		{e01.ID, 1},
		{e02.ID, 2},
	} {
		if _, err := svc.UpsertManualInferenceInSpace(models.DefaultSpaceID, pair.id, library.InferenceManualInput{
			Kind:    models.LibraryKindSeries,
			Title:   "测试剧",
			Season:  1,
			Episode: pair.ep,
		}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/library/media/%d/next-episode", e01.ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Media *models.MediaFile `json:"media"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Media == nil || resp.Media.ID != e02.ID {
		t.Fatalf("下一集应为 E02, got %+v", resp.Media)
	}

	// 末集
	req = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/library/media/%d/next-episode", e02.ID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("末集期望 200, 实际 %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Media != nil {
		t.Fatalf("末集 media 应为 null")
	}

	// 不存在
	req = httptest.NewRequest(http.MethodGet, "/api/library/media/99999/next-episode", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d", w.Code)
	}
}

func TestGetAlbumNeighbor_API(t *testing.T) {
	router, svc, _ := setupNextEpisodeRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "库")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := svc.CreateMediaFile(lp.ID, filepath.ToSlash(filepath.Join(dir, "a.mp4")), 100)
	b, _ := svc.CreateMediaFile(lp.ID, filepath.ToSlash(filepath.Join(dir, "b.mp4")), 100)
	album, err := svc.CreateAlbum("播放列表", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.AddAlbumItem(album.ID, a.ID)
	_ = svc.AddAlbumItem(album.ID, b.ID)

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/albums/%d/neighbor?media_id=%d&dir=next", album.ID, a.ID),
		nil,
	)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Media *models.MediaFile `json:"media"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Media == nil || resp.Media.ID != b.ID {
		t.Fatalf("下一首应为 b, got %+v", resp.Media)
	}

	// 末项
	req = httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/albums/%d/neighbor?media_id=%d&dir=next", album.ID, b.ID),
		nil,
	)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Media != nil {
		t.Fatalf("末项 media 应为 null")
	}
}
