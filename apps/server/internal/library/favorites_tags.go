package library

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// InferenceStatusInferred 等常量表示媒体推断状态筛选值。
const (
	InferenceStatusInferred = "inferred"
	InferenceStatusAuto     = "auto"
	InferenceStatusManual   = "manual"
	InferenceStatusMissing  = "missing"
)

// MediaFilter 表示媒体列表筛选条件（FR-41 起，FR-35 扩展结构化筛选）。
// Favorite 为 nil 表示不按收藏过滤；TagID>0 表示仅返回打了该标签的媒体。
type MediaFilter struct {
	SpaceID         string
	LibraryID       int64
	Sort            string
	Search          string
	Favorite        *bool
	TagID           int64
	InferenceStatus string

	// FR-35 结构化筛选（零值表示不约束，全部走参数化查询，无 SQL 注入面）
	MediaType           string     // "image" / "video" / ""(不限)
	Formats             []string   // 按扩展名过滤（小写，不含点），如 [jpg png]
	MediaTypeExtensions []string   // 已解析的类型后缀集合，由 Service 根据媒体类型规则填充
	SizeMin             int64      // 文件大小下界（字节，含）；>0 生效
	SizeMax             int64      // 文件大小上界（字节，含）；>0 生效
	TimeFrom            *time.Time // 媒体时间下界（含）
	TimeTo              *time.Time // 媒体时间上界（含）
	PathPrefix          string     // 目录前缀过滤（file_path LIKE prefix%）
	Terms               []string   // 裸词关键词（多词 AND），FR-136 起跨文件名/显示名/相机/镜头/备注列匹配
	HasGPS              bool       // 仅返回带 GPS 坐标的媒体（FR-39 照片地图）

	// FR-136 EXIF 专项关键词（多词 AND，全参数化）
	CameraTerms []string // camera: token，仅约束 camera 列
	LensTerms   []string // lens: token，仅约束 lens 列

	// FR2-046 时长/分辨率筛选（秒与像素；0 表示不约束）
	DurationMin float64 // 时长下界（含）
	DurationMax float64 // 时长上界（含）
	WidthMin    int     // 宽度下界（含）
	WidthMax    int     // 宽度上界（含）
	HeightMin   int     // 高度下界（含）
	HeightMax   int     // 高度上界（含）

	// MaxContentRating 调用者最高可见分级（FR2-051）；空表示不限制。
	// 过滤：content_rating 为空/UNRATED 或 rank(content_rating) <= rank(MaxContentRating)。
	MaxContentRating string
}

// searchableColumns 是裸词关键词的可搜列集合（FR-136/FR-137）。
// 统一在此声明，避免多处硬编码列名导致搜索口径漂移。
var searchableColumns = []string{"file_name", "display_name", "camera", "lens", "notes"}

// escapeLike 转义 LIKE 通配符，防止用户输入 % 或 _ 干扰查询。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "%", "\\%")
	return strings.ReplaceAll(s, "_", "\\_")
}

// ListMediaFilesFiltered 按筛选条件分页查询媒体文件列表（FR-41）。
// 在原有 library_id/search/排序之上，新增收藏与标签过滤。
func (s *Service) ListMediaFilesFiltered(filter MediaFilter, page, pageSize int) ([]models.MediaFile, int64, error) {
	result, err := s.ListMediaFilesPage(filter, MediaPageRequest{Page: page, PageSize: pageSize})
	if err != nil {
		return nil, 0, err
	}
	return result.Items, result.Total, nil
}

// SetMediaFavorite 设置或取消媒体收藏标记（FR-41）。重复设同值幂等。
func (s *Service) SetMediaFavorite(id int64, favorite bool) (*models.MediaFile, error) {
	return s.SetMediaFavoriteInSpace(models.DefaultSpaceID, id, favorite)
}

