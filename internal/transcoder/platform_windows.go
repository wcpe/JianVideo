package transcoder

import "os/exec"

// setProcessGroup 在 Windows 上为空操作。
//
// Windows 不支持 Unix 风格的进程组（setpgid），因此此处不做任何设置。
// 进程终止由 exec.CommandContext 配合 WaitDelay 自动处理。
//
// 如需管理子进程生命周期，可使用 Windows Job Object API：
//
//	job, _ := windows.CreateJobObject(nil, nil)
//	windows.AssignProcessToJobObject(job, windows.Handle(cmd.Process.Pid))
//	// 终止时关闭 Job Object 即可杀死所有子进程
//	windows.CloseHandle(job)
//
// 需要导入 golang.org/x/sys/windows 包。
func setProcessGroup(_ *exec.Cmd) {
	// Windows 上无需设置进程组
}
