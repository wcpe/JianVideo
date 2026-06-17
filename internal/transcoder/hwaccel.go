package transcoder

// HWAccelDetector 定义硬件加速检测的接口。
type HWAccelDetector interface {
	// Name 返回硬件加速类型的名称，如 "qsv"、"nvenc"。
	Name() string
	// Available 检测当前环境是否可用该硬件加速。
	Available() (bool, error)
}
