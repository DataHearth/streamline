//go:build linux

package audiotags

import (
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// keptXattr names the extended attributes a write carries over: the user
// namespace and the POSIX ACLs, which decide who can read the file. The
// security and trusted namespaces (an SELinux label) need privileges to set,
// and the label a file gets in its folder is the policy's, not the file's.
func keptXattr(name string) bool {
	return strings.HasPrefix(name, "user.") ||
		name == "system.posix_acl_access"
}

// copyXattrs copies src's kept extended attributes onto dst. A filesystem
// without them has nothing to copy.
func copyXattrs(src, dst string) error {
	names, err := listXattrs(src)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, name := range names {
		if !keptXattr(name) {
			continue
		}
		val, err := getXattr(src, name)
		if err != nil {
			return err
		}
		if err := unix.Setxattr(dst, name, val, 0); err != nil {
			return err
		}
	}
	return nil
}

func listXattrs(path string) ([]string, error) {
	size, err := unix.Listxattr(path, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	buf := make([]byte, size)
	size, err = unix.Listxattr(path, buf)
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range strings.SplitSeq(string(buf[:size]), "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func getXattr(path, name string) ([]byte, error) {
	size, err := unix.Getxattr(path, name, nil)
	if err != nil || size == 0 {
		return nil, err
	}
	buf := make([]byte, size)
	size, err = unix.Getxattr(path, name, buf)
	if err != nil {
		return nil, err
	}
	return buf[:size], nil
}
