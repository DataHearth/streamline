package config

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/datahearth/streamline/internal/quality"
)

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
	// Private is the operator's word for it: a Torznab feed carries no
	// privacy flag, and a Prowlarr entry's applies to every sub-tracker
	// behind it.
	Private bool `koanf:"private"`
}

type QualityProfileEntry struct {
	Name                string `koanf:"name"                 validate:"required"`
	PreferredResolution string `koanf:"preferred_resolution" validate:"required,oneof=720p 1080p 2160p"`
	MinResolution       string `koanf:"min_resolution"       validate:"required,oneof=720p 1080p 2160p"`
	UpgradeAllowed      bool   `koanf:"upgrade_allowed"`
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
	// Transcode is nil for a profile the background transcode worker never
	// touches; transcoding.enabled must also be true for a set policy to
	// take effect (see TranscodeEligible).
	Transcode *TranscodePolicy `koanf:"transcode"`
}

// TranscodePolicy: any failing `if` rule queues a transcode toward `to`.
// HDR/DV video is exempt from re-encode in v1 — the rule evaluator suspends
// codec/bitrate rules for it; container remux still applies.
type TranscodePolicy struct {
	If TranscodeIf `koanf:"if"`
	To TranscodeTo `koanf:"to"`
}

type TranscodeIf struct {
	VideoCodecs     []string `koanf:"video_codecs"      validate:"dive,oneof=h264 hevc av1 vp9 mpeg4 mpeg2video vc1"`
	Containers      []string `koanf:"containers"        validate:"dive,oneof=mkv mp4 avi mov ts m2ts webm wmv"`
	MaxVideoBitrate string   `koanf:"max_video_bitrate"`
	MinVideoBitrate string   `koanf:"min_video_bitrate"`
}

type TranscodeTo struct {
	Container        string   `koanf:"container"         validate:"required,oneof=mkv mp4"`
	VideoCodec       string   `koanf:"video_codec"       validate:"required,oneof=h264 hevc av1"`
	CRF              uint8    `koanf:"crf"               validate:"max=51"`
	Preset           string   `koanf:"preset"            validate:"required,oneof=ultrafast superfast veryfast faster fast medium slow slower veryslow"`
	AudioCodec       string   `koanf:"audio_codec"       validate:"required,oneof=aac opus ac3 flac"`
	AudioPassthrough []string `koanf:"audio_passthrough"`
}

