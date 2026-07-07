package library

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wcpe/JianVideo/internal/db/models"
)

// 上传命名规则常量（FR-149，见 ADR-0051）。
const (
	// UploadNamingOriginal 保留原样：直接落在目标目录，文件名沿用上传原名。
	UploadNamingOriginal = "original"
	// UploadNamingDate 按规则整齐归档：按媒体时间分 YYYY/MM 子目录归档。
	UploadNamingDate = "date"
)

// 上传相关业务错误（供上层映射为对应 HTTP 状态码）。
var (
	// ErrUploadTargetNotInLibrary 目标目录不在任何已注册的本地库目录内（防越权写库外）。
	ErrUploadTargetNotInLibrary = errors.New("上传目标目录不在任何已注册的媒体库目录内")
	// ErrUploadUnsupportedType 上传文件既非图片也非视频。
	ErrUploadUnsupportedType = errors.New("仅支持上传图片或视频文件")
	// ErrInvalidUploadName 上传文件名为空或含非法路径片段。
	ErrInvalidUploadName = errors.New("上传文件名不合法")
)

// NormalizeUploadNamingRule 规范化命名规则：仅 date 视为按规则整齐归档，其余（含空/非法）按保留原样。
// 供端点解析参数与设置回退时使用，保证默认行为为「保留原样」。
func NormalizeUploadNamingRule(rule string) string {
	if rule == UploadNamingDate {
		return UploadNamingDate
	}
	return UploadNamingOriginal
}

// ResolveUploadLibrary 解析上传目标目录归属的本地库（FR-149）。
// targetDir 为期望落盘目录（绝对路径，任意分隔符）；遍历已注册库，找到包含该目录的启用本地库。
// 命中返回 (库, 规范化后的目标目录绝对路径/正斜杠, nil)；
// 未命中（含目录在库外、越权 .. 逃逸、SMB 库）返回 ErrUploadTargetNotInLibrary。
// 纯函数无副作用（不触磁盘），便于穷举测试。
func ResolveUploadLibrary(paths []models.LibraryPath, targetDir string) (*models.LibraryPath, string, error) {
	cleaned := cleanUploadDir(targetDir)
	if cleaned == "" {
		return nil, "", ErrUploadTargetNotInLibrary
	}

	for i := range paths {
		lp := paths[i]
		if lp.Enabled == 0 || lp.Type != "local" {
			continue
		}
		libDir := cleanUploadDir(lp.Path)
		if libDir == "" {
			continue
		}
		if isWithinDir(libDir, cleaned) {
			return &paths[i], cleaned, nil
		}
	}
	return nil, "", ErrUploadTargetNotInLibrary
}

// BuildUploadPath 按命名规则推导上传文件最终落盘路径（FR-149，见 ADR-0051）。
// baseDir 为目标目录（已校验在库内，正斜杠绝对路径）；originalName 为上传原始文件名；
// rule 为 original（直接落 baseDir）或 date（按 now 分 YYYY/MM 子目录归档）。
// 返回最终文件绝对路径（正斜杠）。仅做路径计算、不触磁盘，便于测试。
func BuildUploadPath(baseDir, originalName, rule string, now time.Time) (string, error) {
	name := sanitizeUploadName(originalName)
	if name == "" {
		return "", ErrInvalidUploadName
	}

	dir := baseDir
	if NormalizeUploadNamingRule(rule) == UploadNamingDate {
		// 按媒体时间分 年/月 子目录整齐归档
		dir = joinSlashPath(baseDir, now.Format("2006"))
		dir = joinSlashPath(dir, now.Format("01"))
	}
	return joinSlashPath(dir, name), nil
}

// ResolveUploadConflict 处理重名冲突（FR-149）：若 path 已存在，按「名(1).ext」「名(2).ext」递增直到不冲突。
// 经注入的 exists 谓词判断占用（生产传 os.Stat 包装），便于测试穷举冲突分支。
// 最多尝试 9999 次后仍冲突则返回错误（极端情况，正常不触达）。
func ResolveUploadConflict(path string, exists func(string) bool) (string, error) {
	if !exists(path) {
		return path, nil
	}
	dir := slashDir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for i := 1; i <= 9999; i++ {
		candidate := joinSlashPath(dir, fmt.Sprintf("%s(%d)%s", stem, i, ext))
		if !exists(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("重名冲突无法解决：%s", path)
}

// SaveUploadFile 将上传内容流式写入 destPath（FR-149）：创建所属目录、写临时文件后原子改名落位。
// 先写 destPath+".part" 再 rename，避免半截文件被扫描索引。写入失败尽力清理临时文件。
func SaveUploadFile(destPath string, src io.Reader) error {
	diskDest := filepath.FromSlash(destPath)
	if err := os.MkdirAll(filepath.Dir(diskDest), 0o750); err != nil {
		return fmt.Errorf("创建上传目录失败: %w", err)
	}

	tmpPath := diskDest + ".part"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("写入上传内容失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, diskDest); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("落位上传文件失败: %w", err)
	}
	return nil
}

// cleanUploadDir 规范化目录路径用于前缀比较：转正斜杠、去尾斜杠、Clean 解析 . 与 ..。
// 返回空表示路径为空。
func cleanUploadDir(p string) string {
	s := strings.TrimSpace(p)
	if s == "" {
		return ""
	}
	s = filepath.ToSlash(filepath.Clean(filepath.FromSlash(s)))
	return strings.TrimRight(s, "/")
}

// isWithinDir 判断 target 是否等于 base 或为其子目录（均为 cleanUploadDir 规范化后的正斜杠路径）。
// Windows 盘符大小写不敏感，故比较时统一小写。
func isWithinDir(base, target string) bool {
	b := strings.ToLower(base)
	t := strings.ToLower(target)
	if t == b {
		return true
	}
	return strings.HasPrefix(t, b+"/")
}

// sanitizeUploadName 提取上传文件名的安全单层文件名：取 base、去首尾空白，拒绝空/纯点/含分隔符遗留。
// 防止通过文件名中的路径片段（如 ../../x）逃逸目标目录。
func sanitizeUploadName(name string) string {
	// 统一分隔符后取最后一段，剥离任何路径前缀
	n := strings.ReplaceAll(name, `\`, "/")
	n = strings.TrimSpace(filepath.Base(filepath.FromSlash(n)))
	if n == "" || n == "." || n == ".." {
		return ""
	}
	return n
}

// slashDir 返回正斜杠路径的目录部分（不含尾斜杠）。
func slashDir(path string) string {
	return filepath.ToSlash(filepath.Dir(filepath.FromSlash(path)))
}
