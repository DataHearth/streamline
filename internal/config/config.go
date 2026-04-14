package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Server  ServerConfig `koanf:"server"   validate:"required"`
	DataDir string       `koanf:"data_dir" validate:"required,dir"`
	// ReadOnly rejects every config.Update write-back with ErrReadOnly. For
	// declarative/GitOps deploys where config is mounted read-only and changes
	// flow through git, not the UI.
	ReadOnly    bool              `koanf:"read_only"`
	Auth        AuthConfig        `koanf:"auth"         validate:"required"`
	Library     LibraryConfig     `koanf:"library"      validate:"required"`
	Schedule    ScheduleConfig    `koanf:"schedules"    validate:"required"`
	Metadata    MetadataConfig    `koanf:"metadata"`
	Log         LogConfig         `koanf:"log"          validate:"required"`
	OTel        OTelConfig        `koanf:"otel"`
	MediaServer MediaServerConfig `koanf:"media_server"`
	Events      EventsConfig      `koanf:"events"       validate:"required"`

	DownloadClients       []DownloadClientEntry `koanf:"download_clients"        validate:"unique=Name,dive"`
	Indexers              []IndexerEntry        `koanf:"indexers"                validate:"unique=Name,dive"`
	QualityProfiles       []QualityProfileEntry `koanf:"quality_profiles"        validate:"unique=Name,dive"`
	QualityDefaultProfile string                `koanf:"quality_default_profile"`
}

// DatabasePath is the SQLite database location, derived from DataDir.
func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "streamline.db")
}

type ServerConfig struct {
	Host string `koanf:"host" validate:"required,ip|hostname"`
	Port uint16 `koanf:"port" validate:"required,port"`
}

type AuthConfig struct {
	Mode              string        `koanf:"mode"                validate:"required,oneof=full trusted-network disabled"`
	TrustedNetworks   []string      `koanf:"trusted_networks"    validate:"dive,cidr"`
	TrustedRole       string        `koanf:"trusted_role"        validate:"required,oneof=admin member request_only"`
	SessionSecret     string        `koanf:"session_secret"      validate:"excluded_with=SessionSecretFile"`
	SessionSecretFile string        `koanf:"session_secret_file" validate:"omitempty,excluded_with=SessionSecret,filepath"`
	SessionTTL        string        `koanf:"session_ttl"         validate:"required"`
	RegistrationMode  string        `koanf:"registration_mode"   validate:"required,oneof=disabled open invite"`
	OIDCDefaultRole   string        `koanf:"oidc_default_role"   validate:"required,oneof=admin member request_only"`
	SeedAdmin         SeedAdminCfg  `koanf:"seed_admin"`
	OIDC              []OIDCConfig  `koanf:"oidc"                validate:"dive"`
	Lockout           LockoutConfig `koanf:"lockout"             validate:"required"`
}

// LockoutConfig governs the per-account login-failure lockout. Threshold is
// the failed-attempt count that locks the account; Window is the sliding
// window over which failures accumulate; Duration is how long the lockout
// holds before auto-expiry.
type LockoutConfig struct {
	Threshold uint8  `koanf:"threshold" validate:"required,min=1,max=255"`
	Window    string `koanf:"window"    validate:"required"`
	Duration  string `koanf:"duration"  validate:"required"`
}

type SeedAdminCfg struct {
	Email        string `koanf:"email"         validate:"omitempty,email"`
	Password     string `koanf:"password"      validate:"excluded_with=PasswordFile"`
	PasswordFile string `koanf:"password_file" validate:"omitempty,excluded_with=Password,filepath"`
}

type OIDCConfig struct {
	Name         string `koanf:"name"          validate:"required"`
	Issuer       string `koanf:"issuer"        validate:"required,url"`
	ClientID     string `koanf:"client_id"     validate:"required"`
	ClientSecret string `koanf:"client_secret" validate:"required_without=ClientSecretFile,excluded_with=ClientSecretFile"`
	// ClientSecretFile, when set, is read into ClientSecret after validation.
	// Mutually exclusive with ClientSecret (set exactly one). Lets a GitOps
	// deploy mount the secret from a k8s Secret instead of inlining it.
	ClientSecretFile string `koanf:"client_secret_file" validate:"omitempty,excluded_with=ClientSecret,filepath"`
	// RoleClaim is the ID-token claim (e.g. "groups", "roles") consulted for
	// claim-based role mapping; RoleMapping maps a claim value to a Streamline
	// role. When both are set and a value matches, the mapped role is
	// authoritative — applied at signup and re-synced on every login (the
	// highest-privilege role wins if several values map). Leave empty to give
	// OIDC users auth.oidc_default_role.
	RoleClaim   string            `koanf:"role_claim"`
	RoleMapping map[string]string `koanf:"role_mapping" validate:"omitempty,dive,oneof=admin member request_only"`
}

