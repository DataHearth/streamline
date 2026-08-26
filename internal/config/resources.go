package config

import "github.com/datahearth/streamline/internal/quality"

// MediaServerEntry is one media-server integration. Secrets (api_key) are
// stored plaintext in YAML, consistent with auth.oidc[].client_secret.
type MediaServerEntry struct {
	Name       string `koanf:"name"         validate:"required"`
	ServerType string `koanf:"server_type"  validate:"required,oneof=plex jellyfin emby"`
	Host       string `koanf:"host"         validate:"required"`
	APIKey     string `koanf:"api_key"      validate:"excluded_with=APIKeyFile"`
	APIKeyFile string `koanf:"api_key_file" validate:"omitempty,excluded_with=APIKey,filepath"`
	Enabled    bool   `koanf:"enabled"`
	// LibrarySection is the Plex movie-library section key (play-on hint).
	LibrarySection *string `koanf:"library_section"`
	// LibrarySectionTV is the Plex TV-library section key (play-on hint).
	LibrarySectionTV *string `koanf:"library_section_tv"`
}

type DownloadClientEntry struct {
	Name         string `koanf:"name"          validate:"required"`
	ClientType   string `koanf:"client_type"   validate:"required,oneof=qbittorrent transmission deluge builtin"`
	Host         string `koanf:"host"          validate:"required_unless=ClientType builtin"`
	Port         uint16 `koanf:"port"          validate:"required_unless=ClientType builtin,omitempty,port"`
	AuthMethod   string `koanf:"auth_method"   validate:"required_unless=ClientType builtin,omitempty,oneof=password api_key"`
	Username     string `koanf:"username"`
	Password     string `koanf:"password"      validate:"excluded_with=PasswordFile"`
	PasswordFile string `koanf:"password_file" validate:"omitempty,excluded_with=Password,filepath"`
	APIKey       string `koanf:"api_key"       validate:"excluded_with=APIKeyFile"`
	APIKeyFile   string `koanf:"api_key_file"  validate:"omitempty,excluded_with=APIKey,filepath"`
	UseSSL       bool   `koanf:"use_ssl"`
	Priority     uint8  `koanf:"priority"`
	Enabled      bool   `koanf:"enabled"`

	// builtin-only knobs (client_type "builtin"); ignored for external clients.
	DownloadDir     string `koanf:"download_dir"      validate:"required_if=ClientType builtin"`
	ListenPort      uint16 `koanf:"listen_port"       validate:"omitempty,port"`
	MaxUploadKbps   int    `koanf:"max_upload_kbps"   validate:"min=0"`
	MaxDownloadKbps int    `koanf:"max_download_kbps" validate:"min=0"`
	// SeedRatio/SeedTime stop seeding when either is reached; zero = unlimited.
	SeedRatio  float64 `koanf:"seed_ratio"  validate:"min=0"`
	SeedTime   string  `koanf:"seed_time"`
	DisableDHT bool    `koanf:"disable_dht"`
	// BindInterface pins the engine to one interface (name like wg0 or a
	// literal IP); empty binds all interfaces. Existence is verified at engine
	// boot, not config load — Validate only checks the value shape.
	BindInterface string `koanf:"bind_interface"`
}

type IndexerEntry struct {
	Name       string `koanf:"name"         validate:"required"`
	Host       string `koanf:"host"         validate:"required"`
	Port       uint16 `koanf:"port"         validate:"required,port"`
	Path       string `koanf:"path"`
	UseSSL     bool   `koanf:"use_ssl"`
	APIKey     string `koanf:"api_key"      validate:"required_without=APIKeyFile,excluded_with=APIKeyFile"`
	APIKeyFile string `koanf:"api_key_file" validate:"omitempty,excluded_with=APIKey,filepath"`
	Protocol   string `koanf:"protocol"     validate:"required,oneof=torznab prowlarr"`
	Priority   uint8  `koanf:"priority"`
	Enabled    bool   `koanf:"enabled"`
}

