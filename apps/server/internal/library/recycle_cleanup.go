package library

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// ErrRecycleBinPathUnset 存在软删项的盘符未配置回收站路径（含 SMB/无盘符项），整体拒绝清理（FR-26）。
var ErrRecycleBinPathUnset = errors.New("存在软删项所在盘符未配置回收站路径")

// MissingDrivesError 携带缺失配置的盘符列表，便于上层向用户指出缺哪个盘符。
// 包装 ErrRecycleBinPathUnset，可经 errors.Is 识别。
type MissingDrivesError struct {
	Drives []string
}

func (e *MissingDrivesError) Error() string {
	return fmt.Sprintf("以下盘符未配置回收站路径: %s", strings.Join(e.Drives, ", "))
}

func (e *MissingDrivesError) Unwrap() error { return ErrRecycleBinPathUnset }

// CleanupResult 回收站清理结果统计（FR-26）。
type CleanupResult struct {
	// Moved 成功移动源文件并删除记录的项数
	Moved int `json:"moved"`
	// Failed 移动失败（如文件已被外部删除）跳过的项数
	Failed int `json:"failed"`
}

// 回收恢复状态用于描述源路径占用、目标已暂存或自动恢复失败。
const (
	RecycleRecoveryStateSourceOccupied = "source_occupied"
	RecycleRecoveryStateTargetStaged   = "target_staged"
	RecycleRecoveryStateRestoreFailed  = "restore_failed"
)

const (
	recycleManifestVersion  = 2
	recycleManifestSuffix   = ".manifest.json"
	recycleRecoverySuffix   = ".recovery"
	recycleClaimStatePrefix = "recycle_cleanup:"
)

var (
	errRecycleSourceOccupied = errors.New("源路径已存在，拒绝覆盖")
	recycleCleanupMu         sync.Mutex
)

type recycleManifest struct {
	Version          int    `json:"version"`
	MediaID          int64  `json:"media_id"`
	SpaceID          string `json:"space_id"`
	SourcePathSHA256 string `json:"source_path_sha256"`
	ContentSHA256    string `json:"content_sha256"`
	DeletedAt        string `json:"deleted_at"`
	StagedSize       int64  `json:"staged_size"`
}

type recycleCompensatedError struct {
	cause error
}

func (e *recycleCompensatedError) Error() string {
	return "数据库删除失败，文件已恢复源路径"
}
func (e *recycleCompensatedError) Unwrap() error { return e.cause }

// RecycleRecoveryError 携带可重新发现的回收站恢复上下文。
type RecycleRecoveryError struct {
	MediaID       int64
	Source        string
	Target        string
	State         string
	Action        string
	DatabaseError error
	RecoveryError error
}

func (e *RecycleRecoveryError) Error() string {
	switch e.State {
	case RecycleRecoveryStateSourceOccupied:
		if e.DatabaseError != nil {
			return "回收站清理补偿失败：数据库删除失败且文件恢复失败，源路径已有文件，已保留现有文件与回收站文件"
		}
		return "回收站恢复冲突：源路径已有文件，已保留现有文件与回收站文件"
	case RecycleRecoveryStateTargetStaged:
		return "回收站恢复未完成：回收站文件已保留，请稍后重试"
	default:
		return "回收站恢复失败：文件与数据库状态未能自动同步，请检查服务端日志"
	}
}

func (e *RecycleRecoveryError) Unwrap() error { return e.DatabaseError }

// CleanupRecycle 清理回收站（FR-26）：把全部软删项的源文件移动到其所在盘符对应的回收站目录、
// 按删除日期（deleted_at 的日期）分子目录，移动成功后删除该 media_files 记录。
//
// drivePaths 为已解析的「盘符(大写) → 回收站目录」映射（由上层从 settings 读 JSON 解析），
// 本服务不依赖 settings、不解析 JSON，仅消费映射。
//
// 校验先行：只要存在任一软删项所在盘符在 drivePaths 无非空配置（含 SMB/无盘符项），
// 整体拒绝、不移动任何文件、不删任何记录，返回 *MissingDrivesError（errors.Is ErrRecycleBinPathUnset）。
func (s *Service) CleanupRecycle(drivePaths map[string]string) (CleanupResult, error) {
	return s.CleanupRecycleInSpace(models.DefaultSpaceID, drivePaths)
}

// CleanupRecycleInSpace 清理指定 Space 的回收站（FR-26）。
func (s *Service) CleanupRecycleInSpace(spaceID string, drivePaths map[string]string) (CleanupResult, error) {
	recycleCleanupMu.Lock()
	defer recycleCleanupMu.Unlock()

	deleted, err := s.ListDeletedMediaFilesInSpace(spaceID)
	if err != nil || len(deleted) == 0 {
		return CleanupResult{}, err
	}
	normalized := normalizeRecyclePaths(drivePaths)
	if missing := missingRecycleDrives(deleted, normalized); len(missing) > 0 {
		return CleanupResult{}, &MissingDrivesError{Drives: missing}
	}

	var result CleanupResult
	for i := range deleted {
		completed, itemErr := s.cleanupRecycleItem(spaceID, normalized, deleted[i])
		if completed {
			result.Moved++
		} else {
			result.Failed++
		}
		if itemErr != nil {
			return result, itemErr
		}
	}
	return result, nil
}

// AutoCleanupResult 到期自动清理统计（FR2-054）。
// 与手动 Cleanup 不同：缺盘符路径时跳过该条并记 Skipped，不整轮拒绝。
type AutoCleanupResult struct {
	// Candidate 本轮选中的过期软删项数
	Candidate int `json:"candidate"`
	// Moved 成功移动并删库记录的项数
	Moved int `json:"moved"`
	// Failed 清理过程失败（如 IO）的项数
	Failed int `json:"failed"`
	// Skipped 因缺盘符路径等跳过、仍留在回收站的项数
	Skipped int `json:"skipped"`
	// MissingDrives 本轮遇到的缺失盘符（去重）
	MissingDrives []string `json:"missing_drives,omitempty"`
	// MediaIDs 本轮候选媒体 id（preview 与 run 对齐用）
	MediaIDs []int64 `json:"media_ids,omitempty"`
}

const defaultAutoCleanupBatchLimit = 50

// ListExpiredDeletedMediaInSpace 列出指定 Space 中 deleted_at 早于 before 的软删项（最旧优先）。
func (s *Service) ListExpiredDeletedMediaInSpace(spaceID string, before time.Time, limit int) ([]models.MediaFile, error) {
	return s.mediaRepo.ListExpiredDeleted(normalizeSpaceID(spaceID), before, limit)
}

