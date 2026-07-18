package update

import (
	"regexp"
	"strconv"
	"strings"
)

// semverishPattern 匹配形如 1.2.3 或 1.2.3-dev.abc1234 的语义版本子串。
var semverishPattern = regexp.MustCompile(`\d+\.\d+\.\d+(?:-[0-9A-Za-z.\-]+)?`)

// extractSemverish 从任意文本（如 release 名）中提取首个语义版本子串；无则返回空串。
func extractSemverish(s string) string {
	return semverishPattern.FindString(s)
}

// parsedVersion 解析后的版本：主/次/修订三段 + 预发布标识。
type parsedVersion struct {
	major, minor, patch int
	pre                 string // 预发布标识（如 "dev.3.gabc1234"），无则为空
	ok                  bool   // 是否成功解析为 MAJOR.MINOR.PATCH
}

// RC 标签与预发布标识的严格格式；数字段遵循语义版本规则，不允许无意义的前导零。
var (
	rcTagPattern = regexp.MustCompile(`^v(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)\.(?:0|[1-9]\d*)-rc\.([1-9]\d*)$`)
	rcPrePattern = regexp.MustCompile(`^rc\.([1-9]\d*)$`)
)

// parseRCTag 解析合法 vX.Y.Z-rc.N 标签，并返回基线版本与 RC 数字。
func parseRCTag(tag string) (parsedVersion, int, bool) {
	m := rcTagPattern.FindStringSubmatch(strings.TrimSpace(tag))
	if m == nil {
		return parsedVersion{}, 0, false
	}
	v := parseVersion(tag)
	n, err := strconv.Atoi(m[1])
	if !v.ok || err != nil {
		return parsedVersion{}, 0, false
	}
	return v, n, true
}

// rcSeq 从预发布标识中解析 rc.N 数字。
func rcSeq(pre string) (int, bool) {
	m := rcPrePattern.FindStringSubmatch(pre)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// parseVersion 解析 "v1.2.3" / "1.2.3" / "1.2.3-dev.abc" 为结构；无法解析时 ok=false。
func parseVersion(s string) parsedVersion {
	v := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if v == "" {
		return parsedVersion{}
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return parsedVersion{}
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return parsedVersion{}
		}
		nums[i] = n
	}
	return parsedVersion{nums[0], nums[1], nums[2], pre, true}
}

// baselineCmp 比较两版本的 MAJOR.MINOR.PATCH 基线（忽略预发布标识）：
// a 高于 b 返 1、低于返 -1、相等返 0。
func baselineCmp(a, b parsedVersion) int {
	switch {
	case a.major != b.major:
		if a.major > b.major {
			return 1
		}
		return -1
	case a.minor != b.minor:
		if a.minor > b.minor {
			return 1
		}
		return -1
	case a.patch != b.patch:
		if a.patch > b.patch {
			return 1
		}
		return -1
	default:
		return 0
	}
}

// isNewer 判断 latest 是否比 current 更新（值得更新）。
// 规则：① 主/次/修订按数值比较；② 同基线下「正式版」优于「预发布」；
// ③ 同基线两个预发布标识不同则视为有更新（通用预发布比较）；
// ④ 任一无法解析时，按字符串非空且不等保守判定为有更新。
func isNewer(latest, current string) bool {
	l := parseVersion(latest)
	c := parseVersion(current)
	if !l.ok || !c.ok {
		lt := strings.TrimSpace(latest)
		return lt != "" && lt != strings.TrimSpace(current)
	}
	switch {
	case l.major != c.major:
		return l.major > c.major
	case l.minor != c.minor:
		return l.minor > c.minor
	case l.patch != c.patch:
		return l.patch > c.patch
	}
	// 基线相同：比较预发布标识
	switch {
	case l.pre == "" && c.pre != "":
		return true // 正式版优于同基线预发布
	case l.pre != "" && c.pre == "":
		return false // 预发布不覆盖同基线正式版
	default:
		return l.pre != c.pre // 两个预发布：标识不同即视为有更新
	}
}
