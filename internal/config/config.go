package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"

	"github.com/datahearth/streamline/internal/quality"
)

type Config struct {
	Server ServerConfig `koanf:"server" validate:"required"`
	// Not tagged `dir`: ensureDataDir creates it at boot, and demanding it
	// already exist made Validate — which config.Update asks questions with,
	// about a config it may never store — depend on this host's filesystem.
	DataDir string `koanf:"data_dir" validate:"required"`
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
	FFmpeg      FFmpegConfig      `koanf:"ffmpeg"`
	Download    DownloadConfig    `koanf:"download"`
	// TorrentListenPort overrides the builtin download client's own
	// listen_port, and is a top-level scalar so the environment can reach it:
	// STREAMLINE_TORRENT_LISTEN_PORT lands here, while nothing in
	// download_clients[] is addressable that way — a single underscore is
	// literal, "__" is the path separator, and neither can name a list
	// element. That matters because the value it carries is authored by a VPN
	// tunnel rather than by git: a per-session forwarded port rotates on every
	// reconnect, so it cannot live in a read-only config file.
	TorrentListenPort uint16 `koanf:"torrent_listen_port" validate:"omitempty,port"`

	DownloadClients       []DownloadClientEntry `koanf:"download_clients"        validate:"unique=Name,dive"`
	Indexers              []IndexerEntry        `koanf:"indexers"                validate:"unique=Name,dive"`
	QualityProfiles       []QualityProfileEntry `koanf:"quality_profiles"        validate:"unique=Name,dive"`
	QualityDefaultProfile string                `koanf:"quality_default_profile"`
	CustomFormats         []CustomFormatEntry   `koanf:"custom_formats"          validate:"unique=Name,dive"`
}

// DatabasePath is the SQLite database location, derived from DataDir.
func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, "streamline.db")
}

type ServerConfig struct {
	Host string `koanf:"host" validate:"required,ip|hostname"`
	Port uint16 `koanf:"port" validate:"required,port"`
	// TrustedProxies gates every X-Forwarded-* header: they are believed only
	// when the immediate TCP peer falls inside one of these CIDRs. Empty (the
	// default) trusts nothing, so the peer address is always the client — the
	// only safe assumption for a directly exposed port, where the headers are
	// entirely attacker-supplied.
	//
	// List the proxies themselves, as narrowly as possible — ideally a /32 or
	// /128 per proxy. Never a whole client subnet. Naming a range that clients
	// can also occupy (10.0.0.0/8, 192.168.0.0/16, a Kubernetes pod or node
	// CIDR) makes every host in it a proxy as far as this gate is concerned:
	// any of them may then send X-Forwarded-For naming an address inside
	// auth.trusted_networks and be handed the auth.trusted_role identity
	// without authenticating, on top of taking over another client's login
	// rate-limit budget and access-log identity.
	TrustedProxies []string `koanf:"trusted_proxies" validate:"dive,cidr"`
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
	// authoritative — applied at signup and re-synced on every login of an
	// already-linked identity (the highest-privilege role wins if several
	// values map), subject to the AllowAdmin ceiling. Adopting an existing
	// local account by email is the one login that does not apply it, so
	// establishing a link can never also promote. Leave empty to give OIDC
	// users auth.oidc_default_role.
	//
	// RoleClaim may name a nested claim with a dotted path — Keycloak's roles
	// live at realm_access.roles, not at the top level. A claim whose literal
	// name contains a dot still wins over the path interpretation.
	RoleClaim   string            `koanf:"role_claim"`
	RoleMapping map[string]string `koanf:"role_mapping" validate:"omitempty,dive,oneof=admin member request_only"`
	// EmailLinking decides whether a federated identity this provider has
	// never presented before (a new (provider, subject) pair) may be adopted
	// into an account that merely shares its email address. Enabling it makes
	// the provider's email claim proof of account ownership: any IdP where a
	// user can self-assert a verified address — Keycloak with open
	// self-registration, a misconfigured Authentik/Authelia — can then mint a
	// login for any local user, and with several providers configured the
	// weakest one speaks for accounts belonging to the strongest.
	//
	// It governs adoption and nothing else. Roles are AllowAdmin's business:
	// while one key meant both, tightening the adoption tier could raise the
	// role ceiling (an account of federated origin adopted at non_admin became
	// promotable the moment the operator set the provider back to disabled),
	// and loosening it could lower it. Two keys make each axis monotone —
	// no move on one can add capability on the other.
	//
	// Empty is OIDCEmailLinkingDisabled, spelled out at load by
	// normalizeOIDCEmailLinking.
	EmailLinking string `koanf:"email_linking" validate:"omitempty,oneof=disabled non_admin all"`
	// AllowAdmin is the provider's admin ceiling: with it false — the default,
	// including for a provider added through the REST API, which does not
	// expose the key — no role this provider confers may be admin. That covers
	// every source, not just the claims: the claim-mapped role, the
	// auth.oidc_default_role a provisioning login falls back to, and the role
	// carried by an invite consumed through SSO.
	//
	// It reads no claim of the request being served, deliberately. A ceiling
	// that moved with the claims would be beaten by presenting the harmless set
	// first and the admin group one request later, once the link exists.
	AllowAdmin bool `koanf:"allow_admin"`
}

