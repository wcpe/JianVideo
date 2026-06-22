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

// downloadToTemp 流式下载 url 到 dir 下名为 name 的文件，返回落地路径。
func downloadToTemp(ctx context.Context, client *http.Client, url, dir, name string) (string, error) {
	resp, err := httpGet(ctx, client, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	dst := filepath.Join(dir, name)
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
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
	defer resp.Body.Close()
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
		resp.Body.Close()
		return nil, fmt.Errorf("下载返回非 200: %d", resp.StatusCode)
	}
	return resp, nil
}
