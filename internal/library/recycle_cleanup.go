package library

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// CleanupRecycle 清理回收站（FR-26）：把全部软删项的源文件移动到其所在盘符对应的回收站目录、
// 按删除日期（deleted_at 的日期）分子目录，移动成功后删除该 media_files 记录。
//
// drivePaths 为已解析的「盘符(大写) → 回收站目录」映射（由上层从 settings 读 JSON 解析），
// 本服务不依赖 settings、不解析 JSON，仅消费映射。
//
// 校验先行：只要存在任一软删项所在盘符在 drivePaths 无非空配置（含 SMB/无盘符项），
// 整体拒绝、不移动任何文件、不删任何记录，返回 *MissingDrivesError（errors.Is ErrRecycleBinPathUnset）。
func (s *Service) CleanupRecycle(drivePaths map[string]string) (CleanupResult, error) {
	deleted, err := s.ListDeletedMediaFiles()
	if err != nil {
		return CleanupResult{}, err
	}
	if len(deleted) == 0 {
		return CleanupResult{}, nil
	}

	// 盘符映射统一大写，做大小写不敏感查找
	normalized := make(map[string]string, len(drivePaths))
	for k, v := range drivePaths {
		if strings.TrimSpace(v) == "" {
			continue
		}
		normalized[strings.ToUpper(k)] = v
	}

	// 校验阶段：收集缺失配置的盘符（去重），有缺失即整体拒绝
	missingSet := make(map[string]struct{})
	var missing []string
	for _, mf := range deleted {
		drive := driveOfPath(mf.FilePath)
		if drive == "" || normalized[drive] == "" {
			key := drive
			if key == "" {
				key = "(无盘符)"
			}
			if _, seen := missingSet[key]; !seen {
				missingSet[key] = struct{}{}
				missing = append(missing, key)
			}
		}
	}
	if len(missing) > 0 {
		return CleanupResult{}, &MissingDrivesError{Drives: missing}
	}

	// 移动阶段：逐项移动源文件并删记录；单项失败仅计入 Failed、跳过，不回滚已成功项。
	var result CleanupResult
	for _, mf := range deleted {
		drive := driveOfPath(mf.FilePath)
		dateDir := "未知日期"
		if mf.DeletedAt != nil {
			dateDir = mf.DeletedAt.Format("2006-01-02")
		}
		targetDir := filepath.Join(filepath.FromSlash(normalized[drive]), dateDir)
		if err := os.MkdirAll(targetDir, 0o750); err != nil {
			log.Printf("[WARN] 回收站清理：创建目标目录失败: %s, err=%v", targetDir, err)
			result.Failed++
			continue
		}

		srcDisk := filepath.FromSlash(mf.FilePath)
		dest := uniqueDestPath(targetDir, mf.FileName)
		if err := os.Rename(srcDisk, dest); err != nil {
			log.Printf("[WARN] 回收站清理：移动源文件失败: %s -> %s, err=%v", srcDisk, dest, err)
			result.Failed++
			continue
		}

		// 文件已移出库，删除数据库记录（先移动成功、后删记录，保证一致性）
		if delErr := s.db.Delete(&models.MediaFile{}, mf.ID).Error; delErr != nil {
			log.Printf("[ERROR] 回收站清理：源文件已移动但删除记录失败: id=%d, err=%v", mf.ID, delErr)
			result.Failed++
			continue
		}
		result.Moved++
	}
	return result, nil
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