// Accepted OIDCConfig.EmailLinking values. Disabled is the zero value, so a
// provider that never names the key — including one added through the REST
// API, which does not expose it — cannot adopt existing accounts by email.
//
// "Adopt" is a federated identity the provider has never presented before
// signing in as an existing account because the email addresses match:
//
//   - disabled — adopts nothing.
//   - non_admin — may adopt non-admin accounts. This is the migration setting:
//     turn it on, have everyone sign in once so their identities bind, then
//     turn it back off.
//   - all — adopts any account, admin included. The only way to migrate an
//     admin, at the cost of exposing the account that can rewrite the whole
//     config — prefer moving the admin over during a maintenance window.
//
// The tier gates the adoption, not what it leaves behind. An adopted identity
// stays linked, and the login that matches it consults no tier at all — so a
// provider back at disabled still signs in as every account it adopted while it
// was open, local-password accounts it never created included. Undoing that
// means deleting the user; there is no unlink.
//
// None of them says anything about roles; that is OIDCConfig.AllowAdmin.
const (
	OIDCEmailLinkingDisabled = "disabled"
	OIDCEmailLinkingNonAdmin = "non_admin"
	OIDCEmailLinkingAll      = "all"
)

type LibraryConfig struct {
	MoviePath    string `koanf:"movie_path"    validate:"required"`
	MovieNaming  string `koanf:"movie_naming"  validate:"required"`
	SeriesPath   string `koanf:"series_path"   validate:"required"`
	SeriesNaming string `koanf:"series_naming" validate:"required"`
	// MonitorSpecials opts season 0 into monitoring when a series is added or
	// a refresh discovers the season. Off by default: specials are usually
	// recaps and OVAs nobody wants grabbed automatically. Only applies at seed
	// time — flipping it never touches seasons already in the library.
	MonitorSpecials bool `koanf:"monitor_specials"`
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
	// Probe governs import-time verification against ffprobe results, read by
	// the importer's verifier before a transfer.
	Probe ProbeConfig `koanf:"probe"`
}

// ProbeConfig governs import-time verification against ffprobe results.
type ProbeConfig struct {
	// AlwaysAsk holds every import for a decision, even when every check
	// passes.
	AlwaysAsk bool `koanf:"always_ask"`
	// MinDurationRatio holds an import whose probed duration is less than
	// this share of the known runtime. A ratio in (0, 1] — a value typed as
	// a percentage (90) is rejected rather than silently stored.
	MinDurationRatio float64 `koanf:"min_duration_ratio" validate:"gt=0,lte=1"`
}

// ScheduleConfig carries one interval per registered job. Media-scoped jobs
// are keyed movie_*/tv_* to match their scheduler names; the rest are shared
// across both libraries and stay unprefixed.
type ScheduleConfig struct {
	MovieRSSSync         string `koanf:"movie_rss_sync"         validate:"required"`
	TVRSSSync            string `koanf:"tv_rss_sync"            validate:"required"`
	MovieMissingSearch   string `koanf:"movie_missing_search"   validate:"required"`
	TVMissingSearch      string `koanf:"tv_missing_search"      validate:"required"`
	MovieMetadataRefresh string `koanf:"movie_metadata_refresh" validate:"required"`
	TVMetadataRefresh    string `koanf:"tv_metadata_refresh"    validate:"required"`
	MovieOrphanScan      string `koanf:"movie_orphan_scan"      validate:"required"`
	TVOrphanScan         string `koanf:"tv_orphan_scan"         validate:"required"`
	DownloadMonitor      string `koanf:"download_monitor"       validate:"required"`
	ImportScan           string `koanf:"import_scan"            validate:"required"`
	Cleanup              string `koanf:"cleanup"                validate:"required"`
	DriftCheck           string `koanf:"drift_check"            validate:"required"`
	MediaProbe           string `koanf:"media_probe"            validate:"required"`
	FileSelection        string `koanf:"file_selection"         validate:"required"`
}