// PreviewAutoCleanupInSpace 预览本轮将处理的到期项与缺失盘符，不改数据（FR2-054）。
func (s *Service) PreviewAutoCleanupInSpace(spaceID string, drivePaths map[string]string, before time.Time, limit int) (AutoCleanupResult, error) {
	if limit <= 0 {
		limit = defaultAutoCleanupBatchLimit
	}
	items, err := s.ListExpiredDeletedMediaInSpace(spaceID, before, limit)
	if err != nil {
		return AutoCleanupResult{}, err
	}
	normalized := normalizeRecyclePaths(drivePaths)
	result := AutoCleanupResult{Candidate: len(items), MediaIDs: make([]int64, 0, len(items))}
	missingSet := map[string]struct{}{}
	for i := range items {
		result.MediaIDs = append(result.MediaIDs, items[i].ID)
		drive := driveOfPath(items[i].FilePath)
		if drive != "" && normalized[drive] != "" {
			continue
		}
		result.Skipped++
		if drive == "" {
			drive = "(无盘符)"
		}
		if _, ok := missingSet[drive]; !ok {
			missingSet[drive] = struct{}{}
			result.MissingDrives = append(result.MissingDrives, drive)
		}
	}
	// 可清理候选 = 总数 - 将跳过
	return result, nil
}

// AutoCleanupExpiredInSpace 有界清理到期软删项（FR2-054）。
// 缺盘符路径：跳过该条记 Skipped，不整轮 abort；单条 cleanup 错误记 Failed 并继续。
func (s *Service) AutoCleanupExpiredInSpace(spaceID string, drivePaths map[string]string, before time.Time, limit int) (AutoCleanupResult, error) {
	recycleCleanupMu.Lock()
	defer recycleCleanupMu.Unlock()

	if limit <= 0 {
		limit = defaultAutoCleanupBatchLimit
	}
	spaceID = normalizeSpaceID(spaceID)
	items, err := s.ListExpiredDeletedMediaInSpace(spaceID, before, limit)
	if err != nil {
		return AutoCleanupResult{}, err
	}
	normalized := normalizeRecyclePaths(drivePaths)
	result := AutoCleanupResult{Candidate: len(items), MediaIDs: make([]int64, 0, len(items))}
	missingSet := map[string]struct{}{}
	for i := range items {
		result.MediaIDs = append(result.MediaIDs, items[i].ID)
		drive := driveOfPath(items[i].FilePath)
		if drive == "" || normalized[drive] == "" {
			result.Skipped++
			label := drive
			if label == "" {
				label = "(无盘符)"
			}
			if _, ok := missingSet[label]; !ok {
				missingSet[label] = struct{}{}
				result.MissingDrives = append(result.MissingDrives, label)
			}
			continue
		}
		completed, itemErr := s.cleanupRecycleItem(spaceID, normalized, items[i])
		if completed {
			result.Moved++
		} else {
			result.Failed++
		}
		if itemErr != nil {
			// 自动清理：单条失败记日志并继续，不中断整轮。
			log.Printf("[WARN] 回收站自动清理单条失败: spaceID=%s mediaID=%d err=%v", spaceID, items[i].ID, itemErr)
		}
	}
	return result, nil
}

func normalizeRecyclePaths(drivePaths map[string]string) map[string]string {
	normalized := make(map[string]string, len(drivePaths))
	for drive, path := range drivePaths {
		if strings.TrimSpace(path) != "" {
			normalized[strings.ToUpper(drive)] = path
		}
	}
	return normalized
}

func missingRecycleDrives(deleted []models.MediaFile, drivePaths map[string]string) []string {
	missingSet := make(map[string]struct{})
	var missing []string
	for i := range deleted {
		drive := driveOfPath(deleted[i].FilePath)
		if drive != "" && drivePaths[drive] != "" {
			continue
		}
		if drive == "" {
			drive = "(无盘符)"
		}
		if _, exists := missingSet[drive]; !exists {
			missingSet[drive] = struct{}{}
			missing = append(missing, drive)
		}
	}
	return missing
}

type recycleClaim struct {
	media             models.MediaFile
	state             string
	originalFileState string
}

type recycleStage struct {
	media           models.MediaFile
	source          string
	target          string
	recovery        string
	boundary        *recycleBoundary
	stable          *os.File
	stablePath      string
	recoveryStable  *os.File
	finalLock       *os.File
	stableInfo      os.FileInfo
	targetInfo      os.FileInfo
	recoveryInfo    os.FileInfo
	contentHash     string
	sourceMissing   bool
	sourceRemoved   bool
	trusted         bool
	createdTarget   bool
	createdRecovery bool
	createdManifest bool
}

func (s *Service) cleanupRecycleItem(spaceID string, drivePaths map[string]string, snapshot models.MediaFile) (bool, error) {
	claim, claimed, err := s.claimRecycleMedia(spaceID, snapshot)
	if err != nil || !claimed {
		return false, err
	}
	completed, err := s.cleanupClaimedRecycleItem(drivePaths, claim)
	if err == nil {
		return completed, nil
	}
	var compensated *recycleCompensatedError
	if errors.As(err, &compensated) {
		return false, nil
	}
	return false, err
}

func (s *Service) claimRecycleMedia(spaceID string, snapshot models.MediaFile) (recycleClaim, bool, error) {
	if snapshot.DeletedAt == nil {
		return recycleClaim{}, false, nil
	}
	spaceID = normalizeSpaceID(spaceID)
	var claim recycleClaim
	err := s.mediaRepo.RunInTx(func(tx *gorm.DB) error {
		current, found, err := s.mediaRepo.GetByIDAndDeletedAtTx(tx, spaceID, snapshot.ID, snapshot.DeletedAt)
		if err != nil || !found {
			return err
		}
		claim = buildRecycleClaim(*current)
		if current.FileState == claim.state {
			return nil
		}
		rows, err := s.mediaRepo.UpdateFileStateCASTx(tx, spaceID, current.ID, current.DeletedAt, current.FileState, claim.state)
		if err != nil {
			return err
		}
		if rows != 1 {
			claim = recycleClaim{}
		}
		return nil
	})
	return claim, claim.media.ID != 0, err
}

func buildRecycleClaim(media models.MediaFile) recycleClaim {
	original := media.FileState
	state := media.FileState
	if strings.HasPrefix(state, recycleClaimStatePrefix) {
		original = strings.TrimPrefix(state, recycleClaimStatePrefix)
	} else {
		state = recycleClaimStatePrefix + original
	}
	media.FileState = state
	return recycleClaim{media: media, state: state, originalFileState: original}
}

func (s *Service) cleanupClaimedRecycleItem(drivePaths map[string]string, claim recycleClaim) (bool, error) {
	stage, err := prepareRecycleStage(drivePaths, claim.media)
	if err != nil {
		return s.failRecycleClaim(claim, nil, err, false)
	}
	defer stage.close()
	if err := stage.openStableReference(); err != nil {
		return s.failRecycleClaim(claim, stage, err, false)
	}
	if err := stage.prepareManifest(); err != nil {
		return s.failRecycleClaim(claim, stage, err, false)
	}
	if err := stage.removeSource(); err != nil {
		return s.failRecycleClaim(claim, stage, err, false)
	}
	if s.beforeRecycleFinalLock != nil {
		s.beforeRecycleFinalLock(stage.target)
	}
	if err := stage.lockAndValidateFinal(); err != nil {
		return s.failRecycleClaim(claim, stage, err, false)
	}
	return s.finalizeRecycleClaim(claim, stage)
}

