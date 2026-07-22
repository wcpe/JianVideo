package transcoder

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 预设校验错误（供 api 层映射为 400/404）。
var (
	// ErrPresetNameEmpty 预设名为空。
	ErrPresetNameEmpty = errors.New("预设名不能为空")
	// ErrPresetCodecInvalid 预设编码不是受支持的目标编码。
	ErrPresetCodecInvalid = errors.New("不支持的目标编码")
	// ErrPresetDimensionNegative 宽/高为负。
	ErrPresetDimensionNegative = errors.New("分辨率不能为负")
	// ErrPresetNotFound 预设不存在。
	ErrPresetNotFound = errors.New("预设不存在")
)

// PresetStore 转码预设的持久化读写（FR-77）。
// 仅负责预设 CRUD 与编码白名单校验，不承载队列/转码职责（职责单一）。
type PresetStore struct {
	db *gorm.DB
}

// NewPresetStore 创建预设存储。
func NewPresetStore(db *gorm.DB) *PresetStore {
	return &PresetStore{db: db}
}

// validatePreset 校验预设字段（纯逻辑，无副作用，便于穷举单测）。
// 编码须为管线可参数化输出的目标编码（复用 CodecOutputParams 白名单）；宽高非负。
func validatePreset(name, codec string, width, height int) (normCodec string, err error) {
	if strings.TrimSpace(name) == "" {
		return "", ErrPresetNameEmpty
	}
	c := normalizeCodec(codec)
	if _, ok := CodecOutputParams(c); !ok {
		return "", fmt.Errorf("%w: %q", ErrPresetCodecInvalid, codec)
	}
	if width < 0 || height < 0 {
		return "", ErrPresetDimensionNegative
	}
	return c, nil
}

// List 列出全部预设，按创建时间倒序（最近在前）。
func (s *PresetStore) List() ([]models.TranscodePreset, error) {
	var presets []models.TranscodePreset
	if err := s.db.Order("created_at DESC, id DESC").Find(&presets).Error; err != nil {
		return nil, err
	}
	return presets, nil
}

// Get 按 ID 取预设；不存在返回 ErrPresetNotFound。
func (s *PresetStore) Get(id int64) (*models.TranscodePreset, error) {
	var preset models.TranscodePreset
	if err := s.db.First(&preset, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPresetNotFound
		}
		return nil, err
	}
	return &preset, nil
}

// Create 校验后创建预设。
func (s *PresetStore) Create(name, codec string, width, height int) (*models.TranscodePreset, error) {
	normCodec, err := validatePreset(name, codec, width, height)
	if err != nil {
		return nil, err
	}
	preset := &models.TranscodePreset{
		Name:   strings.TrimSpace(name),
		Codec:  normCodec,
		Width:  width,
		Height: height,
	}
	if err := s.db.Create(preset).Error; err != nil {
		return nil, err
	}
	return preset, nil
}

// Update 校验后更新预设；不存在返回 ErrPresetNotFound。
func (s *PresetStore) Update(id int64, name, codec string, width, height int) (*models.TranscodePreset, error) {
	normCodec, err := validatePreset(name, codec, width, height)
	if err != nil {
		return nil, err
	}
	preset, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	preset.Name = strings.TrimSpace(name)
	preset.Codec = normCodec
	preset.Width = width
	preset.Height = height
	if err := s.db.Save(preset).Error; err != nil {
		return nil, err
	}
	return preset, nil
}

// Delete 删除预设；不存在返回 ErrPresetNotFound。
func (s *PresetStore) Delete(id int64) error {
	res := s.db.Delete(&models.TranscodePreset{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPresetNotFound
	}
	return nil
}
