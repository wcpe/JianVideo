package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// oldSuffix 旧二进制备份后缀，用于回滚。
const oldSuffix = ".old"

// restartDelay 替换后给 HTTP 响应预留的时间，再退出当前进程触发重启。
var restartDelay = 800 * time.Millisecond

// osExit 进程退出钩子，便于测试替换（默认 os.Exit）。
var osExit = os.Exit

// replaceAndRestart 用 newPath 的新二进制替换当前可执行文件并重启。
// Windows 无法覆盖运行中的 exe，故：当前 exe 改名为 .old → 新文件移到原路径 → 启动新进程 → 退出当前。
func replaceAndRestart(newPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前可执行文件失败: %w", err)
	}
	if err := landBinary(exe, newPath); err != nil {
		return err
	}
	return spawnAndExit(exe)
}

// landBinary 把 newPath 的新二进制落地到 exePath：先把原 exe 备份为 .old，再把新文件移到原路径。
// 落地失败即就地回退（用 .old 恢复）。抽成纯文件操作（不含 os.Executable / 进程启动），便于单测
// 断言「落地后 exePath 内容 = 新二进制」（持久化正确，重启后仍为新版）。
func landBinary(exePath, newPath string) error {
	old := exePath + oldSuffix
	_ = os.Remove(old) // 清理上一次备份（若有）
	if err := os.Rename(exePath, old); err != nil {
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}
	if err := moveFile(newPath, exePath); err != nil {
		_ = os.Rename(old, exePath) // 落地失败即就地回退
		return fmt.Errorf("落地新二进制失败: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exePath, 0o755)
	}
	return nil
}

// rollbackAvailableAt 报告 exePath 是否存在 .old 备份（可回滚，FIX-2）。
func rollbackAvailableAt(exePath string) bool {
	_, err := os.Stat(exePath + oldSuffix)
	return err == nil
}

// rollback 用 .old 备份恢复上一版并重启。
func rollback() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前可执行文件失败: %w", err)
	}
	old := exe + oldSuffix
	if !rollbackAvailableAt(exe) {
		return fmt.Errorf("无可回滚的旧版本")
	}
	failed := exe + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(exe, failed); err != nil {
		return fmt.Errorf("移走当前二进制失败: %w", err)
	}
	if err := os.Rename(old, exe); err != nil {
		_ = os.Rename(failed, exe) // 恢复失败即就地回退
		return fmt.Errorf("恢复旧二进制失败: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}
	return spawnAndExit(exe)
}

// spawnAndExit 启动新二进制（继承参数/环境/工作目录/标准流）后，延时退出当前进程。
func spawnAndExit(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	cmd.Env = os.Environ()
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新进程失败: %w", err)
	}
	go func() {
		time.Sleep(restartDelay)
		osExit(0)
	}()
	return nil
}

// moveFile 移动文件，跨设备 rename 失败时退化为复制 + 删除。
// 注意：复制完成后必须先关闭 src 句柄再删除——Windows 下文件被本进程打开时无法删除，
// 若用 defer 关闭，os.Remove(src) 会在句柄仍开时执行而失败，进而让跨卷落地整体失败、
// 触发上层回退到旧二进制（即「在线更新重启后失效」的根因）。
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyAndRemove(src, dst)
}

// copyAndRemove 复制 src 到 dst 后删除 src（moveFile 的跨卷退化路径，独立便于直测）。
func copyAndRemove(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		in.Close()
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		in.Close()
		return err
	}
	// 先刷盘再关闭，保证新二进制完整落地到磁盘（重启后仍是新版）。
	if err := out.Sync(); err != nil {
		out.Close()
		in.Close()
		return err
	}
	if err := out.Close(); err != nil {
		in.Close()
		return err
	}
	// 关键：删除源前先关闭 src 句柄，否则 Windows 下删除失败导致整体落地失败。
	if err := in.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
