package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/audit"
	"github.com/wcpe/JianVideo/internal/db/models"
	tasksvc "github.com/wcpe/JianVideo/internal/tasks"
)

// FR2-033 危险写回：将库内元数据写回原文件（仅图片有限字段）。
const (
	// TaskTypeMetadataWriteback 是写回原文件任务类型。
	TaskTypeMetadataWriteback = "metadata.writeback"
	// writebackSnapshotDirName 快照根目录名（位于数据目录下）。
	writebackSnapshotDirName = "writeback-snapshots"
	// writebackToolTimeout 单次 ImageMagick 写回超时。
	writebackToolTimeout = 60 * time.Second
)

// 哨兵错误（API 映射 HTTP 状态）。
var (
	// ErrWritebackConfirmRequired 缺少 confirm_writeback=true。
	ErrWritebackConfirmRequired = errors.New("写回原文件需 confirm_writeback=true")
	// ErrWritebackVideoUnsupported 视频写回未支持。
	ErrWritebackVideoUnsupported = errors.New("视频仅支持库内元数据，暂不支持写回原文件")
	// ErrWritebackSMBUnsupported SMB 路径不支持写回。
	ErrWritebackSMBUnsupported = errors.New("SMB 文件暂不支持写回原文件")
	// ErrWritebackNotImage 非图片媒体。
	ErrWritebackNotImage = errors.New("仅支持图片写回原文件")
	// ErrWritebackNoFields 无可写字段。
	ErrWritebackNoFields = errors.New("无可写回的元数据字段")
)

// writebackTaskPayload 任务 payload（快照路径与字段快照一并入队）。
type writebackTaskPayload struct {
	SpaceID      string            `json:"space_id"`
	MediaID      int64             `json:"media_id"`
	SourcePath   string            `json:"source_path"`
	SnapshotPath string            `json:"snapshot_path"`
	SourceHash   string            `json:"source_hash"`
	Fields       map[string]string `json:"fields"`
}

// writeImageMetadataFn 可注入，便于单测模拟工具失败。
var writeImageMetadataFn = writeImageMetadataWithMagick

// WritebackSnapshotDir 返回快照根目录。
func WritebackSnapshotDir(baseDir string) string {
	return filepath.Join(baseDir, writebackSnapshotDirName)
}

// InitWritebackSnapshotDir 初始化快照根目录。
func InitWritebackSnapshotDir(baseDir string) {
	dir := WritebackSnapshotDir(baseDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "[WARN] 初始化写回快照目录失败: dir=%s, err=%v\n", dir, err)
	}
}

// writebackPathWithin 判断 target 是否严格位于 root 边界内（不含 root 自身）。
func writebackPathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CleanupWritebackSnapshots 删除 writeback-snapshots 下 ModTime 早于 before 的普通文件。
// 只操作 WritebackSnapshotDir(baseDir) 边界内路径；返回成功删除的文件数。
// before 零值或 root 不存在时返回 0；跳过目录/符号链接；失败任务关联快照暂不特殊保留（首切按 mtime）。
func CleanupWritebackSnapshots(baseDir string, before time.Time) (int, error) {
	if before.IsZero() {
		return 0, nil
	}
	root, err := filepath.Abs(filepath.Clean(WritebackSnapshotDir(baseDir)))
	if err != nil {
		return 0, fmt.Errorf("解析写回快照根目录失败: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取写回快照根目录失败: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, fmt.Errorf("写回快照根不是安全目录: %s", root)
	}

	removed := 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// 单路径错误跳过，不中断整轮
			return nil
		}
		if path == root {
			return nil
		}
		absPath, absErr := filepath.Abs(filepath.Clean(path))
		if absErr != nil || !writebackPathWithin(root, absPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// 仅删除普通文件，跳过符号链接等
		fi, fiErr := d.Info()
		if fiErr != nil {
			return nil
		}
		if !fi.Mode().IsRegular() || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !fi.ModTime().Before(before) {
			return nil
		}
		// #nosec G122 -- absPath 已由 writebackPathWithin 限定在 WritebackSnapshotDir 边界内。
		if rmErr := os.Remove(absPath); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil
		}
		removed++
		return nil
	})
	if err != nil {
		return removed, fmt.Errorf("遍历写回快照目录失败: %w", err)
	}
	return removed, nil
}

// isImageMediaPath 按扩展名判断是否为内置图片类型。
func isImageMediaPath(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return false
	}
	return builtInMediaExtensions[ext] == MediaTypeImage
}

// isVideoMediaPath 按扩展名判断是否为内置视频类型。
func isVideoMediaPath(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if ext == "" {
		return false
	}
	return builtInMediaExtensions[ext] == MediaTypeVideo
}

