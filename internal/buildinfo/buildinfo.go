// Package buildinfo holds the build-time identity of the running binary.
// Values default to the empty string; goreleaser populates them via ldflags
// (see .goreleaser.yaml). Code that needs to surface the version (CLI
// --version, settings page, error reports) reads from here so the same
// strings appear everywhere.
package buildinfo

// Injected by goreleaser via:
//
//	-ldflags "-X 'github.com/datahearth/streamline/internal/buildinfo.Version=...'"
var (
	Version = ""
	Commit  = ""
	Date    = ""
)
