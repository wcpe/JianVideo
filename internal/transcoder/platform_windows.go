package transcoder

import "os/exec"

// setProcessGroup 在 Windows 上为空操作（Windows 使用 Job Object 管理进程组）。
func setProcessGroup(cmd *exec.Cmd) {
	// Windows 上通过 TerminateProcess 处理，参见 killProcessGroup
}

// killProcessGroup 在 Windows 上直接杀主进程。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
