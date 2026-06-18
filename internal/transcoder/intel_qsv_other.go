//go:build !linux
// +build !linux

package transcoder

// 非 Linux 平台上的桩实现。

// isIntelGPU 在非 Linux 平台上始终返回 (false, nil)。
func isIntelGPU() (bool, error) {
	return false, nil
}

// QSVAvailable 在非 Linux 平台上始终返回 (false, nil)。
func QSVAvailable() (bool, error) {
	return false, nil
}
