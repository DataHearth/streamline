//go:build unix

package sysinfo

import "syscall"

// DiskUsageFor reports volume-level usage for the directory at path via statfs.
// Returns nil when statfs fails (missing dir, permission errors). Bsize is
// converted to int64 explicitly: it is typed int64 on linux but uint32 on
// darwin/openbsd, and Bavail is signed on freebsd — the conversions normalize
// all of them so this compiles across every unix goreleaser target.
func DiskUsageFor(path string) *DiskUsage {
	if path == "" {
		return nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil
	}
	//nolint:unconvert // Bsize is int64 on linux (where the linter runs) but uint32 on darwin/openbsd; the conversion is required to cross-compile.
	bsize := int64(st.Bsize)
	return diskUsage(int64(st.Blocks)*bsize, int64(st.Bavail)*bsize)
}
