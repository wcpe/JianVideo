package library

import (
	"path/filepath"
	"strings"
)

// 来源 App 识别（FR-148）：按文件名启发式推断媒体的来源 App / 渠道。
// 纯函数、无副作用、不读数据库；命中规则返回来源标识，无命中返回空字符串（前端不显示）。
// 规则以有序表组织，便于后续扩展；匹配大小写不敏感，仅按文件名基名判定。

// 来源标识常量：作为对外返回值与前端展示文案的唯一来源，避免魔法字符串散落。
const (
	SourceScreenshot = "截图"
	SourceWeChat     = "微信"
	SourceQQ         = "QQ"
	SourceCamera     = "相机"
)

// sourceMatchKind 匹配方式：前缀匹配或任意位置子串匹配。
type sourceMatchKind int

const (
	matchPrefix   sourceMatchKind = iota // 文件名以该模式开头
	matchContains                        // 文件名包含该模式
)

// sourceRule 单条来源识别规则：以小写模式按指定方式匹配，命中返回对应来源标识。
type sourceRule struct {
	pattern string          // 已小写的匹配模式
	kind    sourceMatchKind // 匹配方式
	source  string          // 命中后返回的来源标识
}

// sourceRules 来源识别规则表（按优先级从高到低排列，命中即返回）。
// 顺序敏感：更具体 / 更优先的渠道排在前（如 mmexport 既属微信导出，优先判微信）。
var sourceRules = []sourceRule{
	// 截图：各平台前缀与中文命名
	{"screenshot", matchPrefix, SourceScreenshot},
	{"screen_", matchPrefix, SourceScreenshot},
	{"屏幕截图", matchContains, SourceScreenshot},
	{"截屏", matchContains, SourceScreenshot},
	{"截图", matchContains, SourceScreenshot},

	// 微信：导出 / 图片 / 视频
	{"mmexport", matchPrefix, SourceWeChat},
	{"wechat", matchPrefix, SourceWeChat},
	{"微信图片", matchContains, SourceWeChat},
	{"微信视频", matchContains, SourceWeChat},

	// QQ：图片 / 前缀
	{"qq图片", matchContains, SourceQQ},
	{"qq_image", matchPrefix, SourceQQ},
	{"qq_", matchPrefix, SourceQQ},

	// 相机原图：各厂商通用前缀
	{"img_", matchPrefix, SourceCamera},
	{"vid_", matchPrefix, SourceCamera},
	{"dsc", matchPrefix, SourceCamera},
	{"dcim", matchPrefix, SourceCamera},
}

// IdentifySourceApp 按文件名启发式推断媒体来源 App。
// 入参可为纯文件名或含目录的完整路径，仅取基名判定；大小写不敏感。
// 命中任一规则返回对应来源标识（如「微信」「截图」），无命中返回空字符串。
func IdentifySourceApp(filename string) string {
	base := strings.ToLower(filepath.Base(filename))
	if base == "" || base == "." {
		return ""
	}
	for _, rule := range sourceRules {
		switch rule.kind {
		case matchPrefix:
			if strings.HasPrefix(base, rule.pattern) {
				return rule.source
			}
		case matchContains:
			if strings.Contains(base, rule.pattern) {
				return rule.source
			}
		}
	}
	return ""
}
