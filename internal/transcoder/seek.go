package transcoder

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseRangeHeader 解析 HTTP Range 头，返回起始和结束字节位置。
// 支持的格式：
//   - "bytes=start-end"  → 返回 (start, end)
//   - "bytes=start-"    → 返回 (start, -1)，-1 表示到末尾
//   - "bytes=-suffix"   → 返回 (-1, suffix)，后缀范围特殊处理
//
// 返回 (start, end, error)。start 或 end 为 -1 时表示未指定。
func ParseRangeHeader(header string) (start, end int64, err error) {
	if header == "" {
		return 0, 0, fmt.Errorf("Range 头为空")
	}

	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("Range 头格式错误，缺少 'bytes=' 前缀: %s", header)
	}

	spec := strings.TrimPrefix(header, "bytes=")

	// 后缀范围：bytes=-500
	if strings.HasPrefix(spec, "-") {
		suffix, err := strconv.ParseInt(strings.TrimPrefix(spec, "-"), 10, 64)
		if err != nil || suffix < 0 {
			return 0, 0, fmt.Errorf("无效的后缀范围: %s", spec)
		}
		return -1, suffix, nil
	}

	// 标准范围：bytes=start-end 或 bytes=start-
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("Range 头格式错误，缺少连字符: %s", spec)
	}

	startVal, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || startVal < 0 {
		return 0, 0, fmt.Errorf("无效的起始位置: %s", parts[0])
	}

	if parts[1] == "" {
		// bytes=start- 格式，end 到末尾
		return startVal, -1, nil
	}

	endVal, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || endVal < 0 {
		return 0, 0, fmt.Errorf("无效的结束位置: %s", parts[1])
	}

	if endVal < startVal {
		return 0, 0, fmt.Errorf("结束位置小于起始位置: %s", spec)
	}

	return startVal, endVal, nil
}

// BytesToTimePosition 将字节位置转换为时间位置（秒）。
// 基于文件的总大小和时长进行线性插值估算。
// 如果 totalSize 为 0，返回 0。
func BytesToTimePosition(startByte, totalSize int64, durationSec float64) float64 {
	if totalSize <= 0 || durationSec <= 0 {
		return 0.0
	}
	if startByte <= 0 {
		return 0.0
	}
	if startByte >= totalSize {
		return durationSec
	}
	return float64(startByte) / float64(totalSize) * durationSec
}

// FormatContentRange 生成 Content-Range 响应头值。
// 格式："bytes start-end/totalSize"
func FormatContentRange(start, end, totalSize int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", start, end, totalSize)
}
