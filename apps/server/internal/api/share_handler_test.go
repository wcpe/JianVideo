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

	"github.com/wcpe/JianVideo/internal/db/models"
	"github.com/wcpe/JianVideo/internal/library"
	"github.com/wcpe/JianVideo/internal/share"
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
		&models.Space{}, &models.Album{}, &models.AlbumItem{}, &models.Share{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	for _, spaceID := range []string{models.DefaultSpaceID, "space-a", "space-b"} {
		if err := gdb.Create(&models.Space{ID: spaceID, Name: spaceID, OwnerUserID: 1}).Error; err != nil {
			t.Fatalf("创建测试 Space 失败: %v", err)
		}
	}
	libSvc := library.NewService(gdb)
	shareSvc := share.NewService(gdb)
	h := NewHandler(libSvc).WithShareService(shareSvc)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if spaceID := c.GetHeader("X-JianVideo-Space-Id"); spaceID != "" {
			c.Set("space_id", spaceID)
		}
		c.Next()
	})
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

	sh, err := shareSvc.Create(models.ShareResourceMedia, mfA.ID, nil, "", 0)
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
	sh, _ := shareSvc.Create(models.ShareResourceAlbum, album.ID, nil, "", 0)

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
	expired, _ := shareSvc.Create(models.ShareResourceMedia, mf.ID, &past, "", 0)
	if code := getStatus(r, "/api/share/"+expired.Token); code != http.StatusNotFound {
		t.Fatalf("过期 token 应 404, 实际 %d", code)
	}

	revoked, _ := shareSvc.Create(models.ShareResourceMedia, mf.ID, nil, "", 0)
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

