package models

import "time"

// MediaFile 媒体文件记录。
type MediaFile struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	LibraryID      int64     `gorm:"index;not null" json:"library_id"`
	FilePath       string    `gorm:"not null;index:idx_media_files_file_path" json:"file_path"`
	FileName       string    `gorm:"index;not null" json:"file_name"`
	FileSize       int64     `gorm:"default:0" json:"file_size"`
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
	DeletedAt *time.Time `gorm:"index" json:"deleted_at,omitempty"`

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

	// 收藏（FR-41）
	Favorite bool `gorm:"default:false" json:"favorite"`

	// 观看状态/续播（FR-44）
	LastPosition  float64    `gorm:"default:0" json:"last_position"` // 上次播放位置（秒）
	Watched       bool       `gorm:"default:false" json:"watched"`
	LastWatchedAt *time.Time `json:"last_watched_at,omitempty"`
}
