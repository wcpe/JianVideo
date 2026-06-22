package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
	"jianvideo/internal/share"
)

// setupShareRouter 构造注入分享服务的测试路由（管理端点 + 公开端点，pbSvc 置空）。
func setupShareRouter(t *testing.T) (*gin.Engine, *library.Service, *share.Service) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, _ := gdb.DB()
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{},
		&models.Album{}, &models.AlbumItem{}, &models.Share{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	libSvc := library.NewService(gdb)
	shareSvc := share.NewService(gdb)
	h := NewHandler(libSvc).WithShareService(shareSvc)
	r := gin.New()
	RegisterRoutes(r, h)
	RegisterShareRoutes(r, h, nil)
	return r, libSvc, shareSvc
}

// realMedia 在临时目录建一个真实文件并入库，返回媒体记录。
func realMedia(t *testing.T, svc *library.Service, name string, content string) *models.MediaFile {
	t.Helper()
	fp := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	mf, err := svc.CreateMediaFile(1, fp, int64(len(content)))
	if err != nil {
		t.Fatalf("创建媒体记录失败: %v", err)
	}
	return mf
}

func getStatus(r *gin.Engine, path string) int {
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// TestShare_MediaInfoAndScopeIsolation 媒体分享：元信息正确；范围外媒体一律 404（安全核心）。
func TestShare_MediaInfoAndScopeIsolation(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mfA := realMedia(t, libSvc, "a.mp4", "AAA")
	mfB := realMedia(t, libSvc, "b.mp4", "BBB")

	sh, err := shareSvc.Create(models.ShareResourceMedia, mfA.ID, nil)
	if err != nil {
		t.Fatalf("创建分享失败: %v", err)
	}

	// 元信息
	req := httptest.NewRequest("GET", "/api/share/"+sh.Token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("分享元信息期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var info struct {
		ResourceType string            `json:"resource_type"`
		Media        *models.MediaFile `json:"media"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info.ResourceType != models.ShareResourceMedia || info.Media == nil || info.Media.ID != mfA.ID {
		t.Fatalf("元信息不符: %s", w.Body.String())
	}

	// 范围内媒体可下载
	if code := getStatus(r, "/api/share/"+sh.Token+"/media/"+strconv.FormatInt(mfA.ID, 10)+"/download"); code != http.StatusOK {
		t.Fatalf("范围内媒体下载期望 200, 实际 %d", code)
	}
	// 范围外媒体（B 不在该分享）→ 404，不可越权访问
	if code := getStatus(r, "/api/share/"+sh.Token+"/media/"+strconv.FormatInt(mfB.ID, 10)+"/download"); code != http.StatusNotFound {
		t.Fatalf("范围外媒体应 404, 实际 %d", code)
	}
}

// TestShare_AlbumScope 相册分享：成员可访问、非成员 404。
func TestShare_AlbumScope(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mfIn := realMedia(t, libSvc, "in.mp4", "IN")
	mfOut := realMedia(t, libSvc, "out.mp4", "OUT")

	album, err := libSvc.CreateAlbum("相册", "")
	if err != nil {
		t.Fatalf("建相册失败: %v", err)
	}
	if err := libSvc.AddAlbumItem(album.ID, mfIn.ID); err != nil {
		t.Fatalf("加入相册失败: %v", err)
	}
	sh, _ := shareSvc.Create(models.ShareResourceAlbum, album.ID, nil)

	// 元信息含成员
	req := httptest.NewRequest("GET", "/api/share/"+sh.Token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var info struct {
		ResourceType string             `json:"resource_type"`
		Items        []models.MediaFile `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info.ResourceType != models.ShareResourceAlbum || len(info.Items) != 1 || info.Items[0].ID != mfIn.ID {
		t.Fatalf("相册分享元信息不符: %s", w.Body.String())
	}

	// 成员可下载
	if code := getStatus(r, "/api/share/"+sh.Token+"/media/"+strconv.FormatInt(mfIn.ID, 10)+"/download"); code != http.StatusOK {
		t.Fatalf("相册成员下载期望 200, 实际 %d", code)
	}
	// 非成员 → 404
	if code := getStatus(r, "/api/share/"+sh.Token+"/media/"+strconv.FormatInt(mfOut.ID, 10)+"/download"); code != http.StatusNotFound {
		t.Fatalf("非相册成员应 404, 实际 %d", code)
	}
}

// TestShare_ExpiredRevokedBogus 过期 / 撤销 / 伪造 token 一律 404。
func TestShare_ExpiredRevokedBogus(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "a.mp4", "A")

	past := time.Now().Add(-time.Hour)
	expired, _ := shareSvc.Create(models.ShareResourceMedia, mf.ID, &past)
	if code := getStatus(r, "/api/share/"+expired.Token); code != http.StatusNotFound {
		t.Fatalf("过期 token 应 404, 实际 %d", code)
	}

	revoked, _ := shareSvc.Create(models.ShareResourceMedia, mf.ID, nil)
	_ = shareSvc.Revoke(revoked.Token)
	if code := getStatus(r, "/api/share/"+revoked.Token); code != http.StatusNotFound {
		t.Fatalf("撤销 token 应 404, 实际 %d", code)
	}

	if code := getStatus(r, "/api/share/deadbeefdeadbeef"); code != http.StatusNotFound {
		t.Fatalf("伪造 token 应 404, 实际 %d", code)
	}
}

// TestShare_CreateValidatesResource 创建分享校验资源存在与类型合法。
func TestShare_CreateValidatesResource(t *testing.T) {
	r, _, _ := setupShareRouter(t)

	post := func(body string) int {
		req := httptest.NewRequest("POST", "/api/shares", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := post(`{"resource_type":"media","resource_id":99999}`); code != http.StatusNotFound {
		t.Fatalf("不存在媒体应 404, 实际 %d", code)
	}
	if code := post(`{"resource_type":"bogus","resource_id":1}`); code != http.StatusBadRequest {
		t.Fatalf("非法类型应 400, 实际 %d", code)
	}
}

// TestShare_ManagementListRevoke 管理端点：创建→列出→撤销往返。
func TestShare_ManagementListRevoke(t *testing.T) {
	r, libSvc, _ := setupShareRouter(t)
	mf := realMedia(t, libSvc, "a.mp4", "A")

	// 创建
	req := httptest.NewRequest("POST", "/api/shares",
		bytes.NewBufferString(`{"resource_type":"media","resource_id":`+strconv.FormatInt(mf.ID, 10)+`}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建分享期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var created models.Share
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Token == "" {
		t.Fatal("创建应返回 token")
	}

	// 列出
	var listResp struct {
		Shares []models.Share `json:"shares"`
	}
	req = httptest.NewRequest("GET", "/api/shares", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &listResp)
	if len(listResp.Shares) != 1 {
		t.Fatalf("应有 1 条分享, 实际 %d", len(listResp.Shares))
	}

	// 撤销
	req = httptest.NewRequest("DELETE", "/api/shares/"+created.Token, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("撤销期望 204, 实际 %d", w.Code)
	}
	// 撤销后公开访问 404
	if code := getStatus(r, "/api/share/"+created.Token); code != http.StatusNotFound {
		t.Fatalf("撤销后公开访问应 404, 实际 %d", code)
	}
}
