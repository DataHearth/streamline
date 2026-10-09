package music

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/musiccredit"
	"github.com/datahearth/streamline/internal/quality"
)

// Album statuses as the API reports them. Upcoming is derived, never stored:
// a wanted album dated after today.
const (
	StatusUpcoming = "upcoming"
)

// PersonView is a credited person. ArtistID is the library artist the
// MusicBrainz id belongs to, 0 when the person is not in the library.
type PersonView struct {
	Name     string
	MBID     string
	ArtistID uint32
}

type CreditView struct {
	PersonView
	Role string
}

type PerformerView struct {
	PersonView
	Instruments []string
	Guest       bool
}

type MemberView struct {
	PersonView
	Instruments []string
	// From and To are years; 0 is absent. From falls back to the artist's
	// since when the relationship carries none.
	From uint16
	To   uint16
}

type TrackView struct {
	Track     *ent.Track
	Featuring []PersonView
	Writers   []PersonView
	HasFile   bool
}

// AlbumView is an album with the rollups the API reports. Tracks, Credits and
// Personnel are filled by the detail reads only; a tile carries none.
type AlbumView struct {
	Album  *ent.Album
	Status string
	// TrackCount, TracksHave, Size and Duration roll up the album's tracks and
	// their files.
	TrackCount uint32
	TracksHave uint32
	Size       int64
	Duration   uint32
	// Progress is the live torrent progress as a percentage, set only while
	// the album is downloading.
	Progress      *float64
	TracksPending bool
	// Quality is the weakest tier among the album's files, empty with no
	// tiered file; Format is the label of the weakest file.
	Quality   string
	Format    string
	Tracks    []TrackView
	Credits   []CreditView
	Personnel []PerformerView
}

// ArtistView is an artist with its rollups. Members is filled by the detail
// read only.
type ArtistView struct {
	Artist         *ent.Artist
	Status         string
	AlbumCount     uint32
	TracksHave     uint32
	Size           int64
	Hydrating      bool
	Overview       string
	OverviewSource string
	Albums         []AlbumView
	Members        []MemberView
}

// ArtistPage is one page of the artist list.
type ArtistPage struct {
	Items []ArtistView
	Total uint32
}

// albumStatusAt is the album's stored status, with wanted reported as upcoming
// while its release date is still ahead. An undated album is never upcoming.
func albumStatusAt(a *ent.Album, now time.Time) string {
	if a.Status == album.StatusWanted && a.ReleaseDate != nil &&
		a.ReleaseDate.After(now) {
		return StatusUpcoming
	}
	return string(a.Status)
}

// artistStatusAt rolls the monitored, non-upcoming albums up: downloading if
// any is downloading or paused, else wanted if any is wanted, else available.
// An artist with nothing monitored is available.
func artistStatusAt(albums []*ent.Album, now time.Time) string {
	wanted := false
	for _, a := range albums {
		if !a.Monitored {
			continue
		}
		switch albumStatusAt(a, now) {
		case string(album.StatusDownloading), string(album.StatusPaused):
			return string(album.StatusDownloading)
		case string(album.StatusWanted):
			wanted = true
		}
	}
	if wanted {
		return string(album.StatusWanted)
	}
	return string(album.StatusAvailable)
}

// byNewest orders albums newest first, undated last, then by title.
func byNewest(a, b *ent.Album) int {
	switch {
	case a.ReleaseDate == nil && b.ReleaseDate != nil:
		return 1
	case a.ReleaseDate != nil && b.ReleaseDate == nil:
		return -1
	case a.ReleaseDate != nil && b.ReleaseDate != nil:
		if c := b.ReleaseDate.Compare(*a.ReleaseDate); c != 0 {
			return c
		}
	}
	return cmp.Or(strings.Compare(a.Title, b.Title), cmp.Compare(a.ID, b.ID))
}

// pickOverview serves the overview in lang, falling back to English when that
// locale has no article. The source follows the text actually served.
func pickOverview(a *ent.Artist, lang string) (string, string) {
	if lang == "fr" && a.OverviewFr != "" {
		return a.OverviewFr, a.OverviewSourceFr
	}
	return a.Overview, a.OverviewSource
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// worstTier is the lowest tier among the files that state one; ok is false
// when none does.
func worstTier(files []*ent.MediaFile) (quality.AudioTier, bool) {
	var (
		worst quality.AudioTier
		any   bool
	)
	for _, f := range files {
		t, ok := quality.ParseAudioTier(f.Quality)
		if !ok {
			continue
		}
		if !any || t < worst {
			worst, any = t, true
		}
	}
	return worst, any
}

// formatLabel names the album's weakest file: its codec, or its extension
// when it was never probed, in capitals, with the rate in kbps appended for a
// lossy file whose bit rate is known ("MP3 320"). A lossless file carries no
// suffix: no bit depth or sample rate is stored, and the tier chip already
// says hires.
func formatLabel(files []*ent.MediaFile) string {
	var (
		pick  *ent.MediaFile
		pickT quality.AudioTier
		known bool
	)
	for _, f := range files {
		t, ok := quality.ParseAudioTier(f.Quality)
		switch {
		case pick == nil,
			ok && (!known || t < pickT),
			ok == known && ok && t == pickT && f.Bitrate < pick.Bitrate:
			pick, pickT, known = f, t, ok
		}
	}
	if pick == nil {
		return ""
	}
	label := strings.ToUpper(pick.AudioCodec)
	if label == "" {
		label = strings.ToUpper(pick.Format)
	}
	if label == "" {
		return ""
	}
	if known && pickT <= quality.TierHigh && pick.Bitrate > 0 {
		label += " " + strconv.FormatUint(uint64(pick.Bitrate/1000), 10)
	}
	return label
}

// albumFiles collects every media file of an album whose tracks are loaded
// with theirs.
func albumFiles(a *ent.Album) []*ent.MediaFile {
	var out []*ent.MediaFile
	for _, t := range a.Edges.Tracks {
		out = append(out, t.Edges.MediaFiles...)
	}
	return out
}

// creditsOf splits an edge's credits by kind, in ordinal order.
func creditsOf(
	rows []*ent.MusicCredit,
	kind musiccredit.Kind,
) []*ent.MusicCredit {
	var out []*ent.MusicCredit
	for _, c := range rows {
		if c.Kind == kind {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b *ent.MusicCredit) int {
		return cmp.Compare(a.Ordinal, b.Ordinal)
	})
	return out
}
