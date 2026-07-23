package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/auth"
	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	spacepkg "github.com/wcpe/JianVideo/internal/space"
)

func setupParentalRouter(t *testing.T) (*gin.Engine, *gorm.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	secret := "fr2-051-secret"
	// 每测独立 DSN，避免并行污染
	dsn := fmt.Sprintf("file:fr2051_%d?mode=memory&cache=shared", time.Now().UnixNano())
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
	// G / R / UNRATED 三件媒体
	for i, row := range []struct {
		id     int
		name   string
		rating string
	}{
		{1, "g.mp4", "G"},
		{2, "r.mp4", "R"},
		{3, "u.mp4", "UNRATED"},
	} {
		_ = i
		if err := gdb.Exec(`
			INSERT INTO media_files(id, space_id, library_id, file_path, file_name, file_size, format, added_at, modified_at, file_state, content_rating)
			VALUES (?, ?, 1, ?, ?, 100, 'mp4', ?, ?, 'available', ?)`,
			row.id, models.DefaultSpaceID, "/tmp/"+row.name, row.name, now, now, row.rating,
		).Error; err != nil {
			t.Fatalf("插入媒体 %s: %v", row.name, err)
		}
	}

	// viewer 用户
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

	authSvc := auth.NewService(sqlDB, secret)
	spaceSvc := spacepkg.NewService(gdb)
	h := NewHandler(library.NewService(gdb)).WithAuth(authSvc).WithSpace(spaceSvc)

	r := gin.New()
	r.Use(auth.APIGuard(secret, authSvc), auth.SpaceOwnerGuard(authSvc))
	RegisterRoutes(r, h)
	return r, gdb, secret
}

func TestFR2051_ListAndGetFilterByMaxRating(t *testing.T) {
	r, _, secret := setupParentalRouter(t)

	// kid max=PG：列表不应含 R
	req := httptest.NewRequest(http.MethodGet, "/api/library/media?page_size=50", nil)
	req.AddCookie(tokenCookie(t, secret, "kid"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Items []models.MediaFile `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, it := range list.Items {
		if it.ID == 2 || it.ContentRating == "R" {
			t.Fatalf("kid 列表不应含 R: %+v", it)
		}
	}
	// 直链 R → 404
	req = httptest.NewRequest(http.MethodGet, "/api/library/media/2", nil)
	req.AddCookie(tokenCookie(t, secret, "kid"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("直链 R 期望 404, 得到 %d", w.Code)
	}
	// UNRATED 可读
	req = httptest.NewRequest(http.MethodGet, "/api/library/media/3", nil)
	req.AddCookie(tokenCookie(t, secret, "kid"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UNRATED 期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// owner 无上限可读 R
	req = httptest.NewRequest(http.MethodGet, "/api/library/media/2", nil)
	req.AddCookie(tokenCookie(t, secret, "owner"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner 读 R 期望 200, 得到 %d", w.Code)
	}
}

func TestFR2051_ParentalPolicyRequiresPassword(t *testing.T) {
	r, gdb, secret := setupParentalRouter(t)

	// 无密码 / 错密码 → 401
	body, _ := json.Marshal(map[string]string{"default_max_rating": "PG", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPut, "/api/spaces/"+models.DefaultSpaceID+"/parental", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("错密码期望 401, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// 正确密码 → 204
	body, _ = json.Marshal(map[string]string{"default_max_rating": "PG", "password": "pass"})
	req = httptest.NewRequest(http.MethodPut, "/api/spaces/"+models.DefaultSpaceID+"/parental", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("正确密码期望 204, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var sp models.Space
	if err := gdb.Where("id = ?", models.DefaultSpaceID).First(&sp).Error; err != nil {
		t.Fatal(err)
	}
	if sp.DefaultMaxRating != "PG" {
		t.Fatalf("default_max_rating 未更新: %q", sp.DefaultMaxRating)
	}
}

func TestFR2051_UpdateMediaContentRating(t *testing.T) {
	r, gdb, secret := setupParentalRouter(t)
	body, _ := json.Marshal(map[string]string{"content_rating": "PG-13"})
	req := httptest.NewRequest(http.MethodPut, "/api/library/media/1/content-rating", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("设分级期望 204, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var mf models.MediaFile
	if err := gdb.Where("id = 1").First(&mf).Error; err != nil {
		t.Fatal(err)
	}
	if mf.ContentRating != "PG-13" {
		t.Fatalf("content_rating=%q", mf.ContentRating)
	}
}
