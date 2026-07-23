package models

import "strings"

// 内容分级枚举（FR2-051 / ADR 对齐家庭场景首批）。
const (
	ContentRatingG       = "G"
	ContentRatingPG      = "PG"
	ContentRatingPG13    = "PG-13"
	ContentRatingR       = "R"
	ContentRatingUnrated = "UNRATED"
)

// ContentRatingRank 返回分级权重；数字越大越受限。UNRATED/空 为 0（默认对受限用户可见）。
func ContentRatingRank(rating string) int {
	switch strings.ToUpper(strings.TrimSpace(rating)) {
	case ContentRatingG:
		return 1
	case ContentRatingPG:
		return 2
	case ContentRatingPG13, "PG13":
		return 3
	case ContentRatingR:
		return 4
	default:
		// UNRATED 与空：视为未分级，默认可见（rank 0 ≤ 任何 max）。
		return 0
	}
}

// NormalizeContentRating 规范化分级字符串；非法返回空（调用方拒收）。
func NormalizeContentRating(rating string) string {
	r := strings.ToUpper(strings.TrimSpace(rating))
	switch r {
	case ContentRatingG, ContentRatingPG, ContentRatingPG13, ContentRatingR, ContentRatingUnrated, "":
		return r
	case "PG13":
		return ContentRatingPG13
	default:
		return ""
	}
}

// ValidContentRating 是否合法分级（含空表示清除/未设）。
func ValidContentRating(rating string) bool {
	if strings.TrimSpace(rating) == "" {
		return true
	}
	n := NormalizeContentRating(rating)
	return n == ContentRatingG || n == ContentRatingPG || n == ContentRatingPG13 ||
		n == ContentRatingR || n == ContentRatingUnrated
}

// ContentRatingsAtMost 返回 rank ≤ max 的分级列表（不含 UNRATED；UNRATED 由查询侧单独放行）。
func ContentRatingsAtMost(maxRating string) []string {
	maxRank := ContentRatingRank(maxRating)
	all := []string{ContentRatingG, ContentRatingPG, ContentRatingPG13, ContentRatingR}
	out := make([]string, 0, len(all))
	for _, r := range all {
		if ContentRatingRank(r) <= maxRank {
			out = append(out, r)
		}
	}
	return out
}

// ContentVisible 判断 mediaRating 对 maxRating 是否可见。
// maxRating 空表示无上限（全可见）；media 空/UNRATED 默认可见。
func ContentVisible(mediaRating, maxRating string) bool {
	limit := strings.TrimSpace(maxRating)
	if limit == "" {
		return true
	}
	return ContentRatingRank(mediaRating) <= ContentRatingRank(limit)
}
