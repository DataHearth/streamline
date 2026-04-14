// Package sysinfo gathers the read-only environment summary surfaced by the
// Settings → General page and the GET /api/v1/system/info endpoint. Centralising
// it here keeps the two callers from drifting and avoids a cross-package import
// of unexported helpers.
package sysinfo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/config"
)

// Snapshot is the read-only environment view. Pointer fields are nil when
// the underlying probe fails (non-Linux statfs, missing DB file, etc.).
type Snapshot struct {
	AppName   string
	PublicURL string
	HTTPSWarn bool
	AuthMode  string
	DataDir   string
	DataUsage *DiskUsage
	DBPath    string
	DBSize    string
	DBUsage   *DiskUsage
	Version   string
	Commit    string
	BuiltAt   string
	GoVersion string
	GoOSArch  string
}

// DiskUsage is the volume-level usage for a directory. Used / Total / Free
// are pre-formatted byte strings; Pct is 0–100 (rounded down). Kind is a
// coarse threshold marker for badge / progress-bar styling.
type DiskUsage struct {
	Used  string
	Total string
	Free  string
	Pct   uint8
	Kind  string // "ok" (<70%), "warn" (70–90%), "err" (>=90%)
}

// Collect returns the current environment snapshot.
func Collect() Snapshot {
	cfg := config.Get()
	publicURL := config.PublicURL()
	snap := Snapshot{
		AppName:   "Streamline",
		PublicURL: publicURL,
		HTTPSWarn: !strings.HasPrefix(strings.ToLower(publicURL), "https://"),
		AuthMode:  cfg.Auth.Mode,
		DataDir:   cfg.DataDir,
		DataUsage: DiskUsageFor(cfg.DataDir),
		DBPath:    cfg.DatabasePath(),
		Version:   buildinfo.Version,
		Commit:    buildinfo.Commit,
		BuiltAt:   buildinfo.Date,
		GoVersion: runtime.Version(),
		GoOSArch:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	if snap.Version == "" {
		snap.Version = "dev"
	}
	if st, err := os.Stat(cfg.DatabasePath()); err == nil {
		snap.DBSize = HumanBytes(st.Size())
	}
	snap.DBUsage = DiskUsageFor(filepath.Dir(cfg.DatabasePath()))
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				if snap.Commit == "" {
					snap.Commit = s.Value
				}
			case "vcs.time":
				if snap.BuiltAt == "" {
					snap.BuiltAt = s.Value
				}
			}
		}
	}
	if len(snap.Commit) > 7 {
		snap.Commit = snap.Commit[:7]
	}
	return snap
}

// HumanBytes formats a byte count with binary units (KiB / MiB / GiB).
// Sub-kibibyte values render with the literal byte count.
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// DiskUsageFor reports volume-level usage for the directory at path. Returns
// nil when statfs fails (non-Linux, missing dir, permission errors).
func DiskUsageFor(path string) *DiskUsage {
	if path == "" {
		return nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil
	}
	bsize := st.Bsize
	total := int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize
	if total <= 0 {
		return nil
	}
	used := total - free
	pct := uint8(used * 100 / total)
	kind := "ok"
	switch {
	case pct >= 90:
		kind = "err"
	case pct >= 70:
		kind = "warn"
	}
	return &DiskUsage{
		Used:  HumanBytes(used),
		Total: HumanBytes(total),
		Free:  HumanBytes(free),
		Pct:   pct,
		Kind:  kind,
	}
}
