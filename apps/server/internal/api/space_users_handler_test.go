package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func setupFR2010Router(t *testing.T) (*gin.Engine, *auth.Service, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	secret := "fr2-010-secret"
	gdb, err := gorm.Open(sqlite.Open("file:fr2010?mode=memory&cache=shared"), &gorm.Config{})
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
		`CREATE TABLE library_paths (id INTEGER PRIMARY KEY, space_id TEXT NOT NULL DEFAULT 'space-default', path TEXT, name TEXT)`,
		`CREATE TABLE media_files (id INTEGER PRIMARY KEY, space_id TEXT NOT NULL DEFAULT 'space-default', library_id INTEGER, file_path TEXT, file_name TEXT, deleted_at DATETIME, file_state TEXT)`,
		// FR2-062：会话表，供禁用联动撤会话与管理员全撤测试
		`CREATE TABLE auth_sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			last_seen_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			ip_hash TEXT NOT NULL DEFAULT '',
			revoked_at DATETIME
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
		`INSERT INTO spaces(id, name, owner_user_id, created_at, updated_at) VALUES (?, '默认', 1, ?, ?)`,
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

	authSvc := auth.NewService(sqlDB, secret)
	spaceSvc := spacepkg.NewService(gdb)
	h := NewHandler(library.NewService(gdb)).WithAuth(authSvc).WithSpace(spaceSvc)

	r := gin.New()
	r.Use(auth.APIGuard(secret, authSvc), auth.SpaceOwnerGuard(authSvc))
	RegisterRoutes(r, h)
	return r, authSvc, secret
}

func tokenCookie(t *testing.T, secret, username string) *http.Cookie {
	t.Helper()
	tok, err := auth.GenerateToken(username, secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "auth_token", Value: tok}
}

func TestFR2010_CreateUserAndViewerMember(t *testing.T) {
	r, _, secret := setupFR2010Router(t)

	// owner 创建用户
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "secret12"})
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建用户期望 201, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	userID := int(created["id"].(float64))

	// 加为 viewer
	body, _ = json.Marshal(map[string]any{"user_id": userID, "role": "viewer"})
	req = httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// viewer 可读：列出自己可访问的 Space
	req = httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(tokenCookie(t, secret, "alice"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("viewer 列 Space 期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var spacesResp struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &spacesResp)
	if len(spacesResp.Items) != 1 {
		t.Fatalf("viewer 应看到 1 个 Space, 得到 %d", len(spacesResp.Items))
	}

	// viewer 可读成员列表
	req = httptest.NewRequest(http.MethodGet, "/api/spaces/"+models.DefaultSpaceID+"/members", nil)
	req.AddCookie(tokenCookie(t, secret, "alice"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("viewer 读成员期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// viewer 不可写：建库路径（全局守卫要求 editor+）
	req = httptest.NewRequest(http.MethodPost, "/api/library/paths", bytes.NewReader([]byte(`{"path":"C:/x","name":"x"}`)))
	req.AddCookie(tokenCookie(t, secret, "alice"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JianVideo-Space-Id", models.DefaultSpaceID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer 建库路径期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// viewer 不可建分享
	body, _ = json.Marshal(map[string]any{"resource_type": "media", "resource_id": 1})
	req = httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "alice"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-JianVideo-Space-Id", models.DefaultSpaceID)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer 建分享期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// viewer 不可管理用户
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.AddCookie(tokenCookie(t, secret, "alice"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("viewer 列用户期望 403, 得到 %d", w.Code)
	}
}

func TestFR2010_DisableUserBlocksLogin(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("bob", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	// 加为默认 Space viewer，再持有有效 JWT
	body, _ := json.Marshal(map[string]any{"user_id": u.ID, "role": "viewer"})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d %s", w.Code, w.Body.String())
	}
	bobCookie := tokenCookie(t, secret, "bob")

	// 用 owner API 禁用
	body, _ = json.Marshal(map[string]string{"status": "disabled"})
	req = httptest.NewRequest(http.MethodPut, "/api/users/"+strconv.Itoa(u.ID)+"/status", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("禁用期望 204, 得到 %d %s", w.Code, w.Body.String())
	}
	_, err = authSvc.Login("bob", "secret12")
	if err == nil {
		t.Fatal("禁用后登录应失败")
	}

	// 旧 JWT 下一受保护请求应 401 USER_DISABLED
	req = httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(bobCookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("禁用后旧 JWT 期望 401, 得到 %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "USER_DISABLED") && !strings.Contains(w.Body.String(), "已禁用") {
		t.Fatalf("禁用后旧 JWT 响应应含 USER_DISABLED/已禁用, body=%s", w.Body.String())
	}
}

func TestFR2010_NonMemberCannotAccessSpace(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	if _, err := authSvc.CreateUser("carol", "secret12"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/library/media", nil)
	req.AddCookie(tokenCookie(t, secret, "carol"))
	req.Header.Set("X-JianVideo-Space-Id", models.DefaultSpaceID)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非成员期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}
}

func TestFR2010_CannotDisableSelf(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	owner, err := authSvc.FindUserByUsername("owner")
	if err != nil || owner == nil {
		t.Fatalf("查找 owner 失败: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"status": "disabled"})
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+strconv.Itoa(owner.ID)+"/status", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("禁用自己期望 400, 得到 %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CANNOT_DISABLE_SELF") {
		t.Fatalf("响应应含 CANNOT_DISABLE_SELF, body=%s", w.Body.String())
	}
	// 确认仍可登录（setup 密码哈希对应 "pass"）
	if _, err := authSvc.Login("owner", "pass"); err != nil {
		t.Fatalf("禁用自己被拒后仍应能登录: %v", err)
	}
	// 状态仍为 active
	u, err := authSvc.FindUserByUsername("owner")
	if err != nil || u == nil {
		t.Fatal(err)
	}
	if u.Status != models.UserStatusActive {
		t.Fatalf("禁用自己失败后状态应为 active, 实际 %q", u.Status)
	}
}

// TestFR2062_OwnerRevokeUserSessions owner 可撤销指定用户全部会话。
func TestFR2062_OwnerRevokeUserSessions(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("bob", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	_, sid1, err := authSvc.CreateSessionAndToken(int64(u.ID), "bob", "ua-a", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, sid2, err := authSvc.CreateSessionAndToken(int64(u.ID), "bob", "ua-b", "10.0.0.2", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/users/"+strconv.Itoa(u.ID)+"/sessions", nil)
	req.AddCookie(tokenCookie(t, secret, "owner"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner 撤会话期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Revoked int64 `json:"revoked"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Revoked != 2 {
		t.Fatalf("期望 revoked=2, 得到 %d", resp.Revoked)
	}
	if err := authSvc.ValidateSession(sid1, int64(u.ID)); err == nil {
		t.Fatal("撤销后 sid1 应失效")
	}
	if err := authSvc.ValidateSession(sid2, int64(u.ID)); err == nil {
		t.Fatal("撤销后 sid2 应失效")
	}
}

