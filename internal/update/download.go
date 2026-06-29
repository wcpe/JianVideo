package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// maxChecksumsBytes 校验和清单的读取上限，防异常大响应。
const maxChecksumsBytes = 64 * 1024

// progressFunc 下载进度回调（FR-90）：downloaded 为累计已下载字节，total 为总字节（0 表示未知）。
type progressFunc func(downloaded, total int64)

// countingWriter 包装目标 io.Writer，每写入一批字节后回调上报累计进度（FR-90）。
// total 取自响应 Content-Length（≤0 时视为未知、以 0 上报）。
type countingWriter struct {
	w          io.Writer
	total      int64
	downloaded int64
	onProgress progressFunc
}

// Write 透传写入并累计已下载字节，再回调进度。
func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.downloaded += int64(n)
	if cw.onProgress != nil {
		cw.onProgress(cw.downloaded, cw.total)
	}
	return n, err
}

// downloadToTemp 流式下载 url 到 dir 下名为 name 的文件，返回落地路径。
// onProgress 非 nil 时按字节累进上报「已下载 / 总字节」（FR-90，total 取 Content-Length，未知报 0）。
func downloadToTemp(ctx context.Context, client *http.Client, url, dir, name string, onProgress progressFunc) (string, error) {
	resp, err := httpGet(ctx, client, url)
	if err != nil {
		return "", err
	}
	// 资源清理，关闭错误可忽略
	defer func() { _ = resp.Body.Close() }()
	dst := filepath.Join(dir, name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	// 临时文件完整性后续由 sha256 校验保证，关闭错误可忽略
	defer func() { _ = f.Close() }()
	// Content-Length 未知时 resp.ContentLength 为 -1，归一化为 0（不确定态）
	total := resp.ContentLength
	if total < 0 {
		total = 0
	}
	dstWriter := &countingWriter{w: f, total: total, onProgress: onProgress}
	if _, err := io.Copy(dstWriter, resp.Body); err != nil {
		return "", fmt.Errorf("写入下载文件失败: %w", err)
	}
	return dst, nil
}

// downloadText 下载小文本资产（如 checksums.txt），带大小上限。
func downloadText(ctx context.Context, client *http.Client, url string) (string, error) {
	resp, err := httpGet(ctx, client, url)
	if err != nil {
		return "", err
	}
	// 资源清理，关闭错误可忽略
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// httpGet 发起 GET 并校验 2xx，返回响应（调用方负责关闭 Body）。
func httpGet(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 错误清理分支，关闭错误可忽略
		_ = resp.Body.Close()
		return nil, fmt.Errorf("下载返回非 200: %d", resp.StatusCode)
	}
	return resp, nil
}
