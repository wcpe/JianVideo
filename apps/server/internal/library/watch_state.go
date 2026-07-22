package library

import (
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// continueWatchingMaxLimit 继续观看列表的最大返回条数，防止异常 limit 拉全表。
const continueWatchingMaxLimit = 50

// UpdateWatchPosition 持久化媒体的上次播放位置（秒）并刷新最近观看时间（FR-44）。
// position 为负时归零；媒体不存在返回 gorm.ErrRecordNotFound。
// 仅更新续播位置，不改动 watched 标记。
func (s *Service) UpdateWatchPosition(id int64, position float64) (*models.MediaFile, error) {
	return s.UpdateWatchPositionInSpace(models.DefaultSpaceID, id, position)
}

// UpdateWatchPositionInSpace 通过统一观看状态服务持久化指定 Space 媒体的上次播放位置。
func (s *Service) UpdateWatchPositionInSpace(spaceID string, id int64, position float64) (*models.MediaFile, error) {
	if position < 0 {
		position = 0
	}
	if err := s.applyLegacyWatchEvent(spaceID, id, position, "legacy-position", WatchEventProgress); err != nil {
		return nil, err
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

// MarkWatched 标记媒体已看完（FR-44）：置 watched=true 并清零续播位置（已看完不再续播），
// 同时刷新最近观看时间、观看次数 +1（FR-75，看完计一次）。媒体不存在返回 gorm.ErrRecordNotFound。
// 计数与状态更新在同一次 UPDATE 内原子完成；位置上报（UpdateWatchPosition）不计数，避免重复累加。
func (s *Service) MarkWatched(id int64) (*models.MediaFile, error) {
	return s.MarkWatchedInSpace(models.DefaultSpaceID, id)
}

// MarkWatchedInSpace 通过统一观看状态服务标记指定 Space 媒体已看完。
func (s *Service) MarkWatchedInSpace(spaceID string, id int64) (*models.MediaFile, error) {
	if err := s.applyLegacyWatchEvent(spaceID, id, 0, "legacy-watched", WatchEventEnded); err != nil {
		return nil, err
	}
	return s.GetMediaFileByIDInSpace(spaceID, id)
}

// ListOnThisDay 查询「往年同一天」拍摄的媒体（FR-72「那年今日」），供首页回忆区块展示。
// 命中条件：media_time 非空、未软删，且其月-日等于服务器本地「今天」的月-日、年份不等于今年。
// 「今天」按服务器本地时间算，避免时区/客户端分歧；不回退 added_at（避免混入入库时间噪声）。
// 结果按 media_time 倒序（最近年份在前），limit 小于 1 时回退默认值、超上限收敛到上限。
func (s *Service) ListOnThisDay(limit int) ([]models.MediaFile, error) {
	return s.ListOnThisDayInSpace(models.DefaultSpaceID, limit)
}

// ListOnThisDayInSpace 查询指定 Space 的「往年同一天」媒体。
func (s *Service) ListOnThisDayInSpace(spaceID string, limit int) ([]models.MediaFile, error) {
	if limit < 1 {
		limit = 12
	}
	if limit > continueWatchingMaxLimit {
		limit = continueWatchingMaxLimit
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	monthDay := now.Format("01-02") // 今天的月-日（本地时区）
	year := now.Format("2006")      // 今年（本地时区）
	return s.watchRepo.ListOnThisDay(spaceID, monthDay, year, limit)
}

// ListContinueWatching 查询「有进度且未看完」的媒体（FR-44），按最近观看时间倒序，
// 供首页「继续观看」区块展示。排除已删除（deleted_at 非空）记录。
// limit 小于 1 时回退默认值，超过上限时收敛到上限。
func (s *Service) ListContinueWatching(limit int) ([]models.MediaFile, error) {
	return s.ListContinueWatchingInSpace(models.DefaultSpaceID, limit)
}

// ListContinueWatchingInSpace 从 watch_states 真源查询指定 Space 的继续观看列表。
func (s *Service) ListContinueWatchingInSpace(spaceID string, limit int) ([]models.MediaFile, error) {
	items, err := s.ListContinueWatchingStatesInSpace(spaceID, limit)
	if err != nil {
		return nil, err
	}
	media := make([]models.MediaFile, 0, len(items))
	for _, item := range items {
		media = append(media, item.Media)
	}
	return media, nil
}
