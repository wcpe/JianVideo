package tools

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestRegistryForRuntimeReturnsToolsV1Sources(t *testing.T) {
	cases := []struct {
		goos     string
		goarch   string
		artifact string
		sha256   string
		size     int64
	}{
		{"linux", "amd64", "jianvideo-tools-linux-x86_64.zip", "0badc1e146982a4d2ddc7fca1b69624ba68f34debef35490ca870da84da89c0e", 30657068},
		{"linux", "arm64", "jianvideo-tools-linux-aarch64.zip", "ff8a95830ece5c605e4d210960f3cbe0ab0748e68271803f3da5143646535482", 30215707},
		{"windows", "amd64", "jianvideo-tools-windows-x86_64.zip", "ac4ddd58f077b9cf27058a5c5149a800113ec6e092c58e08176408b63167944f", 34160498},
		{"windows", "arm64", "jianvideo-tools-windows-aarch64.zip", "a3bbca26703bf53ea3f262830dd8fcc9299544082d6bcf17d3a174fe1f76b66c", 26194783},
		{"darwin", "amd64", "jianvideo-tools-macos-x86_64.zip", "d90fdf751428bac9b9521fa392ed9e812fb45229ecd1be1fe7a35f767ac41619", 27131446},
		{"darwin", "arm64", "jianvideo-tools-macos-aarch64.zip", "d824a6b19fec9a01063ba7dfaadb5dbf390d5866959e53751a0114e99da7865e", 25827260},
	}
	tools := []string{ToolFFmpeg, ToolFFprobe, ToolMagick}
	const baseURL = "https://github.com/wcpe/JianVideo/releases/download/tools-v1.0.0/"

	for _, tc := range cases {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			sources := registryForRuntime(tc.goos, tc.goarch)
			if len(sources) != len(tools) {
				t.Fatalf("期望返回三个工具源，实际 %d 个", len(sources))
			}
			for i, tool := range tools {
				source := sources[i]
				if source.Tool != tool || source.ID != tool+"-tools-v1.0.0-"+tc.goos+"-"+tc.goarch {
					t.Fatalf("工具或源 ID 不正确: %+v", source)
				}
				if source.Platform != tc.goos || source.Arch != tc.goarch || source.Version != "tools-v1.0.0" {
					t.Fatalf("运行平台信息不正确: %+v", source)
				}
				if source.URL != baseURL+tc.artifact || source.SHA256 != tc.sha256 || source.Size != tc.size {
					t.Fatalf("制品信息不正确: %+v", source)
				}
			}
		})
	}
}

func TestRuntimeBoundIsNotSerialized(t *testing.T) {
	encoded, err := json.Marshal(Source{ID: "test", runtimeBound: true})
	if err != nil {
		t.Fatalf("序列化下载源失败: %v", err)
	}
	if strings.Contains(string(encoded), "runtimeBound") || strings.Contains(string(encoded), "runtime_bound") {
		t.Fatalf("内部运行时绑定字段不应暴露到 JSON: %s", encoded)
	}
}

func TestRegistryForRuntimeReturnsEmptyForUnsupportedRuntime(t *testing.T) {
	if sources := registryForRuntime("plan9", "amd64"); len(sources) != 0 {
		t.Fatalf("未知平台应返回空结果，实际 %+v", sources)
	}
}

func TestDefaultRegistryReturnsIndependentCopies(t *testing.T) {
	first := DefaultRegistry()
	if len(first) == 0 {
		t.Skipf("当前运行平台 %s/%s 不受支持", runtime.GOOS, runtime.GOARCH)
	}
	originalID := first[0].ID
	first[0].ID = "已修改"

	second := DefaultRegistry()
	if len(second) != 3 || second[0].ID != originalID {
		t.Fatalf("外部修改不应影响默认 registry: %+v", second)
	}
}
