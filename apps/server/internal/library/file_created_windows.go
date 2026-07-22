//go:build windows

package library

import (
	"io/fs"
	"syscall"
	"time"
)

// fileCreatedTime 返回文件创建时间（Windows 有 birthtime）。
// 取不到时返回零值，由媒体时间降级链跳过该层。
func fileCreatedTime(info fs.FileInfo) time.Time {
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && data != nil {
		return time.Unix(0, data.CreationTime.Nanoseconds())
	}
	return time.Time{}
}