// TestFR2062_NonOwnerRevokeUserSessionsForbidden 非 owner 撤会话 403。
func TestFR2062_NonOwnerRevokeUserSessionsForbidden(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("alice", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	// 加为 viewer
	body, _ := json.Marshal(map[string]any{"user_id": u.ID, "role": "viewer"})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/users/1/sessions", nil)
	req.AddCookie(tokenCookie(t, secret, "alice"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非 owner 撤会话期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}
}

// TestFR2010_TransferOwnerAPI_Success owner 转让成功 204，双写生效。
func TestFR2010_TransferOwnerAPI_Success(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("alice", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	// 加为 editor
	body, _ := json.Marshal(map[string]any{"user_id": u.ID, "role": "editor"})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d %s", w.Code, w.Body.String())
	}

	body, _ = json.Marshal(map[string]any{"user_id": u.ID})
	req = httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/transfer-owner", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("转让期望 204, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// 原 owner 不再能管理成员
	body, _ = json.Marshal(map[string]any{"user_id": u.ID, "role": "viewer"})
	req = httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("旧 owner 加成员期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}

	// 新 owner 可读成员
	req = httptest.NewRequest(http.MethodGet, "/api/spaces/"+models.DefaultSpaceID+"/members", nil)
	req.AddCookie(tokenCookie(t, secret, "alice"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("新 owner 列成员期望 200, 得到 %d body=%s", w.Code, w.Body.String())
	}
}

// TestFR2010_TransferOwnerAPI_Forbidden 非 owner 转让 403。
func TestFR2010_TransferOwnerAPI_Forbidden(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("alice", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"user_id": u.ID, "role": "editor"})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d %s", w.Code, w.Body.String())
	}

	// alice（editor）尝试转让
	body, _ = json.Marshal(map[string]any{"user_id": 1})
	req = httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/transfer-owner", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "alice"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非 owner 转让期望 403, 得到 %d body=%s", w.Code, w.Body.String())
	}
}

// TestFR2062_DisableUserRevokesSessions 禁用用户时联动撤销全部会话。
func TestFR2062_DisableUserRevokesSessions(t *testing.T) {
	r, authSvc, secret := setupFR2010Router(t)
	u, err := authSvc.CreateUser("bob", "secret12")
	if err != nil {
		t.Fatal(err)
	}
	// 加为 viewer 以便持 JWT 访问受保护 API
	body, _ := json.Marshal(map[string]any{"user_id": u.ID, "role": "viewer"})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+models.DefaultSpaceID+"/members", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("添加成员期望 204, 得到 %d %s", w.Code, w.Body.String())
	}

	tok, sid, err := authSvc.CreateSessionAndToken(int64(u.ID), "bob", "ua", "10.0.0.1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bobCookie := &http.Cookie{Name: "auth_token", Value: tok}

	body, _ = json.Marshal(map[string]string{"status": "disabled"})
	req = httptest.NewRequest(http.MethodPut, "/api/users/"+strconv.Itoa(u.ID)+"/status", bytes.NewReader(body))
	req.AddCookie(tokenCookie(t, secret, "owner"))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("禁用期望 204, 得到 %d %s", w.Code, w.Body.String())
	}
	if err := authSvc.ValidateSession(sid, int64(u.ID)); err == nil {
		t.Fatal("禁用后会话应已撤销")
	}

	// 旧 JWT：USER_DISABLED 优先于 SESSION_REVOKED（中间件先查用户状态）
	req = httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.AddCookie(bobCookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("禁用后旧 JWT 期望 401, 得到 %d body=%s", w.Code, w.Body.String())
	}
}