func prepareRecycleStage(drivePaths map[string]string, media models.MediaFile) (*recycleStage, error) {
	source := filepath.FromSlash(media.FilePath)
	root := filepath.FromSlash(drivePaths[driveOfPath(media.FilePath)])
	target := recycleTargetPath(root, media)
	if err := validateRecycleBoundaryBeforeCreate(root, target); err != nil {
		return nil, recycleBoundaryFailure(media, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return nil, fmt.Errorf("创建回收站目标目录失败: mediaID=%d: %w", media.ID, err)
	}
	boundary, err := captureRecycleBoundary(root, target)
	if err != nil {
		return nil, recycleBoundaryFailure(media, err)
	}
	return &recycleStage{
		media: media, source: source, target: target,
		recovery: target + recycleRecoverySuffix, boundary: boundary,
	}, nil
}

func (s *recycleStage) openStableReference() error {
	sourceInfo, targetInfo, recoveryInfo, err := s.pathInfos()
	if err != nil {
		return err
	}
	reference := firstRecycleReference(s.source, s.target, s.recovery, sourceInfo, targetInfo, recoveryInfo)
	if reference == "" {
		return errors.New("源文件、回收站目标与恢复引用均不存在")
	}
	s.stablePath = reference
	s.stable, err = openRecycleStableFile(reference, false)
	if err != nil {
		return fmt.Errorf("打开稳定回收文件引用失败: %w", err)
	}
	s.stableInfo, err = s.stable.Stat()
	if err != nil {
		return fmt.Errorf("读取稳定回收文件信息失败: %w", err)
	}
	if err := requireRegularRecycleFile(reference, s.stableInfo); err != nil {
		return err
	}
	if err := s.validateExistingLinks(sourceInfo, targetInfo, recoveryInfo); err != nil {
		return err
	}
	s.sourceMissing = sourceInfo == nil
	if err := s.ensureTargetAndRecovery(targetInfo, recoveryInfo); err != nil {
		return err
	}
	return s.openRecoveryReference()
}

func (s *recycleStage) pathInfos() (os.FileInfo, os.FileInfo, os.FileInfo, error) {
	sourceInfo, sourceErr := recyclePathInfo(s.source)
	targetInfo, targetErr := recyclePathInfo(s.target)
	recoveryInfo, recoveryErr := recyclePathInfo(s.recovery)
	if sourceErr != nil || targetErr != nil || recoveryErr != nil {
		return nil, nil, nil, fmt.Errorf(
			"检查回收站文件状态失败: sourceErr=%v, targetErr=%v, recoveryErr=%v",
			sourceErr, targetErr, recoveryErr,
		)
	}
	return sourceInfo, targetInfo, recoveryInfo, nil
}

func firstRecycleReference(source, target, recovery string, sourceInfo, targetInfo, recoveryInfo os.FileInfo) string {
	if sourceInfo != nil {
		return source
	}
	if targetInfo != nil {
		return target
	}
	if recoveryInfo != nil {
		return recovery
	}
	return ""
}

func (s *recycleStage) validateExistingLinks(sourceInfo, targetInfo, recoveryInfo os.FileInfo) error {
	for _, item := range []struct {
		path        string
		info        os.FileInfo
		independent bool
	}{
		{path: s.source, info: sourceInfo},
		{path: s.target, info: targetInfo},
		{path: s.recovery, info: recoveryInfo, independent: true},
	} {
		if item.info == nil {
			continue
		}
		if err := requireRegularRecycleFile(item.path, item.info); err != nil {
			return err
		}
		if !item.independent && !os.SameFile(s.stableInfo, item.info) {
			return fmt.Errorf("回收站路径未指向稳定原始文件: %s", item.path)
		}
	}
	return nil
}

func (s *recycleStage) ensureTargetAndRecovery(targetInfo, recoveryInfo os.FileInfo) error {
	if targetInfo == nil {
		var err error
		if s.stablePath == s.recovery {
			err = s.createStableSnapshot(s.target, false)
		} else {
			err = s.createStableLink(s.target)
		}
		if err != nil {
			return err
		}
		s.createdTarget = true
	}
	if recoveryInfo == nil {
		if err := s.createStableSnapshot(s.recovery, false); err != nil {
			return err
		}
		s.createdRecovery = true
	}
	var err error
	s.targetInfo, err = requireRecycleFileInfo(s.target)
	if err != nil {
		return err
	}
	s.recoveryInfo, err = requireRecycleFileInfo(s.recovery)
	return err
}

func (s *recycleStage) openRecoveryReference() error {
	file, err := openRecycleStableFile(s.recovery, false)
	if err != nil {
		return fmt.Errorf("打开独立回收恢复副本失败: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("读取独立回收恢复副本失败: %w", err)
	}
	if !os.SameFile(info, s.recoveryInfo) {
		_ = file.Close()
		return errors.New("独立回收恢复副本在打开期间发生变化")
	}
	s.recoveryStable = file
	return nil
}

func (s *recycleStage) createStableLink(path string) error {
	if err := s.boundary.recheck(); err != nil {
		return err
	}
	if err := linkRecycleStableFile(s.stable, path); err != nil {
		return fmt.Errorf("创建稳定回收硬链接失败: %w", err)
	}
	if err := validateRecycleLinkIdentity(path, s.stableInfo); err != nil {
		return err
	}
	return s.boundary.recheck()
}

func (s *recycleStage) createStableSnapshot(path string, replace bool) error {
	if err := s.boundary.recheck(); err != nil {
		return err
	}
	if err := createRecycleSnapshot(s.stable, s.stableInfo, path, replace); err != nil {
		return err
	}
	return s.boundary.recheck()
}

func createRecycleSnapshot(source *os.File, sourceInfo os.FileInfo, target string, replace bool) error {
	before, err := prepareRecycleSnapshotSource(source, sourceInfo)
	if err != nil {
		return err
	}
	tempPath, written, err := writeRecycleSnapshotTemp(source, before, target)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tempPath) }()
	if err := verifyRecycleSnapshotSource(source, before, written); err != nil {
		return err
	}
	return publishRecycleSnapshot(tempPath, target, replace)
}

func prepareRecycleSnapshotSource(source *os.File, expected os.FileInfo) (os.FileInfo, error) {
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("定位回收恢复副本源文件失败: %w", err)
	}
	info, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("读取回收恢复副本源文件失败: %w", err)
	}
	if expected != nil && !os.SameFile(expected, info) {
		return nil, errors.New("回收恢复副本源文件句柄发生变化")
	}
	return info, nil
}

