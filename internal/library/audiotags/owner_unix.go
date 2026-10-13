//go:build unix

package audiotags

import (
	"os"
	"syscall"
)

// keepOwner gives path the owner and group st carries. A copy is created
// owned by Streamline's user, so this is a no-op unless the original belongs
// to someone else; changing the owner needs the privilege to, and changing
// the group membership of it.
func keepOwner(path string, st os.FileInfo) error {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	cur, err := os.Stat(path)
	if err != nil {
		return err
	}
	if now, ok := cur.Sys().(*syscall.Stat_t); ok &&
		now.Uid == sys.Uid && now.Gid == sys.Gid {
		return nil
	}
	return os.Chown(path, int(sys.Uid), int(sys.Gid))
}
