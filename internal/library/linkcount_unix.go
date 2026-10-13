//go:build !windows

package library

import (
	"os"
	"syscall"
)

// LinkCount is the number of hard links to the file at path: above one, some
// other path (a torrent's copy, typically) shares its bytes.
func LinkCount(path string) (uint64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Nlink), nil //nolint:unconvert // Nlink is uint16 on darwin
	}
	return 1, nil
}
