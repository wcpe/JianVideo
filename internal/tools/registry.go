package tools

var defaultRegistry = []Source{
	{
		ID:       "ffmpeg-source-8.1.2",
		Tool:     ToolFFmpeg,
		Platform: "source",
		Arch:     "all",
		Version:  "8.1.2",
		URL:      "https://ffmpeg.org/releases/ffmpeg-8.1.2.tar.gz",
		Label:    "FFmpeg 官方源码包",
	},
	{
		ID:       "imagemagick-download-page",
		Tool:     ToolMagick,
		Platform: "metadata",
		Arch:     "all",
		Version:  "latest",
		URL:      "https://imagemagick.org/script/download.php",
		Label:    "ImageMagick 官方下载页",
	},
}

// DefaultRegistry 返回内置工具下载源副本。
func DefaultRegistry() []Source {
	return cloneSources(defaultRegistry)
}

func cloneSources(sources []Source) []Source {
	result := make([]Source, len(sources))
	copy(result, sources)
	return result
}
