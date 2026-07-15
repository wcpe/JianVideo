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

// TestSRTToWebVTT_Empty 测试空内容拒绝伪成功。
func TestSRTToWebVTT_Empty(t *testing.T) {
	if _, err := SRTToWebVTT(""); err == nil {
		t.Fatal("空 SRT 应返回错误")
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

// TestConvertSubtitleFile_SUP 测试图片字幕不伪装为成功。
func TestConvertSubtitleFile_SUP(t *testing.T) {
	tmp := t.TempDir()
	supPath := filepath.Join(tmp, "test.sup")
	_ = os.WriteFile(supPath, []byte("fake"), 0o644)

	if _, err := ConvertSubtitleFile(supPath); err == nil {
		t.Fatal("SUP 图片字幕必须返回不支持错误")
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

func TestConvertSubtitleFile_VTTNormalizesUnsafeMarkup(t *testing.T) {
	tmp := t.TempDir()
	vttPath := filepath.Join(tmp, "test.vtt")
	content := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n<script>alert(1)</script><b>安全文本</b>\n"
	if err := os.WriteFile(vttPath, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 VTT 失败: %v", err)
	}
	vtt, err := ConvertSubtitleFile(vttPath)
	if err != nil {
		t.Fatalf("转换 VTT 失败: %v", err)
	}
	if strings.Contains(strings.ToLower(vtt), "<script") || strings.Contains(vtt, "<b>") {
		t.Fatalf("不安全标记未转义: %s", vtt)
	}
	if !strings.Contains(vtt, "安全文本") {
		t.Fatalf("安全文本丢失: %s", vtt)
	}
}

func TestConvertSubtitleFileRejectsTraversalPath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "media")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("创建媒体目录失败: %v", err)
	}
	outside := filepath.Join(parent, "outside.srt")
	if err := os.WriteFile(outside, []byte("1\n00:00:01,000 --> 00:00:02,000\n越界内容\n"), 0o600); err != nil {
		t.Fatalf("创建目录外字幕失败: %v", err)
	}
	content, err := ConvertSubtitleFileInRoot(root, filepath.Join("..", "outside.srt"))
	if err == nil || strings.Contains(content, "越界内容") {
		t.Fatalf("路径穿越 basename 必须拒绝且不得泄露内容: err=%v content=%s", err, content)
	}
}

func TestConvertSubtitleFileInRootKeepsSupportedFormats(t *testing.T) {
	root := t.TempDir()
	samples := map[string]string{
		"srt": "1\n00:00:01,000 --> 00:00:02,000\nSRT正文\n",
		"ass": "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,ASS正文\n",
		"ssa": "[Events]\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,SSA正文\n",
		"vtt": "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nVTT正文\n",
	}
	for format, sample := range samples {
		t.Run(format, func(t *testing.T) {
			name := "movie." + format
			if err := os.WriteFile(filepath.Join(root, name), []byte(sample), 0o600); err != nil {
				t.Fatalf("创建 %s 字幕失败: %v", format, err)
			}
			content, err := ConvertSubtitleFileInRoot(root, name)
			if err != nil || !strings.Contains(content, strings.ToUpper(format)+"正文") {
				t.Fatalf("受限读取 %s 字幕失败: err=%v content=%s", format, err, content)
			}
		})
	}
}

func TestConvertSubtitleFileDoesNotFollowEscapingSymlinkAfterEnumeration(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "media")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("创建媒体目录失败: %v", err)
	}
	videoPath := filepath.Join(root, "movie.mkv")
	sidecarPath := filepath.Join(root, "movie.srt")
	outsidePath := filepath.Join(parent, "outside.srt")
	for path, content := range map[string]string{
		videoPath:   "media",
		sidecarPath: "1\n00:00:01,000 --> 00:00:02,000\n原字幕\n",
		outsidePath: "1\n00:00:01,000 --> 00:00:02,000\n目录外秘密\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("创建测试文件失败: %v", err)
		}
	}
	files, err := FindSubtitleFiles(videoPath)
	if err != nil || len(files) != 1 {
		t.Fatalf("枚举外挂字幕失败: files=%#v err=%v", files, err)
	}
	if err := os.Remove(sidecarPath); err != nil {
		t.Fatalf("删除已枚举字幕失败: %v", err)
	}
	if err := os.Symlink(outsidePath, sidecarPath); err != nil {
		t.Skipf("当前环境不支持创建符号链接: %v", err)
	}
	content, err := ConvertSubtitleFileInRoot(root, filepath.Base(files[0].Path))
	if err == nil || strings.Contains(content, "目录外秘密") {
		t.Fatalf("枚举后替换的越界符号链接必须拒绝且不得泄露内容: err=%v content=%s", err, content)
	}
}

func TestASSToWebVTT_PreservesLineBreakAndEscapesMarkup(t *testing.T) {
	content := "[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,第一行\\N<script>alert(1)</script>第二行\n"
	vtt, err := ASSToWebVTT(content)
	if err != nil {
		t.Fatalf("转换 ASS 失败: %v", err)
	}
	if !strings.Contains(vtt, "第一行\n&lt;script&gt;alert(1)&lt;/script&gt;第二行") {
		t.Fatalf("ASS 换行或转义错误: %s", vtt)
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