type LibraryConfig struct {
	MoviePath    string `koanf:"movie_path"    validate:"required"`
	MovieNaming  string `koanf:"movie_naming"  validate:"required"`
	SeriesPath   string `koanf:"series_path"   validate:"required"`
	SeriesNaming string `koanf:"series_naming" validate:"required"`
	// DownloadPath is the host-side directory where streamline reads
	// completed torrents from. qBittorrent (or any other client) decides
	// its own save location; this only tells the importer where to look,
	// per-torrent content lives at <DownloadPath>/<torrent.Name>. The
	// directory is not validated at boot — it may be a bind-mount that
	// comes up after streamline; the importer surfaces a clear stat error
	// if the path is wrong.
	DownloadPath         string   `koanf:"download_path"          validate:"required"`
	ImportMode           string   `koanf:"import_mode"            validate:"required,oneof=hardlink copy move"`
	NoMatchCooldown      string   `koanf:"no_match_cooldown"      validate:"required"`
	MaxGrabFailures      uint8    `koanf:"max_grab_failures"      validate:"required,min=1"`
	KeepTorrentSeeding   bool     `koanf:"keep_torrent_seeding"`
	ImportMaxAttempts    uint8    `koanf:"import_max_attempts"    validate:"required,min=1"`
	AllowedDownloadRoots []string `koanf:"allowed_download_roots"`
	// DriftGraceTicks is the number of consecutive drift_check ticks a
	// MediaFile may be absent from disk before its row is deleted and the
	// owning movie reverts to "wanted". Bounded to give operators a knob
	// for noisy mounts without unbounded patience.
	DriftGraceTicks uint8 `koanf:"drift_grace_ticks" validate:"required,min=1,max=20"`
}

type ScheduleConfig struct {
	RSSSync         string `koanf:"rss_sync"         validate:"required"`
	MetadataRefresh string `koanf:"metadata_refresh" validate:"required"`
	DownloadMonitor string `koanf:"download_monitor" validate:"required"`
	MissingSearch   string `koanf:"missing_search"   validate:"required"`
	Cleanup         string `koanf:"cleanup"          validate:"required"`
	ImportScan      string `koanf:"import_scan"      validate:"required"`
	OrphanScan      string `koanf:"orphan_scan"      validate:"required"`
	DriftCheck      string `koanf:"drift_check"      validate:"required"`
}

type MetadataConfig struct {
	TMDBAPIKey     string `koanf:"tmdb_api_key"      validate:"excluded_with=TMDBAPIKeyFile"`
	TMDBAPIKeyFile string `koanf:"tmdb_api_key_file" validate:"omitempty,excluded_with=TMDBAPIKey,filepath"`
	TVDBAPIKey     string `koanf:"tvdb_api_key"      validate:"excluded_with=TVDBAPIKeyFile"`
	TVDBAPIKeyFile string `koanf:"tvdb_api_key_file" validate:"omitempty,excluded_with=TVDBAPIKey,filepath"`
	Language       string `koanf:"language"          validate:"omitempty,bcp47_language_tag"`
	TMDBRegion     string `koanf:"tmdb_region"       validate:"omitempty,len=2,uppercase"`
}

// EventsConfig governs the MovieEvent retention window. Old rows are deleted
// by the cleanup job after Retention.
type EventsConfig struct {
	Retention string `koanf:"retention" validate:"required"`
}

type LogConfig struct {
	App  AppLog  `koanf:"app"  validate:"required"`
	HTTP HTTPLog `koanf:"http" validate:"required"`
}

type AppLog struct {
	Enabled bool      `koanf:"enabled"`
	Level   string    `koanf:"level"   validate:"required,oneof=debug info warn error"`
	Format  string    `koanf:"format"  validate:"required,oneof=text json"`
	Output  string    `koanf:"output"  validate:"required"`
	Rotate  LogRotate `koanf:"rotate"  validate:"required"`
}

