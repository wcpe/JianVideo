package library

import (
	"reflect"
	"testing"

	"jianvideo/internal/db/models"
)

// TestParseSearchExpression 表达式解析为结构化筛选（FR-35）。
func TestParseSearchExpression(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want MediaFilter
	}{
		{"纯裸词", "海报", MediaFilter{Terms: []string{"海报"}}},
		{"多裸词 AND", "夏天 海边", MediaFilter{Terms: []string{"夏天", "海边"}}},
		{"扩展名单个", "ext:jpg", MediaFilter{Formats: []string{"jpg"}}},
		{"扩展名逗号列表去点", "ext:.jpg,PNG", MediaFilter{Formats: []string{"jpg", "png"}}},
		{"类型 image", "type:image", MediaFilter{MediaType: MediaTypeImage}},
		{"类型别名 vid", "type:vid", MediaFilter{MediaType: MediaTypeVideo}},
		{"大小 大于 mb", "size:>10mb", MediaFilter{SizeMin: 10*(1<<20) + 1}},
		{"大小 大于等于 kb", "size:>=500kb", MediaFilter{SizeMin: 500 * (1 << 10)}},
		{"大小 小于等于 gb", "size:<=2gb", MediaFilter{SizeMax: 2 * (1 << 30)}},
		{"大小 小于 无单位", "size:<1024", MediaFilter{SizeMax: 1023}},
		{"组合", "ext:jpg type:image size:>1mb 风景", MediaFilter{
			Formats: []string{"jpg"}, MediaType: MediaTypeImage, SizeMin: 1*(1<<20) + 1, Terms: []string{"风景"},
		}},
		{"无效大小忽略", "size:abc 海", MediaFilter{Terms: []string{"海"}}},
		{"无效类型忽略", "type:bogus 海", MediaFilter{Terms: []string{"海"}}},
		{"未知 key 退化为裸词", "foo:bar", MediaFilter{Terms: []string{"foo:bar"}}},
		{"空表达式", "   ", MediaFilter{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseSearchExpression(c.expr)
			if !reflect.DeepEqual(got.Formats, c.want.Formats) {
				t.Errorf("Formats: 期望 %v, 实际 %v", c.want.Formats, got.Formats)
			}
			if got.MediaType != c.want.MediaType {
				t.Errorf("MediaType: 期望 %q, 实际 %q", c.want.MediaType, got.MediaType)
			}
			if got.SizeMin != c.want.SizeMin {
				t.Errorf("SizeMin: 期望 %d, 实际 %d", c.want.SizeMin, got.SizeMin)
			}
			if got.SizeMax != c.want.SizeMax {
				t.Errorf("SizeMax: 期望 %d, 实际 %d", c.want.SizeMax, got.SizeMax)
			}
			if !reflect.DeepEqual(got.Terms, c.want.Terms) {
				t.Errorf("Terms: 期望 %v, 实际 %v", c.want.Terms, got.Terms)
			}
		})
	}
}

// TestParseSizeValue 大小数值+单位解析。
func TestParseSizeValue(t *testing.T) {
	cases := []struct {
		in    string
		want  int64
		valid bool
	}{
		{"1024", 1024, true},
		{"10mb", 10 * (1 << 20), true},
		{"500kb", 500 * (1 << 10), true},
		{"2gb", 2 * (1 << 30), true},
		{"1.5mb", int64(1.5 * float64(1<<20)), true},
		{"", 0, false},
		{"abc", 0, false},
		{"10zz", 0, false},
	}
	for _, c := range cases {
		got, ok := parseSizeValue(c.in)
		if ok != c.valid || (ok && got != c.want) {
			t.Errorf("parseSizeValue(%q) = (%d,%v), 期望 (%d,%v)", c.in, got, ok, c.want, c.valid)
		}
	}
}

// TestListMediaFilesFiltered_FR35 结构化筛选（类型/扩展名/大小/目录/关键词）落到参数化查询。
func TestListMediaFilesFiltered_FR35(t *testing.T) {
	svc, _ := newTagTestService(t)
	// 不同类型/大小/目录/文件名
	if _, err := svc.CreateMediaFile(1, "D:/pics/海报.jpg", 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMediaFile(1, "D:/pics/截图.png", 5*(1<<20)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMediaFile(1, "D:/movies/电影.mp4", 20*(1<<20)); err != nil {
		t.Fatal(err)
	}

	count := func(f MediaFilter) int {
		items, total, err := svc.ListMediaFilesFiltered(f, 1, 100)
		if err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if int64(len(items)) != total {
			t.Fatalf("items 与 total 不一致: %d vs %d", len(items), total)
		}
		return len(items)
	}

	if n := count(MediaFilter{MediaType: MediaTypeImage}); n != 2 {
		t.Errorf("type=image 期望 2, 实际 %d", n)
	}
	if n := count(MediaFilter{MediaType: MediaTypeVideo}); n != 1 {
		t.Errorf("type=video 期望 1, 实际 %d", n)
	}
	if n := count(MediaFilter{Formats: []string{"jpg"}}); n != 1 {
		t.Errorf("ext=jpg 期望 1, 实际 %d", n)
	}
	if n := count(MediaFilter{SizeMin: 1 << 20}); n != 2 {
		t.Errorf("size>=1mb 期望 2, 实际 %d", n)
	}
	if n := count(MediaFilter{SizeMax: 2000}); n != 1 {
		t.Errorf("size<=2000 期望 1, 实际 %d", n)
	}
	if n := count(MediaFilter{PathPrefix: "D:/pics/"}); n != 2 {
		t.Errorf("path=D:/pics/ 期望 2, 实际 %d", n)
	}
	if n := count(MediaFilter{Terms: []string{"电影"}}); n != 1 {
		t.Errorf("term=电影 期望 1, 实际 %d", n)
	}
	// 组合：图片 + >=1mb → 仅 png
	if n := count(MediaFilter{MediaType: MediaTypeImage, SizeMin: 1 << 20}); n != 1 {
		t.Errorf("image+>=1mb 期望 1, 实际 %d", n)
	}
}

// TestListMediaFilesFiltered_HasGPS 仅返回带 GPS 坐标的媒体（FR-39）。
func TestListMediaFilesFiltered_HasGPS(t *testing.T) {
	svc, gdb := newTagTestService(t)
	withGPS, err := svc.CreateMediaFile(1, "D:/pics/有定位.jpg", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMediaFile(1, "D:/pics/无定位.jpg", 1000); err != nil {
		t.Fatal(err)
	}
	// 给其中一条置 GPS
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", withGPS.ID).
		Updates(map[string]any{"gps_lat": 31.23, "gps_lon": 121.47}).Error; err != nil {
		t.Fatalf("置 GPS 失败: %v", err)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{HasGPS: true}, 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != withGPS.ID {
		t.Fatalf("has_gps 期望仅 1 条带定位媒体, 实际 total=%d items=%d", total, len(items))
	}
}

// TestBuiltInImageExtensionList 返回内置图片扩展名（含常见与 RAW），有序。
func TestBuiltInImageExtensionList(t *testing.T) {
	list := builtInImageExtensionList()
	set := make(map[string]bool, len(list))
	for _, e := range list {
		set[e] = true
	}
	for _, want := range []string{"jpg", "png", "heic", "dng"} {
		if !set[want] {
			t.Errorf("内置图片扩展名应含 %q", want)
		}
	}
	// 不应含视频扩展名
	if set["mp4"] || set["mkv"] {
		t.Error("内置图片扩展名不应含视频后缀")
	}
}
