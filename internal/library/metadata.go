package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanoberholster/imagemeta"

	"jianvideo/internal/db/models"
)

// 媒体时间来源标识（写入 media_files.media_time_source）。
const (
	MediaTimeSourceEXIF     = "exif"     // 来自图片 EXIF 或视频 creation_time
	MediaTimeSourceFilename = "filename" // 来自文件名日期解析
	MediaTimeSourceCreated  = "created"  // 来自文件创建时间
	MediaTimeSourceModified = "modified" // 来自文件修改时间
)

// filenameDatePattern 文件名日期解析模式：正则 + 对应的 time 解析布局。
// 按 layout 的占位顺序提取捕获组重组为标准串再解析，避免各模式分隔符差异。
type filenameDatePattern struct {
	re     *regexp.Regexp
	layout string
}

// 有序匹配的文件名日期模式，从最具体（带时分秒）到最宽松（仅日期）。
// 捕获组统一为 年 月 日 [时 分 秒]，命中后拼为 "2006-01-02 15:04:05" 解析。
var filenameDatePatterns = []filenameDatePattern{
	// 紧凑 14 位：mmexport20230520183000 / IMG_20230101120000
	{re: regexp.MustCompile(`(\d{4})(\d{2})(\d{2})[_-]?(\d{2})(\d{2})(\d{2})`), layout: "ymdhms"},
	// 分隔日期 + 分隔时间：2023-06-21 15-30-00 / 2023_06_21_15_30_00
	{re: regexp.MustCompile(`(\d{4})[-_](\d{2})[-_](\d{2})[ _-](\d{2})[-_:](\d{2})[-_:](\d{2})`), layout: "ymdhms"},
	// 紧凑 8 位日期：IMG_20221231
	{re: regexp.MustCompile(`(\d{4})(\d{2})(\d{2})`), layout: "ymd"},
	// 分隔 8 位日期：2023-01-01 / 2023_01_01
	{re: regexp.MustCompile(`(\d{4})[-_](\d{2})[-_](\d{2})`), layout: "ymd"},
}

// ParseFilenameDate 从文件名解析媒体日期时间。
// 依次尝试常见相机 / 截图 / 导出命名模式，命中且日期合法时返回本地时区时间与 true；
// 否则返回零值与 false。纯函数，无 IO。
func ParseFilenameDate(name string) (time.Time, bool) {
	base := filepath.Base(name)
	for _, p := range filenameDatePatterns {
		m := p.re.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		t, ok := buildTimeFromGroups(m, p.layout)
		if ok {
			return t, true
		}
	}
	return time.Time{}, false
}

// buildTimeFromGroups 根据布局类型把正则捕获组拼成标准串再严格解析。
// 严格解析（非宽松 Parse）保证非法月份 / 日期（如 13 月）被拒绝。
func buildTimeFromGroups(groups []string, layout string) (time.Time, bool) {
	switch layout {
	case "ymd":
		s := fmt.Sprintf("%s-%s-%s", groups[1], groups[2], groups[3])
		t, err := time.ParseInLocation("2006-01-02", s, time.Local)
		return t, err == nil
	case "ymdhms":
		s := fmt.Sprintf("%s-%s-%s %s:%s:%s", groups[1], groups[2], groups[3], groups[4], groups[5], groups[6])
		t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
		return t, err == nil
	default:
		return time.Time{}, false
	}
}

// ResolveMediaTime 按多层降级链选出媒体时间与来源标识。
// 优先级：EXIF / 媒体拍摄时间 → 文件名日期 → 文件创建时间 → 文件修改时间。
// 前两个为指针，nil 或零值表示该层无数据；创建时间零值（部分平台无 birthtime）跳过。
// 纯函数，无 IO。
func ResolveMediaTime(exifTime, filenameTime *time.Time, createdAt, modifiedAt time.Time) (time.Time, string) {
	if exifTime != nil && !exifTime.IsZero() {
		return *exifTime, MediaTimeSourceEXIF
	}
	if filenameTime != nil && !filenameTime.IsZero() {
		return *filenameTime, MediaTimeSourceFilename
	}
	if !createdAt.IsZero() {
		return createdAt, MediaTimeSourceCreated
	}
	return modifiedAt, MediaTimeSourceModified
}

// enrichMediaMetadata 为媒体记录填充媒体时间与 EXIF 明细。
// 仅对本地文件生效：图片提取 EXIF，视频读 creation_time，再按降级链定媒体时间。
// 远程（smb://）或文件不可访问时按修改时间兜底，不报错。
func enrichMediaMetadata(mf *models.MediaFile) {
	// 远程路径不做本地 stat / EXIF 提取，仅以入库时已设的 ModifiedAt 兜底
	if strings.HasPrefix(mf.FilePath, "smb://") {
		mf.MediaTime = &mf.ModifiedAt
		mf.MediaTimeSource = MediaTimeSourceModified
		return
	}

	diskPath := filepath.FromSlash(mf.FilePath)
	info, err := os.Stat(diskPath)
	modified := mf.ModifiedAt
	var created time.Time
	if err == nil {
		modified = info.ModTime()
		created = fileCreatedTime(info)
	}

	// 提取拍摄时间与 EXIF 明细：图片走 imagemeta，视频走 ffprobe
	var exifTime *time.Time
	ext := normalizeExtension(filepath.Ext(diskPath))
	switch builtInMediaExtensions[ext] {
	case MediaTypeImage:
		if exif := ExtractImageEXIF(diskPath); exif != nil {
			applyImageEXIF(mf, exif)
			if !exif.Taken.IsZero() {
				t := exif.Taken
				exifTime = &t
			}
		}
	case MediaTypeVideo:
		if t := ProbeVideoCreationTime(diskPath); !t.IsZero() {
			exifTime = &t
		}
	}

	// 文件名日期解析
	var filenameTime *time.Time
	if t, ok := ParseFilenameDate(mf.FileName); ok {
		filenameTime = &t
	}

	mediaTime, source := ResolveMediaTime(exifTime, filenameTime, created, modified)
	mf.MediaTime = &mediaTime
	mf.MediaTimeSource = source
}

