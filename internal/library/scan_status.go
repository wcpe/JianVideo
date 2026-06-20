package library

import (
	"sync"
	"time"
)

// ScanStatus 描述当前扫描进度。
type ScanStatus struct {
	Status       string    `json:"status"`        // "idle", "scanning", "completed", "error"
	LibraryID    int64     `json:"library_id"`    // 正在扫描的媒体库 ID
	CurrentPath  string    `json:"current_path"`  // 当前正在扫描的文件路径
	TotalFiles   int       `json:"total_files"`   // 待扫描的文件总数
	ScannedFiles int       `json:"scanned_files"` // 已扫描的文件数
	Error        string    `json:"error"`         // 错误信息（status=error 时）
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
}

var (
	scanMu      sync.RWMutex
	currentScan = &ScanStatus{Status: "idle"}
)

// GetScanStatus 返回当前扫描状态的副本（并发安全）。
func GetScanStatus() ScanStatus {
	scanMu.RLock()
	defer scanMu.RUnlock()
	return *currentScan
}

// updateScanStatus 以互斥方式更新扫描状态。
func updateScanStatus(update func(*ScanStatus)) {
	scanMu.Lock()
	defer scanMu.Unlock()
	update(currentScan)
}