// collectWritebackFields 从媒体记录提取首切可写回字段（非空才纳入）。
func collectWritebackFields(mf *models.MediaFile) map[string]string {
	if mf == nil {
		return nil
	}
	out := make(map[string]string, 8)
	put := func(k, v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			out[k] = v
		}
	}
	put("camera", mf.Camera)
	put("lens", mf.Lens)
	put("aperture", mf.Aperture)
	put("shutter", mf.Shutter)
	if mf.ISO > 0 {
		out["iso"] = strconv.Itoa(mf.ISO)
	}
	if mf.GPSLat != 0 || mf.GPSLon != 0 {
		out["gps_lat"] = strconv.FormatFloat(mf.GPSLat, 'f', -1, 64)
		out["gps_lon"] = strconv.FormatFloat(mf.GPSLon, 'f', -1, 64)
	}
	put("notes", mf.Notes)
	put("display_name", mf.DisplayName)
	return out
}

// SnapshotMediaFileForWriteback 复制原文件到数据目录快照区，返回绝对路径。
func SnapshotMediaFileForWriteback(baseDir, spaceID string, mediaID int64, sourcePath string) (string, error) {
	src := filepath.FromSlash(strings.TrimSpace(sourcePath))
	if src == "" {
		return "", errors.New("源路径为空")
	}
	if strings.HasPrefix(sourcePath, "smb://") {
		return "", ErrWritebackSMBUnsupported
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("源文件不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("源路径不是普通文件")
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	name := filepath.Base(src)
	dir := filepath.Join(WritebackSnapshotDir(baseDir), normalizeSpaceID(spaceID), strconv.FormatInt(mediaID, 10))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("创建快照目录失败: %w", err)
	}
	dst := filepath.Join(dir, ts+"-"+name)
	if err := copyFilePreserve(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func copyFilePreserve(src, dst string) error {
	// #nosec G304 -- src/dst 由 SnapshotMediaFileForWriteback 限定在媒体路径与 writeback-snapshots 边界内。
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer func() { _ = in.Close() }()
	// #nosec G304 -- 同上，dst 位于 WritebackSnapshotDir 下。
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("创建快照失败: %w", err)
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("复制快照失败: %w", err)
	}
	if err := out.Sync(); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("同步快照失败: %w", err)
	}
	return nil
}

// RestoreFileFromWritebackSnapshot 将写回前快照覆盖回原文件（FR2-041 回滚）。
// 不删除快照；目标路径不可为 SMB。
func RestoreFileFromWritebackSnapshot(snapshotPath, targetPath string) error {
	snap := filepath.FromSlash(strings.TrimSpace(snapshotPath))
	dst := filepath.FromSlash(strings.TrimSpace(targetPath))
	if snap == "" || dst == "" {
		return errors.New("快照或目标路径为空")
	}
	if strings.HasPrefix(targetPath, "smb://") || strings.HasPrefix(dst, "smb://") {
		return ErrWritebackSMBUnsupported
	}
	info, err := os.Stat(snap)
	if err != nil {
		return fmt.Errorf("快照不可访问: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("快照不是普通文件")
	}
	// 目标父目录须已存在（媒体源路径应有效）
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("准备目标目录失败: %w", err)
	}
	return copyFilePreserve(snap, dst)
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// EnqueueMetadataWriteback 校验媒体、打快照并入队 metadata.writeback（FR2-033）。
// confirm 必须为 true；返回已入队任务。
func EnqueueMetadataWriteback(
	ctx context.Context,
	enq MetadataTaskEnqueuer,
	svc *Service,
	baseDir, spaceID string,
	mediaID int64,
	confirm bool,
) (*models.Task, error) {
	if !confirm {
		return nil, ErrWritebackConfirmRequired
	}
	if enq == nil {
		return nil, errors.New("任务中心未启用")
	}
	if svc == nil {
		return nil, errors.New("媒体库服务未启用")
	}
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return nil, errors.New("数据目录未配置")
	}
	spaceID = normalizeSpaceID(spaceID)
	mf, err := svc.GetMediaFileByIDInSpace(spaceID, mediaID)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(mf.FilePath, "smb://") {
		return nil, ErrWritebackSMBUnsupported
	}
	if isVideoMediaPath(mf.FilePath) {
		return nil, ErrWritebackVideoUnsupported
	}
	if !isImageMediaPath(mf.FilePath) {
		return nil, ErrWritebackNotImage
	}
	fields := collectWritebackFields(mf)
	if len(fields) == 0 {
		return nil, ErrWritebackNoFields
	}
	srcPath := filepath.FromSlash(mf.FilePath)
	if _, err := os.Stat(srcPath); err != nil {
		return nil, fmt.Errorf("源文件不可访问: %w", err)
	}
	srcHash, err := fileSHA256Hex(srcPath)
	if err != nil {
		return nil, fmt.Errorf("计算源文件哈希失败: %w", err)
	}
	snap, err := SnapshotMediaFileForWriteback(baseDir, spaceID, mediaID, mf.FilePath)
	if err != nil {
		return nil, err
	}
	payload := writebackTaskPayload{
		SpaceID: spaceID, MediaID: mediaID,
		SourcePath: mf.FilePath, SnapshotPath: snap, SourceHash: srcHash, Fields: fields,
	}
	buf, _ := json.Marshal(payload)
	// 幂等键含快照路径时间戳，每次 confirm 均新任务（危险操作不静默复用旧任务）。
	key := fmt.Sprintf("metadata-writeback:%s:%d:%s", spaceID, mediaID, filepath.Base(snap))
	task, err := enq.Enqueue(ctx, tasksvc.EnqueueInput{
		Scope: models.TaskScopeSpace, SpaceID: spaceID, Type: TaskTypeMetadataWriteback,
		Priority: 0, MaxAttempts: 1, IdempotencyKey: key, PayloadJSON: string(buf),
		ResourceType: "media", ResourceID: strconv.FormatInt(mediaID, 10),
	})
	if err != nil {
		return nil, err
	}
	// 入队成功写 started 审计（含快照路径）。
	if svc.audit != nil {
		_ = svc.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: spaceID, ActorType: audit.ActorSystem,
			Action: "metadata.writeback.started", ResourceType: "media",
			ResourceID: strconv.FormatInt(mediaID, 10),
			Before:     map[string]any{"source_hash": srcHash, "fields": fields},
			Metadata: map[string]any{
				"summary": "元数据写回原文件入队", "snapshot_path": snap,
				"task_id": task.ID, "file_path": mf.FilePath,
			},
		})
	}
	return task, nil
}

