package library

import (
	"errors"
	"strings"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// ErrInvalidLibraryKind 表示请求的媒体库分型不在内置分型目录内。
var ErrInvalidLibraryKind = errors.New("媒体库分型不支持")

// KindInfo 描述内置媒体库分型，供 API 与前端展示。
type KindInfo struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	NamingHint   string `json:"naming_hint"`
	ScanStrategy string `json:"scan_strategy"`
}

// ScanContext 是扫描链路向后续元数据与推断能力传递的库上下文。
type ScanContext struct {
	SpaceID     string
	LibraryID   int64
	LibraryKind string
}

// Kinds 返回内置分型目录。
func Kinds() []KindInfo {
	items := []KindInfo{
		{
			Kind:         models.LibraryKindMovie,
			Name:         "电影",
			Description:  "面向电影与长片，后续用于标题与年份解析。",
			NamingHint:   "片名 (年份)/片名.ext",
			ScanStrategy: "按文件与上级目录识别单片资源",
		},
		{
			Kind:         models.LibraryKindSeries,
			Name:         "剧集",
			Description:  "面向电视剧、番剧与课程，后续用于季集入口。",
			NamingHint:   "剧名/Season 01/剧名 S01E01.ext",
			ScanStrategy: "保留季集解析上下文",
		},
		{
			Kind:         models.LibraryKindHomeVideo,
			Name:         "家庭录像",
			Description:  "面向家庭影像、相机视频和生活记录。",
			NamingHint:   "日期_地点_事件.ext",
			ScanStrategy: "优先保留拍摄时间与原始文件名",
		},
		{
			Kind:         models.LibraryKindMixed,
			Name:         "混合",
			Description:  "兼容旧库与混合内容，不套用专门影视规则。",
			NamingHint:   "保持现有目录与文件名",
			ScanStrategy: "使用通用扫描策略",
		},
	}
	return append([]KindInfo(nil), items...)
}

func normalizeLibraryKind(raw string) (string, error) {
	kind := strings.TrimSpace(raw)
	if kind == "" {
		return models.LibraryKindMixed, nil
	}
	switch kind {
	case models.LibraryKindMovie, models.LibraryKindSeries, models.LibraryKindHomeVideo, models.LibraryKindMixed:
		return kind, nil
	default:
		return "", ErrInvalidLibraryKind
	}
}
