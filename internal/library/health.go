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

type healthIssue struct {
	IssueType string
	Detail    string
}

type mediaHealthChecks struct {
	stat       func(path string) (os.FileInfo, error)
	probeVideo func(path string) error
	thumbnail  func(path string) error
	isVideo    func(path string) bool
}

func defaultHealthChecks() mediaHealthChecks {
	return mediaHealthChecks{stat: os.Stat, probeVideo: ProbeVideoHealth, thumbnail: TryGenerateThumbnail, isVideo: isVideoFile}
}

func isVideoFile(path string) bool {
	mediaType, ok := mediaTypeByExtension(normalizeExtension(filepath.Ext(path)))
	return ok && mediaType == MediaTypeVideo
}

type healthSpaceState struct {
	running bool
	status  HealthScanStatus
}

// HealthService 按 Space 隔离健康巡检状态与问题快照。
type HealthService struct {
	db     *gorm.DB
	checks mediaHealthChecks
	mu     sync.Mutex
	states map[string]healthSpaceState
}

// NewHealthService 使用指定健康检查器创建健康巡检服务。
func NewHealthService(db *gorm.DB, checks mediaHealthChecks) *HealthService {
	return &HealthService{db: db, checks: checks, states: make(map[string]healthSpaceState)}
}

// NewDefaultHealthService 使用默认健康检查器创建健康巡检服务。
func NewDefaultHealthService(db *gorm.DB) *HealthService {
	return NewHealthService(db, defaultHealthChecks())
}

// Status 返回默认 Space 巡检状态。
func (s *HealthService) Status() HealthScanStatus {
	return s.StatusInSpace(models.DefaultSpaceID)
}

// StatusInSpace 返回指定 Space 巡检状态。
func (s *HealthService) StatusInSpace(spaceID string) HealthScanStatus {
	spaceID = normalizeSpaceID(spaceID)
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[spaceID]
	if !ok {
		return HealthScanStatus{Status: healthStatusIdle}
	}
	return state.status
}

// ListIssues 返回默认 Space 问题清单。
func (s *HealthService) ListIssues() ([]models.MediaHealthIssue, error) {
	return s.ListIssuesInSpace(models.DefaultSpaceID)
}

// ListIssuesInSpace 返回指定 Space 问题清单。
func (s *HealthService) ListIssuesInSpace(spaceID string) ([]models.MediaHealthIssue, error) {
	var issues []models.MediaHealthIssue
	err := s.db.Where("space_id = ?", normalizeSpaceID(spaceID)).Order("issue_type ASC, media_id ASC").Find(&issues).Error
	return issues, err
}

// StartScan 触发默认 Space 巡检。
func (s *HealthService) StartScan() bool {
	return s.StartScanInSpace(models.DefaultSpaceID)
}

// StartScanInSpace 触发指定 Space 后台巡检，同一 Space 内单飞。
func (s *HealthService) StartScanInSpace(spaceID string) bool {
	spaceID = normalizeSpaceID(spaceID)
	s.mu.Lock()
	state := s.states[spaceID]
	if state.running {
		s.mu.Unlock()
		return false
	}
	state.running = true
	state.status = HealthScanStatus{Status: healthStatusScanning, StartedAt: time.Now()}
	s.states[spaceID] = state
	s.mu.Unlock()
	go func() {
		if err := s.runScanInSpace(spaceID); err != nil {
			log.Printf("[ERROR] Space %s 媒体健康巡检失败: %v", spaceID, err)
		}
	}()
	return true
}

func (s *HealthService) runScan() error {
	return s.runScanInSpace(models.DefaultSpaceID)
}

// runScanInSpace 同步执行指定 Space 巡检，仅替换该 Space 的问题快照。
func (s *HealthService) runScanInSpace(spaceID string) error {
	spaceID = normalizeSpaceID(spaceID)
	defer s.finishRunning(spaceID)
	var media []models.MediaFile
	if err := s.db.Where("space_id = ? AND deleted_at IS NULL", spaceID).Order("id ASC").Find(&media).Error; err != nil {
		s.setError(spaceID, err)
		return err
	}
	s.updateStatus(spaceID, func(status *HealthScanStatus) { status.Total = len(media) })
	found := s.classifySpaceMedia(spaceID, media)
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space_id = ?", spaceID).Delete(&models.MediaHealthIssue{}).Error; err != nil {
			return err
		}
		if len(found) > 0 {
			return tx.Create(&found).Error
		}
		return nil
	})
	if err != nil {
		s.setError(spaceID, err)
		return err
	}
	s.updateStatus(spaceID, func(status *HealthScanStatus) {
		status.Status = healthStatusCompleted
		status.IssueCount = len(found)
		status.CompletedAt = time.Now()
	})
	log.Printf("[INFO] Space %s 媒体健康巡检完成：巡检 %d 个媒体，发现 %d 条问题", spaceID, len(media), len(found))
	return nil
}

func (s *HealthService) classifySpaceMedia(spaceID string, media []models.MediaFile) []models.MediaHealthIssue {
	found := make([]models.MediaHealthIssue, 0)
	now := time.Now()
	for index, file := range media {
		for _, issue := range classifyMediaIssues(file, s.checks) {
			found = append(found, models.MediaHealthIssue{SpaceID: spaceID, MediaID: file.ID, IssueType: issue.IssueType, Detail: issue.Detail, CheckedAt: now})
		}
		checked := index + 1
		s.updateStatus(spaceID, func(status *HealthScanStatus) { status.Checked = checked })
	}
	return found
}

func (s *HealthService) finishRunning(spaceID string) {
	s.mu.Lock()
	state := s.states[spaceID]
	state.running = false
	s.states[spaceID] = state
	s.mu.Unlock()
}

func (s *HealthService) updateStatus(spaceID string, update func(*HealthScanStatus)) {
	s.mu.Lock()
	state := s.states[spaceID]
	update(&state.status)
	s.states[spaceID] = state
	s.mu.Unlock()
}

func (s *HealthService) setError(spaceID string, err error) {
	s.updateStatus(spaceID, func(status *HealthScanStatus) {
		status.Status = healthStatusError
		status.Error = err.Error()
		status.CompletedAt = time.Now()
	})
}

func classifyMediaIssues(mf models.MediaFile, checks mediaHealthChecks) []healthIssue {
	if mf.FileSize == 0 {
		return []healthIssue{{IssueType: models.HealthIssueZeroByte}}
	}
	if !strings.HasPrefix(mf.FilePath, "smb://") {
		if _, err := checks.stat(mf.FilePath); err != nil {
			return []healthIssue{{IssueType: models.HealthIssueMissing, Detail: err.Error()}}
		}
	}
	var issues []healthIssue
	if checks.isVideo(mf.FilePath) {
		if err := checks.probeVideo(mf.FilePath); err != nil {
			issues = append(issues, healthIssue{IssueType: models.HealthIssueBroken, Detail: err.Error()})
		}
	}
	if err := checks.thumbnail(mf.FilePath); err != nil {
		issues = append(issues, healthIssue{IssueType: models.HealthIssueNoThumbnail, Detail: err.Error()})
	}
	return issues
}
