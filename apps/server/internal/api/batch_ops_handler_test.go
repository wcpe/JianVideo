package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wcpe/JianVideo/internal/library"
)

// TestBatchMoveMediaFiles_API 索引层批量移动（FR2-053）。
func TestBatchMoveMediaFiles_API(t *testing.T) {
	router, svc := setupTestRouter(t)
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src, err := svc.CreateLibraryPath(srcDir, "local", "源库")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := svc.CreateLibraryPath(dstDir, "local", "目标库")
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.CreateMediaFile(src.ID, filepath.ToSlash(filepath.Join(srcDir, "a.mp4")), 100)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.CreateMediaFile(src.ID, filepath.ToSlash(filepath.Join(srcDir, "b.jpg")), 100)
	if err != nil {
		t.Fatal(err)
	}

	body := `{"ids":[` + strconv.FormatInt(a.ID, 10) + `,` + strconv.FormatInt(b.ID, 10) + `],"target_library_id":` + strconv.FormatInt(dst.ID, 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/batch-move", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Moved   int64 `json:"moved"`
		Skipped int64 `json:"skipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Moved != 2 {
		t.Fatalf("期望 moved=2, 实际 %d", resp.Moved)
	}

	srcItems, _, err := svc.ListMediaFilesFiltered(library.MediaFilter{LibraryID: src.ID}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcItems) != 0 {
		t.Fatalf("源库应为空, 实际 %d", len(srcItems))
	}
	dstItems, _, err := svc.ListMediaFilesFiltered(library.MediaFilter{LibraryID: dst.ID}, 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(dstItems) != 2 {
		t.Fatalf("目标库应有 2 条, 实际 %d", len(dstItems))
	}
}

// TestBatchMoveMediaFiles_TargetMissing 目标库不存在。
func TestBatchMoveMediaFiles_TargetMissing(t *testing.T) {
	router, svc := setupTestRouter(t)
	srcDir := t.TempDir()
	src, err := svc.CreateLibraryPath(srcDir, "local", "源")
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.CreateMediaFile(src.ID, filepath.ToSlash(filepath.Join(srcDir, "a.mp4")), 100)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"ids":[` + strconv.FormatInt(a.ID, 10) + `],"target_library_id":99999}`
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/batch-move", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404, 实际 %d body=%s", w.Code, w.Body.String())
	}
}

// TestBatchMoveMediaFiles_TooMany 超限拒绝。
func TestBatchMoveMediaFiles_TooMany(t *testing.T) {
	router, _ := setupTestRouter(t)
	ids := make([]any, 0, 101)
	for i := 1; i <= 101; i++ {
		ids = append(ids, i)
	}
	payload, _ := json.Marshal(map[string]any{"ids": ids, "target_library_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/batch-move", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400, 实际 %d", w.Code)
	}
}

// TestBatchTranscodeMediaFiles_NoService 未注入转码服务时 503。
func TestBatchTranscodeMediaFiles_NoService(t *testing.T) {
	router, svc := setupTestRouter(t)
	dir := t.TempDir()
	lp, err := svc.CreateLibraryPath(dir, "local", "库")
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.CreateMediaFile(lp.ID, filepath.ToSlash(filepath.Join(dir, "a.mp4")), 100)
	if err != nil {
		t.Fatal(err)
	}
	// 未注入 presets/hlsPreview：应 503
	body := `{"ids":[` + strconv.FormatInt(a.ID, 10) + `],"preset_id":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/batch-transcode", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("期望 503, 实际 %d body=%s", w.Code, w.Body.String())
	}
}

// TestBatchTranscodeMediaFiles_Empty 空 ids 直接 200。
func TestBatchTranscodeMediaFiles_Empty(t *testing.T) {
	router, _ := setupTestRouter(t)
	req := httptest.NewRequest(http.MethodPost, "/api/library/media/batch-transcode", bytes.NewBufferString(`{"ids":[],"preset_id":1}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200, 实际 %d", w.Code)
	}
}

// TestIsImageMediaFormat 图片后缀识别。
func TestIsImageMediaFormat(t *testing.T) {
	if !isImageMediaFormat("jpg") || !isImageMediaFormat(".PNG") {
		t.Fatal("应识别图片后缀")
	}
	if isImageMediaFormat("mp4") || isImageMediaFormat("") {
		t.Fatal("视频与空串不应判为图片")
	}
}
