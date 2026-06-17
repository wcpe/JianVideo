//go:build !cgo
// +build !cgo

package transcoder

// findQSVEncoder 通过编码器名称查找 QSV 编码器是否可用。
// 无 CGO 时始终返回 (nil, nil)。
func findQSVEncoder(name string) (interface{}, error) {
	_ = name
	return nil, nil
}
