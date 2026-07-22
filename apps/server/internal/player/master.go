package player

import (
	"fmt"
	"strings"
)

// ExtractQualityFromSegment 从切片文件名提取码率档位。
// 格式: {quality}_segment_xxx.ts，如 "1080p_segment_000.ts"。
func ExtractQualityFromSegment(segment string) string {
	parts := strings.SplitN(segment, "_segment_", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// QualityInfo 描述一个码率档位的信息，用于生成 master.m3u8。
type QualityInfo struct {
	Name      string
	Width     int
	Height    int
	Bandwidth int // 比特率（bps）
}

// GenerateMasterM3U8 生成 HLS master playlist 内容。
// qualities 按从高到低排序。
func GenerateMasterM3U8(qualities []QualityInfo) string {
	var sb strings.Builder

	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")

	for _, q := range qualities {
		fmt.Fprintf(&sb,
			"#EXT-X-STREAM-INF:BANDWIDTH=%d,RESOLUTION=%dx%d\n",
			q.Bandwidth, q.Width, q.Height,
		)
		fmt.Fprintf(&sb, "%s.m3u8\n", q.Name)
	}

	return sb.String()
}