type MetadataConfig struct {
	TMDBAPIKey     string `koanf:"tmdb_api_key"      validate:"excluded_with=TMDBAPIKeyFile"`
	TMDBAPIKeyFile string `koanf:"tmdb_api_key_file" validate:"omitempty,excluded_with=TMDBAPIKey,filepath"`
	TVDBAPIKey     string `koanf:"tvdb_api_key"      validate:"excluded_with=TVDBAPIKeyFile"`
	TVDBAPIKeyFile string `koanf:"tvdb_api_key_file" validate:"omitempty,excluded_with=TVDBAPIKey,filepath"`
	Language       string `koanf:"language"          validate:"omitempty,bcp47_language_tag"`
	TMDBRegion     string `koanf:"tmdb_region"       validate:"omitempty,len=2,uppercase"`
}

// EventsConfig governs the MediaEvent retention window. Old rows are deleted
// by the cleanup job after Retention.
type EventsConfig struct {
	Retention string `koanf:"retention" validate:"required"`
}

// FFmpegConfig owns the ffmpeg-suite dependency (ffprobe today, the player's
// transcoder later). Enabled=false makes probing and everything built on it
// inert. Path is a directory holding the binaries; empty resolves via $PATH.
// Enabled is runtime-toggleable through config.Update; Path is read at boot.
type FFmpegConfig struct {
	Enabled bool   `koanf:"enabled"`
	Path    string `koanf:"path"`
}

// DownloadConfig gates selective file download (spec §7). Off is bit-for-bit
// today's behaviour and the rollback path; runtime-toggleable.
type DownloadConfig struct {
	SelectiveFiles bool   `koanf:"selective_files"`
	SelectionGrace string `koanf:"selection_grace" validate:"required"`
}

// SelectionGraceDuration parses SelectionGrace, falling back to 10 minutes
// on an unparseable value — the same default as the field itself, so a
// corrupted config never hangs a pending selection forever (spec §4.5).
func (c DownloadConfig) SelectionGraceDuration() time.Duration {
	if d, err := time.ParseDuration(c.SelectionGrace); err == nil {
		return d
	}
	return 10 * time.Minute
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

// bindInterfacePattern constrains a builtin download client's bind_interface to
// an interface-name or literal-IP shape. Existence is checked at engine boot,
// not config load.
var bindInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// normalizeOIDCEmailLinking spells out the zero value as the tier it already
// means. The write-back marshals the whole struct, zero fields included, so a
// provider that never named the key used to be rewritten as
// `email_linking: ""` — which the Go loader accepts (omitempty) but
// api/config.schema.json rejects, leaving streamline emitting a config its own
// published schema fails. Writing the word also makes the file say which tier
// is in force instead of leaving it to the reader.
//
// Validate is where this hangs because it is the one funnel both entry points
// share: finalize calls it on load, and Update calls it on the clone before
// writeYAMLAtomic, which is what catches a provider added through the REST API
// (that path never revisits finalize).
func (c *Config) normalizeOIDCEmailLinking() {
	for i := range c.Auth.OIDC {
		if c.Auth.OIDC[i].EmailLinking == "" {
			c.Auth.OIDC[i].EmailLinking = OIDCEmailLinkingDisabled
		}
	}
}

// Validate reports whether these values are a config the process can run on.
// Beyond normalising the OIDC linking tier onto c, it only reads c. Creating
// directories is ensureDataDir's job, so a caller asking a question rather than
// booting — config.Update, deciding whether the file it is about to write would
// still load — leaves nothing behind on the filesystem when the answer is no.
//
// The answer carries every rule broken, joined: the struct tags and the
// invariants both, however many of each. Returning at the tags read as a
// config with one flaw in it, and config.Update compares two of these answers
// to tell a flaw it is introducing from one it inherited — an install whose
// tags already fail would have had every invariant it broke hidden behind
// them, and the write saved.
//
// "However many" is across fields, not within one: validator stops at a
// field's first failing tag, so anything behind it — a dive into the
// elements, most of all — never evaluates. Two quality profiles sharing a
// name, one of them carrying a bad preferred_resolution, report the list's
// unique and the invariant and never the element's oneof; that oneof surfaces
// on the next answer, once the name clash is gone. The same shape reaches
// Update, which can only compare the reasons it was told about.
func (c *Config) Validate() error {
	c.normalizeOIDCEmailLinking()
	return errors.Join(validator.New().Struct(c), c.checkInvariants())
}

// ensureDataDir creates data_dir. Nothing else makes it: the poster cache
// mkdirs <data_dir>/posters, long after this. So any data_dir below its
// volume's mount point — `/data/streamline` on a fresh, empty PVC — used to
// fail validation with a message naming a tag rather than the missing
// directory.
//
// A boot step, not a validation step: the database this process serves from is
// opened out of this directory once, at startup. A config.Update that changes
// data_dir writes the key and leaves the directory to the restart that will
// actually use it.
func ensureDataDir(c *Config) error {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return fmt.Errorf("data_dir %q: %w", c.DataDir, err)
	}
	return nil
}