func writeRecycleSnapshotTemp(source *os.File, sourceInfo os.FileInfo, target string) (string, int64, error) {
	temp, err := os.CreateTemp(filepath.Dir(target), ".jianvideo-recovery-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("创建回收恢复副本临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(sourceInfo.Mode().Perm()); err != nil {
		_ = temp.Close()
		return "", 0, fmt.Errorf("设置回收恢复副本权限失败: %w", err)
	}
	written, err := copyAndSyncRecycleSnapshot(temp, source)
	if err != nil {
		_ = temp.Close()
		return "", 0, err
	}
	if err := temp.Close(); err != nil {
		return "", 0, fmt.Errorf("关闭回收恢复副本失败: %w", err)
	}
	keep = true
	return tempPath, written, nil
}

func copyAndSyncRecycleSnapshot(target, source *os.File) (int64, error) {
	written, err := io.CopyBuffer(target, source, make([]byte, 1024*1024))
	if err != nil {
		return 0, fmt.Errorf("复制回收恢复副本失败: %w", err)
	}
	if err := target.Sync(); err != nil {
		return 0, fmt.Errorf("同步回收恢复副本失败: %w", err)
	}
	return written, nil
}

func verifyRecycleSnapshotSource(source *os.File, before os.FileInfo, written int64) error {
	after, err := source.Stat()
	if err != nil {
		return fmt.Errorf("复核回收恢复副本源文件失败: %w", err)
	}
	if !os.SameFile(before, after) || written != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return errors.New("复制回收恢复副本期间源文件发生变化")
	}
	return nil
}

func publishRecycleSnapshot(tempPath, target string, replace bool) error {
	if replace {
		if err := replaceRecycleFile(tempPath, target); err != nil {
			return fmt.Errorf("原子替换最终回收目标失败: %w", err)
		}
		return nil
	}
	if err := os.Link(tempPath, target); err != nil {
		return fmt.Errorf("原子发布回收恢复副本失败: %w", err)
	}
	return nil
}

func (s *recycleStage) prepareManifest() error {
	manifestInfo, err := recyclePathInfo(recycleManifestPath(s.target))
	if err != nil {
		return err
	}
	if s.sourceMissing && manifestInfo == nil {
		return errors.New("源文件缺失且回收站目标没有归属清单")
	}
	s.contentHash, err = hashRecycleStableContent(s.stable, s.stableInfo)
	if err != nil {
		return err
	}
	if err := s.validateStagedCopies(); err != nil {
		return err
	}
	if err := validateTrustedRecycleHash(s.media, s.contentHash); err != nil {
		return err
	}
	if err := s.persistAndValidateManifest(manifestInfo == nil); err != nil {
		return err
	}
	s.trusted = true
	if s.sourceMissing {
		s.sourceRemoved = true
	}
	return nil
}

func (s *recycleStage) validateStagedCopies() error {
	target, err := openRecycleStableFile(s.target, false)
	if err != nil {
		return fmt.Errorf("打开回收目标副本失败: %w", err)
	}
	defer func() { _ = target.Close() }()
	if err := validateRecycleContentHash(target, s.targetInfo, s.contentHash); err != nil {
		return fmt.Errorf("回收目标副本校验失败: %w", err)
	}
	if err := validateRecycleContentHash(s.recoveryStable, s.recoveryInfo, s.contentHash); err != nil {
		return fmt.Errorf("独立回收恢复副本校验失败: %w", err)
	}
	return nil
}

func validateRecycleContentHash(file *os.File, info os.FileInfo, expected string) error {
	actual, err := hashRecycleStableContent(file, info)
	if err != nil {
		return err
	}
	if !strings.EqualFold(actual, expected) {
		return errors.New("回收文件副本内容哈希不一致")
	}
	return nil
}

func (s *recycleStage) persistAndValidateManifest(create bool) error {
	if err := s.boundary.recheck(); err != nil {
		return err
	}
	if create {
		if err := ensureRecycleManifest(s.media, s.target, s.stableInfo, s.contentHash); err != nil {
			return err
		}
		s.createdManifest = true
	}
	expected := expectedRecycleManifest(s.media, s.stableInfo, s.contentHash)
	if err := validateRecycleManifestValue(recycleManifestPath(s.target), expected); err != nil {
		return err
	}
	return s.boundary.recheck()
}

func (s *recycleStage) removeSource() error {
	if s.sourceRemoved {
		return nil
	}
	if err := s.validateProtectedLinks(); err != nil {
		return err
	}
	sourceInfo, err := recyclePathInfo(s.source)
	if err != nil {
		return err
	}
	if sourceInfo == nil {
		s.sourceRemoved = true
		return nil
	}
	if !os.SameFile(sourceInfo, s.stableInfo) {
		return errRecycleSourceOccupied
	}
	if err := os.Remove(s.source); err != nil {
		return fmt.Errorf("移除已暂存源路径失败: %w", err)
	}
	s.sourceRemoved = true
	return nil
}

func (s *recycleStage) lockAndValidateFinal() error {
	if err := s.boundary.recheck(); err != nil {
		return err
	}
	if recycleNeedsAtomicRestage() {
		if err := s.restageFinalTarget(); err != nil {
			return err
		}
	}
	file, err := openRecycleStableFile(s.target, true)
	if err != nil {
		return fmt.Errorf("打开最终回收文件保护句柄失败: %w", err)
	}
	if err := lockRecycleFile(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("锁定最终回收文件字节范围失败: %w", err)
	}
	s.finalLock = file
	finalInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取最终回收文件信息失败: %w", err)
	}
	if !os.SameFile(finalInfo, s.targetInfo) {
		return errors.New("最终回收目标已被替换")
	}
	return s.validateFinalContent(finalInfo)
}

func (s *recycleStage) restageFinalTarget() error {
	if err := s.boundary.recheck(); err != nil {
		return err
	}
	if err := createRecycleSnapshot(s.recoveryStable, s.recoveryInfo, s.target, true); err != nil {
		return err
	}
	info, err := requireRecycleFileInfo(s.target)
	if err != nil {
		return err
	}
	s.targetInfo = info
	if err := validateRecycleContentPath(s.target, info, s.contentHash); err != nil {
		return fmt.Errorf("原子暂存最终回收目标校验失败: %w", err)
	}
	return s.boundary.recheck()
}

func validateRecycleContentPath(path string, info os.FileInfo, expected string) error {
	file, err := openRecycleStableFile(path, false)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	return validateRecycleContentHash(file, info, expected)
}

func (s *recycleStage) validateFinalContent(finalInfo os.FileInfo) error {
	if err := s.validateProtectedLinks(); err != nil {
		return err
	}
	if err := requireRecycleSourceAbsent(s.source); err != nil {
		return err
	}
	finalHash, err := hashRecycleStableContent(s.finalLock, finalInfo)
	if err != nil {
		return err
	}
	if !strings.EqualFold(finalHash, s.contentHash) {
		return errors.New("最终内容哈希与归属清单不匹配")
	}
	expected := expectedRecycleManifest(s.media, finalInfo, finalHash)
	if err := validateRecycleManifestValue(recycleManifestPath(s.target), expected); err != nil {
		return err
	}
	return s.boundary.recheck()
}