// SetMediaFavoriteInSpace 设置指定 Space 媒体的收藏标记（FR-41）。
func (s *Service) SetMediaFavoriteInSpace(spaceID string, id int64, favorite bool) (*models.MediaFile, error) {
	spaceID = normalizeSpaceID(spaceID)
	rows, err := s.tagRepo.UpdateFavorite(spaceID, id, favorite)
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		// 区分「媒体不存在」与「值未变化」：再查一次确认存在性
		count, err := s.tagRepo.CountMedia(spaceID, id)
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

// CreateTag 创建标签（FR-41）。名按去首尾空白规整，非空即可；同名复用已有标签。
func (s *Service) CreateTag(name string) (*models.Tag, error) {
	return s.CreateTagInSpace(models.DefaultSpaceID, name)
}

// CreateTagInSpace 创建指定 Space 的标签（FR-41）。
func (s *Service) CreateTagInSpace(spaceID, name string) (*models.Tag, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("标签名不能为空")
	}
	tag := models.Tag{SpaceID: normalizeSpaceID(spaceID), Name: name}
	if err := s.tagRepo.FirstOrCreateTag(&tag); err != nil {
		return nil, err
	}
	return &tag, nil
}

// ListTags 列出全部标签，按名升序（FR-41）。
func (s *Service) ListTags() ([]models.Tag, error) {
	return s.ListTagsInSpace(models.DefaultSpaceID)
}

// ListTagsInSpace 列出指定 Space 的全部标签，按名升序（FR-41）。
func (s *Service) ListTagsInSpace(spaceID string) ([]models.Tag, error) {
	return s.tagRepo.ListTags(spaceID)
}

// ListMediaTags 列出指定媒体的标签，按名升序（FR-41）。
func (s *Service) ListMediaTags(mediaID int64) ([]models.Tag, error) {
	return s.ListMediaTagsInSpace(models.DefaultSpaceID, mediaID)
}

// ListMediaTagsInSpace 列出指定 Space 媒体的标签，按名升序（FR-41）。
func (s *Service) ListMediaTagsInSpace(spaceID string, mediaID int64) ([]models.Tag, error) {
	if _, err := s.GetMediaFileByIDInSpace(spaceID, mediaID); err != nil {
		return nil, err
	}
	return s.tagRepo.ListMediaTags(mediaID)
}

// AddMediaTag 给媒体打标签（FR-41）。媒体或标签不存在则报错；重复打同标签幂等。
func (s *Service) AddMediaTag(mediaID, tagID int64) error {
	return s.AddMediaTagInSpace(models.DefaultSpaceID, mediaID, tagID)
}

// AddMediaTagInSpace 给指定 Space 的媒体打标签（FR-41）。
func (s *Service) AddMediaTagInSpace(spaceID string, mediaID, tagID int64) error {
	if err := s.ensureMediaExistsInSpace(spaceID, mediaID); err != nil {
		return err
	}
	if err := s.ensureTagExists(tagID); err != nil {
		return err
	}
	mapping := models.TagMapping{TagID: tagID, MediaID: mediaID}
	// (tag_id, media_id) 唯一索引保证去重；已存在则不再插入
	return s.tagRepo.FirstOrCreateMapping(&mapping)
}

// RemoveMediaTag 解除媒体与标签的绑定（FR-41）。绑定不存在视为幂等成功。
func (s *Service) RemoveMediaTag(mediaID, tagID int64) error {
	return s.RemoveMediaTagInSpace(models.DefaultSpaceID, mediaID, tagID)
}

// RemoveMediaTagInSpace 解除指定 Space 媒体与标签的绑定（FR-41）。
func (s *Service) RemoveMediaTagInSpace(spaceID string, mediaID, tagID int64) error {
	if err := s.ensureMediaExistsInSpace(spaceID, mediaID); err != nil {
		return err
	}
	return s.tagRepo.DeleteMapping(mediaID, tagID)
}

func (s *Service) ensureMediaExistsInSpace(spaceID string, mediaID int64) error {
	count, err := s.tagRepo.CountMedia(spaceID, mediaID)
	if err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ensureTagExists 校验标签存在。
func (s *Service) ensureTagExists(tagID int64) error {
	count, err := s.tagRepo.CountTag(tagID)
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("标签不存在")
	}
	return nil
}
