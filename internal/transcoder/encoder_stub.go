//go:build !ffmpeg
// +build !ffmpeg

package transcoder

// findEncoderByName 通过编码器名称查找编码器。
// 无 CGO 时始终返回 (nil, nil)。
func findEncoderByName(name string) (interface{}, error) {
	_ = name
	return nil, nil
}
