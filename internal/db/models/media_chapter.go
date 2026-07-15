package models

import "time"

const MediaChapterSourceEmbedded = "embedded"

// MediaChapter 保存媒体文件内嵌章节的当前规范化结果。
type MediaChapter struct {
	ID                string    `gorm:"primaryKey;size:64" json:"id"`
	SpaceID           string    `gorm:"not null;uniqueIndex:idx_media_chapters_space_media_source_index,priority:1;index:idx_media_chapters_space_media_start,priority:1" json:"-"`
	MediaID           int64     `gorm:"not null;uniqueIndex:idx_media_chapters_space_media_source_index,priority:2;index:idx_media_chapters_space_media_start,priority:2" json:"-"`
	Source            string    `gorm:"not null;uniqueIndex:idx_media_chapters_space_media_source_index,priority:3" json:"source"`
	SourceIndex       int       `gorm:"not null;uniqueIndex:idx_media_chapters_space_media_source_index,priority:4;index:idx_media_chapters_space_media_start,priority:4" json:"source_index"`
	StartMS           int64     `gorm:"not null;check:start_ms >= 0;index:idx_media_chapters_space_media_start,priority:3" json:"start_ms"`
	EndMS             int64     `gorm:"not null;check:end_ms > start_ms" json:"end_ms"`
	Title             string    `gorm:"not null;size:512" json:"title"`
	Language          string    `gorm:"size:64" json:"language,omitempty"`
	SourceFingerprint string    `gorm:"not null;size:64" json:"-"`
	ParsedAt          time.Time `gorm:"not null" json:"-"`
	CreatedAt         time.Time `gorm:"not null" json:"-"`
	UpdatedAt         time.Time `gorm:"not null" json:"-"`
}
