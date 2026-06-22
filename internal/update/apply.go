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
	old := exe + oldSuffix
	_ = os.Remove(old) // 清理上一次备份（若有）
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("备份当前二进制失败: %w", err)
	}
	if err := moveFile(newPath, exe); err != nil {
		_ = os.Rename(old, exe) // 落地失败即就地回退
		return fmt.Errorf("落地新二进制失败: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}
	return spawnAndExit(exe)
}

// rollback 用 .old 备份恢复上一版并重启。
func rollback() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前可执行文件失败: %w", err)
	}
	old := exe + oldSuffix
	if _, err := os.Stat(old); err != nil {
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
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
