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
}