func (s *recycleStage) validateProtectedLinks() error {
	if err := validateRecycleLinkIdentity(s.target, s.targetInfo); err != nil {
		return err
	}
	return validateRecycleLinkIdentity(s.recovery, s.recoveryInfo)
}

func validateRecycleLinkIdentity(path string, expected os.FileInfo) error {
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("复核稳定回收链接失败: %w", err)
	}
	if err := requireRegularRecycleFile(path, current); err != nil {
		return err
	}
	if !os.SameFile(expected, current) {
		return errors.New("稳定回收链接在操作期间发生变化")
	}
	return nil
}

func requireRecycleSourceAbsent(source string) error {
	_, err := os.Lstat(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("复核源路径状态失败: %w", err)
	}
	return errRecycleSourceOccupied
}

func hashRecycleStableContent(file *os.File, expected os.FileInfo) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("定位回收文件内容失败: %w", err)
	}
	before, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("读取稳定回收文件信息失败: %w", err)
	}
	if expected != nil && !os.SameFile(expected, before) {
		return "", errors.New("稳定回收文件句柄指向发生变化")
	}
	h := sha256.New()
	written, err := io.CopyBuffer(h, file, make([]byte, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("流式读取稳定回收文件失败: %w", err)
	}
	after, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("复核稳定回收文件信息失败: %w", err)
	}
	if !os.SameFile(before, after) || written != before.Size() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", errors.New("内容哈希计算期间稳定回收文件发生变化")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type recycleDeleteSnapshot struct {
	media       models.MediaFile
	watchStates []models.WatchState
	metadata    []models.MediaMetadata
	chapters    []models.MediaChapter
	bookmarks   []models.MediaBookmark
}

func (s *Service) finalizeRecycleClaim(claim recycleClaim, stage *recycleStage) (bool, error) {
	snapshot, rows, err := s.deleteRecycleClaim(claim)
	if err != nil || rows != 1 {
		if err == nil {
			err = fmt.Errorf("回收站清理最终 CAS 删除影响行数异常: mediaID=%d, rows=%d", claim.media.ID, rows)
		}
		return s.failRecycleClaim(claim, stage, err, true)
	}
	if s.afterRecycleDelete != nil {
		s.afterRecycleDelete()
	}
	if err := stage.validateAfterRecycleDelete(); err != nil {
		return false, s.compensatePostCASFailure(claim, stage, snapshot, err)
	}
	stage.removeRecoveryLink()
	stage.closeProtection()
	return true, nil
}

func (s *recycleStage) validateAfterRecycleDelete() error {
	finalInfo, err := s.finalLock.Stat()
	if err != nil {
		return fmt.Errorf("数据库提交后读取最终回收文件失败: %w", err)
	}
	if err := s.validateFinalContent(finalInfo); err != nil {
		return fmt.Errorf("数据库提交后复核最终回收文件失败: %w", err)
	}
	return nil
}

func (s *Service) compensatePostCASFailure(
	claim recycleClaim,
	stage *recycleStage,
	snapshot recycleDeleteSnapshot,
	cause error,
) error {
	sourceErr := stage.materializeSourceFromRecovery()
	databaseErr := s.restoreRecycleDeleteSnapshot(claim, snapshot)
	state := postCASRecoveryState(sourceErr, databaseErr)
	return recycleRecoveryError(
		claim.media, stage.source, stage.target, state,
		databaseErr, errors.Join(cause, sourceErr),
	)
}

func postCASRecoveryState(sourceErr, databaseErr error) string {
	if databaseErr != nil {
		return RecycleRecoveryStateRestoreFailed
	}
	if errors.Is(sourceErr, errRecycleSourceOccupied) {
		return RecycleRecoveryStateSourceOccupied
	}
	if sourceErr != nil {
		return RecycleRecoveryStateRestoreFailed
	}
	return RecycleRecoveryStateTargetStaged
}

func (s *recycleStage) materializeSourceFromRecovery() error {
	if s.recoveryStable == nil || s.recoveryInfo == nil {
		return errors.New("缺少 held recovery 文件引用")
	}
	if err := validateRecycleContentHash(s.recoveryStable, s.recoveryInfo, s.contentHash); err != nil {
		return fmt.Errorf("held recovery 内容校验失败: %w", err)
	}
	current, err := recyclePathInfo(s.source)
	if err != nil {
		return err
	}
	if current != nil {
		return errRecycleSourceOccupied
	}
	if err := createRecycleSnapshot(s.recoveryStable, s.recoveryInfo, s.source, false); err != nil {
		if errors.Is(err, os.ErrExist) || os.IsExist(err) {
			return errRecycleSourceOccupied
		}
		return fmt.Errorf("从 held recovery 物化源文件失败: %w", err)
	}
	info, err := requireRecycleFileInfo(s.source)
	if err != nil {
		return err
	}
	return validateRecycleContentPath(s.source, info, s.contentHash)
}

func (s *Service) deleteRecycleClaim(claim recycleClaim) (recycleDeleteSnapshot, int64, error) {
	var snapshot recycleDeleteSnapshot
	var rows int64
	err := s.mediaRepo.RunInTx(func(tx *gorm.DB) error {
		if err := readRecycleDeleteSnapshot(tx, claim, &snapshot); err != nil {
			return err
		}
		if err := deleteRecycleSnapshotRelations(tx, snapshot.media.SpaceID, snapshot.media.ID); err != nil {
			return err
		}
		result := recycleClaimQuery(tx, claim).Delete(&models.MediaFile{})
		rows = result.RowsAffected
		if result.Error != nil {
			return result.Error
		}
		if rows != 1 {
			return fmt.Errorf("回收站清理最终 CAS 删除冲突: mediaID=%d, rows=%d", claim.media.ID, rows)
		}
		return nil
	})
	return snapshot, rows, err
}

func recycleClaimQuery(tx *gorm.DB, claim recycleClaim) *gorm.DB {
	return tx.Where(
		"space_id = ? AND id = ? AND library_id = ? AND file_path = ? AND file_name = ? AND deleted_at = ? AND file_state = ?",
		claim.media.SpaceID, claim.media.ID, claim.media.LibraryID, claim.media.FilePath,
		claim.media.FileName, claim.media.DeletedAt, claim.state,
	)
}

func readRecycleDeleteSnapshot(tx *gorm.DB, claim recycleClaim, snapshot *recycleDeleteSnapshot) error {
	if err := recycleClaimQuery(tx, claim).First(&snapshot.media).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("回收站清理最终 CAS 候选已变化: mediaID=%d", claim.media.ID)
		}
		return err
	}
	return readRecycleDeleteRelations(tx, snapshot)
}

