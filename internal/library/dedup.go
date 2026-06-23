package library

import (
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 感知哈希去重服务（FR-70）：为媒体计算 dHash 并按汉明距离聚类近似重复组。
// dHash 落 media_files.dhash 列，重复组查询统一基于未软删媒体。

// DedupThreshold 返回去重默认汉明距离阈值，供上层端点查询重复组时使用，
// 避免 api 层硬编码魔法值。
func (s *Service) DedupThreshold() int {
	return dedupHammingThreshold
}

// ComputeMissingDHashes 为全部「未软删且 dhash=0」的媒体计算并持久化 dHash。
// 缩略图缺失时先同步生成一次再计算；单条失败仅记 WARN 跳过、不中断整体。
// 有界并发（复用缩略图并发上限语义），返回本次成功计算的条数。已算过的天然跳过（幂等）。
func (s *Service) ComputeMissingDHashes() (int, error) {
	var pending []models.MediaFile
	if err := s.db.
		Where("deleted_at IS NULL AND dhash = 0").
		Order("id ASC").
		Find(&pending).Error; err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}

	sem := make(chan struct{}, thumbnailConcurrency())
	var wg sync.WaitGroup
	var computed int64
	for _, mf := range pending {
		wg.Add(1)
		sem <- struct{}{}
		go func(mf models.MediaFile) {
			defer wg.Done()
			defer func() { <-sem }()

			hash, ok := s.computeDHashForMedia(mf.FilePath)
			if !ok {
				return
			}
			// 仅在仍为 0 时写入，避免与并发计算相互覆盖（幂等）
			if err := s.db.Model(&models.MediaFile{}).
				Where("id = ? AND dhash = 0", mf.ID).
				Update("dhash", int64(hash)).Error; err != nil {
				log.Printf("[WARN] 去重：写入 dHash 失败: id=%d, err=%v", mf.ID, err)
				return
			}
			atomic.AddInt64(&computed, 1)
		}(mf)
	}
	wg.Wait()

	return int(atomic.LoadInt64(&computed)), nil
}

// computeDHashForMedia 读取媒体缩略图计算 dHash；缩略图缺失则先同步生成一次再读。
// 返回 (哈希, 是否成功)；任何失败仅记 WARN 并返回 ok=false（跳过该条，不中断整体扫描）。
func (s *Service) computeDHashForMedia(filePath string) (uint64, bool) {
	thumbPath := FindThumbnailPath(filePath)
	if _, err := os.Stat(thumbPath); err != nil {
		// 缩略图尚未惰性生成，现场同步补一次
		generateThumbnailSync(filePath)
		if _, err := os.Stat(thumbPath); err != nil {
			log.Printf("[WARN] 去重：缩略图缺失且生成失败，跳过: %s", filePath)
			return 0, false
		}
	}
	hash, err := dHashFromThumbnail(thumbPath)
	if err != nil {
		log.Printf("[WARN] 去重：计算 dHash 失败，跳过: %s, err=%v", filePath, err)
		return 0, false
	}
	return hash, true
}

// FindDuplicateGroups 查全部「未软删且已算 dHash」的媒体，按汉明距离阈值聚类为重复组。
// 仅返回成员数 ≥ 2 的组；组内按 id 升序、组间按首成员 id 升序（稳定可测）。
func (s *Service) FindDuplicateGroups(threshold int) ([][]models.MediaFile, error) {
	var media []models.MediaFile
	if err := s.db.
		Where("deleted_at IS NULL AND dhash != 0").
		Order("id ASC").
		Find(&media).Error; err != nil {
		return nil, err
	}

	items := make([]dhashItem, len(media))
	byID := make(map[int64]models.MediaFile, len(media))
	for i, m := range media {
		items[i] = dhashItem{id: m.ID, hash: uint64(m.DHash)}
		byID[m.ID] = m
	}

	idGroups := clusterByHamming(items, threshold)
	groups := make([][]models.MediaFile, 0, len(idGroups))
	for _, ids := range idGroups {
		group := make([]models.MediaFile, 0, len(ids))
		for _, id := range ids {
			group = append(group, byID[id])
		}
		groups = append(groups, group)
	}
	return groups, nil
}
