package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/wcpe/JianVideo/internal/library"
)

// writeThumbAt 在缩略图目录写一份伪 JPEG 内容，模拟某尺寸缓存已存在。
func writeThumbAt(t *testing.T, path string) {
	t.Helper()
	// 缩略图目录由全局 sync.Once 初始化，可能指向已被前一测试清理的临时目录，按需重建父目录。
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建缩略图目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("fake-jpeg"), 0o644); err != nil {
		t.Fatalf("写入伪缩略图失败: %v", err)
	}
}

// TestServeThumbnail_SizeNegotiation 端点按 size 命中对应尺寸缓存：
// 默认尺寸命中既有路径；指定 160 命中 160 缓存；缺失尺寸回 202。
func TestServeThumbnail_SizeNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	thumbDir := t.TempDir()
	library.InitThumbnailDir(thumbDir)

	r, svc, _ := setupShareRouter(t)
	mf := realMedia(t, svc, "pic.png", "data")
	idStr := strconv.FormatInt(mf.ID, 10)

	// 默认尺寸缓存命中 → 200
	writeThumbAt(t, library.FindThumbnailPath(mf.FilePath))
	if code := getStatus(r, "/api/library/thumbnail/"+idStr); code != 200 {
		t.Fatalf("默认尺寸已缓存应回 200，实际 %d", code)
	}

	// 指定 160：先缺失 → 202
	if code := getStatus(r, "/api/library/thumbnail/"+idStr+"?size=160"); code != 202 {
		t.Fatalf("160 尺寸未缓存应回 202，实际 %d", code)
	}

	// 写入 160 缓存后 → 200
	writeThumbAt(t, library.ThumbnailPathForSize(mf.FilePath, 160))
	if code := getStatus(r, "/api/library/thumbnail/"+idStr+"?size=160"); code != 200 {
		t.Fatalf("160 尺寸已缓存应回 200，实际 %d", code)
	}

	// 非法 size 回落默认尺寸（默认已缓存）→ 200
	if code := getStatus(r, "/api/library/thumbnail/"+idStr+"?size=999"); code != 200 {
		t.Fatalf("非法 size 应回落默认尺寸命中缓存回 200，实际 %d", code)
	}
}

// TestServeThumbnail_DefaultPathUnchanged 默认尺寸命中的缓存路径必须是 FindThumbnailPath（保 dHash/FR-73）。
func TestServeThumbnail_DefaultPathUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 注：thumbnailDir 由 sync.Once 全局初始化，包内首个 InitThumbnailDir 生效；
	// 此处只需保证目录已初始化，断言以 FindThumbnailPath 返回的实际路径为准。
	library.InitThumbnailDir(t.TempDir())

	r, svc, _ := setupShareRouter(t)
	mf := realMedia(t, svc, "pic.png", "data")
	idStr := strconv.FormatInt(mf.ID, 10)

	// 仅写默认路径，缺省 size 请求应命中它
	def := library.FindThumbnailPath(mf.FilePath)
	if library.GetThumbnailDir() == "" || filepath.Dir(def) != library.GetThumbnailDir() {
		t.Fatalf("默认缩略图应位于缩略图目录下：%s", def)
	}
	writeThumbAt(t, def)

	req := httptest.NewRequest("GET", "/api/library/thumbnail/"+idStr, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "fake-jpeg" {
		t.Fatalf("缺省 size 应命中默认路径文件，code=%d body=%q", w.Code, w.Body.String())
	}
}
