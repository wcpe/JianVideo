package library

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// healthIssue 单条问题判定结果（巡检内部用，落库前转 models.MediaHealthIssue）。
type healthIssue struct {
	IssueType string
	Detail    string
}

// mediaHealthChecks 健康判定所需的外部依赖，经函数注入以便穷举单测（不连真实 ffprobe / 文件系统）。
type mediaHealthChecks struct {
	stat       func(path string) (os.FileInfo, error) // 文件是否存在
	probeVideo func(path string) error                // 视频是否可解析
	thumbnail  func(path string) error                // 缩略图是否可生成
	isVideo    func(path string) bool                 // 是否视频（决定是否走视频损坏探测）
}

// defaultHealthChecks 返回接入真实 ffprobe / 文件系统 / 缩略图生成的判定依赖。
func defaultHealthChecks() mediaHealthChecks {
	return mediaHealthChecks{
		stat:       os.Stat,
		probeVideo: ProbeVideoHealth,
		thumbnail:  TryGenerateThumbnail,
		isVideo:    isVideoFile,
	}
}

// isVideoFile 按内置后缀判断是否视频文件（巡检判定损坏仅针对视频）。
func isVideoFile(path string) bool {
	mediaType, ok := mediaTypeByExtension(normalizeExtension(filepath.Ext(path)))
	return ok && mediaType == MediaTypeVideo
}

// HealthService 媒体健康巡检服务（FR-73）：后台只读巡检全部未软删媒体，
// 把问题汇总写入独立的 media_health_issues 表（每轮先清空再写入），绝不改写 media_files.deleted_at。
//
// 不复用 TaskQueue：巡检是单次全局操作、语义与「按库排队的扫描任务」不同；
// 用独立 goroutine + 单飞标志 + 状态单例（HealthScanStatus），与 scan_status 模式一致。
type HealthService struct {
	db     *gorm.DB
	checks mediaHealthChecks

	mu      sync.Mutex // 保护 running 标志与 status
	running bool
	status  HealthScanStatus
}

// NewHealthService 创建健康巡检服务。checks 为判定依赖（生产用 defaultHealthChecks，测试可注入替身）。
func NewHealthService(db *gorm.DB, checks mediaHealthChecks) *HealthService {
	return &HealthService{
		db:     db,
		checks: checks,
		status: HealthScanStatus{Status: healthStatusIdle},
	}
}

// NewDefaultHealthService 创建接入真实 ffprobe / 文件系统的健康巡检服务（供 main 注入）。
func NewDefaultHealthService(db *gorm.DB) *HealthService {
	return NewHealthService(db, defaultHealthChecks())
}

// Status 返回当前巡检进度的副本（并发安全）。
func (s *HealthService) Status() HealthScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// ListIssues 返回当前问题清单，按问题类型再按 media_id 排序（稳定输出便于前端分组）。
func (s *HealthService) ListIssues() ([]models.MediaHealthIssue, error) {
	var issues []models.MediaHealthIssue
	if err := s.db.Order("issue_type ASC, media_id ASC").Find(&issues).Error; err != nil {
		return nil, err
	}
	return issues, nil
}

// StartScan 触发一次后台巡检（单飞）：已有巡检在跑时返回 false 不重复启动。
func (s *HealthService) StartScan() bool {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	s.running = true
	s.status = HealthScanStatus{Status: healthStatusScanning, StartedAt: time.Now()}
	s.mu.Unlock()

	go func() {
		if err := s.runScan(); err != nil {
			log.Printf("[ERROR] 媒体健康巡检失败: %v", err)
		}
	}()
	return true
}

// runScan 同步执行一轮巡检：遍历未软删媒体逐项判定，先清空问题表再写入当轮快照。
// 抽出供单测直接驱动（不经 goroutine）。执行完置 running=false 与终态。
func (s *HealthService) runScan() error {
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	var media []models.MediaFile
	if err := s.db.Where("deleted_at IS NULL").Order("id ASC").Find(&media).Error; err != nil {
		s.setError(err)
		return err
	}

	s.mu.Lock()
	s.status.Total = len(media)
	s.mu.Unlock()

	var found []models.MediaHealthIssue
	now := time.Now()
	checked := 0
	for _, mf := range media {
		for _, issue := range classifyMediaIssues(mf, s.checks) {
			found = append(found, models.MediaHealthIssue{
				MediaID:   mf.ID,
				IssueType: issue.IssueType,
				Detail:    issue.Detail,
				CheckedAt: now,
			})
		}
		checked++
		s.mu.Lock()
		s.status.Checked = checked
		s.mu.Unlock()
	}

	// 单事务内先清空旧问题再写入当轮快照（只动 media_health_issues，绝不碰 media_files）
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("1 = 1").Delete(&models.MediaHealthIssue{}).Error; err != nil {
			return err
		}
		if len(found) > 0 {
			if err := tx.Create(&found).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.setError(err)
		return err
	}

	s.mu.Lock()
	s.status.Status = healthStatusCompleted
	s.status.IssueCount = len(found)
	s.status.CompletedAt = time.Now()
	s.mu.Unlock()
	log.Printf("[INFO] 媒体健康巡检完成：巡检 %d 个媒体，发现 %d 条问题", len(media), len(found))
	return nil
}

// setError 把巡检状态置为出错。
func (s *HealthService) setError(err error) {
	s.mu.Lock()
	s.status.Status = healthStatusError
	s.status.Error = err.Error()
	s.status.CompletedAt = time.Now()
	s.mu.Unlock()
}

// classifyMediaIssues 对单条媒体逐项判定健康问题，返回问题列表（纯函数、无副作用，便于穷举单测）。
// 判定顺序：0 字节 → 文件缺失 → 视频损坏 → 缩略图无法生成；任一前置硬问题命中即不再做后续昂贵探测。
func classifyMediaIssues(mf models.MediaFile, checks mediaHealthChecks) []healthIssue {
	// 0 字节：直接判定，无需碰文件系统
	if mf.FileSize == 0 {
		return []healthIssue{{IssueType: models.HealthIssueZeroByte}}
	}

	// 文件缺失：仅对本地路径判定，排除 SMB 远程路径（远程列举不保证完整，不误判）
	isSMB := strings.HasPrefix(mf.FilePath, "smb://")
	if !isSMB {
		if _, err := checks.stat(mf.FilePath); err != nil {
			return []healthIssue{{IssueType: models.HealthIssueMissing, Detail: err.Error()}}
		}
	}

	var issues []healthIssue
	// 视频损坏：仅对视频走 ffprobe 探测
	if checks.isVideo(mf.FilePath) {
		if err := checks.probeVideo(mf.FilePath); err != nil {
			issues = append(issues, healthIssue{IssueType: models.HealthIssueBroken, Detail: err.Error()})
		}
	}

	// 缩略图无法生成
	if err := checks.thumbnail(mf.FilePath); err != nil {
		issues = append(issues, healthIssue{IssueType: models.HealthIssueNoThumbnail, Detail: err.Error()})
	}

	return issues
}
