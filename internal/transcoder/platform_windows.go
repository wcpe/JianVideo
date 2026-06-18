package transcoder

import "os/exec"

// setProcessGroup 在 Windows 上为空操作。
//
// Windows 不支持 Unix 风格的进程组（setpgid），因此此处不做任何设置。
// 进程终止由 killProcessGroup 通过 TerminateProcess 实现。
//
// 如需管理子进程生命周期，可使用 Windows Job Object API：
//
//	job, _ := windows.CreateJobObject(nil, nil)
//	windows.AssignProcessToJobObject(job, windows.Handle(cmd.Process.Pid))
//	// 终止时关闭 Job Object 即可杀死所有子进程
//	windows.CloseHandle(job)
//
// 需要导入 golang.org/x/sys/windows 包。
func setProcessGroup(cmd *exec.Cmd) {
	// Windows 上通过 TerminateProcess 处理，参见 killProcessGroup
}

// killProcessGroup 在 Windows 上直接杀主进程。
//
// 注意：Windows 上仅终止主进程（cmd.Process），子进程不会自动终止。
// 若需同时终止所有子进程，建议使用 Windows Job Object（参见 setProcessGroup 注释）。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
