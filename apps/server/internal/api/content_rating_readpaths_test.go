package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/playback"
	"github.com/wcpe/JianVideo/internal/player"
	"github.com/wcpe/JianVideo/internal/share"
	spacepkg "github.com/wcpe/JianVideo/internal/space"
)

// setupReadPathsParentalRouter 读路径统一 max 测试脚手架（含 share / stream / HLS）。
func setupReadPathsParentalRouter(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	secret := "fr2-051-readpaths-secret"
	dsn := fmt.Sprintf("file:fr2051_rp_%d?mode=memory&cache=shared", time.Now().UnixNano())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })

	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active', created_at DATETIME NOT NULL)`,
		`CREATE TABLE spaces (id TEXT PRIMARY KEY, name TEXT NOT NULL, owner_user_id INTEGER NOT NULL, default_max_rating TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE space_members (space_id TEXT NOT NULL, user_id INTEGER NOT NULL, role TEXT NOT NULL, max_rating TEXT, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL, PRIMARY KEY (space_id, user_id))`,
		`CREATE TABLE library_paths (id INTEGER PRIMARY KEY, space_id TEXT NOT NULL DEFAULT 'space-default', path TEXT, type TEXT, library_kind TEXT DEFAULT 'mixed', library_profile_json TEXT DEFAULT '{}', label TEXT, enabled INTEGER DEFAULT 1, created_at DATETIME)`,
		`CREATE TABLE media_files (
			id INTEGER PRIMARY KEY, space_id TEXT NOT NULL DEFAULT 'space-default', library_id INTEGER, file_path TEXT, file_name TEXT,
			file_size INTEGER DEFAULT 0, format TEXT, video_codec TEXT, audio_codec TEXT, duration REAL DEFAULT 0,
			width INTEGER DEFAULT 0, height INTEGER DEFAULT 0, bitrate INTEGER DEFAULT 0, subtitle_tracks TEXT,
			added_at DATETIME, modified_at DATETIME, display_name TEXT, deleted_at DATETIME, file_state TEXT DEFAULT 'available',
			media_time DATETIME, media_time_source TEXT, camera TEXT, lens TEXT, aperture TEXT, shutter TEXT,
			iso INTEGER DEFAULT 0, gps_lat REAL DEFAULT 0, gps_lon REAL DEFAULT 0, location TEXT, favorite INTEGER DEFAULT 0,
			notes TEXT, content_rating TEXT NOT NULL DEFAULT '', dhash INTEGER DEFAULT 0,
			content_hash TEXT DEFAULT '', content_hash_algo TEXT DEFAULT '', content_hash_computed_at DATETIME,
			content_hash_stale INTEGER DEFAULT 1, last_position REAL DEFAULT 0, watched INTEGER DEFAULT 0,
			last_watched_at DATETIME, last_viewed_at DATETIME
		)`,
		`CREATE TABLE shares (
			token TEXT PRIMARY KEY, space_id TEXT NOT NULL DEFAULT 'space-default',
			resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL, created_at DATETIME NOT NULL,
			expires_at DATETIME, password_hash TEXT, max_uses INTEGER DEFAULT 0, used_count INTEGER DEFAULT 0,
			allow_download INTEGER NOT NULL DEFAULT 1
		)`,
	} {
		if err := gdb.Exec(stmt).Error; err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	now := time.Now()
	if err := gdb.Exec(
		`INSERT INTO users(id, username, password_hash, status, created_at) VALUES (1, 'owner', ?, 'active', ?)`,
		string(hash), now,
	).Error; err != nil {
		t.Fatalf("插入 owner: %v", err)
	}
	if err := gdb.Exec(
		`INSERT INTO spaces(id, name, owner_user_id, default_max_rating, created_at, updated_at) VALUES (?, '默认', 1, '', ?, ?)`,
		models.DefaultSpaceID, now, now,
	).Error; err != nil {
		t.Fatalf("插入 space: %v", err)
	}
	if err := gdb.Exec(
		`INSERT INTO space_members(space_id, user_id, role, created_at, updated_at) VALUES (?, 1, 'owner', ?, ?)`,
		models.DefaultSpaceID, now, now,
	).Error; err != nil {
		t.Fatalf("插入 member: %v", err)
	}
	if err := gdb.Exec(
		`INSERT INTO library_paths(id, space_id, path, type, created_at) VALUES (1, ?, '/tmp/lib', 'local', ?)`,
		models.DefaultSpaceID, now,
	).Error; err != nil {
		t.Fatalf("插入 library: %v", err)
	}

	dir := t.TempDir()
	for _, row := range []struct {
		id     int
		name   string
		rating string
	}{
		{1, "g.mp4", "G"},
		{2, "r.mp4", "R"},
		{3, "u.mp4", "UNRATED"},
	} {
		fp := filepath.Join(dir, row.name)
		if err := os.WriteFile(fp, []byte(row.name+"-bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := gdb.Exec(`
			INSERT INTO media_files(id, space_id, library_id, file_path, file_name, file_size, format, added_at, modified_at, file_state, content_rating)
			VALUES (?, ?, 1, ?, ?, 100, 'mp4', ?, ?, 'available', ?)`,
			row.id, models.DefaultSpaceID, fp, row.name, now, now, row.rating,
		).Error; err != nil {
			t.Fatalf("插入媒体 %s: %v", row.name, err)
		}
	}

	if err := gdb.Exec(
		`INSERT INTO users(id, username, password_hash, status, created_at) VALUES (2, 'kid', ?, 'active', ?)`,
		string(hash), now,
	).Error; err != nil {
		t.Fatalf("插入 kid: %v", err)
	}
	if err := gdb.Exec(
		`INSERT INTO space_members(space_id, user_id, role, max_rating, created_at, updated_at) VALUES (?, 2, 'viewer', 'PG', ?, ?)`,
		models.DefaultSpaceID, now, now,
	).Error; err != nil {
		t.Fatalf("插入 kid member: %v", err)
	}
	// editor + max=PG：可写但受分级限制（CreateShare / negotiate 等写路径）
	if err := gdb.Exec(
		`INSERT INTO users(id, username, password_hash, status, created_at) VALUES (3, 'editor_pg', ?, 'active', ?)`,
		string(hash), now,
	).Error; err != nil {
		t.Fatalf("插入 editor_pg: %v", err)
	}
	if err := gdb.Exec(
		`INSERT INTO space_members(space_id, user_id, role, max_rating, created_at, updated_at) VALUES (?, 3, 'editor', 'PG', ?, ?)`,
		models.DefaultSpaceID, now, now,
	).Error; err != nil {
		t.Fatalf("插入 editor_pg member: %v", err)
	}

	authSvc := auth.NewService(sqlDB, secret)
	spaceSvc := spacepkg.NewService(gdb)
	libSvc := library.NewService(gdb)
	shareSvc := share.NewService(gdb)
	pbSvc := playback.NewService()
	t.Cleanup(pbSvc.Stop)
	h := NewHandler(libSvc).WithAuth(authSvc).WithSpace(spaceSvc).WithShareService(shareSvc)

	r := gin.New()
	r.Use(auth.APIGuard(secret, authSvc), auth.SpaceOwnerGuard(authSvc))
	RegisterRoutes(r, h, pbSvc)
	hlsMgr := player.NewHLSManager(t.TempDir())
	_ = hlsMgr.SaveMasterM3U8(2, "#EXTM3U\n")
	RegisterHLSRoutesWithMaxRating(r, hlsMgr, t.TempDir(), libSvc, h.ViewerMaxContentRating)
	return r, gdb, secret
}

// TestFR2051_ReadPaths_MaxPG_HidesR max=PG 时 stream/hls/thumbnail/raw/download/v2/negotiate 对 R → 404。
func TestFR2051_ReadPaths_MaxPG_HidesR(t *testing.T) {
	r, _, secret := setupReadPathsParentalRouter(t)

	type step struct {
		method string
		path   string
		body   string
	}
	// kid 不可见 R
	for _, s := range []step{
		{http.MethodGet, "/api/library/media/2", ""},
		{http.MethodGet, "/api/library/thumbnail/2", ""},
		{http.MethodGet, "/api/library/media/2/raw", ""},
		{http.MethodGet, "/api/library/media/2/download", ""},
		{http.MethodGet, "/api/v2/media/2", ""},
		{http.MethodGet, "/api/play/2/stream", ""},
		{http.MethodGet, "/api/play/hls/2/master.m3u8", ""},
	} {
		req := httptest.NewRequest(s.method, s.path, nil)
		req.AddCookie(tokenCookie(t, secret, "kid"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("kid %s %s 期望 404, 得到 %d body=%s", s.method, s.path, w.Code, w.Body.String())
		}
	}
	// negotiate 为 POST（写权限）；用 editor+max=PG 验证分级 404
	reqN := httptest.NewRequest(http.MethodPost, "/api/play/2/negotiate", bytes.NewReader([]byte(`{"client_caps":{}}`)))
	reqN.Header.Set("Content-Type", "application/json")
	reqN.AddCookie(tokenCookie(t, secret, "editor_pg"))
	wN := httptest.NewRecorder()
	r.ServeHTTP(wN, reqN)
	if wN.Code != http.StatusNotFound {
		t.Fatalf("editor_pg negotiate R 期望 404, 得到 %d body=%s", wN.Code, wN.Body.String())
	}

	// owner 可读 R 详情与 stream
	for _, path := range []string{"/api/library/media/2", "/api/play/2/stream"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(tokenCookie(t, secret, "owner"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("owner %s 期望 200, 得到 %d body=%s", path, w.Code, w.Body.String())
		}
	}

	// kid 可读 UNRATED / G
	for _, id := range []string{"1", "3"} {
		req := httptest.NewRequest(http.MethodGet, "/api/library/media/"+id, nil)
		req.AddCookie(tokenCookie(t, secret, "kid"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("kid 读 id=%s 期望 200, 得到 %d body=%s", id, w.Code, w.Body.String())
		}
	}
}

// TestFR2051_CreateShare_InvisibleMedia404 创建分享时创建者不可见媒体 → 404。
func TestFR2051_CreateShare_InvisibleMedia404(t *testing.T) {
	r, _, secret := setupReadPathsParentalRouter(t)
	body, _ := json.Marshal(map[string]any{
		"resource_type": models.ShareResourceMedia,
		"resource_id":   2,
	})

	// viewer 写路径会被角色守卫 403；用 editor+max=PG 验证 ForViewer 404
	req := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "editor_pg"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("editor_pg 分享 R 期望 404, 得到 %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner 分享 R 期望 201, 得到 %d body=%s", w.Code, w.Body.String())
	}
}

// TestFR2051_ListMediaV2_FiltersR ListMediaV2 注入 MaxContentRating。
func TestFR2051_ListMediaV2_FiltersR(t *testing.T) {
	r, _, secret := setupReadPathsParentalRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v2/media?page_size=50", nil)
	req.AddCookie(tokenCookie(t, secret, "kid"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListMediaV2 期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it.ID == "2" {
			t.Fatal("kid ListMediaV2 不应含 R(id=2)")
		}
	}
}
