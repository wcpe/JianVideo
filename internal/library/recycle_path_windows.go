//go:build windows

package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type recycleFileLinkInfo struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

func recyclePathIsReparsePoint(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func openRecycleStableFile(path string, lockDelete bool) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	shareMode := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	if lockDelete {
		shareMode = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE
	}
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		shareMode,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}

func linkRecycleStableFile(file *os.File, target string) error {
	name, err := recycleNTPath(target)
	if err != nil {
		return err
	}
	nameUTF16, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	nameUnits := len(nameUTF16) - 1
	if nameUnits <= 0 {
		return errors.New("Win32 硬链接目标名称不能为空")
	}
	const maxUint32 = uint64(^uint32(0))
	nameLength64 := uint64(nameUnits) * 2
	if nameLength64%2 != 0 || nameLength64 > maxUint32 {
		return errors.New("Win32 硬链接目标名称长度越界")
	}
	var template recycleFileLinkInfo
	headerSize := int(unsafe.Offsetof(template.FileName))
	bufferSize64 := uint64(headerSize) + nameLength64
	if bufferSize64 <= uint64(headerSize) || bufferSize64 > maxUint32 || (strconv.IntSize == 32 && bufferSize64 > 1<<31-1) {
		return errors.New("Win32 硬链接信息缓冲区长度越界")
	}
	nameLength := nameUnits * 2
	bufferSize := headerSize + nameLength
	buffer := make([]byte, bufferSize)
	if len(buffer) <= headerSize || len(buffer)-headerSize != nameLength || uint64(len(buffer)) > maxUint32 {
		return errors.New("Win32 硬链接信息缓冲区边界无效")
	}
	// #nosec G103 -- Win32 FILE_LINK_INFORMATION 要求把已校验缓冲区映射为系统结构。
	info := (*recycleFileLinkInfo)(unsafe.Pointer(&buffer[0]))
	// #nosec G115 -- nameLength 已校验为正偶数且不超过 uint32 上限。
	info.FileNameLength = uint32(nameLength)
	// #nosec G103 -- 变长 UTF-16 文件名区域已由缓冲区边界与长度检查限定。
	nameBuffer := unsafe.Slice((*uint16)(unsafe.Pointer(&info.FileName[0])), nameUnits)
	copy(nameBuffer, nameUTF16)
	// #nosec G115 -- len(buffer) 已校验不超过 Win32 uint32 长度上限。
	bufferLength := uint32(len(buffer))
	var status windows.IO_STATUS_BLOCK
	err = windows.NtSetInformationFile(
		windows.Handle(file.Fd()),
		&status,
		&buffer[0],
		bufferLength,
		windows.FileLinkInformation,
	)
	runtime.KeepAlive(file)
	if err != nil {
		return fmt.Errorf("从稳定文件句柄创建硬链接失败: %w", err)
	}
	return nil
}

func recycleNTPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(absolute, `\\`) {
		return `\??\UNC\` + strings.TrimPrefix(absolute, `\\`), nil
	}
	return `\??\` + absolute, nil
}

func lockRecycleFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		^uint32(0),
		^uint32(0),
		&overlapped,
	)
}

func unlockRecycleFile(file *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, ^uint32(0), ^uint32(0), &overlapped)
}

func recycleNeedsAtomicRestage() bool {
	return false
}

func replaceRecycleFile(tempPath, target string) error {
	temp, err := windows.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	dest, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(temp, dest, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
