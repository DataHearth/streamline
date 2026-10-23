//go:build windows

package sysinfo

import "golang.org/x/sys/windows"

// DiskUsageFor reports volume-level usage for the directory at path via
// GetDiskFreeSpaceEx. freeBytesAvailableToCaller matches the unix Bavail
// semantics (space available to the current, possibly unprivileged, user).
// Returns nil when the path can't be resolved or the call fails.
func DiskUsageFor(path string) *DiskUsage {
	if path == "" {
		return nil
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var freeToCaller, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(
		p,
		&freeToCaller,
		&totalBytes,
		&totalFree,
	); err != nil {
		return nil
	}
	return diskUsage(int64(totalBytes), int64(freeToCaller))
}