func readRecycleDeleteRelations(tx *gorm.DB, snapshot *recycleDeleteSnapshot) error {
	rows := []struct {
		model any
		dest  any
	}{
		{model: &models.WatchState{}, dest: &snapshot.watchStates},
		{model: &models.MediaMetadata{}, dest: &snapshot.metadata},
		{model: &models.MediaChapter{}, dest: &snapshot.chapters},
		{model: &models.MediaBookmark{}, dest: &snapshot.bookmarks},
	}
	for _, row := range rows {
		if tx.Migrator().HasTable(row.model) {
			if err := tx.Where("space_id = ? AND media_id = ?", snapshot.media.SpaceID, snapshot.media.ID).Find(row.dest).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteRecycleSnapshotRelations(tx *gorm.DB, spaceID string, mediaID int64) error {
	for _, model := range []any{
		&models.WatchState{}, &models.MediaMetadata{}, &models.MediaChapter{}, &models.MediaBookmark{},
	} {
		if tx.Migrator().HasTable(model) {
			if err := tx.Where("space_id = ? AND media_id = ?", spaceID, mediaID).Delete(model).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) restoreRecycleDeleteSnapshot(claim recycleClaim, snapshot recycleDeleteSnapshot) error {
	snapshot.media.FileState = claim.originalFileState
	snapshot.media.DeletedAt = claim.media.DeletedAt
	return s.mediaRepo.RunInTx(func(tx *gorm.DB) error {
		if err := ensureRecycleRestoreConflictsAbsent(tx, snapshot); err != nil {
			return err
		}
		return createRecycleDeleteSnapshot(tx, snapshot)
	})
}

func ensureRecycleRestoreConflictsAbsent(tx *gorm.DB, snapshot recycleDeleteSnapshot) error {
	if err := requireNoRecycleRestoreConflict(tx, &models.MediaFile{}, "媒体 ID", "id = ?", snapshot.media.ID); err != nil {
		return err
	}
	if err := ensureWatchStateRestoreConflictsAbsent(tx, snapshot.watchStates); err != nil {
		return err
	}
	if err := ensureMetadataRestoreConflictsAbsent(tx, snapshot.metadata); err != nil {
		return err
	}
	if err := ensureChapterRestoreConflictsAbsent(tx, snapshot.chapters); err != nil {
		return err
	}
	return ensureBookmarkRestoreConflictsAbsent(tx, snapshot.bookmarks)
}

func ensureWatchStateRestoreConflictsAbsent(tx *gorm.DB, rows []models.WatchState) error {
	for i := range rows {
		if err := requireNoRecycleRestoreConflict(
			tx, &models.WatchState{}, "观看状态主键", "space_id = ? AND media_id = ?", rows[i].SpaceID, rows[i].MediaID,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureMetadataRestoreConflictsAbsent(tx *gorm.DB, rows []models.MediaMetadata) error {
	for i := range rows {
		if err := requireNoRecycleRestoreConflict(tx, &models.MediaMetadata{}, "媒体元数据 ID", "id = ?", rows[i].ID); err != nil {
			return err
		}
		if err := requireNoRecycleRestoreConflict(
			tx, &models.MediaMetadata{}, "媒体元数据唯一键", "space_id = ? AND media_id = ? AND source = ?",
			rows[i].SpaceID, rows[i].MediaID, rows[i].Source,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureChapterRestoreConflictsAbsent(tx *gorm.DB, rows []models.MediaChapter) error {
	for i := range rows {
		if err := requireNoRecycleRestoreConflict(tx, &models.MediaChapter{}, "媒体章节 ID", "id = ?", rows[i].ID); err != nil {
			return err
		}
		if err := requireNoRecycleRestoreConflict(
			tx, &models.MediaChapter{}, "媒体章节唯一键", "space_id = ? AND media_id = ? AND source = ? AND source_index = ?",
			rows[i].SpaceID, rows[i].MediaID, rows[i].Source, rows[i].SourceIndex,
		); err != nil {
			return err
		}
	}
	return nil
}

func ensureBookmarkRestoreConflictsAbsent(tx *gorm.DB, rows []models.MediaBookmark) error {
	for i := range rows {
		if err := requireNoRecycleRestoreConflict(tx, &models.MediaBookmark{}, "媒体书签 ID", "id = ?", rows[i].ID); err != nil {
			return err
		}
	}
	return nil
}

func requireNoRecycleRestoreConflict(tx *gorm.DB, model any, key, query string, args ...any) error {
	var count int64
	if err := tx.Model(model).Where(query, args...).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("回收站数据库前像恢复冲突: %s 已存在", key)
	}
	return nil
}

func createRecycleDeleteSnapshot(tx *gorm.DB, snapshot recycleDeleteSnapshot) error {
	if err := tx.Create(&snapshot.media).Error; err != nil {
		return err
	}
	if len(snapshot.watchStates) > 0 {
		if err := tx.Create(&snapshot.watchStates).Error; err != nil {
			return err
		}
	}
	if len(snapshot.metadata) > 0 {
		if err := tx.Create(&snapshot.metadata).Error; err != nil {
			return err
		}
	}
	if len(snapshot.chapters) > 0 {
		if err := tx.Create(&snapshot.chapters).Error; err != nil {
			return err
		}
	}
	if len(snapshot.bookmarks) > 0 {
		return tx.Create(&snapshot.bookmarks).Error
	}
	return nil
}

func (s *Service) failRecycleClaim(claim recycleClaim, stage *recycleStage, cause error, databaseFailure bool) (bool, error) {
	if stage == nil {
		return false, s.releaseClaimOrRecovery(claim, "", "", cause)
	}
	if !stage.sourceRemoved || !stage.trusted {
		return false, s.failBeforeSourceRemoval(claim, stage, cause)
	}
	return false, s.compensateRemovedSource(claim, stage, cause, databaseFailure)
}

func (s *Service) failBeforeSourceRemoval(claim recycleClaim, stage *recycleStage, cause error) error {
	if err := s.releaseRecycleClaim(claim); err != nil {
		return recycleRecoveryError(claim.media, stage.source, stage.target, RecycleRecoveryStateTargetStaged, err, cause)
	}
	if stage.sourceMissing {
		return recycleRecoveryError(claim.media, stage.source, stage.target, RecycleRecoveryStateTargetStaged, nil, cause)
	}
	stage.removeCreatedArtifacts()
	return cause
}

func (s *Service) compensateRemovedSource(claim recycleClaim, stage *recycleStage, cause error, databaseFailure bool) error {
	restoreErr := stage.restoreSource()
	if restoreErr != nil {
		state := RecycleRecoveryStateRestoreFailed
		if errors.Is(restoreErr, errRecycleSourceOccupied) {
			state = RecycleRecoveryStateSourceOccupied
		}
		return recycleRecoveryError(claim.media, stage.source, stage.target, state, databaseError(databaseFailure, cause), restoreErr)
	}
	stage.closeProtection()
	if err := s.releaseRecycleClaim(claim); err != nil {
		return recycleRecoveryError(claim.media, stage.source, stage.target, RecycleRecoveryStateTargetStaged, errors.Join(databaseError(databaseFailure, cause), err), nil)
	}
	stage.removeStagedArtifacts()
	if databaseFailure {
		return &recycleCompensatedError{cause: cause}
	}
	return cause
}

func databaseError(databaseFailure bool, cause error) error {
	if databaseFailure {
		return cause
	}
	return nil
}

func (s *Service) releaseClaimOrRecovery(claim recycleClaim, source, target string, cause error) error {
	if err := s.releaseRecycleClaim(claim); err != nil {
		return recycleRecoveryError(claim.media, source, target, RecycleRecoveryStateTargetStaged, err, cause)
	}
	return cause
}

func (s *Service) releaseRecycleClaim(claim recycleClaim) error {
	rows, err := s.mediaRepo.UpdateFileStateCAS(
		claim.media.SpaceID, claim.media.ID, claim.media.DeletedAt, claim.state, claim.originalFileState,
	)
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("释放回收站清理 claim 影响行数异常: mediaID=%d, rows=%d", claim.media.ID, rows)
	}
	return nil
}

func (s *recycleStage) restoreSource() error {
	current, err := recyclePathInfo(s.source)
	if err != nil {
		return err
	}
	if current != nil {
		if os.SameFile(current, s.recoveryInfo) {
			return nil
		}
		return errRecycleSourceOccupied
	}
	if err := linkRecycleStableFile(s.recoveryStable, s.source); err != nil {
		if os.IsExist(err) {
			return errRecycleSourceOccupied
		}
		return err
	}
	return validateRecycleLinkIdentity(s.source, s.recoveryInfo)
}

func (s *recycleStage) closeProtection() {
	if s.finalLock != nil {
		if err := unlockRecycleFile(s.finalLock); err != nil {
			log.Printf("[WARN] 回收站清理：释放最终文件锁失败: mediaID=%d, target=%s, err=%v", s.media.ID, s.target, err)
		}
		_ = s.finalLock.Close()
		s.finalLock = nil
	}
}

func (s *recycleStage) close() {
	s.closeProtection()
	if s.recoveryStable != nil {
		_ = s.recoveryStable.Close()
		s.recoveryStable = nil
	}
	if s.stable != nil {
		_ = s.stable.Close()
		s.stable = nil
	}
}

func (s *recycleStage) removeCreatedArtifacts() {
	s.closeProtection()
	if s.createdManifest {
		removeRecycleManifest(s.media, s.target)
	}
	if s.createdRecovery {
		s.removeStablePath(s.recovery, s.recoveryInfo)
	}
	if s.createdTarget {
		s.removeStablePath(s.target, s.targetInfo)
	}
}

func (s *recycleStage) removeStagedArtifacts() {
	s.closeProtection()
	removeRecycleManifest(s.media, s.target)
	s.removeStablePath(s.recovery, s.recoveryInfo)
	s.removeStablePath(s.target, s.targetInfo)
}

func (s *recycleStage) removeRecoveryLink() {
	s.removeStablePath(s.recovery, s.recoveryInfo)
}

func (s *recycleStage) removeStablePath(path string, expected os.FileInfo) {
	info, err := recyclePathInfo(path)
	if err != nil || info == nil || expected == nil || !os.SameFile(info, expected) {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[WARN] 回收站清理：移除稳定恢复路径失败: mediaID=%d, path=%s, err=%v", s.media.ID, path, err)
	}
}

func recycleTargetPath(recycleRoot string, media models.MediaFile) string {
	dateDir := "未知日期"
	if media.DeletedAt != nil {
		dateDir = media.DeletedAt.Format("2006-01-02")
	}
	name := ".jianvideo-" + strconv.FormatInt(media.ID, 10) + "-" + media.FileName
	return filepath.Join(filepath.FromSlash(recycleRoot), dateDir, name)
}

func recyclePathInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err == nil {
		return info, nil
	}
	if os.IsNotExist(err) {
		return nil, nil
	}
	return nil, err
}

func requireRecycleFileInfo(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("读取回收文件失败: %w", err)
	}
	if err := requireRegularRecycleFile(path, info); err != nil {
		return nil, err
	}
	return info, nil
}

type recycleBoundary struct {
	root       string
	target     string
	rootInfo   os.FileInfo
	parentInfo os.FileInfo
}

func validateRecycleBoundaryBeforeCreate(root, target string) error {
	_, targetPath, err := normalizeRecycleBoundaryPaths(root, target)
	if err != nil {
		return err
	}
	return rejectRecycleReparseAncestors(filepath.Dir(targetPath))
}

func captureRecycleBoundary(root, target string) (*recycleBoundary, error) {
	rootPath, targetPath, err := normalizeRecycleBoundaryPaths(root, target)
	if err != nil {
		return nil, err
	}
	if err := rejectRecycleReparseAncestors(filepath.Dir(targetPath)); err != nil {
		return nil, err
	}
	rootInfo, err := requireRecycleDirectory(rootPath)
	if err != nil {
		return nil, err
	}
	parentInfo, err := requireRecycleDirectory(filepath.Dir(targetPath))
	if err != nil {
		return nil, err
	}
	if err := requireResolvedRecycleTarget(rootPath, targetPath); err != nil {
		return nil, err
	}
	return &recycleBoundary{root: rootPath, target: targetPath, rootInfo: rootInfo, parentInfo: parentInfo}, nil
}

func normalizeRecycleBoundaryPaths(root, target string) (string, string, error) {
	rootPath, err := filepath.Abs(filepath.Clean(filepath.FromSlash(root)))
	if err != nil {
		return "", "", fmt.Errorf("解析回收站根目录失败: %w", err)
	}
	targetPath, err := filepath.Abs(filepath.Clean(filepath.FromSlash(target)))
	if err != nil {
		return "", "", fmt.Errorf("解析回收站目标失败: %w", err)
	}
	if !recyclePathWithin(rootPath, targetPath) {
		return "", "", errors.New("回收站目标不在配置根目录内")
	}
	return rootPath, targetPath, nil
}

func rejectRecycleReparseAncestors(path string) error {
	for _, candidate := range recycleAncestorPaths(path) {
		info, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("检查回收站目录边界失败: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || recyclePathIsReparsePoint(info) {
			return fmt.Errorf("回收站目录边界包含链接或 reparse point: %s", candidate)
		}
		if !info.IsDir() {
			return fmt.Errorf("回收站目录边界不是目录: %s", candidate)
		}
	}
	return nil
}

func recycleAncestorPaths(path string) []string {
	current := filepath.Clean(path)
	var reversed []string
	for {
		reversed = append(reversed, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	paths := make([]string, len(reversed))
	for i := range reversed {
		paths[len(reversed)-1-i] = reversed[i]
	}
	return paths
}

func requireRecycleDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("读取回收站目录失败: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || recyclePathIsReparsePoint(info) {
		return nil, fmt.Errorf("回收站目录不是安全的真实目录: %s", path)
	}
	return info, nil
}

func requireResolvedRecycleTarget(root, target string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("解析回收站真实根目录失败: %w", err)
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("解析回收站真实目标目录失败: %w", err)
	}
	realTarget := filepath.Join(realParent, filepath.Base(target))
	if _, err := os.Lstat(target); err == nil {
		realTarget, err = filepath.EvalSymlinks(target)
		if err != nil {
			return fmt.Errorf("解析回收站真实目标失败: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查回收站真实目标失败: %w", err)
	}
	if !recyclePathWithin(realRoot, realTarget) {
		return errors.New("解析后的回收站目标不在真实根目录内")
	}
	return nil
}

func recyclePathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (b *recycleBoundary) recheck() error {
	current, err := captureRecycleBoundary(b.root, b.target)
	if err != nil {
		return err
	}
	if !os.SameFile(b.rootInfo, current.rootInfo) || !os.SameFile(b.parentInfo, current.parentInfo) {
		return errors.New("回收站目录边界在操作期间发生变化")
	}
	return nil
}

func recycleBoundaryFailure(media models.MediaFile, err error) error {
	return fmt.Errorf("回收站路径边界校验失败: mediaID=%d: %w", media.ID, err)
}

func recycleRecoveryError(media models.MediaFile, source, target, state string, databaseErr, recoveryErr error) *RecycleRecoveryError {
	action := "保留 recovery 与归属清单，检查数据库和文件系统后重试"
	switch state {
	case RecycleRecoveryStateSourceOccupied:
		action = "保留源路径现有文件、recovery 与归属清单；确认源文件后移走或删除，再重试清理"
	case RecycleRecoveryStateTargetStaged:
		action = "保留回收站目标、recovery 与归属清单；校验文件状态后重试"
	}
	return &RecycleRecoveryError{
		MediaID: media.ID, Source: filepath.ToSlash(source), Target: filepath.ToSlash(target),
		State: state, Action: action, DatabaseError: databaseErr, RecoveryError: recoveryErr,
	}
}

func requireRegularRecycleFile(path string, info os.FileInfo) error {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || recyclePathIsReparsePoint(info) {
		return fmt.Errorf("回收站路径不是安全的普通文件: %s", path)
	}
	return nil
}

func recycleManifestPath(target string) string {
	return target + recycleManifestSuffix
}

func validateTrustedRecycleHash(media models.MediaFile, contentHash string) error {
	if media.ContentHashAlgo != ContentHashAlgoSHA256 || media.ContentHashStale || media.ContentHash == "" {
		return nil
	}
	if !strings.EqualFold(media.ContentHash, contentHash) {
		return errors.New("媒体记录中的可信内容哈希与暂存文件不匹配")
	}
	return nil
}

func expectedRecycleManifest(media models.MediaFile, info os.FileInfo, contentHash string) recycleManifest {
	sum := sha256.Sum256([]byte(filepath.ToSlash(media.FilePath)))
	deletedAt := ""
	if media.DeletedAt != nil {
		deletedAt = media.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	return recycleManifest{
		Version: recycleManifestVersion, MediaID: media.ID, SpaceID: media.SpaceID,
		SourcePathSHA256: hex.EncodeToString(sum[:]), ContentSHA256: contentHash,
		DeletedAt: deletedAt, StagedSize: info.Size(),
	}
}

func ensureRecycleManifest(media models.MediaFile, target string, targetInfo os.FileInfo, contentHash string) error {
	expected := expectedRecycleManifest(media, targetInfo, contentHash)
	path := recycleManifestPath(target)
	if _, err := os.Lstat(path); err == nil {
		return validateRecycleManifestValue(path, expected)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查回收站归属清单失败: %w", err)
	}
	if err := createRecycleManifest(path, expected); err != nil {
		if errors.Is(err, os.ErrExist) {
			return validateRecycleManifestValue(path, expected)
		}
		return err
	}
	return validateRecycleManifestValue(path, expected)
}

func createRecycleManifest(path string, manifest recycleManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("编码回收站归属清单失败: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".jianvideo-manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("创建回收站归属清单临时文件失败: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("写入回收站归属清单失败: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("同步回收站归属清单失败: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("关闭回收站归属清单失败: %w", err)
	}
	if err := os.Link(tempPath, path); err != nil {
		return fmt.Errorf("原子发布回收站归属清单失败: %w", err)
	}
	return nil
}

func validateRecycleManifestValue(path string, expected recycleManifest) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("读取回收站归属清单失败: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || recyclePathIsReparsePoint(info) {
		return fmt.Errorf("回收站归属清单不是安全的普通文件: %s", path)
	}
	file, err := openRecycleStableFile(path, false)
	if err != nil {
		return fmt.Errorf("读取回收站归属清单失败: %w", err)
	}
	defer func() { _ = file.Close() }()
	openedInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("读取回收站归属清单失败: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || recyclePathIsReparsePoint(openedInfo) {
		return fmt.Errorf("回收站归属清单不是安全的普通文件: %s", path)
	}
	if !os.SameFile(info, openedInfo) {
		return errors.New("回收站归属清单在打开期间发生变化")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("读取回收站归属清单失败: %w", err)
	}
	var actual recycleManifest
	if err := json.Unmarshal(data, &actual); err != nil {
		return fmt.Errorf("解析回收站归属清单失败: %w", err)
	}
	if actual != expected {
		return errors.New("回收站归属清单与媒体记录或暂存文件不匹配")
	}
	return nil
}

func removeRecycleManifest(media models.MediaFile, target string) {
	path := recycleManifestPath(target)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[WARN] 回收站清理：文件补偿成功但归属清单移除失败: mediaID=%d, manifest=%s, err=%v", media.ID, path, err)
	}
}

// restoreRecycleSource 以硬链接原子占位恢复源文件，源路径已存在时拒绝覆盖。
func restoreRecycleSource(src, dest string) error {
	info, err := os.Lstat(dest)
	if err != nil {
		return fmt.Errorf("读取回收站暂存文件失败: %w", err)
	}
	if err := requireRegularRecycleFile(dest, info); err != nil {
		return err
	}
	if err := os.Link(dest, src); err != nil {
		if os.IsExist(err) {
			return errRecycleSourceOccupied
		}
		return fmt.Errorf("原子恢复源文件失败: %w", err)
	}
	if err := os.Remove(dest); err != nil {
		return fmt.Errorf("移除回收站暂存文件失败: %w", err)
	}
	return nil
}

// driveOfPath 解析路径所在的 Windows 盘符字母（大写）。
// 形如 "D:/a/b.mp4" → "D"；SMB（smb://）、Unix 绝对/相对路径无盘符 → 返回空串。
func driveOfPath(filePath string) string {
	p := filepath.ToSlash(filePath)
	if len(p) >= 2 && p[1] == ':' {
		ch := p[0]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
			return strings.ToUpper(string(ch))
		}
	}
	return ""
}

// uniqueDestPath 在 dir 下为 name 选一个不覆盖已有文件的目标路径。
// 若 dir/name 已存在，则追加 "(1)"、"(2)"… 直到不冲突。
func uniqueDestPath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate = filepath.Join(dir, base+"("+strconv.Itoa(i)+")"+ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}