// checkInvariants holds the rules the struct tags cannot express: relations
// between fields, and formats the tags accept but the code cannot use.
//
// It reports all of them, joined, rather than stopping at the first. Update
// tells the reasons it is introducing from the ones it inherited by comparing
// two of these answers reason by reason, and an answer that stops early makes
// every rule behind the stopping one read the same on both sides — present
// nowhere, so introduced nowhere, so saved.
func (c *Config) checkInvariants() error {
	var errs []error
	// The proxy-trust gate compares peers with netip, which never matches a
	// v4-mapped prefix against a plain v4 peer. The `cidr` tag accepts that
	// form, so reject it here: it would otherwise look configured while
	// silently trusting nobody.
	for _, cidr := range c.Server.TrustedProxies {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			errs = append(
				errs, fmt.Errorf("server.trusted_proxies %q: %w", cidr, err),
			)
			continue
		}
		if p.Addr().Is4In6() {
			errs = append(errs, fmt.Errorf(
				"server.trusted_proxies %q: write IPv4 ranges in plain IPv4 form",
				cidr,
			))
		}
	}
	// The other name-keyed lists get this from a `unique=Name` tag; auth.oidc
	// is spelled out because a duplicate there is a privilege split rather than
	// a config typo, and the operator needs the offending name. The provider a
	// callback authenticates against is oidcManager's map entry — last write
	// wins — while every trust decision reads findOIDCProvider's first match,
	// so two entries sharing a name let a token minted by one issuer be capped
	// by the other's allow_admin.
	seenOIDC := map[string]bool{}
	for _, p := range c.Auth.OIDC {
		if seenOIDC[p.Name] {
			errs = append(
				errs,
				fmt.Errorf("auth.oidc: duplicate provider name %q", p.Name),
			)
			continue
		}
		seenOIDC[p.Name] = true
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
			errs = append(errs, fmt.Errorf(
				"quality_default_profile %q names no profile in quality_profiles",
				c.QualityDefaultProfile,
			))
		}
	}
	builtin := 0
	for _, dc := range c.DownloadClients {
		if dc.ClientType == "builtin" {
			builtin++
		}
		if dc.BindInterface != "" &&
			!bindInterfacePattern.MatchString(dc.BindInterface) {
			errs = append(errs, fmt.Errorf(
				"download client %q: invalid bind_interface %q",
				dc.Name, dc.BindInterface,
			))
		}
	}
	if builtin > 1 {
		errs = append(errs, fmt.Errorf(
			"at most one builtin download client is allowed, found %d", builtin,
		))
	}
	for _, e := range c.CustomFormats {
		if quality.IsBuiltinName(e.Name) {
			errs = append(errs, fmt.Errorf(
				"custom_formats %q: name collides with a built-in format",
				e.Name,
			))
		}
		if _, err := e.ToFormat(); err != nil {
			errs = append(errs, fmt.Errorf("custom_formats %q: %w", e.Name, err))
		}
	}
	for _, p := range c.QualityProfiles {
		// preferred_resolution is the band's hard ceiling (it becomes
		// quality.Profile.MaxResolution), so a min above it leaves an empty
		// band that rejects every release with a reason naming neither key.
		if quality.CompareResolutions(p.MinResolution, p.PreferredResolution) > 0 {
			errs = append(errs, fmt.Errorf(
				"quality profile %q: min_resolution %q is above "+
					"preferred_resolution %q, the band's ceiling — "+
					"no release can satisfy both",
				p.Name,
				p.MinResolution,
				p.PreferredResolution,
			))
		}
		for _, fs := range p.Formats {
			if quality.IsBuiltinName(fs.Name) {
				continue
			}
			if _, ok := findCustomFormatEntry(c.CustomFormats, fs.Name); ok {
				continue
			}
			errs = append(errs, fmt.Errorf(
				"quality profile %q: formats[].name %q names neither a built-in nor a user format",
				p.Name,
				fs.Name,
			))
		}
	}
	return errors.Join(errs...)
}

