package tools

import (
	"runtime"
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

func TestResolveDownloadRequestUsesBuiltinSourceForCurrentRuntime(t *testing.T) {
	registry := []Source{
		builtinTestSource(runtime.GOOS, runtime.GOARCH),
	}
	source, err := ResolveDownloadRequest(DownloadRequest{Tool: ToolFFmpeg, SourceID: "ffmpeg-test"}, registry)
	if err != nil {
		t.Fatalf("当前运行平台架构的内置源应可解析: %v", err)
	}
	if source.Label != "测试镜像" || source.SHA256 != strings.Repeat("c", 64) {
		t.Fatalf("解析到的内置源不正确: %+v", source)
	}
}

func TestResolveDownloadRequestAllowsUnboundSourceForOtherRuntime(t *testing.T) {
	registry := []Source{
		builtinTestSource("other-platform", "other-arch"),
	}
	source, err := ResolveDownloadRequest(DownloadRequest{Tool: ToolFFmpeg, SourceID: "ffmpeg-test"}, registry)
	if err != nil {
		t.Fatalf("未绑定运行环境的异平台注入源应可解析: %v", err)
	}
	if source.Platform != "other-platform" || source.Arch != "other-arch" {
		t.Fatalf("解析到的注入源不正确: %+v", source)
	}
}

func TestResolveDownloadRequestRejectsBuiltinSourceForOtherPlatform(t *testing.T) {
	source := builtinTestSource("other-platform", runtime.GOARCH)
	source.runtimeBound = true
	registry := []Source{source}
	_, err := ResolveDownloadRequest(DownloadRequest{Tool: ToolFFmpeg, SourceID: "ffmpeg-test"}, registry)
	if err == nil || !strings.Contains(err.Error(), "平台") {
		t.Fatalf("期望平台不匹配错误，实际 %v", err)
	}
}

func TestResolveDownloadRequestRejectsBuiltinSourceForOtherArch(t *testing.T) {
	source := builtinTestSource(runtime.GOOS, "other-arch")
	source.runtimeBound = true
	registry := []Source{source}
	_, err := ResolveDownloadRequest(DownloadRequest{Tool: ToolFFmpeg, SourceID: "ffmpeg-test"}, registry)
	if err == nil || !strings.Contains(err.Error(), "架构") {
		t.Fatalf("期望架构不匹配错误，实际 %v", err)
	}
}

func builtinTestSource(platform, arch string) Source {
	return Source{
		ID:       "ffmpeg-test",
		Tool:     ToolFFmpeg,
		Platform: platform,
		Arch:     arch,
		Version:  "8.1.2",
		URL:      "https://downloads.example.com/ffmpeg.zip",
		SHA256:   strings.Repeat("c", 64),
		Size:     1024,
		Label:    "测试镜像",
	}
}
