package transcoder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jianvideo/internal/db/models"
	"jianvideo/internal/library"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupTestDB 创建内存数据库。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.LibraryPath{}, &models.MediaFile{}, &models.MediaExtension{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

// setupStreamTestRouter 创建带流式端点的测试路由。
func setupStreamTestRouter(t *testing.T, svc *library.Service, factory func() Transcoder) *gin.Engine {
	t.Helper()
	sh := NewStreamHandlerWithFactory(svc, factory)
	r := gin.New()
	r.GET("/api/play/stream/:id", sh.StreamMedia)
	return r
}

// TestStreamMedia_InvalidID 验证无效 ID 返回 400。
func TestStreamMedia_InvalidID(t *testing.T) {
	db := setupTestDB(t)
	svc := library.NewService(db)
	r := setupStreamTestRouter(t, svc, nil)

	req := httptest.NewRequest("GET", "/api/play/stream/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// TestStreamMedia_NotFound 验证不存在的媒体文件返回 404。
func TestStreamMedia_NotFound(t *testing.T) {
	db := setupTestDB(t)
	svc := library.NewService(db)
	r := setupStreamTestRouter(t, svc, nil)

	req := httptest.NewRequest("GET", "/api/play/stream/9999", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d, body: %s", w.Code, w.Body.String())
	}
}

// TestStreamMedia_Success 验证正常流式输出。
func TestStreamMedia_Success(t *testing.T) {
	db := setupTestDB(t)
	svc := library.NewService(db)

	mf, err := svc.CreateMediaFile(1, "/tmp/test_video.mp4", 1024)
	if err != nil {
		t.Fatalf("创建媒体文件失败: %v", err)
	}

	mockFactory := func() Transcoder {
		return &mockTranscoder{
			runFunc: func(ctx context.Context, inputPath string, dst io.Writer) error {
				_, err := dst.Write([]byte("test ts stream data"))
				return err
			},
		}
	}

	r := setupStreamTestRouter(t, svc, mockFactory)

	req := httptest.NewRequest("GET", "/api/play/stream/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "video/MP2T" {
		t.Fatalf("期望 Content-Type=video/MP2T, 实际 %s", ct)
	}

	body := w.Body.String()
	if body == "" {
		t.Fatal("响应体不应为空")
	}
}

// TestStreamMedia_CacheControl 验证 Cache-Control 头正确设置。
func TestStreamMedia_CacheControl(t *testing.T) {
	db := setupTestDB(t)
	svc := library.NewService(db)

	mf, _ := svc.CreateMediaFile(1, "/tmp/test2.mp4", 2048)

	mockFactory := func() Transcoder {
		return &mockTranscoder{
			runFunc: func(ctx context.Context, inputPath string, dst io.Writer) error {
				return nil
			},
		}
	}

	r := setupStreamTestRouter(t, svc, mockFactory)

	req := httptest.NewRequest("GET", "/api/play/stream/"+strconv.FormatInt(mf.ID, 10), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("期望 Cache-Control=no-cache, 实际 %s", cc)
	}
}