// findCustomFormatEntry mirrors FindCustomFormat but reads from an explicit
// list rather than the singleton — checkInvariants runs on a *Config before
// it is stored, both at boot (finalize) and inside config.Update (a clone
// being validated before write-back), so Get() would answer with the
// previous config or nil.
func findCustomFormatEntry(
	formats []CustomFormatEntry,
	name string,
) (CustomFormatEntry, bool) {
	for _, e := range formats {
		if e.Name == name {
			return e, true
		}
	}
	return CustomFormatEntry{}, false
}

// defaults returns the canonical default values for all config keys.
// Keys use koanf's dotted notation.
func defaults() map[string]any {
	return map[string]any{
		"server.host":                      "0.0.0.0",
		"server.port":                      8080,
		"server.trusted_proxies":           []string{},
		"data_dir":                         "./data",
		"read_only":                        false,
		"torrent_listen_port":              0,
		"auth.mode":                        "full",
		"auth.trusted_networks":            []string{},
		"auth.trusted_role":                "member",
		"auth.session_secret":              "",
		"auth.session_secret_file":         "",
		"auth.session_ttl":                 "168h",
		"auth.registration_mode":           "disabled",
		"auth.oidc_default_role":           "member",
		"auth.seed_admin.email":            "",
		"auth.seed_admin.password":         "",
		"auth.seed_admin.password_file":    "",
		"auth.oidc":                        []any{},
		"auth.lockout.threshold":           10,
		"auth.lockout.window":              "15m",
		"auth.lockout.duration":            "15m",
		"library.movie_path":               "/media/movies",
		"library.series_path":              "/media/series",
		"library.series_naming":            "{title} ({year})/Season {season}/{title} - S{season:2}E{episode:2} - {episode_title} [{quality}].{ext}",
		"library.download_path":            "/downloads",
		"library.movie_naming":             "{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}",
		"library.import_mode":              "hardlink",
		"library.monitor_specials":         false,
		"library.keep_torrent_seeding":     true,
		"library.import_max_attempts":      3,
		"library.allowed_download_roots":   []string{},
		"library.no_match_cooldown":        "6h",
		"library.max_grab_failures":        3,
		"schedules.movie_rss_sync":         "15m",
		"schedules.tv_rss_sync":            "15m",
		"schedules.movie_missing_search":   "12h",
		"schedules.tv_missing_search":      "12h",
		"schedules.movie_metadata_refresh": "24h",
		"schedules.tv_metadata_refresh":    "24h",
		"schedules.movie_orphan_scan":      "6h",
		"schedules.tv_orphan_scan":         "6h",
		"schedules.download_monitor":       "30s",
		"schedules.cleanup":                "24h",
		"schedules.import_scan":            "60s",
		"schedules.drift_check":            "15m",
		"schedules.media_probe":            "15m",
		"schedules.file_selection":         "30s",
		"library.drift_grace_ticks":        3,
		"library.probe.always_ask":         false,
		"library.probe.min_duration_ratio": 0.5,
		"metadata.tmdb_api_key":            "",
		"metadata.tmdb_api_key_file":       "",
		"metadata.tvdb_api_key":            "",
		"metadata.tvdb_api_key_file":       "",
		"metadata.language":                "en",
		"metadata.tmdb_region":             "FR",
		"otel.endpoint":                    "",
		"media_server.plex_client_id":      "",
		"media_server.servers":             []any{},
		"download_clients":                 []any{},
		"indexers":                         []any{},
		"quality_profiles": []map[string]any{
			{
				"name":                 "default",
				"preferred_resolution": "1080p",
				"min_resolution":       "1080p",
				"upgrade_allowed":      true,
			},
		},
		"quality_default_profile":      "default",
		"custom_formats":               []any{},
		"events.retention":             "2160h",
		"ffmpeg.enabled":               true,
		"ffmpeg.path":                  "",
		"download.selective_files":     false,
		"download.selection_grace":     "10m",
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
	fileK := koanf.New(".")
	if len(raw) > 0 {
		if err := fileK.Load(rawbytes.Provider(raw), yaml.Parser()); err != nil {
			return err
		}
		if err := k.Merge(fileK); err != nil {
			return err
		}
	}
	cfg, layer, err := finalize(k, fileK)
	if err != nil {
		return err
	}
	store(cfg, "", k, layer)
	return nil
}

func newDefaultsKoanf() *koanf.Koanf {
	k := koanf.New(".")
	for key, val := range defaults() {
		_ = k.Set(key, val)
	}
	return k
}

// renamedScheduleKeys maps each pre-media-split schedules key to the keys that
// replaced it. missing_search, metadata_refresh and orphan_scan each drove both
// the movie and the TV job, so a value set under the old name has to land on
// both replacements or the operator's cadence silently changes for one library.
var renamedScheduleKeys = map[string][]string{
	"schedules.rss_sync": {"schedules.movie_rss_sync"},
	"schedules.missing_search": {
		"schedules.movie_missing_search",
		"schedules.tv_missing_search",
	},
	"schedules.metadata_refresh": {
		"schedules.movie_metadata_refresh",
		"schedules.tv_metadata_refresh",
	},
	"schedules.orphan_scan": {
		"schedules.movie_orphan_scan",
		"schedules.tv_orphan_scan",
	},
}

// applyRenamedScheduleKeys copies a value still set under a pre-media-split
// schedules key onto its replacements, skipping any replacement the operator
// already set themselves, and returns the replacements it actually wrote keyed
// by the deprecated key they came from. Without it an old config keeps parsing
// — koanf drops keys the struct no longer has — while every renamed job
// silently reverts to its default interval.
//
// "Already set themselves" is read as "carries a value that differs from the
// default", which covers both layer shapes this runs against: the merged koanf,
// where the defaults are always present, and the file-only koanf, where a key
// the operator did not write is simply absent.
func applyRenamedScheduleKeys(k *koanf.Koanf) (map[string][]string, error) {
	d := defaults()
	applied := map[string][]string{}
	for old, replacements := range renamedScheduleKeys {
		if !k.Exists(old) {
			continue
		}
		val := k.String(old)
		for _, key := range replacements {
			if def, _ := d[key].(string); k.Exists(key) && k.String(key) != def {
				continue
			}
			if err := k.Set(key, val); err != nil {
				return nil, err
			}
			applied[old] = append(applied[old], key)
		}
	}
	return applied, nil
}

func finalize(k, fileK *koanf.Koanf) (*Config, *envLayer, error) {
	// Double-underscore is the path separator; a single underscore is literal
	// so keys with underscore segments (data_dir, session_secret, tmdb_api_key)
	// stay reachable: STREAMLINE_AUTH__SESSION_SECRET -> auth.session_secret.
	envProvider := env.Provider(".", env.Opt{
		Prefix: "STREAMLINE_",
		TransformFunc: func(k, v string) (string, any) {
			key := strings.ToLower(strings.TrimPrefix(k, "STREAMLINE_"))
			return strings.ReplaceAll(key, "__", "."), v
		},
	})
	envK := koanf.New(".")
	if err := envK.Load(envProvider, nil); err != nil {
		return nil, nil, err
	}
	if err := k.Merge(envK); err != nil {
		return nil, nil, err
	}

	// The file layer gets the same expansion, so a file that still names a
	// deprecated schedules key owns the replacement too and keeps its own
	// cadence when write-back reverts an environment-supplied one.
	if _, err := applyRenamedScheduleKeys(fileK); err != nil {
		return nil, nil, err
	}
	applied, err := applyRenamedScheduleKeys(k)
	if err != nil {
		return nil, nil, err
	}
	envKeys := envK.Keys()
	for old, written := range applied {
		slog.WarnContext(
			context.Background(),
			"config: schedules key was renamed; still honouring the old one",
			"deprecated.key", old,
			"replacement.keys", strings.Join(written, ", "),
			"interval", k.String(old),
		)
		// The alias lands environment data on a key the environment never
		// named. Unless the replacement joins the env key set, write-back reads
		// it as the file's own and copies it to disk.
		if envK.Exists(old) {
			envKeys = append(envKeys, written...)
		}
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	if err := ensureDataDir(&cfg); err != nil {
		return nil, nil, err
	}
	m, err := loadSecretFiles(&cfg)
	if err != nil {
		return nil, nil, err
	}
	secretFiles.Store(&m)

	structK, err := flatten(&cfg)
	if err != nil {
		return nil, nil, err
	}
	envKeys = configKeys(envKeys, structK)

	announceEnvLayer(envKeys)
	warnDefaultedKeys(&cfg, k, fileK, envK)
	warnEnvShadowsFile(fileK, envK)
	warnFileNeedsEnv(fileK)
	warnDatabaseElsewhere(&cfg, fileK, envK)
	return &cfg, &envLayer{file: fileK, keys: envKeys}, nil
}

// configKeys drops the STREAMLINE_* variables that name no config key at all —
// STREAMLINE_PUBLIC_URL, the hidden e2e seams, anything else in the process
// environment that happens to share the prefix — then sorts and dedupes what
// is left.
//
// Write-back already ignores them, since they never reach the struct-shaped
// koanf it strips. This is about what Load claims out loud, and what the
// withheld-keys warning lists: "these settings come from the environment" has
// to be true of every key it names, in the same order on every run. Ranging
// over the environment map gave neither.
func configKeys(keys []string, structK *koanf.Koanf) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if structK.Exists(key) {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// announceEnvLayer names, once per boot, the settings this process took from
// the environment. Write-back never records their values (see envLayer), so
// this line — from the boot where they still hold — is what an operator has to
// compare against when a setting silently changes after a variable is dropped.
// Keys only: several of them are secrets.
func announceEnvLayer(envKeys []string) {
	if len(envKeys) == 0 {
		return
	}
	slog.InfoContext(
		context.Background(),
		"these settings come from the environment and are never written to the config file",
		"config.env.keys",
		strings.Join(envKeys, ", "),
	)
}

// provenanceWatchKeys are the settings whose built-in default silently
// redirects real work instead of failing closed: where the database, the
// library and the downloads live, whether imports are fenced to known roots,
// where telemetry goes, how long a session lasts. Losing
// STREAMLINE_LIBRARY__MOVIE_PATH does not stop the importer — it starts filing
// films into /media/movies instead, and nothing else in the process has a word
// to say about it.
//
// Keys whose default fails closed stay off the list on purpose:
// auth.trusted_networks and server.trusted_proxies revert to trusting nobody,
// which surfaces as an outage rather than as data going quietly to the wrong
// place. So does the rest of the config — a defaulted log format or schedule
// interval is not a loss worth a warning every boot.
//
// Nothing here is a secret, so these warnings may print values.
var provenanceWatchKeys = []string{
	"auth.session_ttl",
	"data_dir",
	"library.allowed_download_roots",
	"library.download_path",
	"library.movie_path",
	"library.series_path",
	"otel.endpoint",
}

// warnDefaultedKeys fires on a boot where a watched key came from neither the
// file nor the environment, so the built-in default decides.
//
// Nothing else catches this. data_dir's default is a relative path, Validate
// creates it, and SQLite opens a fresh empty database inside it without
// complaint — an install whose STREAMLINE_DATA_DIR went away comes up looking
// wiped. That boot is where the loss lands, so that is where it has to be
// audible: the withheld-keys warning at write-back happens long before, on a
// run that is still working.
//
// It stays a warning and the boot continues, so an operator running at
// log.app.level: error sees nothing. That is the accepted cost. Refusing to
// start is not available — running on the defaults is a supported
// configuration, and the shipped config.example.yaml is DumpDefaults output
// naming every one of these keys, so only an under-specified install reaches
// this line at all. Raising it to error would cry wolf on every one of those
// installs. The write path returns errors where it can instead of logging —
// ErrEnvOwned for a change the environment would discard, and
// ErrWriteBackUnloadable for one that would leave the file unloadable — but
// not for a file that already needed the environment before the update, which
// Update states its reasons for saving anyway.
func warnDefaultedKeys(c *Config, merged, fileK, envK *koanf.Koanf) {
	var pairs []string
	defaultedDataDir := false
	for _, key := range provenanceWatchKeys {
		if fileK.Exists(key) || envK.Exists(key) {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%v", key, merged.Get(key)))
		if key == "data_dir" {
			defaultedDataDir = true
		}
	}
	if len(pairs) == 0 {
		return
	}
	attrs := []any{"config.defaulted", strings.Join(pairs, ", ")}
	if defaultedDataDir {
		attrs = append(attrs, "database.path", c.DatabasePath())
	}
	slog.WarnContext(
		context.Background(),
		"these settings are named in neither the config file nor the environment, so the built-in default decides where this instance reads and writes — if one of them used to come from the environment, it is looking somewhere else now",
		attrs...,
	)
}

// warnEnvShadowsFile fires on a boot where the environment overrides a watched
// key the config file also sets, naming the value the file would fall back to.
//
// Only this boot can see it. With the variable gone the next one reads the
// file's value and is indistinguishable from an install that was always
// configured that way, so warnDefaultedKeys has nothing to catch: dropping
// STREAMLINE_DATA_DIR moves the process onto whatever data_dir the file names —
// a different, probably empty database — in silence.
func warnEnvShadowsFile(fileK, envK *koanf.Koanf) {
	var pairs []string
	for _, key := range provenanceWatchKeys {
		if !fileK.Exists(key) || !envK.Exists(key) {
			continue
		}
		if slices.Equal(
			settingValues(fileK.Get(key)),
			settingValues(envK.Get(key)),
		) {
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%v", key, fileK.Get(key)))
	}
	if len(pairs) == 0 {
		return
	}
	slog.WarnContext(
		context.Background(),
		"the environment is overriding settings the config file also names — drop those variables and this instance moves to the file's values",
		"config.file.shadowed",
		strings.Join(pairs, ", "),
	)
}

// settingValues renders a koanf value as the list of values the setting
// resolves to, so two layers can be compared for what they mean rather than
// for how they print. An environment variable is always one string while the
// file holds a list as []any, and every scalar arrives typed on one side and
// as text on the other: rendering both whole reported
// library.allowed_download_roots: ["/downloads"] as shadowed by
// STREAMLINE_LIBRARY__ALLOWED_DOWNLOAD_ROOTS=/downloads, which is the same
// setting.
func settingValues(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case []string:
		return t
	default:
		return []string{fmt.Sprint(v)}
	}
}

// warnFileNeedsEnv fires when the config file, on its own, is not a config
// this process would start on — the environment is filling a gap in it, and
// the boot that loses that variable stops here instead of running.
//
// The write-back reports the same thing (see config.Update), but only when
// something saves, and only to whoever reads the response. This is the boot
// that can still be fixed before a restart proves it.
func warnFileNeedsEnv(fileK *koanf.Koanf) {
	if len(fileK.Keys()) == 0 {
		return
	}
	err := checkLoadable(fileK)
	if err == nil {
		return
	}
	slog.WarnContext(
		context.Background(),
		"the config file does not load without the environment — with these variables gone, the next boot stops here instead of starting",
		"error",
		err,
	)
}

// warnDatabaseElsewhere fires when the data_dir this boot resolved holds no
// database while another data_dir the same config names does. That is the boot
// where an instance comes up looking wiped, and the one that can still say so:
// the write-back warnings all happened on the previous run, whose logs went
// with the container that was replaced.
//
// It only sees the data_dirs this config names — the file's, the
// environment's, the built-in default. A variable that pointed somewhere else
// entirely and was then dropped leaves nothing here to compare against; that
// direction is warnEnvShadowsFile's to announce, one boot early.
func warnDatabaseElsewhere(c *Config, fileK, envK *koanf.Koanf) {
	if _, err := os.Stat(c.DatabasePath()); err == nil {
		return
	}
	configured := filepath.Clean(c.DataDir)
	seen := map[string]bool{configured: true}
	var found []string
	for _, dir := range []string{
		fileK.String("data_dir"),
		envK.String("data_dir"),
		fmt.Sprint(defaults()["data_dir"]),
	} {
		if dir == "" || seen[filepath.Clean(dir)] {
			continue
		}
		seen[filepath.Clean(dir)] = true
		db := filepath.Join(dir, "streamline.db")
		if _, err := os.Stat(db); err == nil {
			found = append(found, db)
		}
	}
	if len(found) == 0 {
		return
	}
	slog.WarnContext(
		context.Background(),
		"this instance is starting on an empty database while a database sits under a data_dir this config also names — check data_dir before anything writes to the new one",
		"database.missing",
		c.DatabasePath(),
		"database.elsewhere",
		strings.Join(found, ", "),
	)
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
// map, and returns nothing but the joined error when any of them is
// unreadable.
//
// It reports every unreadable path rather than the first, so an operator
// materialising secrets learns about all of them in one boot — and so
// config.Update can tell a path this update broke from one that was already
// broken, which it cannot do when a single failure stands for the whole set.
func loadSecretFiles(c *Config) (map[string]string, error) {
	m := map[string]string{}
	seen := map[string]bool{}
	var errs []error
	read := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		b, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("read secret file %q: %w", path, err))
			return
		}
		m[path] = strings.TrimSpace(string(b))
	}

	read(c.Auth.SessionSecretFile)
	read(c.Metadata.TMDBAPIKeyFile)
	read(c.Metadata.TVDBAPIKeyFile)
	for _, o := range c.Auth.OIDC {
		read(o.ClientSecretFile)
	}
	for _, x := range c.Indexers {
		read(x.APIKeyFile)
	}
	for _, d := range c.DownloadClients {
		read(d.PasswordFile)
		read(d.APIKeyFile)
	}
	for _, s := range c.MediaServer.Servers {
		read(s.APIKeyFile)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return m, nil
}

func Load(cfgFile string) (*Config, error) {
	k := newDefaultsKoanf()
	fileK := koanf.New(".")

	if cfgFile != "" {
		if err := fileK.Load(file.Provider(cfgFile), yaml.Parser()); err != nil {
			return nil, err
		}
		if err := k.Merge(fileK); err != nil {
			return nil, err
		}
	}
	cfg, layer, err := finalize(k, fileK)
	if err != nil {
		return nil, err
	}
	store(cfg, cfgFile, k, layer)
	return cfg, nil
}
