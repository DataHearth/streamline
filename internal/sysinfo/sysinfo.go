// Package sysinfo gathers the read-only environment summary surfaced by the
// Settings → General page and the GET /api/v1/system/info endpoint. Centralising
// it here keeps the two callers from drifting and avoids a cross-package import
// of unexported helpers.
package sysinfo

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/config"
)

// Snapshot is the read-only environment view. Pointer fields are nil when
// the underlying probe fails (statfs error, missing DB file, etc.).
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
	// LibraryDir/LibraryUsage describe where media actually lands, which is
	// normally a different mount from DataDir — reporting DataUsage as "free
	// space" tells the operator about the config volume, not the library.
	LibraryDir   string
	LibraryUsage *DiskUsage
	SeriesDir    string
	SeriesUsage  *DiskUsage
	Version      string
	Commit       string
	BuiltAt      string
	GoVersion    string
	GoOSArch     string

	// The settings below are file-only by design — the trust boundary, the
	// bootstrap block, and values the process generates for itself. They are
	// reported so the one screen an operator opens can say what is in force,
	// which is otherwise only knowable by reading the YAML on the host. None of
	// them is a secret: SeedAdminPassword and the session secret are reported as
	// a source, never a value. Admin-only, like the rest of this snapshot.
	ServerHost        string
	ServerPort        uint16
	ReadOnly          bool
	TrustedProxies    []string
	TrustedNetworks   []string
	TrustedRole       string
	SeedAdminEmail    string
	SeedAdminSecret   string
	SessionSecretFile string
	PlexClientID      string
	TorrentListenPort uint16
	TMDBAPIKeyFile    string
	TVDBAPIKeyFile    string
}

// secretSource names where a secret comes from without naming the secret:
// a file path wins over an inline value, and neither set is "unset". Used for
// the seed-admin password, whose inline form an older release may still have
// left in the config.
func secretSource(inline, file string) string {
	switch {
	case file != "":
		return "file"
	case inline != "":
		return "config"
	default:
		return "unset"
	}
}

// DiskUsage is the volume-level usage for a directory. Used / Total / Free
// are pre-formatted byte strings; Pct is 0–100 (rounded down). Kind is a
// coarse threshold marker for badge / progress-bar styling.
type DiskUsage struct {
	Used      string
	Total     string
	Free      string
	FreeBytes int64
	Pct       uint8
	Kind      string // "ok" (<70%), "warn" (70–90%), "err" (>=90%)
}

// displayVersion renders the ldflag value the way every surface shows it, so
// no caller prefixes a "v" of its own: goreleaser injects a bare semver
// ("1.3.0") while the image build passes the tag through ("v1.3.0"), and a
// plain go build leaves it empty.
//
// The "v" is only for a release. docker/metadata-action resolves its version
// output to the BRANCH NAME on a branch push, so every image built off main
// carries "main" — dressing that up as "vmain" reads like a release that does
// not exist. Anything not starting with a digit is shown as injected.
func displayVersion(v string) string {
	if v == "" {
		return "dev"
	}
	bare := strings.TrimPrefix(v, "v")
	if bare == "" || bare[0] < '0' || bare[0] > '9' {
		return v
	}
	return "v" + bare
}

// Collect returns the current environment snapshot.
func Collect() Snapshot {
	cfg := config.Get()
	publicURL := config.PublicURL()
	snap := Snapshot{
		AppName:      "Streamline",
		PublicURL:    publicURL,
		HTTPSWarn:    !strings.HasPrefix(strings.ToLower(publicURL), "https://"),
		AuthMode:     cfg.Auth.Mode,
		DataDir:      cfg.DataDir,
		DataUsage:    DiskUsageFor(cfg.DataDir),
		LibraryDir:   cfg.Library.MoviePath,
		LibraryUsage: DiskUsageFor(cfg.Library.MoviePath),
		SeriesDir:    cfg.Library.SeriesPath,
		SeriesUsage:  DiskUsageFor(cfg.Library.SeriesPath),
		DBPath:       cfg.DatabasePath(),
		Version:      displayVersion(buildinfo.Version),
		Commit:       buildinfo.Commit,
		BuiltAt:      buildinfo.Date,
		GoVersion:    runtime.Version(),
		GoOSArch:     runtime.GOOS + "/" + runtime.GOARCH,

		ServerHost:      cfg.Server.Host,
		ServerPort:      cfg.Server.Port,
		ReadOnly:        cfg.ReadOnly,
		TrustedProxies:  cfg.Server.TrustedProxies,
		TrustedNetworks: cfg.Auth.TrustedNetworks,
		TrustedRole:     cfg.Auth.TrustedRole,
		SeedAdminEmail:  cfg.Auth.SeedAdmin.Email,
		SeedAdminSecret: secretSource(
			cfg.Auth.SeedAdmin.Password,
			cfg.Auth.SeedAdmin.PasswordFile,
		),
		SessionSecretFile: cfg.Auth.SessionSecretFile,
		PlexClientID:      cfg.MediaServer.PlexClientID,
		TorrentListenPort: cfg.TorrentListenPort,
		TMDBAPIKeyFile:    cfg.Metadata.TMDBAPIKeyFile,
		TVDBAPIKeyFile:    cfg.Metadata.TVDBAPIKeyFile,
	}
	if st, err := os.Stat(cfg.DatabasePath()); err == nil {
		snap.DBSize = humanBytes(st.Size())
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

// humanBytes formats a byte count with binary units (KiB / MiB / GiB).
// Sub-kibibyte values render with the literal byte count.
func humanBytes(n int64) string {
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

// diskUsage builds a DiskUsage from raw volume byte totals. free is the space
// available to the current (unprivileged) user. Returns nil for a bogus total.
// DiskUsageFor is provided per-platform (diskusage_{unix,windows}.go).
func diskUsage(total, free int64) *DiskUsage {
	if total <= 0 {
		return nil
	}
	// Clamp the reported free figure into [0, total] before anything is
	// derived from it, so every number the badge shows agrees with the
	// others. Root-reserved blocks can push free above the unprivileged
	// total, and an unsigned Bavail that overflowed int64 arrives negative;
	// either way, rendering the raw value produces a nonsense "-524288000 B"
	// or a Free larger than Total, and the used it implies wraps the
	// percentage. Clamping once here is what keeps used + free == total.
	free = min(max(free, 0), total)
	used := total - free
	// used*100 overflows int64 past ~92PB and the wrap would read as 0% on
	// a nearly-full volume; only that range takes the divide-first
	// approximation, so ordinary volumes keep the exact arithmetic. used is
	// now bounded by total, so reaching that range implies total/100 > 0 and
	// the division is safe. The upper clamp stays an explicit branch so
	// gosec's range analysis can see the uint8 narrowing is safe.
	var pctWide int64
	if used <= math.MaxInt64/100 {
		pctWide = used * 100 / total
	} else {
		pctWide = used / (total / 100)
	}
	if pctWide > 100 {
		pctWide = 100
	}
	pct := uint8(pctWide)
	kind := "ok"
	switch {
	case pct >= 90:
		kind = "err"
	case pct >= 70:
		kind = "warn"
	}
	return &DiskUsage{
		Used:      humanBytes(used),
		Total:     humanBytes(total),
		Free:      humanBytes(free),
		FreeBytes: free,
		Pct:       pct,
		Kind:      kind,
	}
}
