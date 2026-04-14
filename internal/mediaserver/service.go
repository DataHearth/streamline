// Package mediaserver: Manager owns connection testing + Plex PIN auth for
// Plex / Jellyfin / Emby entries. CRUD over the entries lives in the YAML
// config (config.AddMediaServer etc.); handlers call those directly. The
// Dispatcher (this package) owns runtime fan-out.
package mediaserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/datahearth/streamline/internal/config"
)

// ephemeralClientID is a process-stable Plex client identifier used when none
// is configured. Read-only deploys can't persist a generated id, so this keeps
// begin/poll consistent within a process and is surfaced to the operator to
// commit into media_server.plex_client_id.
var ephemeralClientID = sync.OnceValue(func() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
})

// plexClientID returns the configured Plex client id, or a process-stable
// ephemeral one when config has none (e.g. a read-only deploy that never
// persisted it).
func plexClientID() string {
	if id := config.Get().MediaServer.PlexClientID; id != "" {
		return id
	}
	return ephemeralClientID()
}

// Manager is the consumer-facing surface used by HTTP handlers for behavioral
// operations. Persistence of media-server entries is via config mutate.
type Manager interface {
	Test(ctx context.Context, p TestParams) error
	TestByName(ctx context.Context, name string) error
	DiscoverSections(ctx context.Context, p TestParams) ([]Section, error)
	// BeginPlexPin starts a Plex PIN OAuth flow. Caller opens PlexPin.AuthURL
	// in a browser popup; admin authenticates with Plex; caller polls
	// PollPlexPin(pin.ID) until AuthToken is non-empty (5-minute window).
	BeginPlexPin(ctx context.Context) (PlexPin, error)
	PollPlexPin(ctx context.Context, pinID uint64) (PlexPinResult, error)
}

type TestParams struct {
	ServerType string
	Host       string
	APIKey     string
}

type service struct{}

func New() Manager {
	return &service{}
}

func (s *service) Test(ctx context.Context, p TestParams) error {
	srv, err := buildServer(p.ServerType, p.Host, p.APIKey)
	if err != nil {
		return err
	}
	if err := srv.TestConnection(ctx); err != nil {
		return fmt.Errorf("%w: %w", ErrTestFailed, err)
	}
	return nil
}

func (s *service) TestByName(ctx context.Context, name string) error {
	ms, ok := config.FindMediaServer(name)
	if !ok {
		return ErrServerNotFound
	}
	return s.Test(ctx, TestParams{
		ServerType: ms.ServerType,
		Host:       ms.Host,
		APIKey:     config.SecretValue(ms.APIKey, ms.APIKeyFile),
	})
}

func (s *service) DiscoverSections(
	ctx context.Context,
	p TestParams,
) ([]Section, error) {
	if p.ServerType != "plex" {
		return nil, nil
	}
	if !isValidServerType(p.ServerType) {
		return nil, ErrInvalidServerType
	}
	return NewPlex(p.Host, p.APIKey).ListSections(ctx)
}

func isValidServerType(t string) bool {
	switch t {
	case "plex", "jellyfin", "emby":
		return true
	}
	return false
}

func (s *service) BeginPlexPin(ctx context.Context) (PlexPin, error) {
	creds := plexCreds()
	pin, err := beginPlexPin(ctx, plexTVBaseURL, creds)
	if err != nil {
		return PlexPin{}, err
	}
	pin.ClientID = creds.ClientID
	return pin, nil
}

func (s *service) PollPlexPin(
	ctx context.Context,
	pinID uint64,
) (PlexPinResult, error) {
	return pollPlexPin(ctx, plexTVBaseURL, plexCreds(), pinID)
}

// plexCreds assembles the per-install Plex client identity (see plexClientID)
// plus the build-info version.
func plexCreds() PlexClientCreds {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" &&
		info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	return PlexClientCreds{
		ClientID: plexClientID(),
		Product:  "Streamline",
		Version:  version,
	}
}
