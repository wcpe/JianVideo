//go:build !windows

package transcoder

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 在 Unix 系统上设置进程组，确保 context 取消时能精确控制子进程组。
// 设计权衡：Setpgid=true 使得子进程脱离主进程组，可独立通过 kill(-pgid) 杀死整个进程组；
// 代价是主进程崩溃后子进程可能变成孤儿进程（未被 init 接管清理）。
// 由于转码场景中子进程必须被精确终止，此权衡可接受。
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// killProcessGroup 杀掉进程组。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return cmd.Process.Kill()
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}