// DefaultAudioPassthrough is applied when a policy names none: TrueHD/E-AC-3
// carry Atmos and DTS(-HD) cannot be re-encoded losslessly, so copying is the
// only way they survive.
var DefaultAudioPassthrough = []string{
	"truehd", "eac3", "ac3", "dts", "aac", "opus", "flac",
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
// (name falls back to media's configured default; ok is false only when no
// profiles are configured at all) while assembling a quality.Profile with
// its formats resolved and scored.
// scoredProfiles memoises assembled profiles. Assembling one compiles every
// user custom format's conditions through regexp.Compile, and the series
// detail path resolves a profile per episode — a 200-episode show was ~2,000
// compilations and megabytes of garbage for one response.
//
// Keyed by the *resolved* entry's name, not the requested one: an unknown name
// falls back to the default, so keying on the request would let arbitrary
// stored strings grow the map. Invalidated wholesale on any config commit,
// which is what makes a hot-edited custom format take effect immediately.
var scoredProfiles struct {
	mu     sync.Mutex
	gen    uint64
	byName map[string]quality.Profile
}

// ResolveScoredProfile mirrors ResolveQualityProfile's fallback semantics
// (name falls back to media's configured default; ok is false only when no
// profiles are configured at all) while assembling a quality.Profile with
// its formats resolved and scored.
//
// The returned Profile is shared between callers and must not be mutated.
func ResolveScoredProfile(media Media, name string) (quality.Profile, bool) {
	e, ok := ResolveQualityProfile(media, name)
	if !ok {
		return quality.Profile{}, false
	}

	gen := Generation()
	scoredProfiles.mu.Lock()
	defer scoredProfiles.mu.Unlock()
	if scoredProfiles.byName == nil || scoredProfiles.gen != gen {
		scoredProfiles.byName = make(map[string]quality.Profile)
		scoredProfiles.gen = gen
	}
	if p, hit := scoredProfiles.byName[e.Name]; hit {
		return p, true
	}
	p := buildScoredProfile(e)
	scoredProfiles.byName[e.Name] = p
	return p, true
}

func buildScoredProfile(e QualityProfileEntry) quality.Profile {
	p := quality.Profile{
		MinResolution:     e.MinResolution,
		MaxResolution:     e.PreferredResolution,
		UpgradeAllowed:    e.UpgradeAllowed,
		MinScore:          e.MinScore,
		UpgradeUntilScore: e.UpgradeUntilScore,
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
	return p
}

// Media names the library a quality profile is resolved for: movies and series
// each have their own default.
type Media string

const (
	MediaMovie  Media = "movie"
	MediaSeries Media = "series"
)

// DefaultProfile is the quality profile name configured as media's default.
func (c *Config) DefaultProfile(media Media) string {
	if media == MediaSeries {
		return c.SeriesQualityDefaultProfile
	}
	return c.MovieQualityDefaultProfile
}

// DefaultFor lists the media name is the default quality profile of.
func (c *Config) DefaultFor(name string) []Media {
	out := []Media{}
	if c.MovieQualityDefaultProfile == name {
		out = append(out, MediaMovie)
	}
	if c.SeriesQualityDefaultProfile == name {
		out = append(out, MediaSeries)
	}
	return out
}

// LookupQualityProfile returns the profile named exactly name, with no
// fallback to a default.
func LookupQualityProfile(name string) (QualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return QualityProfileEntry{}, false
	}
	return findProfile(c.QualityProfiles, name)
}

// ResolveQualityProfile returns the profile named by name, falling back to
// media's default when name is empty or unknown. ok is false only when no
// profiles are configured at all.
func ResolveQualityProfile(media Media, name string) (QualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return QualityProfileEntry{}, false
	}
	if p, ok := findProfile(c.QualityProfiles, name); ok {
		return p, true
	}
	return findProfile(c.QualityProfiles, c.DefaultProfile(media))
}

