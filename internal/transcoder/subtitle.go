package transcoder

import (
	"bufio"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	srtTimePattern = regexp.MustCompile(`^\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})\s*-->\s*(\d{2}:\d{2}:\d{2}[,.]\d{3})(?:\s+.*)?$`)
	assTimePattern = regexp.MustCompile(`^\s*(\d+:\d{2}:\d{2}[.]\d{2})\s*$`)
	assTagPattern  = regexp.MustCompile(`\{\\[^}]*\}`)
	vttTimePattern = regexp.MustCompile(`^\s*(?:(\d{2}):)?(\d{2}):(\d{2})[.](\d{3})\s*-->\s*(?:(\d{2}):)?(\d{2}):(\d{2})[.](\d{3})(?:\s+.*)?$`)
)

// SubtitleEntry 表示一条字幕。
type SubtitleEntry struct {
	StartTime float64
	EndTime   float64
	Text      string
}

// ErrSubtitleFileUnavailable 表示受限目录中的字幕文件不可安全读取。
var ErrSubtitleFileUnavailable = errors.New("字幕文件不可用")

// ConvertSubtitleFile 根据文件扩展名转换并安全规范化为 WebVTT。
func ConvertSubtitleFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取字幕文件失败: %w", err)
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
	return ConvertSubtitle(format, data)
}

// ConvertSubtitleFileInRoot 在受限根目录中按 basename 打开并转换字幕。
func ConvertSubtitleFileInRoot(rootDir, baseName string) (string, error) {
	data, err := readSubtitleFileInRoot(rootDir, baseName)
	if err != nil {
		return "", err
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(baseName)), ".")
	return ConvertSubtitle(format, data)
}

func readSubtitleFileInRoot(rootDir, baseName string) ([]byte, error) {
	if baseName == "" || baseName == "." || filepath.Base(baseName) != baseName {
		return nil, fmt.Errorf("%w: 字幕文件名必须为 basename", ErrSubtitleFileUnavailable)
	}
	file, err := os.OpenInRoot(rootDir, baseName)
	if err != nil {
		return nil, fmt.Errorf("%w: 打开字幕文件失败", ErrSubtitleFileUnavailable)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: 字幕文件不是普通文件", ErrSubtitleFileUnavailable)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("%w: 读取字幕文件失败", ErrSubtitleFileUnavailable)
	}
	return data, nil
}

// ConvertSubtitle 将受支持的纯文本字幕转换为安全 WebVTT。
func ConvertSubtitle(format string, data []byte) (string, error) {
	if err := ValidateSubtitle(format, data); err != nil {
		return "", err
	}
	switch strings.ToLower(format) {
	case "srt":
		return SRTToWebVTT(string(data))
	case "ass", "ssa":
		return ASSToWebVTT(string(data))
	case "vtt":
		return normalizeWebVTT(string(data))
	default:
		return "", fmt.Errorf("不支持的字幕格式: %s", format)
	}
}

// ValidateSubtitle 校验字幕文本编码和格式结构。
func ValidateSubtitle(format string, data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("字幕内容为空")
	}
	if !utf8.Valid(data) || containsBinaryByte(data) {
		return fmt.Errorf("字幕内容不是有效 UTF-8 文本")
	}
	normalized := normalizeNewlines(string(data))
	switch strings.ToLower(format) {
	case "srt":
		return validateSRT(normalized)
	case "ass", "ssa":
		return validateASS(normalized)
	case "vtt":
		return validateVTT(normalized)
	default:
		return fmt.Errorf("不支持的字幕格式: %s", format)
	}
}

func containsBinaryByte(data []byte) bool {
	for _, value := range data {
		if value == 0 || value < 0x09 || value > 0x0d && value < 0x20 {
			return true
		}
	}
	return false
}

func validateSRT(content string) error {
	for _, line := range strings.Split(content, "\n") {
		if srtTimePattern.MatchString(line) {
			return nil
		}
	}
	return fmt.Errorf("SRT 缺少有效时间轴")
}

func validateASS(content string) error {
	if !strings.Contains(strings.ToLower(content), "[events]") {
		return fmt.Errorf("ASS/SSA 缺少 Events 段")
	}
	for _, line := range strings.Split(content, "\n") {
		if _, ok := parseASSDialogue(line); ok {
			return nil
		}
	}
	return fmt.Errorf("ASS/SSA 缺少有效 Dialogue")
}

func validateVTT(content string) error {
	lines := strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "WEBVTT" {
		return fmt.Errorf("VTT 缺少 WEBVTT 文件头")
	}
	for _, line := range lines[1:] {
		if vttTimePattern.MatchString(line) {
			return nil
		}
	}
	return fmt.Errorf("VTT 缺少有效时间轴")
}

// SRTToWebVTT 将 SRT 字幕内容转换为 WebVTT 格式。
func SRTToWebVTT(content string) (string, error) {
	blocks := splitSubtitleBlocks(normalizeNewlines(content))
	entries := make([]SubtitleEntry, 0, len(blocks))
	for _, block := range blocks {
		entry, err := parseSRTBlock(block)
		if err != nil {
			continue
		}
		entries = append(entries, *entry)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("SRT 没有可转换字幕")
	}
	return buildWebVTT(entries), nil
}

func splitSubtitleBlocks(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return regexp.MustCompile(`\n[\t ]*\n+`).Split(content, -1)
}

func parseSRTBlock(block string) (*SubtitleEntry, error) {
	lines := strings.Split(block, "\n")
	for index, line := range lines {
		match := srtTimePattern.FindStringSubmatch(line)
		if match == nil || index+1 >= len(lines) {
			continue
		}
		return buildSRTEntry(match[1], match[2], lines[index+1:])
	}
	return nil, fmt.Errorf("SRT 块缺少时间轴或文本")
}

