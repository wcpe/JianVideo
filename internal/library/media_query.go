package library

import (
	"sort"
	"strconv"
	"strings"
)

// builtInImageExtensionList 返回内置图片扩展名（小写、不含点）的有序列表，供类型粗筛用。
// 排序保证查询稳定、便于测试断言。
func builtInImageExtensionList() []string {
	exts := make([]string, 0, len(builtInMediaExtensions))
	for ext, mediaType := range builtInMediaExtensions {
		if mediaType == MediaTypeImage {
			exts = append(exts, ext)
		}
	}
	sort.Strings(exts)
	return exts
}

// lowerAll 返回全部转小写的副本。
func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

// 大小单位换算（字节）。
var sizeUnits = map[string]int64{
	"b": 1, "kb": 1 << 10, "mb": 1 << 20, "gb": 1 << 30, "tb": 1 << 40,
}

// ParseSearchExpression 把 everything 式表达式解析为结构化 MediaFilter（FR-35）。
//
// 安全：只产出结构化字段（类型/扩展名/大小/关键词），由调用方走参数化查询，
// 不拼接任何 SQL，无注入面。无法识别的 token 退化为文件名关键词。
//
// 支持的 token（空白分隔）：
//   - ext:jpg 或 ext:jpg,png      → Formats
//   - type:image / type:video     → MediaType
//   - size:>10mb / size:<1gb /
//     size:>=500kb / size:<=2mb   → SizeMin/SizeMax（单位 b/kb/mb/gb/tb，缺省 b）
//   - camera:Canon                → CameraTerms（仅约束 EXIF 相机列，FR-136）
//   - lens:50mm                   → LensTerms（仅约束 EXIF 镜头列，FR-136）
//   - 其余裸词                    → Terms（跨文件名/显示名/相机/镜头/备注，多词 AND）
func ParseSearchExpression(expr string) MediaFilter {
	var f MediaFilter
	for _, tok := range strings.Fields(expr) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "ext:"):
			for _, e := range strings.Split(tok[len("ext:"):], ",") {
				e = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(e)), ".")
				if e != "" {
					f.Formats = append(f.Formats, e)
				}
			}
		case strings.HasPrefix(lower, "type:"):
			switch strings.ToLower(tok[len("type:"):]) {
			case "image", "img", "photo":
				f.MediaType = MediaTypeImage
			case "video", "vid":
				f.MediaType = MediaTypeVideo
			}
		case strings.HasPrefix(lower, "size:"):
			applySizeToken(&f, tok[len("size:"):])
		case strings.HasPrefix(lower, "camera:"):
			if v := strings.TrimSpace(tok[len("camera:"):]); v != "" {
				f.CameraTerms = append(f.CameraTerms, v)
			}
		case strings.HasPrefix(lower, "lens:"):
			if v := strings.TrimSpace(tok[len("lens:"):]); v != "" {
				f.LensTerms = append(f.LensTerms, v)
			}
		default:
			f.Terms = append(f.Terms, tok)
		}
	}
	return f
}

// applySizeToken 解析 size 比较式（如 >10mb、<=2gb），写入 SizeMin/SizeMax（含义为闭区间）。
// 无法解析则忽略（不报错，避免搜索因输入瑕疵整体失败）。
func applySizeToken(f *MediaFilter, s string) {
	op := ""
	switch {
	case strings.HasPrefix(s, ">="):
		op, s = ">=", s[2:]
	case strings.HasPrefix(s, "<="):
		op, s = "<=", s[2:]
	case strings.HasPrefix(s, ">"):
		op, s = ">", s[1:]
	case strings.HasPrefix(s, "<"):
		op, s = "<", s[1:]
	default:
		return // 不带比较符的 size 视为无效，忽略
	}

	bytes, ok := parseSizeValue(s)
	if !ok {
		return
	}
	switch op {
	case ">":
		f.SizeMin = bytes + 1 // 严格大于
	case ">=":
		f.SizeMin = bytes
	case "<":
		if bytes > 0 {
			f.SizeMax = bytes - 1 // 严格小于
		}
	case "<=":
		f.SizeMax = bytes
	}
}

// parseSizeValue 解析「数字+单位」为字节数，如 "10mb"、"500kb"、"1024"（缺省单位 b）。
func parseSizeValue(s string) (int64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	// 拆出末尾单位字母
	i := len(s)
	for i > 0 && (s[i-1] < '0' || s[i-1] > '9') {
		i--
	}
	numPart, unitPart := s[:i], s[i:]
	if numPart == "" {
		return 0, false
	}
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil || num < 0 {
		return 0, false
	}
	mult := int64(1)
	if unitPart != "" {
		m, ok := sizeUnits[unitPart]
		if !ok {
			return 0, false
		}
		mult = m
	}
	return int64(num * float64(mult)), true
}