// TranscodeEligible reports whether profile is subject to the background
// transcode worker: the feature must be enabled globally, and the resolved
// profile must carry a transcode policy.
func TranscodeEligible(media Media, profile string) bool {
	c := Get()
	if c == nil || !c.Transcoding.Enabled {
		return false
	}
	p, ok := ResolveQualityProfile(media, profile)
	return ok && p.Transcode != nil
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

// MusicTiers is the canonical tier order, best first. A profile's Tiers are
// stored in this order whatever order they arrive in.
var MusicTiers = []string{"hires", "lossless", "high", "standard", "low"}

// BookKinds lists the kinds a book carries, each with its own default profile.
var BookKinds = []string{"novel", "bd", "comic", "manga"}

// EbookFormats and AudiobookFormats are the canonical format ladders, best
// first, upper case everywhere: config, wire and MediaFile.quality.
var (
	EbookFormats     = quality.EbookLadder
	AudiobookFormats = quality.AudiobookLadder
)

type MusicQualityProfileEntry struct {
	Name           string   `koanf:"name"            validate:"required"`
	Tiers          []string `koanf:"tiers"           validate:"required,min=1,unique,dive,oneof=hires lossless high standard low"`
	Preferred      string   `koanf:"preferred"       validate:"required,oneof=hires lossless high standard low"`
	UpgradeAllowed bool     `koanf:"upgrade_allowed"`
}

type EbookSlot struct {
	Formats   []string `koanf:"formats"   validate:"required,min=1,unique,dive,oneof=EPUB AZW3 MOBI PDF CBZ CBR"`
	Preferred string   `koanf:"preferred" validate:"required,oneof=EPUB AZW3 MOBI PDF CBZ CBR"`
}

type AudiobookSlot struct {
	Formats    []string `koanf:"formats"     validate:"required,min=1,unique,dive,oneof=M4B MP3 M4A FLAC"`
	Preferred  string   `koanf:"preferred"   validate:"required,oneof=M4B MP3 M4A FLAC"`
	MinBitrate uint16   `koanf:"min_bitrate" validate:"max=1024"`
}

type BookQualityProfileEntry struct {
	Name           string        `koanf:"name"            validate:"required"`
	UpgradeAllowed bool          `koanf:"upgrade_allowed"`
	Ebook          EbookSlot     `koanf:"ebook"`
	Audiobook      AudiobookSlot `koanf:"audiobook"`
}

// BookQualityDefaults names the default profile for each book kind.
type BookQualityDefaults struct {
	Novel string `koanf:"novel"`
	BD    string `koanf:"bd"`
	Comic string `koanf:"comic"`
	Manga string `koanf:"manga"`
}

// For returns the default profile name for kind; an unknown kind reads as
// novel, the kind a book carries before it is classified.
func (d BookQualityDefaults) For(kind string) string {
	switch kind {
	case "bd":
		return d.BD
	case "comic":
		return d.Comic
	case "manga":
		return d.Manga
	}
	return d.Novel
}

func (d *BookQualityDefaults) set(kind, name string) {
	switch kind {
	case "bd":
		d.BD = name
	case "comic":
		d.Comic = name
	case "manga":
		d.Manga = name
	default:
		d.Novel = name
	}
}

// BookDefaultFor lists the book kinds name is the default profile of, in
// BookKinds order.
func (c *Config) BookDefaultFor(name string) []string {
	out := []string{}
	for _, k := range BookKinds {
		if c.BookQualityDefaultProfiles.For(k) == name {
			out = append(out, k)
		}
	}
	return out
}

// ResolveMusicQualityProfile returns the profile named by name, falling back
// to MusicQualityDefaultProfile when name is empty or unknown. ok is false
// only when no music profiles are configured at all.
func ResolveMusicQualityProfile(name string) (MusicQualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return MusicQualityProfileEntry{}, false
	}
	if p, ok := findMusicProfile(c.MusicQualityProfiles, name); ok {
		return p, true
	}
	return findMusicProfile(c.MusicQualityProfiles, c.MusicQualityDefaultProfile)
}

// LookupMusicQualityProfile returns the profile named exactly name, with no
// fallback to the default.
func LookupMusicQualityProfile(name string) (MusicQualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return MusicQualityProfileEntry{}, false
	}
	return findMusicProfile(c.MusicQualityProfiles, name)
}

func findMusicProfile(
	profiles []MusicQualityProfileEntry,
	name string,
) (MusicQualityProfileEntry, bool) {
	if name == "" {
		return MusicQualityProfileEntry{}, false
	}
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return MusicQualityProfileEntry{}, false
}

// ResolveBookQualityProfile returns the profile named by name, falling back to
// the default of kind (novel, bd, comic, manga; empty reads as novel) when
// name is empty or unknown. ok is false only when no book profiles are
// configured at all.
func ResolveBookQualityProfile(
	name, kind string,
) (BookQualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return BookQualityProfileEntry{}, false
	}
	if p, ok := findBookProfile(c.BookQualityProfiles, name); ok {
		return p, true
	}
	return findBookProfile(
		c.BookQualityProfiles, c.BookQualityDefaultProfiles.For(kind),
	)
}

// LookupBookQualityProfile returns the profile named exactly name, with no
// fallback to a default.
func LookupBookQualityProfile(name string) (BookQualityProfileEntry, bool) {
	c := Get()
	if c == nil {
		return BookQualityProfileEntry{}, false
	}
	return findBookProfile(c.BookQualityProfiles, name)
}