// WritebackTaskRunner 执行 metadata.writeback 任务。
type WritebackTaskRunner struct {
	dataDir string
	library *Service
	tasks   *tasksvc.Service
}

// NewWritebackTaskRunner 创建写回任务执行器。
func NewWritebackTaskRunner(dataDir string, svc *Service, tasks *tasksvc.Service) *WritebackTaskRunner {
	return &WritebackTaskRunner{dataDir: dataDir, library: svc, tasks: tasks}
}

// RegisterWritebackWorker 注册 metadata.writeback worker。
func (r *WritebackTaskRunner) RegisterWritebackWorker(registry *tasksvc.WorkerRegistry) error {
	if registry == nil {
		return errors.New("worker 注册表为空")
	}
	return registry.Register(TaskTypeMetadataWriteback, 1, r.handleWriteback)
}

func (r *WritebackTaskRunner) handleWriteback(ctx context.Context, task models.Task) error {
	payload, err := parseWritebackTask(task)
	if err != nil {
		return err
	}
	src := filepath.FromSlash(payload.SourcePath)
	// 执行前再校验原文件哈希与入队时一致（防止并发外部改写）。
	curHash, err := fileSHA256Hex(src)
	if err != nil {
		r.recordWritebackFailed(ctx, payload, err)
		return err
	}
	if payload.SourceHash != "" && curHash != payload.SourceHash {
		err := fmt.Errorf("源文件在写回前已变更，已中止")
		r.recordWritebackFailed(ctx, payload, err)
		return err
	}
	if _, err := os.Stat(payload.SnapshotPath); err != nil {
		err = fmt.Errorf("快照不存在: %w", err)
		r.recordWritebackFailed(ctx, payload, err)
		return err
	}
	// 写临时文件 → rename 替换；失败保留原文件与快照。
	if err := writeImageMetadataFn(ctx, src, payload.Fields); err != nil {
		// 校验原文件哈希未变
		afterHash, _ := fileSHA256Hex(src)
		if afterHash != "" && afterHash != curHash {
			// 极端：工具半截改写；尝试从快照恢复
			_ = copyFilePreserve(payload.SnapshotPath, src)
		}
		r.recordWritebackFailed(ctx, payload, err)
		return err
	}
	if r.tasks != nil {
		cp, _ := json.Marshal(map[string]any{
			"snapshot_path": payload.SnapshotPath,
			"fields":        payload.Fields,
		})
		_ = r.tasks.UpdateProgress(ctx, task.ID, tasksvc.ProgressInput{Progress: 100, Checkpoint: string(cp)})
	}
	if r.library != nil && r.library.audit != nil {
		_ = r.library.audit.Record(ctx, audit.EventInput{
			Scope: audit.ScopeSpace, SpaceID: payload.SpaceID, ActorType: audit.ActorSystem,
			Action: "metadata.writeback.succeeded", ResourceType: "media",
			ResourceID: strconv.FormatInt(payload.MediaID, 10),
			After:      map[string]any{"fields": payload.Fields},
			Metadata: map[string]any{
				"summary": "元数据写回原文件成功", "snapshot_path": payload.SnapshotPath,
				"task_id": task.ID, "file_path": payload.SourcePath,
			},
		})
	}
	return nil
}

