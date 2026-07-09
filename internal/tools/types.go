// Package tools 提供外部工具下载、校验与受控安装能力。
package tools

import "time"

// 工具标识和任务类型常量。
const (
	ToolFFmpeg  = "ffmpeg"
	ToolFFprobe = "ffprobe"
	ToolMagick  = "magick"

	// TaskTypeDownload 是外部工具下载任务类型。
	TaskTypeDownload = "tool.download"
)

// Source 描述一个可下载的外部工具源。
type Source struct {
	ID        string `json:"id"`
	Tool      string `json:"tool"`
	Platform  string `json:"platform"`
	Arch      string `json:"arch"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Label     string `json:"label"`
	AllowHTTP bool   `json:"allow_http,omitempty"`
}

// DownloadRequest 描述外部工具下载请求。
type DownloadRequest struct {
	Tool              string `json:"tool"`
	SourceID          string `json:"source_id"`
	CustomURL         string `json:"custom_url"`
	SHA256            string `json:"sha256"`
	Version           string `json:"version"`
	AllowInsecureHTTP bool   `json:"allow_insecure_http"`
}

// InstallResult 描述工具安装完成后的路径与版本。
type InstallResult struct {
	Tool        string `json:"tool"`
	Path        string `json:"path"`
	Version     string `json:"version"`
	VersionText string `json:"version_text"`
}

// Status 描述工具当前配置路径与已安装版本。
type Status struct {
	Tool           string          `json:"tool"`
	SettingKey     string          `json:"setting_key"`
	ConfiguredPath string          `json:"configured_path"`
	Installed      []InstallRecord `json:"installed"`
}

// InstallRecord 描述一个受控目录下已安装的工具版本。
type InstallRecord struct {
	Version   string    `json:"version"`
	Path      string    `json:"path"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProgressFunc 上报下载与安装进度。
type ProgressFunc func(downloaded, total int64, step string) error

// ApplyFunc 在工具路径写入设置前热应用安装结果。
type ApplyFunc func(InstallResult) error