type QualityProfileEntry struct {
	Name                string `koanf:"name"                 validate:"required"`
	PreferredResolution string `koanf:"preferred_resolution" validate:"required,oneof=720p 1080p 2160p"`
	MinResolution       string `koanf:"min_resolution"       validate:"required,oneof=720p 1080p 2160p"`
	UpgradeAllowed      bool   `koanf:"upgrade_allowed"`
	ReplaceWholeSeason  bool   `koanf:"replace_whole_season"`
	// AllowedCodecs holds a grab for a decision when the probed video codec
	// isn't in this list. ffprobe codec names ("hevc", "av1", "h264"), not
	// validated against a fixed set — ffprobe's namespace is larger than any
	// list this package could keep in sync. Empty means any codec.
	AllowedCodecs []string `koanf:"allowed_codecs"`
	// Formats scores built-in and custom_formats entries by name; each
	// name must resolve via quality.IsBuiltinName or FindCustomFormat
	// (checked in checkInvariants).
	Formats           []QualityProfileFormatScore `koanf:"formats"             validate:"dive"`
	MinScore          int                         `koanf:"min_score"`
	UpgradeUntilScore int                         `koanf:"upgrade_until_score"`
}

// QualityProfileFormatScore attaches a score to a format named by name,
// resolved against either the built-in library or CustomFormats.
type QualityProfileFormatScore struct {
	Name  string `koanf:"name"  validate:"required"`
	Score int    `koanf:"score"`
}

// CustomFormatConditionEntry is one quality.Condition as read from config.
// Which of Pattern/Value/MinGB/MaxGB/Min apply depends on Type; ToFormat (via
// quality.NewFormat) is where that is validated and compiled.
type CustomFormatConditionEntry struct {
	Type     string  `koanf:"type"     validate:"required"`
	Pattern  string  `koanf:"pattern"`
	Value    string  `koanf:"value"`
	MinGB    float64 `koanf:"min_gb"   validate:"min=0"`
	MaxGB    float64 `koanf:"max_gb"   validate:"min=0"`
	Min      int     `koanf:"min"      validate:"min=0"`
	Required bool    `koanf:"required"`
	Negate   bool    `koanf:"negate"`
}

// CustomFormatEntry is a user-defined quality.Format, name-keyed like the
// other config-backed resources.
type CustomFormatEntry struct {
	Name        string                       `koanf:"name"        validate:"required"`
	Description string                       `koanf:"description"`
	Conditions  []CustomFormatConditionEntry `koanf:"conditions"  validate:"min=1,dive"`
}

// ToFormat compiles e into a quality.Format. Condition-shape validation
// (regex compilation, resolution value, condition type) happens here, inside
// quality.NewFormat, in the one place that does both validation and
// compilation.
func (e CustomFormatEntry) ToFormat() (quality.Format, error) {
	conds := make([]quality.Condition, len(e.Conditions))
	for i, c := range e.Conditions {
		conds[i] = quality.Condition{
			Type:     quality.ConditionType(c.Type),
			Pattern:  c.Pattern,
			Value:    c.Value,
			MinGB:    c.MinGB,
			MaxGB:    c.MaxGB,
			Min:      c.Min,
			Required: c.Required,
			Negate:   c.Negate,
		}
	}
	f, err := quality.NewFormat(e.Name, conds)
	if err != nil {
		return quality.Format{}, err
	}
	f.Description = e.Description
	return f, nil
}

// FindCustomFormat returns the user-defined format named name.
func FindCustomFormat(name string) (CustomFormatEntry, bool) {
	c := Get()
	if c == nil {
		return CustomFormatEntry{}, false
	}
	for _, e := range c.CustomFormats {
		if e.Name == name {
			return e, true
		}
	}
	return CustomFormatEntry{}, false
}

// ResolveScoredProfile mirrors ResolveQualityProfile's fallback semantics
// (name falls back to the configured default; ok is false only when no
// profiles are configured at all) while assembling a quality.Profile with
// its formats resolved and scored.
func ResolveScoredProfile(name string) (quality.Profile, bool) {
	e, ok := ResolveQualityProfile(name)
	if !ok {
		return quality.Profile{}, false
	}
	p := quality.Profile{
		MinResolution:      e.MinResolution,
		MaxResolution:      e.PreferredResolution,
		UpgradeAllowed:     e.UpgradeAllowed,
		ReplaceWholeSeason: e.ReplaceWholeSeason,
		MinScore:           e.MinScore,
		UpgradeUntilScore:  e.UpgradeUntilScore,
	}
	for _, fs := range e.Formats {
		f, found := quality.BuiltinByName(fs.Name)
		if !found {
			ce, ok := FindCustomFormat(fs.Name)
			if !ok {
				continue // validated at load; a race with a delete just drops the format
			}
			var err error
			if f, err = ce.ToFormat(); err != nil {
				continue
			}
		}
		p.Formats = append(
			p.Formats,
			quality.ScoredFormat{Format: f, Score: fs.Score},
		)
	}
	return p, true
}