type HTTPLog struct {
	Enabled bool      `koanf:"enabled"`
	Output  string    `koanf:"output"  validate:"required"`
	Format  string    `koanf:"format"  validate:"required,oneof=json combined"`
	Rotate  LogRotate `koanf:"rotate"  validate:"required"`
}

type LogRotate struct {
	MaxSizeMB  int  `koanf:"max_size_mb"  validate:"min=0"`
	MaxBackups int  `koanf:"max_backups"  validate:"min=0"`
	MaxAgeDays int  `koanf:"max_age_days" validate:"min=0"`
	Compress   bool `koanf:"compress"`
}

type OTelConfig struct {
	Endpoint string `koanf:"endpoint"`
}

// MediaServerConfig holds media-server integration identifiers. PlexClientID
// is generated and persisted the first time a Plex server is configured (see
// EnsurePlexClientID); required by the Plex PIN OAuth flow as the
// X-Plex-Client-Identifier header value.
type MediaServerConfig struct {
	PlexClientID string             `koanf:"plex_client_id"`
	Servers      []MediaServerEntry `koanf:"servers"        validate:"unique=Name,dive"`
}

func (c *Config) Validate() error {
	if err := validator.New().Struct(c); err != nil {
		return err
	}
	if len(c.QualityProfiles) > 0 {
		found := false
		for _, p := range c.QualityProfiles {
			if p.Name == c.QualityDefaultProfile {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf(
				"quality_default_profile %q names no profile in quality_profiles",
				c.QualityDefaultProfile,
			)
		}
	}
	return nil
}

// defaults returns the canonical default values for all config keys.
// Keys use koanf's dotted notation.
func defaults() map[string]any {
	return map[string]any{
		"server.host":                    "0.0.0.0",
		"server.port":                    8080,
		"data_dir":                       "./data",
		"read_only":                      false,
		"auth.mode":                      "full",
		"auth.trusted_networks":          []string{},
		"auth.trusted_role":              "admin",
		"auth.session_secret":            "",
		"auth.session_secret_file":       "",
		"auth.session_ttl":               "168h",
		"auth.registration_mode":         "disabled",
		"auth.oidc_default_role":         "member",
		"auth.seed_admin.email":          "",
		"auth.seed_admin.password":       "",
		"auth.seed_admin.password_file":  "",
		"auth.oidc":                      []any{},
		"auth.lockout.threshold":         10,
		"auth.lockout.window":            "15m",
		"auth.lockout.duration":          "15m",
		"library.movie_path":             "/media/movies",
		"library.series_path":            "/media/series",
		"library.series_naming":          "{title} ({year})/Season {season}/{title} - S{season:2}E{episode:2} - {episode_title} [{quality}].{ext}",
		"library.download_path":          "/downloads",
		"library.movie_naming":           "{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}",
		"library.import_mode":            "hardlink",
		"library.keep_torrent_seeding":   true,
		"library.import_max_attempts":    3,
		"library.allowed_download_roots": []string{},
		"library.no_match_cooldown":      "6h",
		"library.max_grab_failures":      3,
		"schedules.rss_sync":             "15m",
		"schedules.metadata_refresh":     "24h",
		"schedules.download_monitor":     "30s",
		"schedules.missing_search":       "12h",
		"schedules.cleanup":              "24h",
		"schedules.import_scan":          "60s",
		"schedules.orphan_scan":          "6h",
		"schedules.drift_check":          "15m",
		"library.drift_grace_ticks":      3,
		"metadata.tmdb_api_key":          "",
		"metadata.tmdb_api_key_file":     "",
		"metadata.tvdb_api_key":          "",
		"metadata.tvdb_api_key_file":     "",
		"metadata.language":              "en",
		"metadata.tmdb_region":           "FR",
		"otel.endpoint":                  "",
		"media_server.plex_client_id":    "",
		"media_server.servers":           []any{},
		"download_clients":               []any{},
		"indexers":                       []any{},
		"quality_profiles": []map[string]any{
			{
				"name":                 "default",
				"preferred_resolution": "1080p",
				"min_resolution":       "1080p",
				"upgrade_allowed":      true,
			},
		},
		"quality_default_profile":      "default",
		"events.retention":             "2160h",
		"log.app.enabled":              true,
		"log.app.level":                "info",
		"log.app.format":               "text",
		"log.app.output":               "stderr",
		"log.app.rotate.max_size_mb":   100,
		"log.app.rotate.max_backups":   5,
		"log.app.rotate.max_age_days":  30,
		"log.app.rotate.compress":      true,
		"log.http.enabled":             true,
		"log.http.output":              "stderr",
		"log.http.format":              "json",
		"log.http.rotate.max_size_mb":  100,
		"log.http.rotate.max_backups":  5,
		"log.http.rotate.max_age_days": 30,
		"log.http.rotate.compress":     true,
	}
}

// DumpDefaults writes the default configuration as YAML to w.
// Output is a ready-to-use config file; running `config validate` on it
// should succeed once data_dir exists on disk.
func DumpDefaults(w io.Writer) error {
	k := newDefaultsKoanf()
	out, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// LoadReader reads YAML config from r, overlays env vars, validates, and
// stores the resulting Config in the singleton (access via Get).
func LoadReader(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	k := newDefaultsKoanf()
	if len(raw) > 0 {
		if err := k.Load(rawbytes.Provider(raw), yaml.Parser()); err != nil {
			return err
		}
	}
	cfg, err := finalize(k)
	if err != nil {
		return err
	}
	store(cfg, "")
	return nil
}

func newDefaultsKoanf() *koanf.Koanf {
	k := koanf.New(".")
	for key, val := range defaults() {
		_ = k.Set(key, val)
	}
	return k
}

func finalize(k *koanf.Koanf) (*Config, error) {
	// Double-underscore is the path separator; a single underscore is literal
	// so keys with underscore segments (data_dir, session_secret, tmdb_api_key)
	// stay reachable: STREAMLINE_AUTH__SESSION_SECRET -> auth.session_secret.
	envProvider := env.Provider("STREAMLINE_", ".", func(s string) string {
		key := strings.ToLower(strings.TrimPrefix(s, "STREAMLINE_"))
		return strings.ReplaceAll(key, "__", ".")
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	m, err := loadSecretFiles(&cfg)
	if err != nil {
		return nil, err
	}
	secretFiles.Store(&m)
	return &cfg, nil
}

// secretFiles caches the trimmed contents of every *_file secret reference,
// keyed by path. Rebuilt on each Load; lock-free reads via atomic.Pointer.
var secretFiles atomic.Pointer[map[string]string]

// SecretValue returns the effective secret for an inline/file pair: the cached
// contents of file when a *_file path is set, otherwise the inline value. The
// pair is validated mutually exclusive (excluded_with), so at most one is ever
// set. Use this instead of reading the inline field directly so file-backed
// secrets resolve.
func SecretValue(inline, file string) string {
	if file == "" {
		return inline
	}
	if m := secretFiles.Load(); m != nil {
		return (*m)[file]
	}
	return ""
}

// loadSecretFiles reads every *_file path referenced by c into a path->content
// map. Fails fast if any referenced file is unreadable.
func loadSecretFiles(c *Config) (map[string]string, error) {
	m := map[string]string{}
	read := func(path string) error {
		if path == "" {
			return nil
		}
		if _, ok := m[path]; ok {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read secret file %q: %w", path, err)
		}
		m[path] = strings.TrimSpace(string(b))
		return nil
	}

	for _, p := range []string{
		c.Auth.SessionSecretFile,
		c.Metadata.TMDBAPIKeyFile,
		c.Metadata.TVDBAPIKeyFile,
	} {
		if err := read(p); err != nil {
			return nil, err
		}
	}
	for _, o := range c.Auth.OIDC {
		if err := read(o.ClientSecretFile); err != nil {
			return nil, err
		}
	}
	for _, x := range c.Indexers {
		if err := read(x.APIKeyFile); err != nil {
			return nil, err
		}
	}
	for _, d := range c.DownloadClients {
		if err := read(d.PasswordFile); err != nil {
			return nil, err
		}
		if err := read(d.APIKeyFile); err != nil {
			return nil, err
		}
	}
	for _, s := range c.MediaServer.Servers {
		if err := read(s.APIKeyFile); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func Load(cfgFile string) (*Config, error) {
	k := newDefaultsKoanf()

	if cfgFile != "" {
		if err := k.Load(file.Provider(cfgFile), yaml.Parser()); err != nil {
			return nil, err
		}
	}
	cfg, err := finalize(k)
	if err != nil {
		return nil, err
	}
	store(cfg, cfgFile)
	return cfg, nil
}