func findBookProfile(
	profiles []BookQualityProfileEntry,
	name string,
) (BookQualityProfileEntry, bool) {
	if name == "" {
		return BookQualityProfileEntry{}, false
	}
	for _, p := range profiles {
		if p.Name == name {
			return p, true
		}
	}
	return BookQualityProfileEntry{}, false
}

// Profile is the entry as the scoring engine reads it. An unknown tier name is
// dropped, which validation has already refused on every path that stores one.
func (e MusicQualityProfileEntry) Profile() quality.MusicProfile {
	p := quality.MusicProfile{UpgradeAllowed: e.UpgradeAllowed}
	for _, name := range e.Tiers {
		if t, ok := quality.ParseAudioTier(name); ok {
			p.Tiers = append(p.Tiers, t)
		}
	}
	p.Preferred, _ = quality.ParseAudioTier(e.Preferred)
	return p
}

// EbookProfile is the ebook slot as the scoring engine reads it; the slot
// inherits the profile's single upgrade switch.
func (e BookQualityProfileEntry) EbookProfile() quality.EbookProfile {
	return quality.EbookProfile{
		Formats:        e.Ebook.Formats,
		Preferred:      e.Ebook.Preferred,
		UpgradeAllowed: e.UpgradeAllowed,
	}
}

// AudiobookProfile is the audiobook slot as the scoring engine reads it.
func (e BookQualityProfileEntry) AudiobookProfile() quality.AudiobookProfile {
	return quality.AudiobookProfile{
		Formats:        e.Audiobook.Formats,
		Preferred:      e.Audiobook.Preferred,
		MinBitrate:     e.Audiobook.MinBitrate,
		UpgradeAllowed: e.UpgradeAllowed,
	}
}

// check holds the rule the struct tags cannot express: the preferred tier must
// be one of the ticked ones. It runs from the mutators and from
// checkInvariants, so a hand-edited file fails the same way.
func (e MusicQualityProfileEntry) check() error {
	if !slices.Contains(e.Tiers, e.Preferred) {
		return fmt.Errorf(
			"music quality profile %q: preferred %q is not one of its tiers",
			e.Name, e.Preferred,
		)
	}
	return nil
}

func (e BookQualityProfileEntry) check() error {
	var errs []error
	if !slices.Contains(e.Ebook.Formats, e.Ebook.Preferred) {
		errs = append(errs, fmt.Errorf(
			"book quality profile %q: ebook preferred %q is not one of its formats",
			e.Name, e.Ebook.Preferred,
		))
	}
	if !slices.Contains(e.Audiobook.Formats, e.Audiobook.Preferred) {
		errs = append(errs, fmt.Errorf(
			"book quality profile %q: audiobook preferred %q is not one of its formats",
			e.Name,
			e.Audiobook.Preferred,
		))
	}
	if e.Audiobook.MinBitrate > MaxAudiobookBitrate {
		errs = append(errs, fmt.Errorf(
			"book quality profile %q: audiobook min_bitrate %d is above %d",
			e.Name, e.Audiobook.MinBitrate, MaxAudiobookBitrate,
		))
	}
	return errors.Join(errs...)
}

// MaxAudiobookBitrate bounds AudiobookSlot.MinBitrate in kbps.
const MaxAudiobookBitrate = 1024

// canonical returns values in ladder order; a value outside the ladder sorts
// last and is left for validation to reject.
func canonical(values, ladder []string) []string {
	out := slices.Clone(values)
	slices.SortStableFunc(out, func(a, b string) int {
		ia, ib := slices.Index(ladder, a), slices.Index(ladder, b)
		if ia < 0 {
			ia = len(ladder)
		}
		if ib < 0 {
			ib = len(ladder)
		}
		return ia - ib
	})
	return out
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
