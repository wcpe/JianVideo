package library

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
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
		{"相机 token", "camera:Canon", MediaFilter{CameraTerms: []string{"Canon"}}},
		{"镜头 token", "lens:50mm", MediaFilter{LensTerms: []string{"50mm"}}},
		{"相机镜头组合裸词", "camera:Sony lens:GM 风景", MediaFilter{
			CameraTerms: []string{"Sony"}, LensTerms: []string{"GM"}, Terms: []string{"风景"},
		}},
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
			if !reflect.DeepEqual(got.CameraTerms, c.want.CameraTerms) {
				t.Errorf("CameraTerms: 期望 %v, 实际 %v", c.want.CameraTerms, got.CameraTerms)
			}
			if !reflect.DeepEqual(got.LensTerms, c.want.LensTerms) {
				t.Errorf("LensTerms: 期望 %v, 实际 %v", c.want.LensTerms, got.LensTerms)
			}
		})
	}
}

// TestListMediaFilesFiltered_SearchSpansDisplayNameAndEXIF 验证裸词关键词搜索覆盖
// 文件名 / 显示名 / 相机 / 镜头（FR-136 可搜字段扩展）。
func TestListMediaFilesFiltered_SearchSpansDisplayNameAndEXIF(t *testing.T) {
	svc, gdb := newTagTestService(t)
	a, _ := svc.CreateMediaFile(1, "D:/pics/IMG_001.jpg", 1000) // 显示名命中
	b, _ := svc.CreateMediaFile(1, "D:/pics/IMG_002.jpg", 1000) // 相机命中
	c, _ := svc.CreateMediaFile(1, "D:/pics/海边.jpg", 1000)      // 文件名命中
	d, _ := svc.CreateMediaFile(1, "D:/pics/IMG_003.jpg", 1000) // 镜头命中
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", a.ID).
		Update("display_name", "毕业典礼").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", b.ID).
		Update("camera", "Canon EOS R5").Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", d.ID).
		Update("lens", "RF 50mm F1.2").Error; err != nil {
		t.Fatal(err)
	}

	ids := func(items []models.MediaFile) map[int64]bool {
		m := make(map[int64]bool, len(items))
		for _, it := range items {
			m[it.ID] = true
		}
		return m
	}

	// 显示名命中
	items, _, err := svc.ListMediaFilesFiltered(MediaFilter{Terms: []string{"毕业"}}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(items); len(got) != 1 || !got[a.ID] {
		t.Errorf("显示名搜索期望仅命中 %d, 实际 %v", a.ID, items)
	}
	// 相机命中
	items, _, _ = svc.ListMediaFilesFiltered(MediaFilter{Terms: []string{"Canon"}}, 1, 100)
	if got := ids(items); len(got) != 1 || !got[b.ID] {
		t.Errorf("相机搜索期望仅命中 %d, 实际 %v", b.ID, items)
	}
	// 文件名命中
	items, _, _ = svc.ListMediaFilesFiltered(MediaFilter{Terms: []string{"海边"}}, 1, 100)
	if got := ids(items); len(got) != 1 || !got[c.ID] {
		t.Errorf("文件名搜索期望仅命中 %d, 实际 %v", c.ID, items)
	}
	// 镜头命中
	items, _, _ = svc.ListMediaFilesFiltered(MediaFilter{Terms: []string{"50mm"}}, 1, 100)
	if got := ids(items); len(got) != 1 || !got[d.ID] {
		t.Errorf("镜头搜索期望仅命中 %d, 实际 %v", d.ID, items)
	}
	// 旧 Search 字段同样跨列
	items, _, _ = svc.ListMediaFilesFiltered(MediaFilter{Search: "毕业"}, 1, 100)
	if got := ids(items); len(got) != 1 || !got[a.ID] {
		t.Errorf("Search 字段显示名搜索期望仅命中 %d, 实际 %v", a.ID, items)
	}
}

// TestListMediaFilesFiltered_CameraLensTerms 验证 camera:/lens: 专项 token 仅约束对应 EXIF 列。
func TestListMediaFilesFiltered_CameraLensTerms(t *testing.T) {
	svc, gdb := newTagTestService(t)
	a, _ := svc.CreateMediaFile(1, "D:/pics/a.jpg", 1000)
	b, _ := svc.CreateMediaFile(1, "D:/pics/b.jpg", 1000)
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", a.ID).
		Updates(map[string]any{"camera": "Sony A7M4", "lens": "FE 35mm"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", b.ID).
		Updates(map[string]any{"camera": "Nikon Z6", "lens": "Z 24-70"}).Error; err != nil {
		t.Fatal(err)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{CameraTerms: []string{"Sony"}}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != a.ID {
		t.Errorf("camera:Sony 期望仅 %d, 实际 total=%d items=%v", a.ID, total, items)
	}
	items, total, _ = svc.ListMediaFilesFiltered(MediaFilter{LensTerms: []string{"24-70"}}, 1, 100)
	if total != 1 || len(items) != 1 || items[0].ID != b.ID {
		t.Errorf("lens:24-70 期望仅 %d, 实际 total=%d items=%v", b.ID, total, items)
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

// TestListMediaFilesFiltered_FR2046DurationResolution 时长与分辨率筛选（FR2-046）。
func TestListMediaFilesFiltered_FR2046DurationResolution(t *testing.T) {
	svc, gdb := newTagTestService(t)
	shortClip, err := svc.CreateMediaFile(1, "D:/movies/短片.mp4", 1024)
	if err != nil {
		t.Fatal(err)
	}
	feature, err := svc.CreateMediaFile(1, "D:/movies/长片.mp4", 2048)
	if err != nil {
		t.Fatal(err)
	}
	hdStill, err := svc.CreateMediaFile(1, "D:/pics/海报.jpg", 512)
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", shortClip.ID).
		Updates(map[string]any{"duration": 90.0, "width": 1280, "height": 720}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", feature.ID).
		Updates(map[string]any{"duration": 7200.0, "width": 1920, "height": 1080}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaFile{}).Where("id = ?", hdStill.ID).
		Updates(map[string]any{"duration": 0.0, "width": 3840, "height": 2160}).Error; err != nil {
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
	if n := count(MediaFilter{DurationMin: 600}); n != 1 {
		t.Errorf("duration>=600 期望 1, 实际 %d", n)
	}
	if n := count(MediaFilter{DurationMax: 120}); n != 2 {
		// 短片 90s + 图片 0s
		t.Errorf("duration<=120 期望 2, 实际 %d", n)
	}
	if n := count(MediaFilter{HeightMin: 1080}); n != 2 {
		t.Errorf("height>=1080 期望 2, 实际 %d", n)
	}
	if n := count(MediaFilter{WidthMin: 3000, HeightMin: 2000}); n != 1 {
		t.Errorf("4K 下界期望 1, 实际 %d", n)
	}
	if n := count(MediaFilter{DurationMin: 60, HeightMin: 1000}); n != 1 {
		t.Errorf("时长+分辨率组合期望 1, 实际 %d", n)
	}
}

// TestListMediaFilesFiltered_FR2046InferenceTitle 裸词可命中推断片名（FR2-046）。
func TestListMediaFilesFiltered_FR2046InferenceTitle(t *testing.T) {
	svc, gdb := newTagTestService(t)
	hit, err := svc.CreateMediaFile(1, "D:/movies/S01E01.mkv", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateMediaFile(1, "D:/movies/other.mkv", 1024); err != nil {
		t.Fatal(err)
	}
	// CreateMediaFile 可能已写入空推断行；覆盖 title 即可（唯一键 media_id）
	if err := gdb.Where(models.MediaInference{MediaID: hit.ID}).
		Assign(models.MediaInference{
			SpaceID: models.DefaultSpaceID,
			Kind:    "series",
			Title:   "绝命毒师",
			Season:  1,
			Episode: 1,
			Source:  "offline_rule",
		}).
		FirstOrCreate(&models.MediaInference{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&models.MediaInference{}).
		Where("media_id = ?", hit.ID).
		Update("title", "绝命毒师").Error; err != nil {
		t.Fatal(err)
	}

	items, total, err := svc.ListMediaFilesFiltered(MediaFilter{Terms: []string{"绝命"}}, 1, 100)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != hit.ID {
		t.Fatalf("推断标题搜索期望仅命中 %d, total=%d items=%v", hit.ID, total, items)
	}
	items, total, err = svc.ListMediaFilesFiltered(MediaFilter{Search: "绝命"}, 1, 100)
	if err != nil {
		t.Fatalf("Search 查询失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != hit.ID {
		t.Fatalf("Search 推断标题期望仅命中 %d, total=%d", hit.ID, total)
	}
}

// TestApplyMediaOrder_FR2046DurationResolution 时长与分辨率排序（FR2-046）。
func TestApplyMediaOrder_FR2046DurationResolution(t *testing.T) {
	svc, gdb := newTagTestService(t)
	a, _ := svc.CreateMediaFile(1, "D:/a.mp4", 1)
	b, _ := svc.CreateMediaFile(1, "D:/b.mp4", 1)
	c, _ := svc.CreateMediaFile(1, "D:/c.mp4", 1)
	_ = gdb.Model(&models.MediaFile{}).Where("id = ?", a.ID).Updates(map[string]any{"duration": 10.0, "width": 640, "height": 360})
	_ = gdb.Model(&models.MediaFile{}).Where("id = ?", b.ID).Updates(map[string]any{"duration": 100.0, "width": 1920, "height": 1080})
	_ = gdb.Model(&models.MediaFile{}).Where("id = ?", c.ID).Updates(map[string]any{"duration": 50.0, "width": 1280, "height": 720})

	items, _, err := svc.ListMediaFilesFiltered(MediaFilter{Sort: "duration"}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 3 || items[0].ID != b.ID {
		t.Fatalf("duration 降序期望最长片在前, 实际 %#v", items)
	}
	items, _, err = svc.ListMediaFilesFiltered(MediaFilter{Sort: "resolution"}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 3 || items[0].ID != b.ID {
		t.Fatalf("resolution 降序期望 1080p 在前, 实际 %#v", items)
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

func TestListMediaFilesPageKeysetKeepsSameTimestampBoundary(t *testing.T) {
	svc, gdb := newTestService(t)
	newer := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)
	older := newer.Add(-time.Hour)
	files := make([]models.MediaFile, 0, 7)
	for id := int64(1); id <= 7; id++ {
		addedAt := older
		if id >= 4 {
			addedAt = newer
		}
		files = append(files, models.MediaFile{
			ID:         id,
			SpaceID:    models.DefaultSpaceID,
			LibraryID:  1,
			FilePath:   fmt.Sprintf("/tmp/keyset-%d.mp4", id),
			FileName:   fmt.Sprintf("keyset-%d.mp4", id),
			Format:     "mp4",
			FileState:  models.MediaFileStateAvailable,
			AddedAt:    addedAt,
			ModifiedAt: addedAt,
		})
	}
	if err := gdb.Create(&files).Error; err != nil {
		t.Fatalf("写入游标边界测试数据失败: %v", err)
	}

	var got []int64
	cursor := ""
	for {
		result, err := svc.ListMediaFilesPage(MediaFilter{
			SpaceID: models.DefaultSpaceID,
			Sort:    "time_desc",
		}, MediaPageRequest{PageSize: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("游标分页失败: %v", err)
		}
		for _, item := range result.Items {
			got = append(got, item.ID)
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}

	want := []int64{7, 6, 5, 4, 3, 2, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("同时间戳游标分页出现重复或遗漏: got=%v want=%v", got, want)
	}
}
