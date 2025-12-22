//go:build !windows
// +build !windows

package local

import "syscall"

// syncFilesystem syncs all filesystems on Unix-like systems.
func syncFilesystem() {
	syscall.Sync()
}
