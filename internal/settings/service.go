// Package settings 提供运行期键值设置的读写能力（FR-24）。
// 设置以 SQLite settings 表为唯一真源，供回收站、定时扫描等后续能力消费。
package settings

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wcpe/JianVideo/internal/audit"
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
	// KeyUploadTargetDir Web 上传默认落盘目录（FR-149，见 ADR-0051），须为已注册本地库目录或其子目录；空=上传时必须显式指定。
	KeyUploadTargetDir = "upload_target_dir"
	// KeyUploadNamingRule Web 上传命名规则（FR-149）：original=保留原样、date=按日期 YYYY/MM 整齐归档；空/非法回退 original。
	KeyUploadNamingRule = "upload_naming_rule"
	// KeyOpenTabs 目录浏览打开标签持久化快照（FR-151），值为 JSON 数组。
	KeyOpenTabs = "open_tabs"
	// KeyLastOpenedPath 目录浏览上次打开位置（FR-151），值为路径字符串。
	KeyLastOpenedPath = "last_opened_path"
	// KeyTaskWorkerScanConcurrency 扫描任务 worker 并发上限（FR2-037），默认 1。
	KeyTaskWorkerScanConcurrency = "task_worker_scan_concurrency"
	// KeyTaskWorkerTranscodeConcurrency 转码任务 worker 并发上限（FR2-037），默认 1。
	KeyTaskWorkerTranscodeConcurrency = "task_worker_transcode_concurrency"
	// KeyTaskWorkerThumbnailConcurrency 缩略图任务 worker 并发上限（FR2-037），默认 4。
	KeyTaskWorkerThumbnailConcurrency = "task_worker_thumbnail_concurrency"
	// KeyTaskWorkerLightConcurrency 轻量后台任务 worker 并发上限（FR2-037），默认 2。
	KeyTaskWorkerLightConcurrency = "task_worker_light_concurrency"
	// KeyMediaInferenceEnabled 本地离线影视信息推断全局开关（FR2-031），"1"/"true" 为开启。
	KeyMediaInferenceEnabled = "media_inference_enabled"
	// KeyMediaInferenceDisabledLibraries 关闭影视推断的媒体库 ID JSON 数组（FR2-031）。
	KeyMediaInferenceDisabledLibraries = "media_inference_disabled_libraries"
)

// Service 运行期设置业务逻辑。
type Service struct {
	db    *gorm.DB
	audit audit.Recorder
}

// NewService 创建设置服务。
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// WithAudit 注入审计记录器，使设置变更与审计事件同事务提交。
func (s *Service) WithAudit(rec audit.Recorder) *Service {
	s.audit = rec
	return s
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

// GetAll 读取已登记运行期设置，返回 key → 公开展示值映射。
func (s *Service) GetAll() (map[string]string, error) {
	var items []models.Setting
	if err := s.db.Find(&items).Error; err != nil {
		return nil, err
	}
	raw := make(map[string]string, len(items))
	for _, item := range items {
		raw[item.Key] = item.Value
	}
	defs := Definitions()
	result := make(map[string]string, len(defs))
	for _, def := range defs {
		if def.Layer != LayerRuntime {
			continue
		}
		value, ok := raw[def.Key]
		if !ok {
			value = def.DefaultValue
		}
		result[def.Key] = publicValue(def, value)
	}
	return result, nil
}

// Set 写入单项设置（upsert：存在则覆盖 value 与 updated_at）。
func (s *Service) Set(key, value string) error {
	if err := validateWritable(key, value); err != nil {
		return err
	}
	return s.upsert(s.db, key, value)
}

// SetMany 批量写入设置，在单事务内原子完成。
func (s *Service) SetMany(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	for key, value := range values {
		if err := validateWritable(key, value); err != nil {
			return err
		}
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		before, err := s.getValuesTx(tx, values)
		if err != nil {
			return err
		}
		for key, value := range values {
			if err := s.upsert(tx, key, value); err != nil {
				return err
			}
		}
		if s.audit != nil {
			if err := s.audit.RecordTx(context.Background(), tx, audit.EventInput{
				Scope:        audit.ScopeSystem,
				ActorType:    audit.ActorSystem,
				Action:       "settings.updated",
				ResourceType: "settings",
				Before:       redactedValues(before),
				After:        redactedValues(values),
			}); err != nil {
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

func (s *Service) getValuesTx(tx *gorm.DB, values map[string]string) (map[string]string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	var items []models.Setting
	if err := tx.Where("key IN ?", keys).Find(&items).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(items))
	for _, item := range items {
		result[item.Key] = item.Value
	}
	return result, nil
}

func redactedValues(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		def, ok := definitionFor(key)
		if ok && def.Sensitive {
			result[key] = publicValue(def, value)
			continue
		}
		result[key] = value
	}
	return result
}
