package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDownloadToTemp_ReportsProgressByBytes 验证 downloadToTemp 通过回调按字节累进上报进度，
// 且总字节取响应 Content-Length、终值等于实际下载字节数（本地 httptest 提供带 Content-Length 的响应）。
func TestDownloadToTemp_ReportsProgressByBytes(t *testing.T) {
	// 构造 64KB 内容，确保 io.Copy 分多次写入、回调被多次调用
	body := make([]byte, 64*1024)
	for i := range body {
		body[i] = byte(i % 251)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 显式设置 Content-Length，使 resp.ContentLength 为已知总字节
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	var lastDownloaded, lastTotal int64
	var calls int
	var prev int64 = -1
	progressOK := true
	cb := func(downloaded, total int64) {
		calls++
		// 已下载字节必须单调不减
		if downloaded < prev {
			progressOK = false
		}
		prev = downloaded
		lastDownloaded, lastTotal = downloaded, total
	}

	dst, err := downloadToTemp(context.Background(), srv.Client(), srv.URL, dir, "bin", cb)
	if err != nil {
		t.Fatalf("downloadToTemp 失败: %v", err)
	}
	if dst == "" {
		t.Fatal("downloadToTemp 应返回落地路径")
	}
	if calls == 0 {
		t.Fatal("进度回调应至少被调用一次")
	}
	if !progressOK {
		t.Fatal("已下载字节应单调不减")
	}
	if lastDownloaded != int64(len(body)) {
		t.Errorf("已下载终值应等于总字节 %d，实际 %d", len(body), lastDownloaded)
	}
	if lastTotal != int64(len(body)) {
		t.Errorf("总字节应取 Content-Length %d，实际 %d", len(body), lastTotal)
	}
}

// TestDownloadToTemp_UnknownTotalReportsZero 当响应无 Content-Length（总字节未知）时，
// 回调 total 报 0（不确定态），已下载字节仍正常累进。
func TestDownloadToTemp_UnknownTotalReportsZero(t *testing.T) {
	body := []byte("hello-jianvideo-update-progress")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 用 Flusher + chunked，使 Content-Length 未知（resp.ContentLength = -1）
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	var lastTotal int64 = -999
	var lastDownloaded int64
	cb := func(downloaded, total int64) {
		lastDownloaded, lastTotal = downloaded, total
	}
	if _, err := downloadToTemp(context.Background(), srv.Client(), srv.URL, dir, "bin", cb); err != nil {
		t.Fatalf("downloadToTemp 失败: %v", err)
	}
	if lastTotal != 0 {
		t.Errorf("总字节未知时应报 0（不确定态），实际 %d", lastTotal)
	}
	if lastDownloaded != int64(len(body)) {
		t.Errorf("已下载终值应等于实际字节 %d，实际 %d", len(body), lastDownloaded)
	}
}

// TestProgressState_LifecycleViaApply 验证进度状态单例随下载推进更新：
// 下载阶段为 downloading 且 total/downloaded 被填，下载完成进入校验阶段。
// 这里直接驱动下载链路 + 状态写入，覆盖 Service.Progress() 读取并发安全副本。
func TestProgressState_DownloadingThenVerifying(t *testing.T) {
	body := make([]byte, 8*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	s := &Service{}
	// 初始为 idle
	if got := s.Progress(); got.State != progressIdle {
		t.Fatalf("初始进度状态应为 idle，实际 %q", got.State)
	}

	dir := t.TempDir()
	s.setProgressDownloading(0, 0)
	_, err := downloadToTemp(context.Background(), srv.Client(), srv.URL, dir, "bin", s.setProgressDownloading)
	if err != nil {
		t.Fatalf("downloadToTemp 失败: %v", err)
	}
	p := s.Progress()
	if p.State != progressDownloading {
		t.Errorf("下载后状态应为 downloading，实际 %q", p.State)
	}
	if p.Downloaded != int64(len(body)) || p.Total != int64(len(body)) {
		t.Errorf("下载进度应记满，实际 downloaded=%d total=%d", p.Downloaded, p.Total)
	}
	if p.Percent != 100 {
		t.Errorf("下载完成百分比应为 100，实际 %d", p.Percent)
	}

	s.setProgressVerifying()
	if got := s.Progress().State; got != progressVerifying {
		t.Errorf("进入校验后状态应为 verifying，实际 %q", got)
	}

	s.setProgressFailed()
	if got := s.Progress().State; got != progressFailed {
		t.Errorf("失败后状态应为 failed，实际 %q", got)
	}
}

// TestProgressPercent 校验百分比纯函数各分支（FR-90）。
func TestProgressPercent(t *testing.T) {
	cases := []struct {
		downloaded, total int64
		want              int
	}{
		{0, 0, 0},       // 全未知
		{50, 0, 0},      // 总字节未知 → 0（不确定态）
		{0, 100, 0},     // 尚未下载
		{50, 100, 50},   // 半程
		{100, 100, 100}, // 完成
		{150, 100, 100}, // 超过总字节也钳到 100
		{1, 3, 33},      // 取整向下
	}
	for _, c := range cases {
		if got := progressPercent(c.downloaded, c.total); got != c.want {
			t.Errorf("progressPercent(%d,%d)=%d，期望 %d", c.downloaded, c.total, got, c.want)
		}
	}
}
