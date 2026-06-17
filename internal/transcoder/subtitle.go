package transcoder

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// SubtitleEntry 表示一条字幕。
type SubtitleEntry struct {
	StartTime float64
	EndTime   float64
	Text      string
}

// ConvertSubtitleFile 根据文件扩展名选择对应转换器。
func ConvertSubtitleFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
	switch ext {
	case "srt":
		return SRTToWebVTT(string(data))
	case "ass", "ssa":
		return ASSToWebVTT(string(data))
	case "sup":
		// SUP 为图片字幕，本期占位
		return "WEBVTT\n\n", nil
	default:
		return "", fmt.Errorf("不支持的字幕格式: %s", ext)
	}
}

// SRTToWebVTT 将 SRT 字幕内容转换为 WebVTT 格式。
func SRTToWebVTT(srtContent string) (string, error) {
	var entries []SubtitleEntry

	scanner := bufio.NewScanner(strings.NewReader(srtContent))
	var currentBlock []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			// 空行表示一个块结束
			if len(currentBlock) > 0 {
				entry, err := parseSRTBlock(strings.Join(currentBlock, "\n"))
				if err == nil && entry != nil {
					entries = append(entries, *entry)
				}
				currentBlock = nil
			}
			continue
		}
		currentBlock = append(currentBlock, line)
	}

	// 处理最后一个块（文件末尾可能没有空行）
	if len(currentBlock) > 0 {
		entry, err := parseSRTBlock(strings.Join(currentBlock, "\n"))
		if err == nil && entry != nil {
			entries = append(entries, *entry)
		}
	}

	return buildWebVTT(entries), nil
}

// parseSRTBlock 解析一个 SRT 字幕块。
// 格式: 序号\n时间轴\n文本（可能多行）
func parseSRTBlock(block string) (*SubtitleEntry, error) {
	lines := strings.Split(block, "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("SRT 块至少需要 2 行")
	}

	// 解析时间轴行（第 2 行，索引 1）
	timeLine := ""
	timeLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "-->") {
			timeLine = line
			timeLineIdx = i
			break
		}
	}

	if timeLine == "" {
		return nil, fmt.Errorf("未找到时间轴")
	}

	parts := strings.Split(timeLine, "-->")
	if len(parts) != 2 {
		return nil, fmt.Errorf("时间轴格式错误")
	}

	startTime, err := parseSRTTimestamp(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("开始时间解析失败: %w", err)
	}

	endTime, err := parseSRTTimestamp(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("结束时间解析失败: %w", err)
	}

	// 文本在时间轴之后
	textLines := lines[timeLineIdx+1:]
	text := strings.Join(textLines, "\n")

	return &SubtitleEntry{
		StartTime: startTime,
		EndTime:   endTime,
		Text:      text,
	}, nil
}

// parseSRTTimestamp 解析 SRT 时间格式 HH:MM:SS,mmm 为秒数。
func parseSRTTimestamp(ts string) (float64, error) {
	// 替换逗号为点以便统一处理
	ts = strings.Replace(ts, ",", ".", 1)

	parts := strings.Split(ts, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("时间格式错误: %s", ts)
	}

	hours, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, err
	}

	minutes, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, err
	}

	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, err
	}

	return hours*3600 + minutes*60 + seconds, nil
}

// ASSToWebVTT 将 ASS/SSA 字幕内容转换为 WebVTT 格式。
func ASSToWebVTT(assContent string) (string, error) {
	var entries []SubtitleEntry

	// 匹配 Dialogue 行
	// 格式: Dialogue: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text
	dialogueRegex := regexp.MustCompile(`^Dialogue:\s*(.+)$`)

	scanner := bufio.NewScanner(strings.NewReader(assContent))
	for scanner.Scan() {
		line := scanner.Text()
		matches := dialogueRegex.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		fields := splitASSDialogueFields(matches[1])
		if len(fields) < 10 {
			continue
		}

		startTime := parseASSTimestamp(fields[1])
		endTime := parseASSTimestamp(fields[2])
		text := fields[9]

		// 清理 ASS 格式标签
		text = cleanASSTags(text)
		if text == "" {
			continue
		}

		entries = append(entries, SubtitleEntry{
			StartTime: startTime,
			EndTime:   endTime,
			Text:      text,
		})
	}

	return buildWebVTT(entries), nil
}

// splitASSDialogueFields 分割 ASS Dialogue 行字段。
// 前 9 个字段用逗号分隔，第 10 个字段（Text）可能包含逗号。
func splitASSDialogueFields(s string) []string {
	const fieldCount = 10
	fields := make([]string, 0, fieldCount)

	firstComma := 0
	for i := 0; i < fieldCount-1; i++ {
		idx := strings.Index(s[firstComma:], ",")
		if idx == -1 {
			fields = append(fields, s[firstComma:])
			return fields
		}
		fields = append(fields, s[firstComma:firstComma+idx])
		firstComma += idx + 1
	}

	// 剩余部分作为最后一个字段（Text）
	fields = append(fields, s[firstComma:])
	return fields
}

// parseASSTESTAMP 解析 ASS 时间格式 H:MM:SS.cc 为秒数。
func parseASSTimestamp(ts string) float64 {
	ts = strings.TrimSpace(ts)
	parts := strings.Split(ts, ":")
	if len(parts) != 3 {
		return 0
	}

	hours, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	minutes, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)

	// 秒可能包含小数点
	secParts := strings.Split(parts[2], ".")
	seconds, _ := strconv.ParseFloat(strings.TrimSpace(secParts[0]), 64)
	var centiseconds float64
	if len(secParts) == 2 {
		// ASS 使用百分秒（centiseconds），转为秒
		centiseconds, _ = strconv.ParseFloat(secParts[1], 64)
		centiseconds = centiseconds / 100.0
	}

	return hours*3600 + minutes*60 + seconds + centiseconds
}

// cleanASSTags 清理 ASS 格式标签。
func cleanASSTags(text string) string {
	// 移除常见的 ASS 标签
	tagPatterns := []string{
		`\{\\[^}]*\}`, // {...} 标签块
		`\\N`,         // 换行符
		`\\n`,         // 软换行
	}

	for _, pattern := range tagPatterns {
		re := regexp.MustCompile(pattern)
		text = re.ReplaceAllString(text, "")
	}

	// 将 \h 替换为空格
	text = strings.ReplaceAll(text, `\h`, " ")

	return strings.TrimSpace(text)
}

// buildWebVTT 将字幕条目构建为 WebVTT 字符串。
func buildWebVTT(entries []SubtitleEntry) string {
	var sb strings.Builder

	sb.WriteString("WEBVTT\n\n")

	for i, entry := range entries {
		sb.WriteString(strconv.Itoa(i + 1))
		sb.WriteString("\n")
		sb.WriteString(formatWebVTTTime(entry.StartTime))
		sb.WriteString(" --> ")
		sb.WriteString(formatWebVTTTime(entry.EndTime))
		sb.WriteString("\n")
		sb.WriteString(entry.Text)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// formatWebVTTTime 将秒数格式化为 WebVTT 时间格式 HH:MM:SS.mmm。
func formatWebVTTTime(seconds float64) string {
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	ms := int((seconds - float64(int(seconds))) * 1000)

	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