// applyImageEXIF 把图片 EXIF 明细写入媒体记录字段。
func applyImageEXIF(mf *models.MediaFile, exif *ImageEXIF) {
	mf.Camera = exif.Camera
	mf.Lens = exif.Lens
	mf.Aperture = exif.Aperture
	mf.Shutter = exif.Shutter
	mf.ISO = exif.ISO
	mf.GPSLat = exif.GPSLat
	mf.GPSLon = exif.GPSLon
}

// ImageEXIF 图片 EXIF 提取结果，字段缺失时为零值。
type ImageEXIF struct {
	Taken    time.Time // 拍摄时间，零值表示未提取到
	Camera   string
	Lens     string
	Aperture string
	Shutter  string
	ISO      int
	GPSLat   float64
	GPSLon   float64
}

// ExtractImageEXIF 从图片文件提取 EXIF 信息。
// 解析失败、文件无 EXIF 或不支持时返回 nil（不视为错误，交由降级链兜底）。
func ExtractImageEXIF(path string) *ImageEXIF {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("[DEBUG] 打开图片读取 EXIF 失败: %s, err=%v", path, err)
		return nil
	}
	defer f.Close()

	ex, err := imagemeta.Decode(f)
	if err != nil {
		// 多数无 EXIF 的图片在此返回错误，按降级链处理，仅 DEBUG 记录
		log.Printf("[DEBUG] 解析图片 EXIF 失败: %s, err=%v", path, err)
		return nil
	}

	result := &ImageEXIF{
		Taken:    ex.SelectedDate(),
		Camera:   joinCameraName(ex.CameraMake(), ex.IFD0.Model),
		Lens:     strings.TrimSpace(ex.ExifIFD.LensModel),
		ISO:      int(ex.ExifIFD.ISOSpeedRatings),
		GPSLat:   ex.GPS.Latitude(),
		GPSLon:   ex.GPS.Longitude(),
		Aperture: apertureString(float64(ex.ExifIFD.FNumber)),
		Shutter:  ex.ExifIFD.ExposureTime.String(),
	}
	return result
}

// joinCameraName 拼接相机厂商与型号，去重前缀（型号常已含厂商名）。
func joinCameraName(make, model string) string {
	make = strings.TrimSpace(make)
	model = strings.TrimSpace(model)
	switch {
	case make == "" && model == "":
		return ""
	case make == "":
		return model
	case model == "":
		return make
	}
	// 型号已含厂商前缀时不重复拼接，例如 "Canon" + "Canon EOS R5"
	if strings.HasPrefix(strings.ToLower(model), strings.ToLower(make)) {
		return model
	}
	return make + " " + model
}

// apertureString 把光圈数值格式化为 "f/2.8"，零或负值返回空串。
func apertureString(f float64) string {
	if f <= 0 {
		return ""
	}
	return "f/" + strconv.FormatFloat(f, 'f', -1, 64)
}

// ffprobePath ffprobe 可执行文件路径，由 main.go 经 SetFFprobePath 注入。
// 默认 "ffprobe"（PATH 查找）。本包直接调用以读取视频 creation_time，
// 不导入 transcoder 包以保持模块依赖方向（transcoder → library）。
var (
	ffprobePathMu sync.RWMutex
	ffprobePath   = "ffprobe"
)

// SetFFprobePath 注入 ffprobe 可执行文件路径，供视频媒体时间提取使用。
// 由 main.go 与 transcoder 的解析结果保持一致。空值忽略。
func SetFFprobePath(path string) {
	if path == "" {
		return
	}
	ffprobePathMu.Lock()
	ffprobePath = path
	ffprobePathMu.Unlock()
}

func getFFprobePath() string {
	ffprobePathMu.RLock()
	defer ffprobePathMu.RUnlock()
	return ffprobePath
}

// ffprobeFormatTags ffprobe -show_format 输出中的 format.tags 子集。
type ffprobeFormatTags struct {
	Format struct {
		Tags struct {
			CreationTime string `json:"creation_time"`
		} `json:"tags"`
	} `json:"format"`
}

// videoCreationTimeLayouts ffprobe creation_time 常见时间格式。
var videoCreationTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000000Z",
	"2006-01-02 15:04:05",
}

// ProbeVideoCreationTime 用 ffprobe 读取视频容器的 creation_time。
// 解析失败、ffprobe 不可用或无该标签时返回零值（交降级链兜底，不报错）。
func ProbeVideoCreationTime(path string) time.Time {
	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		path,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	output, err := exec.CommandContext(ctx, getFFprobePath(), args...).Output()
	if err != nil {
		log.Printf("[DEBUG] ffprobe 读取视频时间失败: %s, err=%v", path, err)
		return time.Time{}
	}

	var probe ffprobeFormatTags
	if err := json.Unmarshal(output, &probe); err != nil {
		log.Printf("[DEBUG] 解析 ffprobe 输出失败: %s, err=%v", path, err)
		return time.Time{}
	}

	raw := strings.TrimSpace(probe.Format.Tags.CreationTime)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range videoCreationTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