func buildSRTEntry(startRaw, endRaw string, textLines []string) (*SubtitleEntry, error) {
	start, err := parseSRTTimestamp(startRaw)
	if err != nil {
		return nil, err
	}
	end, err := parseSRTTimestamp(endRaw)
	if err != nil || end <= start {
		return nil, fmt.Errorf("SRT 结束时间无效")
	}
	text := escapeCueLines(textLines)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("SRT 字幕文本为空")
	}
	return &SubtitleEntry{StartTime: start, EndTime: end, Text: text}, nil
}

func parseSRTTimestamp(timestamp string) (float64, error) {
	parts := strings.Split(strings.Replace(timestamp, ",", ".", 1), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("时间格式错误: %s", timestamp)
	}
	return parseTimestampParts(parts)
}

func parseTimestampParts(parts []string) (float64, error) {
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
func ASSToWebVTT(content string) (string, error) {
	entries := make([]SubtitleEntry, 0)
	scanner := bufio.NewScanner(strings.NewReader(normalizeNewlines(content)))
	scanner.Buffer(make([]byte, 1024), 16<<20)
	for scanner.Scan() {
		entry, ok := parseASSDialogue(scanner.Text())
		if ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("读取 ASS/SSA 失败: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("ASS/SSA 没有可转换字幕")
	}
	return buildWebVTT(entries), nil
}

func parseASSDialogue(line string) (SubtitleEntry, bool) {
	separator := strings.Index(line, ":")
	if separator < 0 || !strings.EqualFold(strings.TrimSpace(line[:separator]), "Dialogue") {
		return SubtitleEntry{}, false
	}
	fields := splitASSDialogueFields(strings.TrimSpace(line[separator+1:]))
	if len(fields) < 10 || !assTimePattern.MatchString(fields[1]) || !assTimePattern.MatchString(fields[2]) {
		return SubtitleEntry{}, false
	}
	start, end := parseASSTimestamp(fields[1]), parseASSTimestamp(fields[2])
	text := sanitizeASSText(fields[9])
	return SubtitleEntry{StartTime: start, EndTime: end, Text: text}, end > start && strings.TrimSpace(text) != ""
}

func splitASSDialogueFields(value string) []string {
	return strings.SplitN(value, ",", 10)
}

func parseASSTimestamp(timestamp string) float64 {
	parts := strings.Split(strings.TrimSpace(timestamp), ":")
	if len(parts) != 3 {
		return 0
	}
	value, _ := parseTimestampParts(parts)
	return value
}

func sanitizeASSText(text string) string {
	text = assTagPattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, `\N`, "\n")
	text = strings.ReplaceAll(text, `\n`, "\n")
	text = strings.ReplaceAll(text, `\h`, " ")
	return escapeCueLines(strings.Split(text, "\n"))
}

func normalizeWebVTT(content string) (string, error) {
	lines := strings.Split(normalizeNewlines(strings.TrimPrefix(content, "\ufeff")), "\n")
	entries := make([]SubtitleEntry, 0)
	for index := 1; index < len(lines); index++ {
		match := vttTimePattern.FindStringSubmatch(lines[index])
		if match == nil {
			continue
		}
		entry, next, err := parseVTTCue(lines, index, match)
		if err == nil {
			entries = append(entries, entry)
		}
		index = next
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("VTT 没有可转换字幕")
	}
	return buildWebVTT(entries), nil
}

func parseVTTCue(lines []string, index int, match []string) (SubtitleEntry, int, error) {
	start, err := parseVTTMatch(match, 1)
	if err != nil {
		return SubtitleEntry{}, index, err
	}
	end, err := parseVTTMatch(match, 5)
	if err != nil || end <= start {
		return SubtitleEntry{}, index, fmt.Errorf("VTT 结束时间无效")
	}
	textLines, next := collectCueLines(lines, index+1)
	text := escapeCueLines(textLines)
	return SubtitleEntry{StartTime: start, EndTime: end, Text: text}, next, nil
}

func parseVTTMatch(match []string, offset int) (float64, error) {
	hour := match[offset]
	if hour == "" {
		hour = "0"
	}
	return parseTimestampParts([]string{hour, match[offset+1], match[offset+2] + "." + match[offset+3]})
}

func collectCueLines(lines []string, start int) ([]string, int) {
	result := make([]string, 0)
	index := start
	for index < len(lines) && strings.TrimSpace(lines[index]) != "" {
		result = append(result, lines[index])
		index++
	}
	return result, index
}

func escapeCueLines(lines []string) string {
	escaped := make([]string, len(lines))
	for index, line := range lines {
		escaped[index] = html.EscapeString(line)
	}
	return strings.TrimSpace(strings.Join(escaped, "\n"))
}

func buildWebVTT(entries []SubtitleEntry) string {
	var builder strings.Builder
	builder.WriteString("WEBVTT\n\n")
	for index, entry := range entries {
		fmt.Fprintf(&builder, "%d\n%s --> %s\n%s\n\n", index+1, formatWebVTTTime(entry.StartTime), formatWebVTTTime(entry.EndTime), entry.Text)
	}
	return builder.String()
}

func formatWebVTTTime(seconds float64) string {
	totalMilliseconds := int64(seconds*1000 + 0.5)
	hours := totalMilliseconds / 3600000
	minutes := totalMilliseconds / 60000 % 60
	wholeSeconds := totalMilliseconds / 1000 % 60
	milliseconds := totalMilliseconds % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, wholeSeconds, milliseconds)
}

func normalizeNewlines(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return strings.ReplaceAll(content, "\r", "\n")
}
