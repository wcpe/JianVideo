package transcoder

import (
	"testing"
)

func TestParseRangeHeader(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		wantStart int64
		wantEnd   int64
		wantErr   bool
	}{
		{
			name:      "基本范围",
			header:    "bytes=0-499",
			wantStart: 0,
			wantEnd:   499,
		},
		{
			name:      "中间范围",
			header:    "bytes=500-999",
			wantStart: 500,
			wantEnd:   999,
		},
		{
			name:      "从指定位置到末尾",
			header:    "bytes=9500-",
			wantStart: 9500,
			wantEnd:   -1, // -1 表示到末尾
		},
		{
			name:      "最后 N 字节",
			header:    "bytes=-500",
			wantStart: -1, // 后缀范围，特殊处理
			wantEnd:   500,
		},
		{
			name:    "缺少 bytes= 前缀",
			header:  "0-499",
			wantErr: true,
		},
		{
			name:    "空字符串",
			header:  "",
			wantErr: true,
		},
		{
			name:    "无效格式",
			header:  "bytes=abc-def",
			wantErr: true,
		},
		{
			name:    "缺少连字符",
			header:  "bytes=100",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := ParseRangeHeader(tt.header)
			if tt.wantErr {
				if err == nil {
					t.Fatal("期望错误, 但返回 nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("不期望错误, 但返回: %v", err)
			}
			if start != tt.wantStart {
				t.Errorf("start: 期望 %d, 实际 %d", tt.wantStart, start)
			}
			if end != tt.wantEnd {
				t.Errorf("end: 期望 %d, 实际 %d", tt.wantEnd, end)
			}
		})
	}
}

func TestBytesToTimePosition(t *testing.T) {
	tests := []struct {
		name        string
		startByte   int64
		totalSize   int64
		durationSec float64
		wantTimeSec float64
	}{
		{
			name:        "起始位置",
			startByte:   0,
			totalSize:   1000000,
			durationSec: 120.0,
			wantTimeSec: 0.0,
		},
		{
			name:        "中间位置",
			startByte:   500000,
			totalSize:   1000000,
			durationSec: 120.0,
			wantTimeSec: 60.0,
		},
		{
			name:        "四分之三位置",
			startByte:   750000,
			totalSize:   1000000,
			durationSec: 120.0,
			wantTimeSec: 90.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToTimePosition(tt.startByte, tt.totalSize, tt.durationSec)
			// 允许 0.01 秒误差
			if result < tt.wantTimeSec-0.01 || result > tt.wantTimeSec+0.01 {
				t.Errorf("期望 %f, 实际 %f", tt.wantTimeSec, result)
			}
		})
	}
}

func TestBytesToTimePositionZeroSize(t *testing.T) {
	result := BytesToTimePosition(0, 0, 120.0)
	if result != 0.0 {
		t.Fatalf("总大小为 0 时应返回 0, 实际 %f", result)
	}
}

func TestFormatContentRange(t *testing.T) {
	result := FormatContentRange(0, 499, 1000000)
	expected := "bytes 0-499/1000000"
	if result != expected {
		t.Fatalf("期望 '%s', 实际 '%s'", expected, result)
	}
}
