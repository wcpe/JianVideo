package library

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianvideo/internal/db/models"
)

// TestParseFilenameDate 表驱动校验文件名日期解析。
func TestParseFilenameDate(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    time.Time
		wantOK  bool
		comment string
	}{
		{
			name:    "IMG 带时分秒",
			input:   "IMG_20230101_120000.jpg",
			want:    time.Date(2023, 1, 1, 12, 0, 0, 0, time.Local),
			wantOK:  true,
			comment: "相机常见命名",
		},
		{
			name:   "VID 带时分秒",
			input:  "VID_20230815_093045.mp4",
			want:   time.Date(2023, 8, 15, 9, 30, 45, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "纯日期 8 位",
			input:  "IMG_20221231.png",
			want:   time.Date(2022, 12, 31, 0, 0, 0, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "横杠日期",
			input:  "2023-01-01.jpg",
			want:   time.Date(2023, 1, 1, 0, 0, 0, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "横杠日期带时间",
			input:  "2023-06-21 15-30-00.jpg",
			want:   time.Date(2023, 6, 21, 15, 30, 0, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "Screenshot 下划线分隔",
			input:  "Screenshot_2023-03-15-08-09-10.png",
			want:   time.Date(2023, 3, 15, 8, 9, 10, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "微信图片紧凑时间戳",
			input:  "mmexport20230520183000.jpg",
			want:   time.Date(2023, 5, 20, 18, 30, 0, 0, time.Local),
			wantOK: true,
		},
		{
			name:   "无日期",
			input:  "photo.jpg",
			wantOK: false,
		},
		{
			name:   "非法月份不匹配",
			input:  "IMG_20231301_120000.jpg",
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseFilenameDate(c.input)
			if ok != c.wantOK {
				t.Fatalf("ok 期望 %v, 实际 %v", c.wantOK, ok)
			}
			if !c.wantOK {
				return
			}
			if !got.Equal(c.want) {
				t.Fatalf("时间期望 %v, 实际 %v", c.want, got)
			}
		})
	}
}

// TestResolveMediaTime 表驱动校验媒体时间多层降级选择。
func TestResolveMediaTime(t *testing.T) {
	exifT := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	fnT := time.Date(2021, 2, 2, 0, 0, 0, 0, time.Local)
	created := time.Date(2022, 3, 3, 0, 0, 0, 0, time.Local)
	modified := time.Date(2023, 4, 4, 0, 0, 0, 0, time.Local)

	cases := []struct {
		name       string
		exif       *time.Time
		filename   *time.Time
		created    time.Time
		modified   time.Time
		wantTime   time.Time
		wantSource string
	}{
		{
			name:       "exif 优先",
			exif:       &exifT,
			filename:   &fnT,
			created:    created,
			modified:   modified,
			wantTime:   exifT,
			wantSource: MediaTimeSourceEXIF,
		},
		{
			name:       "无 exif 降级文件名",
			exif:       nil,
			filename:   &fnT,
			created:    created,
			modified:   modified,
			wantTime:   fnT,
			wantSource: MediaTimeSourceFilename,
		},
		{
			name:       "无 exif 无文件名降级创建时间",
			exif:       nil,
			filename:   nil,
			created:    created,
			modified:   modified,
			wantTime:   created,
			wantSource: MediaTimeSourceCreated,
		},
		{
			name:       "仅有修改时间",
			exif:       nil,
			filename:   nil,
			created:    time.Time{},
			modified:   modified,
			wantTime:   modified,
			wantSource: MediaTimeSourceModified,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, source := ResolveMediaTime(c.exif, c.filename, c.created, c.modified)
			if source != c.wantSource {
				t.Fatalf("来源期望 %s, 实际 %s", c.wantSource, source)
			}
			if !got.Equal(c.wantTime) {
				t.Fatalf("时间期望 %v, 实际 %v", c.wantTime, got)
			}
		})
	}
}

// imagemeta 自带的样片相对路径，用于真实 EXIF 解析回归。
// assets/a1.jpg 为佳能样片（含拍摄时间/相机/镜头/光圈/快门/ISO），a2.jpg 无 EXIF。
const (
	sampleWithEXIF = "assets/a1.jpg"
	sampleNoEXIF   = "assets/a2.jpg"
)

// imagemetaModuleDir 借助 go list 解析 imagemeta 模块目录，找不到则返回空串。
func imagemetaModuleDir(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/evanoberholster/imagemeta").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// resolveSample 解析 imagemeta 模块内样片绝对路径，缺失时跳过测试。
func resolveSample(t *testing.T, rel string) string {
	t.Helper()
	modDir := imagemetaModuleDir(t)
	if modDir == "" {
		t.Skip("无法定位 imagemeta 模块目录，跳过真实 EXIF 回归")
	}
	sample := filepath.Join(modDir, filepath.FromSlash(rel))
	if _, err := os.Stat(sample); err != nil {
		t.Skipf("imagemeta 样片不存在（%s），跳过", rel)
	}
	return sample
}

// TestExtractImageEXIF_RealSample 用佳能样片验证真实 EXIF 提取与字段映射。
func TestExtractImageEXIF_RealSample(t *testing.T) {
	sample := resolveSample(t, sampleWithEXIF)

	exif := ExtractImageEXIF(sample)
	if exif == nil {
		t.Fatal("应解析出 EXIF")
	}
	if !strings.Contains(exif.Camera, "Canon") {
		t.Fatalf("相机应含 Canon, 实际 %q", exif.Camera)
	}
	if !strings.Contains(exif.Lens, "18-55") {
		t.Fatalf("镜头应含 18-55, 实际 %q", exif.Lens)
	}
	if exif.ISO != 400 {
		t.Fatalf("ISO 期望 400, 实际 %d", exif.ISO)
	}
	if exif.Aperture != "f/5" {
		t.Fatalf("光圈期望 f/5, 实际 %q", exif.Aperture)
	}
	if exif.Shutter != "1/60" {
		t.Fatalf("快门期望 1/60, 实际 %q", exif.Shutter)
	}
	if exif.Taken.IsZero() {
		t.Fatal("拍摄时间不应为零")
	}
	if exif.Taken.Year() != 2013 {
		t.Fatalf("拍摄年份期望 2013, 实际 %d", exif.Taken.Year())
	}
}

// TestExtractImageEXIF_NoExif 无 EXIF 图片不应解析出拍摄时间与相机（交降级链兜底）。
func TestExtractImageEXIF_NoExif(t *testing.T) {
	sample := resolveSample(t, sampleNoEXIF)
	if exif := ExtractImageEXIF(sample); exif != nil {
		if !exif.Taken.IsZero() || exif.Camera != "" {
			t.Fatalf("无 EXIF 图片不应解析出拍摄时间/相机: %+v", exif)
		}
	}
}

// TestCreateMediaFile_MediaTimeFromFilename 校验入库时按文件名解析出 media_time。
// 无 EXIF 的普通图片，文件名含日期时来源应为 filename。
func TestCreateMediaFile_MediaTimeFromFilename(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	// 写一个无 EXIF 的占位图片文件
	path := filepath.Join(dir, "IMG_20230101_120000.jpg")
	if err := os.WriteFile(path, []byte("not-a-real-jpeg"), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	mf, err := svc.CreateMediaFile(1, path, 16)
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if mf.MediaTime == nil {
		t.Fatal("media_time 不应为空")
	}
	if mf.MediaTimeSource != MediaTimeSourceFilename {
		t.Fatalf("来源期望 %s, 实际 %s", MediaTimeSourceFilename, mf.MediaTimeSource)
	}
	want := time.Date(2023, 1, 1, 12, 0, 0, 0, time.Local)
	if !mf.MediaTime.Equal(want) {
		t.Fatalf("media_time 期望 %v, 实际 %v", want, *mf.MediaTime)
	}
}

// TestCreateMediaFile_MediaTimeFallbackModified 校验既无 EXIF 又无文件名日期时，
// media_time 落到文件修改/创建时间，来源为 created 或 modified。
func TestCreateMediaFile_MediaTimeFallbackModified(t *testing.T) {
	svc, _ := newTestService(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "plain-photo.jpg")
	if err := os.WriteFile(path, []byte("not-a-real-jpeg"), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	mf, err := svc.CreateMediaFile(1, path, 16)
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	if mf.MediaTime == nil {
		t.Fatal("media_time 不应为空")
	}
	if mf.MediaTimeSource != MediaTimeSourceCreated && mf.MediaTimeSource != MediaTimeSourceModified {
		t.Fatalf("来源期望 created 或 modified, 实际 %s", mf.MediaTimeSource)
	}
}

// TestListMediaFiles_SortByMediaTime 校验 media_time 排序：
// 有 media_time 的按其排，缺失项回退 added_at 参与排序。
func TestListMediaFiles_SortByMediaTime(t *testing.T) {
	svc, gdb := newTestService(t)

	older := time.Date(2020, 1, 1, 0, 0, 0, 0, time.Local)
	newer := time.Date(2024, 1, 1, 0, 0, 0, 0, time.Local)

	// A：有较早 media_time
	a := &models.MediaFile{LibraryID: 1, FilePath: "/m/a.jpg", FileName: "a.jpg", AddedAt: time.Now(), MediaTime: &older}
	// B：有较新 media_time
	b := &models.MediaFile{LibraryID: 1, FilePath: "/m/b.jpg", FileName: "b.jpg", AddedAt: time.Now(), MediaTime: &newer}
	// C：无 media_time，靠 added_at 回退（设为最新入库）
	c := &models.MediaFile{LibraryID: 1, FilePath: "/m/c.jpg", FileName: "c.jpg", AddedAt: time.Now().Add(time.Hour)}
	for _, m := range []*models.MediaFile{a, b, c} {
		if err := gdb.Create(m).Error; err != nil {
			t.Fatalf("写测试记录失败: %v", err)
		}
	}

	// 降序：C（added_at 最新）> B（2024）> A（2020）
	items, _, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1, Sort: "media_time"}, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("期望 3 条, 实际 %d", len(items))
	}
	gotOrder := []string{items[0].FileName, items[1].FileName, items[2].FileName}
	wantOrder := []string{"c.jpg", "b.jpg", "a.jpg"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("media_time 降序期望 %v, 实际 %v", wantOrder, gotOrder)
		}
	}

	// 升序：A（2020）> B（2024）> C（added_at 最新）
	itemsAsc, _, err := svc.ListMediaFilesFiltered(MediaFilter{LibraryID: 1, Sort: "media_time_asc"}, 1, 20)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	gotAsc := []string{itemsAsc[0].FileName, itemsAsc[1].FileName, itemsAsc[2].FileName}
	wantAsc := []string{"a.jpg", "b.jpg", "c.jpg"}
	for i := range wantAsc {
		if gotAsc[i] != wantAsc[i] {
			t.Fatalf("media_time 升序期望 %v, 实际 %v", wantAsc, gotAsc)
		}
	}
}
