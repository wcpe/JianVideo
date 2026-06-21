//go:build !windows

package library

import (
	"io/fs"
	"time"
)

// fileCreatedTime 返回文件创建时间。
// 多数类 Unix 平台的 fs.FileInfo 不暴露 birthtime，统一返回零值，
// 由媒体时间降级链跳过该层落到修改时间，保持简单不引第三方库。
func fileCreatedTime(_ fs.FileInfo) time.Time {
	return time.Time{}
}
