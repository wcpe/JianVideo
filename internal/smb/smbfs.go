package smb

import (
	"io/fs"
	"os"
	"strings"

	smb2 "github.com/cloudsoda/go-smb2"
)

// FS 包装 SMB Share 实现 io/fs.FS 接口。
type FS struct {
	share *smb2.Share
}

// NewFS 从 SMB 共享创建文件系统。
func NewFS(share *smb2.Share) *FS {
	return &FS{share: share}
}

// Open 打开文件。
func (f *FS) Open(name string) (fs.File, error) {
	name = normalize(name)
	file, err := f.share.Open(name)
	if err != nil {
		return nil, mapSMBError(err)
	}
	return &smbFile{File: file}, nil
}

// ReadDir 读取目录内容。
func (f *FS) ReadDir(name string) ([]fs.DirEntry, error) {
	name = normalize(name)
	entries, err := f.share.ReadDir(name)
	if err != nil {
		return nil, mapSMBError(err)
	}
	dirEntries := make([]fs.DirEntry, 0, len(entries))
	for _, e := range entries {
		dirEntries = append(dirEntries, &smbDirEntry{FileInfo: e})
	}
	return dirEntries, nil
}

// Stat 获取文件信息。
func (f *FS) Stat(name string) (fs.FileInfo, error) {
	name = normalize(name)
	info, err := f.share.Stat(name)
	if err != nil {
		return nil, mapSMBError(err)
	}
	return info, nil
}

// smbFile 包装 smb2.File 实现 fs.File 接口。
// smb2.File 的 Readdir 返回 []os.FileInfo，需转为 []fs.DirEntry。
type smbFile struct {
	*smb2.File
}

// Readdir 将 smb2.File 的 []os.FileInfo 转为 []fs.DirEntry。
func (f *smbFile) Readdir(n int) ([]fs.DirEntry, error) {
	infos, err := f.File.Readdir(n)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, 0, len(infos))
	for _, info := range infos {
		entries = append(entries, &smbDirEntry{FileInfo: info})
	}
	return entries, nil
}

// smbDirEntry 包装 os.FileInfo 实现 fs.DirEntry 接口。
type smbDirEntry struct {
	fs.FileInfo
}

// Type 返回文件模式类型位。
func (e *smbDirEntry) Type() fs.FileMode {
	return e.FileInfo.Mode() & fs.ModeType
}

// Info 返回 FileInfo。
func (e *smbDirEntry) Info() (fs.FileInfo, error) {
	return e.FileInfo, nil
}

// normalize 清理路径：移除前导斜杠和反斜杠，统一为正斜杠。
func normalize(name string) string {
	if name == "." {
		name = ""
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	return name
}

// mapSMBError 将 SMB 错误映射为 fs 标准错误。
func mapSMBError(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return fs.ErrNotExist
	}
	if os.IsPermission(err) {
		return fs.ErrPermission
	}
	return err
}
