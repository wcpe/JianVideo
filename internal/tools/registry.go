package tools

import "runtime"

const (
	toolsVersion = "tools-v1.0.0"
	toolsBaseURL = "https://github.com/wcpe/JianVideo/releases/download/tools-v1.0.0/"
)

type runtimeArtifact struct {
	filename string
	sha256   string
	size     int64
}

var runtimeArtifacts = map[string]runtimeArtifact{
	"linux/amd64":   {"jianvideo-tools-linux-x86_64.zip", "0badc1e146982a4d2ddc7fca1b69624ba68f34debef35490ca870da84da89c0e", 30657068},
	"linux/arm64":   {"jianvideo-tools-linux-aarch64.zip", "ff8a95830ece5c605e4d210960f3cbe0ab0748e68271803f3da5143646535482", 30215707},
	"windows/amd64": {"jianvideo-tools-windows-x86_64.zip", "ac4ddd58f077b9cf27058a5c5149a800113ec6e092c58e08176408b63167944f", 34160498},
	"windows/arm64": {"jianvideo-tools-windows-aarch64.zip", "a3bbca26703bf53ea3f262830dd8fcc9299544082d6bcf17d3a174fe1f76b66c", 26194783},
	"darwin/amd64":  {"jianvideo-tools-macos-x86_64.zip", "d90fdf751428bac9b9521fa392ed9e812fb45229ecd1be1fe7a35f767ac41619", 27131446},
	"darwin/arm64":  {"jianvideo-tools-macos-aarch64.zip", "d824a6b19fec9a01063ba7dfaadb5dbf390d5866959e53751a0114e99da7865e", 25827260},
}

var defaultRegistry = registryForRuntime(runtime.GOOS, runtime.GOARCH)

// DefaultRegistry 返回当前运行平台的内置工具下载源副本。
func DefaultRegistry() []Source {
	return cloneSources(defaultRegistry)
}

func registryForRuntime(goos, goarch string) []Source {
	artifact, ok := runtimeArtifacts[goos+"/"+goarch]
	if !ok {
		return nil
	}

	tools := []string{ToolFFmpeg, ToolFFprobe, ToolMagick}
	sources := make([]Source, 0, len(tools))
	for _, tool := range tools {
		sources = append(sources, Source{
			ID:           tool + "-" + toolsVersion + "-" + goos + "-" + goarch,
			Tool:         tool,
			Platform:     goos,
			Arch:         goarch,
			Version:      toolsVersion,
			URL:          toolsBaseURL + artifact.filename,
			SHA256:       artifact.sha256,
			Size:         artifact.size,
			Label:        "JianVideo 工具包 " + toolsVersion,
			runtimeBound: true,
		})
	}
	return sources
}

func cloneSources(sources []Source) []Source {
	result := make([]Source, len(sources))
	copy(result, sources)
	return result
}
