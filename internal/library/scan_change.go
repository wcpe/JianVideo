package library

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

const (
	// ScanChangeAdded 表示发现新增文件。
	ScanChangeAdded = "added"
	// ScanChangeModified 表示发现文件内容或元数据变化。
	ScanChangeModified = "modified"
	// ScanChangeRemoved 表示发现文件丢失或删除。
	ScanChangeRemoved = "removed"
	// ScanChangeRenamed 表示发现文件重命名。
	ScanChangeRenamed = "renamed"
)

// ScanChange 是扫描、watcher、轮询统一提交的路径变更模型。
type ScanChange struct {
	SpaceID            string
	LibraryID          int64
	Path               string
	OldPath            string
	Op                 string
	ObservedAt         time.Time
	FingerprintChanged bool
}

// NormalizeScanChange 归一化扫描变更，保证 Space、路径、操作和观测时间有稳定默认值。
func NormalizeScanChange(change ScanChange) ScanChange {
	change.SpaceID = normalizeSpaceID(change.SpaceID)
	change.Path = filepath.ToSlash(strings.TrimSpace(change.Path))
	change.OldPath = filepath.ToSlash(strings.TrimSpace(change.OldPath))
	change.Op = normalizeScanChangeOp(change.Op)
	if change.ObservedAt.IsZero() {
		change.ObservedAt = time.Now()
	}
	return change
}

func normalizeScanChangeOp(op string) string {
	switch strings.TrimSpace(op) {
	case ScanChangeAdded:
		return ScanChangeAdded
	case ScanChangeRemoved:
		return ScanChangeRemoved
	case ScanChangeRenamed:
		return ScanChangeRenamed
	default:
		return ScanChangeModified
	}
}

func activeFileStateCondition() string {
	return "(file_state IS NULL OR file_state = '' OR file_state = '" + models.MediaFileStateAvailable + "')"
}
