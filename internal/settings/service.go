// Package settings 提供运行期键值设置的读写能力（FR-24）。
// 设置以 SQLite settings 表为唯一真源，供回收站、定时扫描等后续能力消费。
package settings

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 已知设置键集中定义，避免魔法字符串散落。
const (
	// KeyRecycleBinPaths 每盘符回收站路径，值为 JSON 字符串（盘符 → 路径）。
	KeyRecycleBinPaths = "recycle_bin_paths"
	// KeyScanInterval 定时扫描周期（秒），值为字符串形式的整数。
	KeyScanInterval = "scan_interval"
	// KeyUpdateChannel 自更新频道：stable=正式版（正式 release）/ prerelease=测试版（最新预发布 dev）。
	KeyUpdateChannel = "update_channel"
	// KeyTranscodeCodecPriority 转码首选目标编码优先级（FR-50），值为 JSON 数组（如 ["av1","h265","h264"]）。
	KeyTranscodeCodecPriority = "transcode_codec_priority"
	// KeyFFmpegPath ffmpeg 可执行文件路径（FR-56），非空时启动覆盖自动发现并应用到转码运行期。
	KeyFFmpegPath = "ffmpeg_path"
	// KeyFFprobePath ffprobe 可执行文件路径（FR-56），非空时启动覆盖自动发现并应用到转码运行期。
	KeyFFprobePath = "ffprobe_path"
	// KeyMagickPath ImageMagick magick 可执行文件路径（FR-63），非空时启动覆盖自动发现并应用到 HEIC/RAW 转换运行期。
	KeyMagickPath = "magick_path"
	// KeyNetworkProxy 后端出站网络代理 URL（FR-80），空=直连；保存即生效，后端外部 HTTP 出站经此代理。
	KeyNetworkProxy = "network_proxy"
	// KeyDebugLog 运行时调试日志开关（FR-110），值 "1"=开启详细日志、其余=安静；保存即生效，启动读取决定初始级别。
	KeyDebugLog = "debug_log"
)

// Service 运行期设置业务逻辑。
type Service struct {
	db *gorm.DB
}

// NewService 创建设置服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Get 按 key 读取单项设置；键不存在时返回空串且不报错。
func (s *Service) Get(key string) (string, error) {
	var setting models.Setting
	err := s.db.First(&setting, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

// ScanInterval 读取定时扫描周期（FR-28）：值为秒的整数字符串。
// 空 / 非法 / <=0 一律视为关闭定时扫描，返回 0。
func (s *Service) ScanInterval() time.Duration {
	raw, err := s.Get(KeyScanInterval)
	if err != nil {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}

// DebugLog 读取运行时调试日志开关（FR-110）：值为 "1"/"true" 视为开启，其余（含空/缺失/出错）视为关闭。
func (s *Service) DebugLog() bool {
	raw, err := s.Get(KeyDebugLog)
	if err != nil {
		return false
	}
	return ParseDebugLog(raw)
}

// ParseDebugLog 解析调试日志开关字符串：忽略首尾空白，"1"/"true" 为开启，其余为关闭。
func ParseDebugLog(raw string) bool {
	v := strings.TrimSpace(raw)
	return v == "1" || v == "true"
}

// GetAll 读取全部设置，返回 key → value 映射。
func (s *Service) GetAll() (map[string]string, error) {
	var items []models.Setting
	if err := s.db.Find(&items).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[item.Key] = item.Value
	}
	return result, nil
}

// Set 写入单项设置（upsert：存在则覆盖 value 与 updated_at）。
func (s *Service) Set(key, value string) error {
	return s.upsert(s.db, key, value)
}

// SetMany 批量写入设置，在单事务内原子完成。
func (s *Service) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		for key, value := range values {
			if err := s.upsert(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// upsert 以主键冲突更新的方式写入单条设置。
func (s *Service) upsert(tx *gorm.DB, key, value string) error {
	setting := models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error
}
