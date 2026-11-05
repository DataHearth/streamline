//go:build !linux

package bittorrent

import "os"

func noReadahead(*os.File) error { return nil }

func dropCached(*os.File, int64, int64) error { return nil }
