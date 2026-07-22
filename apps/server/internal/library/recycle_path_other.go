//go:build !windows

package library

import "os"

func recyclePathIsReparsePoint(os.FileInfo) bool {
	return false
}

func openRecycleStableFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}

func linkRecycleStableFile(file *os.File, target string) error {
	return os.Link(file.Name(), target)
}

func recycleNeedsAtomicRestage() bool {
	return true
}

func replaceRecycleFile(tempPath, target string) error {
	return os.Rename(tempPath, target)
}