func (r *WritebackTaskRunner) recordWritebackFailed(ctx context.Context, payload writebackTaskPayload, cause error) {
	if r == nil || r.library == nil || r.library.audit == nil {
		return
	}
	_ = r.library.audit.Record(ctx, audit.EventInput{
		Scope: audit.ScopeSpace, SpaceID: payload.SpaceID, ActorType: audit.ActorSystem,
		Action: "metadata.writeback.failed", ResourceType: "media",
		ResourceID: strconv.FormatInt(payload.MediaID, 10),
		Metadata: map[string]any{
			"summary": "元数据写回原文件失败", "error": cause.Error(),
			"snapshot_path": payload.SnapshotPath, "file_path": payload.SourcePath,
		},
	})
}

func parseWritebackTask(task models.Task) (writebackTaskPayload, error) {
	if task.Scope != models.TaskScopeSpace || task.SpaceID == nil {
		return writebackTaskPayload{}, errors.New("写回任务必须归属 Space")
	}
	var payload writebackTaskPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		return payload, fmt.Errorf("解析写回任务失败: %w", err)
	}
	payload.SpaceID = strings.TrimSpace(payload.SpaceID)
	if payload.SpaceID == "" || payload.SpaceID != strings.TrimSpace(*task.SpaceID) {
		return payload, errors.New("写回任务 Space 与参数不一致")
	}
	if payload.MediaID <= 0 {
		return payload, errors.New("写回任务缺少媒体 ID")
	}
	if strings.TrimSpace(payload.SourcePath) == "" || strings.TrimSpace(payload.SnapshotPath) == "" {
		return payload, errors.New("写回任务缺少路径")
	}
	return payload, nil
}

// writeImageMetadataWithMagick 用 ImageMagick 将有限字段写入图片（写临时文件再原子替换）。
func writeImageMetadataWithMagick(ctx context.Context, sourcePath string, fields map[string]string) error {
	if !IsMagickAvailable() {
		return errors.New("ImageMagick 不可用，无法写回图片元数据")
	}
	src := filepath.FromSlash(sourcePath)
	tmp := src + ".jv-writeback.tmp"
	_ = os.Remove(tmp)
	args := []string{src}
	// 有限字段映射到 EXIF/IPTC（magick -set）。
	if v, ok := fields["camera"]; ok {
		args = append(args, "-set", "EXIF:Model", v)
	}
	if v, ok := fields["lens"]; ok {
		args = append(args, "-set", "EXIF:LensModel", v)
	}
	if v, ok := fields["aperture"]; ok {
		args = append(args, "-set", "EXIF:FNumber", v)
	}
	if v, ok := fields["shutter"]; ok {
		args = append(args, "-set", "EXIF:ExposureTime", v)
	}
	if v, ok := fields["iso"]; ok {
		args = append(args, "-set", "EXIF:ISOSpeedRatings", v)
	}
	if v, ok := fields["notes"]; ok {
		args = append(args, "-set", "EXIF:ImageDescription", v)
		args = append(args, "-set", "IPTC:Caption", v)
	}
	if v, ok := fields["display_name"]; ok {
		args = append(args, "-set", "EXIF:XPTitle", v)
	}
	if lat, ok := fields["gps_lat"]; ok {
		args = append(args, "-set", "EXIF:GPSLatitude", lat)
	}
	if lon, ok := fields["gps_lon"]; ok {
		args = append(args, "-set", "EXIF:GPSLongitude", lon)
	}
	args = append(args, tmp)

	runCtx, cancel := context.WithTimeout(ctx, writebackToolTimeout)
	defer cancel()
	// #nosec G204 -- magick 路径来自受控配置，参数固定模板，不经 shell。
	cmd := exec.CommandContext(runCtx, GetMagickPath(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("图片元数据写回失败: %w, 输出: %s", err, strings.TrimSpace(string(out)))
	}
	// 原子替换：Windows 上 rename 覆盖需先删目标（同卷）。
	bak := src + ".jv-writeback.bak"
	_ = os.Remove(bak)
	if err := os.Rename(src, bak); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("备份原文件失败: %w", err)
	}
	if err := os.Rename(tmp, src); err != nil {
		// 回滚
		_ = os.Rename(bak, src)
		_ = os.Remove(tmp)
		return fmt.Errorf("替换原文件失败: %w", err)
	}
	_ = os.Remove(bak)
	return nil
}
