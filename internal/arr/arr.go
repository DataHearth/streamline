// Package arr reads a Radarr or Sonarr v3 API. It holds the wire types and the
// pure translation of those types into streamline's config entries, and
// imports nothing from the server or the database.
package arr

import "context"

type App string

const (
	Radarr App = "radarr"
	Sonarr App = "sonarr"
)

// Library is the read surface of one *arr instance. Declared here rather than
// in the consumer so mockery generates one mock for every caller.
type Library interface {
	Status(ctx context.Context) (Status, error)
	RootFolders(ctx context.Context) ([]RootFolder, error)
	QualityProfiles(ctx context.Context) ([]QualityProfile, error)
	Indexers(ctx context.Context) ([]Provider, error)
	DownloadClients(ctx context.Context) ([]Provider, error)
	Movies(ctx context.Context) ([]Movie, error)
	Series(ctx context.Context) ([]Series, error)
	Episodes(ctx context.Context, seriesID uint32) ([]Episode, error)
	TestConnection(ctx context.Context) error
}

// Factory builds a Library for one instance. Injected the same way the other
// collaborators are, so tests hand it a mock instead of a server.
type Factory interface {
	Client(app App, baseURL, apiKey string) (Library, error)
}

type Status struct {
	AppName      string `json:"appName"`
	Version      string `json:"version"`
	InstanceName string `json:"instanceName"`
	URLBase      string `json:"urlBase"`
}

type RootFolder struct {
	ID         uint32 `json:"id"`
	Path       string `json:"path"`
	Accessible bool   `json:"accessible"`
}

type Quality struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	// Source is bluray|webdl|webrip|dvd|tv|... — no streamline equivalent.
	Source string `json:"source"`
	// Resolution is an integer (480, 720, 1080, 2160), not a band string.
	Resolution int `json:"resolution"`
}

// QualityItem is either a leaf (Quality set) or a group (Name + Items set).
// A profile's Cutoff id can name either, which is why Items is walked.
type QualityItem struct {
	ID      uint32        `json:"id"`
	Name    string        `json:"name"`
	Quality *Quality      `json:"quality"`
	Items   []QualityItem `json:"items"`
	Allowed bool          `json:"allowed"`
}

type FormatItem struct {
	Format uint32 `json:"format"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
}

type QualityProfile struct {
	ID                uint32        `json:"id"`
	Name              string        `json:"name"`
	UpgradeAllowed    bool          `json:"upgradeAllowed"`
	Cutoff            uint32        `json:"cutoff"`
	Items             []QualityItem `json:"items"`
	MinFormatScore    int           `json:"minFormatScore"`
	CutoffFormatScore int           `json:"cutoffFormatScore"`
	// MinUpgradeFormatScore is the smallest score gain an upgrade must bring.
	// Streamline has no such threshold, so it only surfaces as a note.
	MinUpgradeFormatScore int          `json:"minUpgradeFormatScore"`
	FormatItems           []FormatItem `json:"formatItems"`
}

// Field is one provider setting. Every indexer and download-client setting
// lives in this array rather than a named property, so callers read by name.
// Privacy is normal|password|apiKey|userName and marks the secrets.
type Field struct {
	Name    string `json:"name"`
	Value   any    `json:"value"`
	Privacy string `json:"privacy"`
}

type Provider struct {
	ID                    uint32  `json:"id"`
	Name                  string  `json:"name"`
	Implementation        string  `json:"implementation"`
	Protocol              string  `json:"protocol"`
	Priority              int     `json:"priority"`
	Enable                bool    `json:"enable"`
	EnableRSS             bool    `json:"enableRss"`
	EnableAutomaticSearch bool    `json:"enableAutomaticSearch"`
	Fields                []Field `json:"fields"`
}

type MovieFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
	ReleaseGroup string `json:"releaseGroup"`
}

type Movie struct {
	ID               uint32 `json:"id"`
	TMDBID           uint32 `json:"tmdbId"`
	Title            string `json:"title"`
	Year             uint16 `json:"year"`
	Monitored        bool   `json:"monitored"`
	QualityProfileID uint32 `json:"qualityProfileId"`
	Path             string `json:"path"`
	RootFolderPath   string `json:"rootFolderPath"`
	HasFile          bool   `json:"hasFile"`
	// MovieFile is the whole file record, embedded: one GET /movie is the
	// entire Radarr library, files included.
	MovieFile *MovieFile `json:"movieFile"`
}

type Season struct {
	SeasonNumber uint16 `json:"seasonNumber"`
	Monitored    bool   `json:"monitored"`
}

type Series struct {
	ID               uint32            `json:"id"`
	TVDBID           uint32            `json:"tvdbId"`
	Title            string            `json:"title"`
	Year             uint16            `json:"year"`
	Monitored        bool              `json:"monitored"`
	QualityProfileID uint32            `json:"qualityProfileId"`
	Path             string            `json:"path"`
	RootFolderPath   string            `json:"rootFolderPath"`
	SeriesType       string            `json:"seriesType"` // standard|daily|anime
	Seasons          []Season          `json:"seasons"`
	Statistics       *SeriesStatistics `json:"statistics"`
}

type SeriesStatistics struct {
	EpisodeFileCount int `json:"episodeFileCount"`
}

type EpisodeFile struct {
	ID   uint32 `json:"id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type Episode struct {
	SeasonNumber  uint16 `json:"seasonNumber"`
	EpisodeNumber uint16 `json:"episodeNumber"`
	Monitored     bool   `json:"monitored"`
	HasFile       bool   `json:"hasFile"`
	// EpisodeFileID is the authoritative file link: EpisodeFileResource has no
	// episodeIds of its own, the episode points at the file. Fetching with
	// includeEpisodeFile=true fills EpisodeFile in the same response on a
	// Sonarr that honours it; Episodes joins the rest from /episodefile.
	EpisodeFileID uint32       `json:"episodeFileId"`
	EpisodeFile   *EpisodeFile `json:"episodeFile"`
}
