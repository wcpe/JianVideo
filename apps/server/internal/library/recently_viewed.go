package library

import (
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// SetMediaViewed 记录媒体被「打开」（详情面板/播放页）的时刻（FR-120）：把 last_viewed_at 置为当前时间。
// 媒体不存在（含已软删或 missing）返回 gorm.ErrRecordNotFound。仅写入查看时间，不改动续播/观看次数等其他状态。
func (s *Service) SetMediaViewed(id int64) error {
	return s.SetMediaViewedInSpace(models.DefaultSpaceID, id)
}

// SetMediaViewedInSpace 记录指定 Space 媒体被打开的时刻。
func (s *Service) SetMediaViewedInSpace(spaceID string, id int64) error {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	rows, err := s.viewRepo.SetLastViewedAt(spaceID, id, now)
	if err != nil {
		return err
	}
	if rows == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// RecentlyViewed 查询「最近查看」的媒体（FR-120），按 last_viewed_at 倒序，供时间轴回忆区块展示。
// 命中条件：last_viewed_at 非空、未软删且 active，不论进度与类型（图片+视频）。
// limit 小于 1 时回退默认 12，超过上限时收敛到上限（复用继续观看的最大条数上限）。
func (s *Service) RecentlyViewed(limit int) ([]models.MediaFile, error) {
	return s.RecentlyViewedInSpace(models.DefaultSpaceID, limit)
}

// RecentlyViewedInSpace 查询指定 Space 的最近查看媒体。
func (s *Service) RecentlyViewedInSpace(spaceID string, limit int) ([]models.MediaFile, error) {
	if limit < 1 {
		limit = 12
	}
	if limit > continueWatchingMaxLimit {
		limit = continueWatchingMaxLimit
	}
	return s.viewRepo.ListRecentlyViewed(spaceID, limit)
}
