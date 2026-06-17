package transcoder

// Intel QSV 硬件加速检测。
//
// sysfs Intel GPU 检测使用纯 Go（os.ReadFile），编码器查找使用 CGO 条件编译。
// 无 CGO 时编码器查找返回 nil，QSV 标记为不可用。

import "os"

// isIntelGPU 通过 sysfs 检测是否存在 Intel 核显。
// 仅在 Linux 上有效，非 Linux 环境返回 (false, nil)。
func isIntelGPU() (bool, error) {
	vendorPath := "/sys/class/drm/card0/device/vendor"
	data, err := os.ReadFile(vendorPath)
	if err != nil {
		return false, nil
	}
	s := string(data)
	return s == "0x8086\n" || s == "0x8086", nil
}

// QSVAvailable 检测 QSV 整体可用性。
// 必须同时支持 H.264 和 H.265 编码器才算可用。
func QSVAvailable() (bool, error) {
	h264, err := findQSVEncoder("h264_qsv")
	if err != nil {
		return false, err
	}
	h265, err := findQSVEncoder("hevc_qsv")
	if err != nil {
		return false, err
	}
	return h264 != nil && h265 != nil, nil
}
