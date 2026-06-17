package transcoder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSRTToWebVTT 测试 SRT 转 WebVTT。
func TestSRTToWebVTT(t *testing.T) {
	srtContent := `1
00:00:01,000 --> 00:00:02,500
第一行字幕

2
00:00:03,000 --> 00:00:04,000
第二行字幕
`

	vtt, err := SRTToWebVTT(srtContent)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	// WebVTT 必须以 WEBVTT 开头
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Errorf("WebVTT 应以 WEBVTT 开头, 实际开头: %s", vtt[:20])
	}

	// 应包含时间戳
	if !strings.Contains(vtt, "00:00:01.000") {
		t.Error("应包含第一个时间戳")
	}
	if !strings.Contains(vtt, "00:00:02.500") {
		t.Error("应包含第一个结束时间戳")
	}

	// 应包含字幕文本
	if !strings.Contains(vtt, "第一行字幕") {
		t.Error("应包含第一行字幕文本")
	}
	if !strings.Contains(vtt, "第二行字幕") {
		t.Error("应包含第二行字幕文本")
	}

	// SRT 的逗号时间分隔符应转为点号
	if strings.Contains(vtt, "00:00:01,000") {
		t.Error("SRT 逗号应转为 WebVTT 点号")
	}
}

// TestSRTToWebVTT_Empty 测试空内容。
func TestSRTToWebVTT_Empty(t *testing.T) {
	vtt, err := SRTToWebVTT("")
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Error("即使空内容也应有 WebVTT 头")
	}
}

// TestSRTToWebVTT_MultiLine 测试多行字幕文本。
func TestSRTToWebVTT_MultiLine(t *testing.T) {
	srtContent := `1
00:00:01,000 --> 00:00:03,000
第一行
第二行
第三行
`

	vtt, err := SRTToWebVTT(srtContent)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !strings.Contains(vtt, "第一行") || !strings.Contains(vtt, "第二行") || !strings.Contains(vtt, "第三行") {
		t.Errorf("应保留所有字幕行, 实际:\n%s", vtt)
	}
}

// TestASSToWebVTT 测试 ASS 转 WebVTT。
func TestASSToWebVTT(t *testing.T) {
	assContent := `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,2,1,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,ASS字幕测试
`

	vtt, err := ASSToWebVTT(assContent)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Errorf("WebVTT 应以 WEBVTT 开头, 实际开头: %s", vtt[:20])
	}
	if !strings.Contains(vtt, "ASS字幕测试") {
		t.Error("应包含 ASS 字幕文本")
	}
	if !strings.Contains(vtt, "00:00:01.000") {
		t.Error("应包含开始时间戳")
	}
	if !strings.Contains(vtt, "00:00:03.000") {
		t.Error("应包含结束时间戳")
	}
}

// TestASSToWebVTT_MultiDialogue 测试多条 ASS 对话。
func TestASSToWebVTT_MultiDialogue(t *testing.T) {
	assContent := `[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,第一条
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,第二条
`

	vtt, err := ASSToWebVTT(assContent)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}

	if !strings.Contains(vtt, "第一条") || !strings.Contains(vtt, "第二条") {
		t.Errorf("应包含所有对话, 实际:\n%s", vtt)
	}
}

// TestASSParseTimestamp 测试 ASS 时间戳解析。
func TestASSParseTimestamp(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"0:00:01.00", 1.0},
		{"0:00:10.50", 10.5},
		{"1:00:00.00", 3600.0},
		{"0:01:30.25", 90.25},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseASSTimestamp(tt.input)
			if result != tt.expected {
				t.Errorf("期望 %.2f, 实际 %.2f", tt.expected, result)
			}
		})
	}
}

// TestSRTParseTimestamp 测试 SRT 时间戳解析。
func TestSRTParseTimestamp(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"00:00:01,000", 1.0},
		{"00:00:10,500", 10.5},
		{"01:00:00,000", 3600.0},
		{"00:01:30,250", 90.25},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseSRTTimestamp(tt.input)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if result != tt.expected {
				t.Errorf("期望 %.2f, 实际 %.2f", tt.expected, result)
			}
		})
	}
}

// TestConvertSubtitleFile_SRT 测试完整的 SRT 文件转换。
func TestConvertSubtitleFile_SRT(t *testing.T) {
	tmp := t.TempDir()
	srtPath := filepath.Join(tmp, "test.srt")
	srtContent := "1\n00:00:01,000 --> 00:00:02,000\n测试字幕\n"
	_ = os.WriteFile(srtPath, []byte(srtContent), 0o644)

	vtt, err := ConvertSubtitleFile(srtPath)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Error("应以 WEBVTT 开头")
	}
	if !strings.Contains(vtt, "测试字幕") {
		t.Error("应包含字幕文本")
	}
}

// TestConvertSubtitleFile_ASS 测试完整的 ASS 文件转换。
func TestConvertSubtitleFile_ASS(t *testing.T) {
	tmp := t.TempDir()
	assPath := filepath.Join(tmp, "test.ass")
	assContent := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,测试\n"
	_ = os.WriteFile(assPath, []byte(assContent), 0o644)

	vtt, err := ConvertSubtitleFile(assPath)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Error("应以 WEBVTT 开头")
	}
}

// TestConvertSubtitleFile_SUP 测试 SUP 占位。
func TestConvertSubtitleFile_SUP(t *testing.T) {
	tmp := t.TempDir()
	supPath := filepath.Join(tmp, "test.sup")
	_ = os.WriteFile(supPath, []byte("fake"), 0o644)

	vtt, err := ConvertSubtitleFile(supPath)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	// SUP 占位：返回空 WebVTT
	if !strings.HasPrefix(vtt, "WEBVTT") {
		t.Error("应以 WEBVTT 开头")
	}
}

// TestConvertSubtitleFile_Unsupported 测试不支持的格式。
func TestConvertSubtitleFile_Unsupported(t *testing.T) {
	tmp := t.TempDir()
	txtPath := filepath.Join(tmp, "test.txt")
	_ = os.WriteFile(txtPath, []byte("not subtitle"), 0o644)

	_, err := ConvertSubtitleFile(txtPath)
	if err == nil {
		t.Error("不支持的格式应返回错误")
	}
}

// TestParseSRTBlock 测试 SRT 字幕块解析。
func TestParseSRTBlock(t *testing.T) {
	block := "1\n00:00:01,000 --> 00:00:02,500\nHello World"
	result, err := parseSRTBlock(block)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if result == nil {
		t.Fatal("不应返回 nil")
	}
	if result.Text != "Hello World" {
		t.Errorf("文本不匹配: %s", result.Text)
	}
	if result.StartTime != 1.0 {
		t.Errorf("开始时间不匹配: %f", result.StartTime)
	}
	if result.EndTime != 2.5 {
		t.Errorf("结束时间不匹配: %f", result.EndTime)
	}
}
