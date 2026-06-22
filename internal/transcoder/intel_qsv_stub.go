//go:build !ffmpeg

package transcoder

// findQSVEncoder 通过编码器名称查找 QSV 编码器是否可用。
// 非 ffmpeg 构建无 libav 检测能力，始终返回 (false, nil)；与 cgo 版签名（bool）一致。
func findQSVEncoder(name string) (bool, error) {
	_ = name
	return false, nil
}
