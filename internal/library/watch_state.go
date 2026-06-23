package library

import (
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// continueWatchingMaxLimit 继续观看列表的最大返回条数，防止异常 limit 拉全表。
const continueWatchingMaxLimit = 50

// UpdateWatchPosition 持久化媒体的上次播放位置（秒）并刷新最近观看时间（FR-44）。
// position 为负时归零；媒体不存在返回 gorm.ErrRecordNotFound。
// 仅更新续播位置，不改动 watched 标记。
func (s *Service) UpdateWatchPosition(id int64, position float64) (*models.MediaFile, error) {
	if position < 0 {
		position = 0
	}
	now := time.Now()
	result := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Updates(map[string]any{
		"last_position":   position,
		"last_watched_at": now,
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return s.GetMediaFileByID(id)
}

// MarkWatched 标记媒体已看完（FR-44）：置 watched=true 并清零续播位置（已看完不再续播），
// 同时刷新最近观看时间、观看次数 +1（FR-75，看完计一次）。媒体不存在返回 gorm.ErrRecordNotFound。
// 计数与状态更新在同一次 UPDATE 内原子完成；位置上报（UpdateWatchPosition）不计数，避免重复累加。
func (s *Service) MarkWatched(id int64) (*models.MediaFile, error) {
	now := time.Now()
	result := s.db.Model(&models.MediaFile{}).Where("id = ?", id).Updates(map[string]any{
		"watched":         true,
		"last_position":   0,
		"last_watched_at": now,
		"view_count":      gorm.Expr("view_count + 1"),
	})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return s.GetMediaFileByID(id)
}

// ListOnThisDay 查询「往年同一天」拍摄的媒体（FR-72「那年今日」），供首页回忆区块展示。
// 命中条件：media_time 非空、未软删，且其月-日等于服务器本地「今天」的月-日、年份不等于今年。
// 「今天」按服务器本地时间算，避免时区/客户端分歧；不回退 added_at（避免混入入库时间噪声）。
// 结果按 media_time 倒序（最近年份在前），limit 小于 1 时回退默认值、超上限收敛到上限。
func (s *Service) ListOnThisDay(limit int) ([]models.MediaFile, error) {
	if limit < 1 {
		limit = 12
	}
	if limit > continueWatchingMaxLimit {
		limit = continueWatchingMaxLimit
	}
	now := time.Now()
	monthDay := now.Format("01-02") // 今天的月-日（本地时区），对应 strftime('%m-%d', ..., 'localtime')
	year := now.Format("2006")      // 今年（本地时区），对应 strftime('%Y', ..., 'localtime')
	var items []models.MediaFile
	// media_time 以 UTC 存储、strftime 默认按 UTC 取值；加 'localtime' 修饰符转本地时区后再取月-日/年，
	// 与按本地 time.Now() 算出的「今天」口径一致（否则本地午夜后 UTC 仍是前一天，结果整体偏一天）。
	if err := s.db.
		Where("media_time IS NOT NULL AND deleted_at IS NULL AND strftime('%m-%d', media_time, 'localtime') = ? AND strftime('%Y', media_time, 'localtime') != ?", monthDay, year).
		Order("media_time DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListContinueWatching 查询「有进度且未看完」的媒体（FR-44），按最近观看时间倒序，
// 供首页「继续观看」区块展示。排除已删除（deleted_at 非空）记录。
// limit 小于 1 时回退默认值，超过上限时收敛到上限。
func (s *Service) ListContinueWatching(limit int) ([]models.MediaFile, error) {
	if limit < 1 {
		limit = 12
	}
	if limit > continueWatchingMaxLimit {
		limit = continueWatchingMaxLimit
	}
	var items []models.MediaFile
	if err := s.db.
		Where("last_position > 0 AND watched = ? AND deleted_at IS NULL", false).
		Order("last_watched_at DESC").
		Limit(limit).
		Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
