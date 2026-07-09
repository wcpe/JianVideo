package tools

import (
	"strings"
	"testing"
)

func TestResolveDownloadRequestRejectsUnsafeCustomURL(t *testing.T) {
	cases := []struct {
		name string
		req  DownloadRequest
		want string
	}{
		{
			name: "自定义 URL 必须带 sha256",
			req:  DownloadRequest{Tool: ToolFFmpeg, CustomURL: "https://example.com/ffmpeg.zip"},
			want: "sha256",
		},
		{
			name: "默认拒绝 HTTP 自定义 URL",
			req: DownloadRequest{
				Tool:      ToolFFmpeg,
				CustomURL: "http://example.com/ffmpeg.zip",
				SHA256:    strings.Repeat("a", 64),
			},
			want: "HTTP",
		},
		{
			name: "拒绝 file 协议",
			req: DownloadRequest{
				Tool:      ToolFFmpeg,
				CustomURL: "file:///tmp/ffmpeg.zip",
				SHA256:    strings.Repeat("a", 64),
			},
			want: "协议",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveDownloadRequest(tc.req, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误包含 %q，实际 %v", tc.want, err)
			}
		})
	}
}

func TestResolveDownloadRequestAllowsExplicitLocalHTTP(t *testing.T) {
	source, err := ResolveDownloadRequest(DownloadRequest{
		Tool:              ToolFFmpeg,
		CustomURL:         "http://127.0.0.1:18080/ffmpeg.zip",
		SHA256:            strings.Repeat("b", 64),
		Version:           "test",
		AllowInsecureHTTP: true,
	}, nil)
	if err != nil {
		t.Fatalf("显式允许的本地 HTTP 测试源应通过: %v", err)
	}
	if source.URL != "http://127.0.0.1:18080/ffmpeg.zip" || !source.AllowHTTP {
		t.Fatalf("解析后的测试源不正确: %+v", source)
	}
}

func TestResolveDownloadRequestUsesBuiltinSource(t *testing.T) {
	registry := []Source{
		{
			ID:       "ffmpeg-test",
			Tool:     ToolFFmpeg,
			Platform: "windows",
			Arch:     "amd64",
			Version:  "8.1.2",
			URL:      "https://downloads.example.com/ffmpeg.zip",
			SHA256:   strings.Repeat("c", 64),
			Size:     1024,
			Label:    "测试镜像",
		},
	}
	source, err := ResolveDownloadRequest(DownloadRequest{Tool: ToolFFmpeg, SourceID: "ffmpeg-test"}, registry)
	if err != nil {
		t.Fatalf("内置源应可解析: %v", err)
	}
	if source.Label != "测试镜像" || source.SHA256 != strings.Repeat("c", 64) {
		t.Fatalf("解析到的内置源不正确: %+v", source)
	}
}