// ResolveQualityProfile returns the profile named by name, falling back to
// QualityDefaultProfile when name is empty or unknown. ok is false only when
// no profiles are configured at all.
func ResolveQualityProfile(name string) (QualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return QualityProfileEntry{}, false
	}
	if p, ok := findProfile(c.QualityProfiles, name); ok {
		return p, true
	}
	return findProfile(c.QualityProfiles, c.QualityDefaultProfile)
}

func findProfile(
	profiles []QualityProfileEntry,
	name string,
) (QualityProfileEntry, bool) {
	if name == "" {
		return QualityProfileEntry{}, false
	}
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return QualityProfileEntry{}, false
}

// PickDownloadClient returns the highest-priority enabled download client.
func PickDownloadClient() (DownloadClientEntry, bool) {
	c := Get()
	if c == nil {
		return DownloadClientEntry{}, false
	}
	var best DownloadClientEntry
	found := false
	for _, dc := range c.DownloadClients {
		if !dc.Enabled {
			continue
		}
		if !found || dc.Priority > best.Priority {
			best, found = dc, true
		}
	}
	return best, found
}

// EnabledDownloadClients returns every enabled download client. The adoption
// pass scans each for untracked managed-category torrents.
func EnabledDownloadClients() []DownloadClientEntry {
	c := Get()
	if c == nil {
		return nil
	}
	out := make([]DownloadClientEntry, 0, len(c.DownloadClients))
	for _, dc := range c.DownloadClients {
		if dc.Enabled {
			out = append(out, dc)
		}
	}
	return out
}

func FindDownloadClient(name string) (DownloadClientEntry, bool) {
	c := Get()
	if c == nil {
		return DownloadClientEntry{}, false
	}
	for _, dc := range c.DownloadClients {
		if dc.Name == name {
			return dc, true
		}
	}
	return DownloadClientEntry{}, false
}

// BuiltinDownloadClient returns the enabled builtin download-client entry,
// if one is configured. Config validation guarantees at most one exists.
func BuiltinDownloadClient() (DownloadClientEntry, bool) {
	c := Get()
	if c == nil {
		return DownloadClientEntry{}, false
	}
	for _, dc := range c.DownloadClients {
		if dc.ClientType == "builtin" && dc.Enabled {
			// torrent_listen_port wins where it is set. It exists so the
			// environment can supply a port the config file cannot know — a
			// VPN's per-session forward — so a file naming a different one is
			// stale by construction. Resolved here rather than in the engine
			// so the entry every caller sees already carries the effective
			// port.
			if c.TorrentListenPort != 0 {
				dc.ListenPort = c.TorrentListenPort
			}
			return dc, true
		}
	}
	return DownloadClientEntry{}, false
}

func EnabledIndexers() []IndexerEntry {
	c := Get()
	if c == nil {
		return nil
	}
	out := make([]IndexerEntry, 0, len(c.Indexers))
	for _, ix := range c.Indexers {
		if ix.Enabled {
			out = append(out, ix)
		}
	}
	return out
}

func FindIndexer(name string) (IndexerEntry, bool) {
	c := Get()
	if c == nil {
		return IndexerEntry{}, false
	}
	for _, ix := range c.Indexers {
		if ix.Name == name {
			return ix, true
		}
	}
	return IndexerEntry{}, false
}

func EnabledMediaServers() []MediaServerEntry {
	c := Get()
	if c == nil {
		return nil
	}
	out := make([]MediaServerEntry, 0, len(c.MediaServer.Servers))
	for _, ms := range c.MediaServer.Servers {
		if ms.Enabled {
			out = append(out, ms)
		}
	}
	return out
}

func FindMediaServer(name string) (MediaServerEntry, bool) {
	c := Get()
	if c == nil {
		return MediaServerEntry{}, false
	}
	for _, ms := range c.MediaServer.Servers {
		if ms.Name == name {
			return ms, true
		}
	}
	return MediaServerEntry{}, false
}
