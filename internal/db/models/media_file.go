package models

import "time"

const (
	// MediaFileStateAvailable 表示源文件当前可访问。
	MediaFileStateAvailable = "available"
	// MediaFileStateMissing 表示源文件在扫描或监听中被判定丢失。
	MediaFileStateMissing = "missing"
)

// MediaFile 媒体文件记录。
type MediaFile struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	SpaceID        string    `gorm:"not null;default:space-default;index:idx_media_files_space_id;index:idx_media_files_space_size_content_hash,priority:1;index:idx_media_files_space_content_hash_stale,priority:1" json:"space_id"`
	LibraryID      int64     `gorm:"index;not null" json:"library_id"`
	FilePath       string    `gorm:"not null;index:idx_media_files_file_path" json:"file_path"`
	FileName       string    `gorm:"index;not null" json:"file_name"`
	FileSize       int64     `gorm:"default:0;index:idx_media_files_space_size_content_hash,priority:2" json:"file_size"`
	Format         string    `json:"format"`
	VideoCodec     string    `json:"video_codec"`
	AudioCodec     string    `json:"audio_codec"`
	Duration       float64   `gorm:"default:0" json:"duration"`
	Width          int       `gorm:"default:0" json:"width"`
	Height         int       `gorm:"default:0" json:"height"`
	Bitrate        int       `gorm:"default:0" json:"bitrate"`
	SubtitleTracks string    `json:"subtitle_tracks"`
	AddedAt        time.Time `json:"added_at"`
	ModifiedAt     time.Time `json:"modified_at"`

	// 显示名（FR-30）：仅库内展示用，空则回退 FileName，不影响磁盘真实文件名
	DisplayName string `json:"display_name"`

	// 软删除/回收站（FR-25）：非空表示已软删（进回收站），源文件不动
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// 文件状态（FR2-027）：missing 表示源文件丢失但尚未进入回收站。
	FileState string `gorm:"not null;default:'available';index" json:"file_state"`

	// 媒体时间与 EXIF（FR-31）
	MediaTime       *time.Time `gorm:"index" json:"media_time,omitempty"` // 多层降级解析出的媒体时间，供时间轴排序
	MediaTimeSource string     `json:"media_time_source"`                 // exif / filename / created / modified
	Camera          string     `json:"camera"`
	Lens            string     `json:"lens"`
	Aperture        string     `json:"aperture"`
	Shutter         string     `json:"shutter"`
	ISO             int        `gorm:"default:0" json:"iso"`
	GPSLat          float64    `gorm:"default:0" json:"gps_lat"`
	GPSLon          float64    `gorm:"default:0" json:"gps_lon"`

	// 逆地理编码地名（FR-147，见 ADR-0050）：由 GPS 经纬度离线解析的粗粒度「省·市」可读地名，
	// 空表示无 GPS 或未解析。供后续聚合 / 筛选展示（前端展示见 FR-146），不影响磁盘文件。
	Location string `json:"location"`

	// 收藏（FR-41）
	Favorite bool `gorm:"default:false" json:"favorite"`

	// 备注（FR-137）：用户对媒体的自由文本备注，仅库内存储，纳入基础搜索；空表示无备注
	Notes string `json:"notes"`

	// 感知哈希去重（FR-70）：基于缩略图计算的 64 位 dHash，0 表示尚未计算。
	// 汉明距离 ≤ 阈值的媒体视为近似重复，供「重复项」页聚类清理。
	// 显式列名 dhash，与去重服务的手写 SQL 条件保持一致。
	DHash int64 `gorm:"column:dhash;default:0" json:"dhash,omitempty"`

	// 内容哈希去重（FR2-061）：源文件 SHA-256。stale=true 表示文件大小或 mtime 已变化，需回填重算。
	ContentHash           string     `gorm:"size:64;default:'';index:idx_media_files_space_size_content_hash,priority:3;index:idx_media_files_space_content_hash_stale,priority:2" json:"content_hash,omitempty"`
	ContentHashAlgo       string     `gorm:"default:''" json:"content_hash_algo,omitempty"`
	ContentHashComputedAt *time.Time `json:"content_hash_computed_at,omitempty"`
	ContentHashStale      bool       `gorm:"not null;default:true;index:idx_media_files_space_content_hash_stale,priority:3" json:"content_hash_stale"`

	// 观看状态/续播（FR-44）
	LastPosition  float64    `gorm:"default:0" json:"last_position"` // 上次播放位置（秒）
	Watched       bool       `gorm:"default:false" json:"watched"`
	LastWatchedAt *time.Time `json:"last_watched_at,omitempty"`

	// 最近查看（FR-120）：媒体被打开（详情面板/播放页）时置为当前时间，供时间轴「最近查看」回忆区块倒序展示。
	// 与 LastWatchedAt 语义不同：后者仅视频播放进度，前者覆盖图片+视频的「打开」动作。
	LastViewedAt *time.Time `gorm:"index" json:"last_viewed_at,omitempty"`

	// 观看次数（FR-75）：每「看完」一次 +1（由 MarkWatched 自增），位置上报不计数。
	// 供观看统计页的「观看次数 Top N」与各维度聚合，默认 0。
	ViewCount int `gorm:"default:0" json:"view_count"`
}
