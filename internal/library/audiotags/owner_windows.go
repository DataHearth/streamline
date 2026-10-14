//go:build windows

package audiotags

import "os"

// keepOwner is a no-op: a file's ACL on Windows follows its folder's.
func keepOwner(string, os.FileInfo) error { return nil }
