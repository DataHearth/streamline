package bittorrent

import (
	"os"

	"golang.org/x/sys/unix"
)

// CephFS reads ahead up to 8 MiB (its rasize default): one 16 KiB peer
// request would otherwise pull megabytes into the page cache.
func noReadahead(f *os.File) error {
	return unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_RANDOM)
}

// dropCached rounds outward: the kernel keeps every page the range only
// partly covers, so rounding inward would leave each page a request boundary
// splits cached for good.
func dropCached(f *os.File, off, n int64) error {
	page := int64(os.Getpagesize())
	start, end := off&^(page-1), (off+n+page-1)&^(page-1)
	return unix.Fadvise(int(f.Fd()), start, end-start, unix.FADV_DONTNEED)
}