// getStatusWithPwd 带 X-Share-Password 头发 GET，返回状态码。
func getStatusWithPwd(r *gin.Engine, path, pwd string) int {
	req := httptest.NewRequest("GET", path, nil)
	if pwd != "" {
		req.Header.Set("X-Share-Password", pwd)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// TestShare_PasswordGate 密码门禁（FR-78）：元信息无密码只回提示不泄露内容、错误密码访问资源 404、正确密码放行。
func TestShare_PasswordGate(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "a.mp4", "AAA")
	sh, err := shareSvc.Create(models.ShareResourceMedia, mf.ID, nil, "p@ss", 0)
	if err != nil {
		t.Fatalf("创建带密码分享失败: %v", err)
	}
	dl := "/api/share/" + sh.Token + "/media/" + strconv.FormatInt(mf.ID, 10) + "/download"

	// 元信息：无密码只回 requires_password=true，不含 media（不泄露）
	req := httptest.NewRequest("GET", "/api/share/"+sh.Token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("需密码分享元信息应 200 提示, 实际 %d", w.Code)
	}
	var info struct {
		RequiresPassword bool              `json:"requires_password"`
		Media            *models.MediaFile `json:"media"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if !info.RequiresPassword || info.Media != nil {
		t.Fatalf("无密码时应仅提示需密码且不含 media, body: %s", w.Body.String())
	}

	// 资源访问：错误密码 / 无密码 → 404；正确密码 → 200
	if code := getStatusWithPwd(r, dl, "wrong"); code != http.StatusNotFound {
		t.Fatalf("错误密码访问资源应 404, 实际 %d", code)
	}
	if code := getStatusWithPwd(r, dl, ""); code != http.StatusNotFound {
		t.Fatalf("无密码访问资源应 404, 实际 %d", code)
	}
	if code := getStatusWithPwd(r, dl, "p@ss"); code != http.StatusOK {
		t.Fatalf("正确密码访问资源应 200, 实际 %d", code)
	}

	// 元信息带正确密码 → 返回完整内容
	req = httptest.NewRequest("GET", "/api/share/"+sh.Token, nil)
	req.Header.Set("X-Share-Password", "p@ss")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &info)
	if info.Media == nil || info.Media.ID != mf.ID {
		t.Fatalf("正确密码元信息应含 media, body: %s", w.Body.String())
	}
}

// TestShare_MaxUsesExhausted 限次（FR-78）：达到上限后资源访问 404；查看元信息不耗次。
func TestShare_MaxUsesExhausted(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "a.mp4", "AAA")
	sh, _ := shareSvc.Create(models.ShareResourceMedia, mf.ID, nil, "", 2)
	dl := "/api/share/" + sh.Token + "/media/" + strconv.FormatInt(mf.ID, 10) + "/download"

	// 查看元信息多次：不应消耗访问额度
	for i := 0; i < 3; i++ {
		if code := getStatus(r, "/api/share/"+sh.Token); code != http.StatusOK {
			t.Fatalf("查看元信息应 200, 实际 %d", code)
		}
	}

	// 前两次资源访问成功、第三次耗尽 404
	if code := getStatus(r, dl); code != http.StatusOK {
		t.Fatalf("第 1 次资源访问应 200, 实际 %d", code)
	}
	if code := getStatus(r, dl); code != http.StatusOK {
		t.Fatalf("第 2 次资源访问应 200, 实际 %d", code)
	}
	if code := getStatus(r, dl); code != http.StatusNotFound {
		t.Fatalf("第 3 次资源访问应耗尽 404, 实际 %d", code)
	}
}

// TestShare_CreateWithPasswordNotPlaintextInDB 带密码创建后库中存哈希、API 不回显（FR-78）。
func TestShare_ManagementAndPublicTokenAreIsolatedBySpace(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mediaA, err := libSvc.CreateMediaFileInSpace("space-a", 1, filepath.Join(t.TempDir(), "a.mp4"), 1)
	if err != nil {
		t.Fatalf("创建 Space A 媒体失败: %v", err)
	}
	mediaB, err := libSvc.CreateMediaFileInSpace("space-b", 2, filepath.Join(t.TempDir(), "b.mp4"), 1)
	if err != nil {
		t.Fatalf("创建 Space B 媒体失败: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/shares", bytes.NewBufferString(`{"resource_type":"media","resource_id":`+strconv.FormatInt(mediaB.ID, 10)+`}`))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-JianVideo-Space-Id", "space-b")
	createdW := httptest.NewRecorder()
	r.ServeHTTP(createdW, createReq)
	if createdW.Code != http.StatusCreated {
		t.Fatalf("Space B 创建自身分享失败: code=%d body=%s", createdW.Code, createdW.Body.String())
	}
	var created models.Share
	_ = json.Unmarshal(createdW.Body.Bytes(), &created)
	if created.SpaceID != "space-b" {
		t.Fatalf("分享应归属 space-b，实际 %q", created.SpaceID)
	}

	listA := httptest.NewRequest(http.MethodGet, "/api/shares", nil)
	listA.Header.Set("X-JianVideo-Space-Id", "space-a")
	listAW := httptest.NewRecorder()
	r.ServeHTTP(listAW, listA)
	if bytes.Contains(listAW.Body.Bytes(), []byte(created.Token)) {
		t.Fatalf("Space A 不得列举 Space B 分享: %s", listAW.Body.String())
	}

	forged, err := shareSvc.CreateInSpace("space-b", models.ShareResourceMedia, mediaA.ID, nil, "", 0, true)
	if err != nil {
		t.Fatalf("创建伪造跨 Space 分享失败: %v", err)
	}
	if code := getStatus(r, "/api/share/"+forged.Token); code != http.StatusNotFound {
		t.Fatalf("公开 token 不得读取其他 Space 资源，实际 %d", code)
	}
}

func TestShare_CreateWithPasswordNotPlaintextInDB(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "a.mp4", "A")

	body := `{"resource_type":"media","resource_id":` + strconv.FormatInt(mf.ID, 10) + `,"password":"top-secret","max_uses":3}`
	req := httptest.NewRequest("POST", "/api/shares", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建期望 201, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	// 响应体不应含密码哈希字段
	if bytes.Contains(w.Body.Bytes(), []byte("password_hash")) || bytes.Contains(w.Body.Bytes(), []byte("top-secret")) {
		t.Fatalf("响应不应回显密码或哈希, body: %s", w.Body.String())
	}
	var created models.Share
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.MaxUses != 3 {
		t.Fatalf("max_uses 应为 3, 实际 %d", created.MaxUses)
	}
	// 库中应存非明文哈希
	got, _ := shareSvc.Get(created.Token)
	if got.PasswordHash == "" || got.PasswordHash == "top-secret" {
		t.Fatalf("库中应存 bcrypt 哈希、非明文, 实际 %q", got.PasswordHash)
	}
}


// TestShare_DisallowDownload allow_download=false 时公开 download 404（FR2-055）。
func TestShare_DisallowDownload(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "nodl.mp4", "NODL")
	sh, err := shareSvc.CreateInSpace(models.DefaultSpaceID, models.ShareResourceMedia, mf.ID, nil, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if sh.AllowDownload {
		t.Fatal("AllowDownload 应为 false")
	}
	// 元信息仍可访问且回显 allow_download
	req := httptest.NewRequest("GET", "/api/share/"+sh.Token, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("元信息期望 200, 实际 %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"allow_download":false`)) {
		t.Fatalf("元信息应含 allow_download=false: %s", w.Body.String())
	}
	path := "/api/share/" + sh.Token + "/media/" + strconv.FormatInt(mf.ID, 10) + "/download"
	if code := getStatus(r, path); code != http.StatusNotFound {
		t.Fatalf("禁下载时期望 404, 实际 %d", code)
	}
	// stream 仍可（只禁止 download）
	// pbSvc 为 nil 时 stream 503，此处仅断言 download 门禁
}

// TestShare_CreateAllowDownloadField API 创建可传 allow_download。
func TestShare_CreateAllowDownloadField(t *testing.T) {
	r, libSvc, _ := setupShareRouter(t)
	mf := realMedia(t, libSvc, "x.mp4", "X")
	body := `{"resource_type":"media","resource_id":` + strconv.FormatInt(mf.ID, 10) + `,"allow_download":false}`
	req := httptest.NewRequest("POST", "/api/shares", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("创建期望 201, 实际 %d %s", w.Code, w.Body.String())
	}
	var sh models.Share
	_ = json.Unmarshal(w.Body.Bytes(), &sh)
	if sh.AllowDownload {
		t.Fatalf("应落库 allow_download=false: %+v", sh)
	}
}

// TestShare_ThumbnailMissingDoesNotGenerate 公开缩略图缺失不异步生成（成本门）。
func TestShare_ThumbnailMissingDoesNotGenerate(t *testing.T) {
	r, libSvc, shareSvc := setupShareRouter(t)
	mf := realMedia(t, libSvc, "t.jpg", "img")
	sh, err := shareSvc.Create(models.ShareResourceMedia, mf.ID, nil, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	// 无缩略图缓存：应 404 THUMBNAIL_NOT_READY，且不 panic / 不 202 GENERATING
	path := "/api/share/" + sh.Token + "/media/" + strconv.FormatInt(mf.ID, 10) + "/thumbnail"
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("缺失缩略图期望 404, 实际 %d body=%s", w.Code, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("GENERATING")) {
		t.Fatal("公开路径不得返回 GENERATING（会触发生成队列）")
	}
}
