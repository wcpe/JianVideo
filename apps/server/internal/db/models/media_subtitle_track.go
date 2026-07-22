package models

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	// MediaSubtitleSourceEmbedded 表示容器内嵌字幕流。
	MediaSubtitleSourceEmbedded = "embedded"
	// MediaSubtitleSourceSidecar 表示媒体目录中的只读外挂字幕。
	MediaSubtitleSourceSidecar = "sidecar"
	// MediaSubtitleSourceUploaded 表示用户上传到应用数据目录的字幕。
	MediaSubtitleSourceUploaded = "uploaded"
)

// MediaSubtitleTrack 保存字幕轨道的持久化索引。
type MediaSubtitleTrack struct {
	ID                  string    `gorm:"primaryKey;size:64" json:"id"`
	SpaceID             string    `gorm:"not null;size:128;index:idx_media_subtitle_tracks_space_media,priority:1" json:"space_id"`
	MediaID             int64     `gorm:"not null;index:idx_media_subtitle_tracks_space_media,priority:2" json:"media_id"`
	Source              string    `gorm:"not null;size:16;index:idx_media_subtitle_tracks_source" json:"source"`
	SourceRef           string    `gorm:"not null;default:''" json:"source_ref"`
	StorageRelativePath string    `gorm:"not null;default:''" json:"storage_relative_path"`
	StreamIndex         int       `gorm:"not null;default:-1" json:"stream_index"`
	Format              string    `gorm:"not null;size:16" json:"format"`
	Language            string    `gorm:"not null;default:''" json:"language"`
	Title               string    `gorm:"not null;default:''" json:"title"`
	IsDefault           bool      `gorm:"not null;default:false" json:"is_default"`
	IsForced            bool      `gorm:"not null;default:false" json:"is_forced"`
	CreatedAt           time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt           time.Time `gorm:"not null" json:"updated_at"`
}

// BeforeSave 在写入前规范化来源引用并校验来源专属字段。
func (track *MediaSubtitleTrack) BeforeSave(_ *gorm.DB) error {
	track.ID = strings.TrimSpace(track.ID)
	track.SpaceID = strings.TrimSpace(track.SpaceID)
	track.Source = strings.ToLower(strings.TrimSpace(track.Source))
	track.Format = strings.ToLower(strings.TrimSpace(track.Format))
	if track.ID == "" || track.SpaceID == "" || track.MediaID <= 0 || !safeSubtitleToken(track.Format) {
		return errors.New("字幕轨道基础字段无效")
	}
	return track.validateSource()
}

func (track *MediaSubtitleTrack) validateSource() error {
	switch track.Source {
	case MediaSubtitleSourceEmbedded:
		return track.validateEmbedded()
	case MediaSubtitleSourceSidecar:
		return track.validateSidecar()
	case MediaSubtitleSourceUploaded:
		return track.validateUploaded()
	default:
		return fmt.Errorf("字幕轨道来源无效: %s", track.Source)
	}
}

func (track *MediaSubtitleTrack) validateEmbedded() error {
	if track.StreamIndex < 0 || track.SourceRef != "" || track.StorageRelativePath != "" {
		return errors.New("内嵌字幕轨道字段无效")
	}
	return nil
}

func (track *MediaSubtitleTrack) validateSidecar() error {
	track.SourceRef = normalizeSubtitleSourceRef(track.SourceRef)
	if track.SourceRef == "" || track.StreamIndex != -1 || track.StorageRelativePath != "" {
		return errors.New("外挂字幕轨道字段无效")
	}
	return nil
}

func (track *MediaSubtitleTrack) validateUploaded() error {
	if track.StreamIndex != -1 || !safeSubtitleToken(track.ID) {
		return errors.New("上传字幕轨道字段无效")
	}
	if filepath.Base(track.SourceRef) != track.SourceRef || strings.Contains(track.SourceRef, "..") {
		return errors.New("上传字幕来源文件名无效")
	}
	expected := filepath.ToSlash(filepath.Join(
		"subtitles", track.SpaceID, strconv.FormatInt(track.MediaID, 10), track.ID+"."+track.Format,
	))
	if track.StorageRelativePath != expected {
		return errors.New("上传字幕存储路径无效")
	}
	track.SourceRef = filepath.Base(strings.TrimSpace(track.SourceRef))
	return nil
}

func normalizeSubtitleSourceRef(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	return strings.ToLower(value)
}

func safeSubtitleToken(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}
