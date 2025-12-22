//go:build windows
// +build windows

package local

// syncFilesystem is a no-op on Windows.
// Windows doesn't have an equivalent syscall.Sync() function.
// File handles are synced individually via FlushFileBuffers.
func syncFilesystem() {
	// No-op on Windows
}
