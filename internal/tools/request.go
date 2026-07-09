package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strings"
)

// ResolveDownloadRequest 将下载请求解析为安全可执行的下载源。
func ResolveDownloadRequest(req DownloadRequest, registry []Source) (Source, error) {
	tool := normalizeTool(req.Tool)
	if tool == "" {
		return Source{}, fmt.Errorf("工具不受支持")
	}
	if strings.TrimSpace(req.CustomURL) != "" {
		return resolveCustomSource(req, tool)
	}
	sourceID := strings.TrimSpace(req.SourceID)
	if sourceID == "" {
		return Source{}, fmt.Errorf("source_id 或 custom_url 不能为空")
	}
	for _, source := range registry {
		if source.ID == sourceID {
			if source.Tool != tool {
				return Source{}, fmt.Errorf("工具与下载源不匹配")
			}
			if strings.TrimSpace(source.SHA256) == "" {
				return Source{}, fmt.Errorf("内置下载源缺少 sha256，不能自动下载")
			}
			if err := validateSourceURL(source.URL, source.AllowHTTP); err != nil {
				return Source{}, err
			}
			return source, nil
		}
	}
	return Source{}, fmt.Errorf("下载源不存在")
}

func resolveCustomSource(req DownloadRequest, tool string) (Source, error) {
	sum := strings.TrimSpace(strings.ToLower(req.SHA256))
	if !isSHA256(sum) {
		return Source{}, fmt.Errorf("自定义 URL 必须提供 sha256")
	}
	if err := validateSourceURL(req.CustomURL, req.AllowInsecureHTTP); err != nil {
		return Source{}, err
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = "custom-" + sum[:12]
	}
	return Source{
		ID:        customSourceID(tool, req.CustomURL, sum),
		Tool:      tool,
		Platform:  runtime.GOOS,
		Arch:      runtime.GOARCH,
		Version:   version,
		URL:       strings.TrimSpace(req.CustomURL),
		SHA256:    sum,
		Label:     "自定义下载源",
		AllowHTTP: req.AllowInsecureHTTP,
	}, nil
}

func validateSourceURL(raw string, allowHTTP bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("下载 URL 格式不正确")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" {
			return fmt.Errorf("下载 URL 格式不正确")
		}
		return nil
	case "http":
		if u.Host == "" {
			return fmt.Errorf("下载 URL 格式不正确")
		}
		if !allowHTTP {
			return fmt.Errorf("默认拒绝 HTTP 下载 URL")
		}
		if isLocalHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("HTTP 下载 URL 仅允许本地测试源")
	default:
		return fmt.Errorf("下载 URL 协议不受支持")
	}
}

func normalizeTool(tool string) string {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case ToolFFmpeg:
		return ToolFFmpeg
	case ToolFFprobe:
		return ToolFFprobe
	case ToolMagick, "imagemagick":
		return ToolMagick
	default:
		return ""
	}
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func customSourceID(tool, rawURL, sum string) string {
	digest := sha256.Sum256([]byte(tool + "|" + rawURL + "|" + sum))
	return "custom-" + hex.EncodeToString(digest[:])[:16]
}
