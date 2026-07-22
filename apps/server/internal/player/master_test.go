package player

import (
	"strconv"
	"strings"
	"testing"
)

// TestGenerateMasterM3U8 验证 master playlist 生成。
func TestGenerateMasterM3U8(t *testing.T) {
	qualities := []QualityInfo{
		{Name: "1080p", Width: 1920, Height: 1080, Bandwidth: 5000000},
		{Name: "720p", Width: 1280, Height: 720, Bandwidth: 2500000},
		{Name: "480p", Width: 854, Height: 480, Bandwidth: 1000000},
	}

	content := GenerateMasterM3U8(qualities)

	// 验证头部
	if !strings.Contains(content, "#EXTM3U") {
		t.Fatal("master.m3u8 应包含 #EXTM3U")
	}

	// 验证每个码率都有 EXT-X-STREAM-INF
	for _, q := range qualities {
		expected := "#EXT-X-STREAM-INF:BANDWIDTH=" + strconv.Itoa(q.Bandwidth) +
			",RESOLUTION=" + strconv.Itoa(q.Width) + "x" + strconv.Itoa(q.Height)
		if !strings.Contains(content, expected) {
			t.Fatalf("应包含 %s, 实际:\n%s", expected, content)
		}
		// 验证对应的 m3u8 引用
		ref := q.Name + ".m3u8"
		if !strings.Contains(content, ref) {
			t.Fatalf("应包含 %s 引用, 实际:\n%s", ref, content)
		}
	}

	// 验证顺序：高码率在前
	idx1080 := strings.Index(content, "1080p")
	idx720 := strings.Index(content, "720p")
	idx480 := strings.Index(content, "480p")
	if idx1080 >= idx720 || idx720 >= idx480 {
		t.Fatal("码率应按从高到低排序")
	}
}

// TestGenerateMasterM3U8_SingleQuality 验证单码率 master playlist。
func TestGenerateMasterM3U8_SingleQuality(t *testing.T) {
	qualities := []QualityInfo{
		{Name: "480p", Width: 854, Height: 480, Bandwidth: 1000000},
	}

	content := GenerateMasterM3U8(qualities)

	if !strings.Contains(content, "#EXT-X-STREAM-INF:BANDWIDTH=1000000,RESOLUTION=854x480") {
		t.Fatalf("单码率 master 内容异常: %s", content)
	}
	if !strings.Contains(content, "480p.m3u8") {
		t.Fatal("应包含 480p.m3u8 引用")
	}
}
