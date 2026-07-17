//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package library

import "os"

func lockRecycleFile(*os.File) error {
	return nil
}

func unlockRecycleFile(*os.File) error {
	return nil
}
