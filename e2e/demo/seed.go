package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/e2e/demomedia"
	"github.com/datahearth/streamline/internal/library/audiotags"
)

// seeder fills the instance. A step that fails is logged and skipped: a
// title a provider stopped answering for, or a cassette entry that is
// missing, costs that title, not the run.
type seeder struct {
	in     *instance
	api    *api
	member *api
	record bool

	// What the shots open, filled as the library is built.
	movieIDs   map[string]int
	showIDs    map[string]int
	artistIDs  map[string]int
	bookIDs    map[string]int
	seriesIDs  map[string]int
	pendingImp int
	failures   []string
}

func (s *seeder) warn(what string, err error) {
	slog.WarnContext(
		context.Background(),
		"seed step skipped",
		"step",
		what,
		"error",
		err,
	)
	s.failures = append(s.failures, what+": "+err.Error())
}

func (s *seeder) seed(ctx context.Context) error {
	s.movieIDs, s.showIDs, s.artistIDs = map[string]int{}, map[string]int{}, map[string]int{}
	s.bookIDs, s.seriesIDs = map[string]int{}, map[string]int{}
	if err := s.api.login(ctx, adminEmail, demoPassword); err != nil {
		return err
	}
	var me struct {
		ID int `json:"id"`
	}
	if err := s.api.get(ctx, "/auth/me", nil, &me); err != nil {
		return err
	}
	if err := s.api.patch(
		ctx,
		"/auth/me",
		map[string]any{"display_name": "Demo Admin"},
		nil,
	); err != nil {
		s.warn("admin display name", err)
	}
	if err := s.profiles(ctx); err != nil {
		s.warn("quality profiles", err)
	}

	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"movie library", s.movieLibrary},
		{"series library", s.seriesLibrary},
		{"music library", s.musicLibrary},
		{"book library", s.bookLibrary},
		{"book series", s.followBookSeries},
		{"wanted titles", s.wanted},
		{"downloads", s.downloads},
		{"requests", s.requests},
		{"pending import", s.pendingImport},
		{"discographies", s.discographies},
		{"artwork", s.artwork},
	}
	for _, st := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.InfoContext(ctx, "seeding", "step", st.name)
		start := time.Now()
		if err := st.fn(ctx); err != nil {
			s.warn(st.name, err)
		}
		slog.InfoContext(
			ctx,
			"seeded",
			"step",
			st.name,
			"took",
			time.Since(start).Round(time.Second),
		)
	}
	if len(s.failures) > 0 {
		slog.WarnContext(
			ctx,
			"the demo library is incomplete",
			"skipped",
			len(s.failures),
		)
		for _, f := range s.failures {
			slog.WarnContext(ctx, "  "+f)
		}
	}
	return nil
}

// profiles adds a 4K profile for films; series stay on 1080p.
func (s *seeder) profiles(ctx context.Context) error {
	err := s.api.post(ctx, "/quality-profiles", map[string]any{
		"name":                 "Ultra HD",
		"preferred_resolution": "2160p",
		"min_resolution":       "1080p",
		"upgrade_allowed":      true,
	}, nil)
	if err != nil {
		return err
	}
	return s.api.post(
		ctx,
		"/quality-profiles/"+url.PathEscape("Ultra HD")+"/default?media=movie",
		nil,
		nil,
	)
}

// ---- movies ----

func filmFile(root string, f film) string {
	dotted := strings.NewReplacer(" ", ".", ":", "", "'", "", "!", "").
		Replace(f.Title)
	folder := strings.NewReplacer(":", "", "!", "").Replace(f.Title)
	audio := "AAC5.1"
	extra := "x265.10bit"
	if f.Height == 2160 {
		audio = "TrueHD.7.1.Atmos"
		if f.HDR {
			extra = "DV.HDR.x265"
		}
	}
	name := fmt.Sprintf(
		"%s.%d.%dp.%s.%s.%s-%s.mkv",
		dotted,
		f.Year,
		f.Height,
		f.Source,
		extra,
		audio,
		f.Group,
	)
	return filepath.Join(root, fmt.Sprintf("%s (%d)", folder, f.Year), name)
}

func writeFilm(root string, f film) error {
	channels := 6
	audio := "eac3"
	if f.Height == 2160 {
		channels = 8
	}
	return demomedia.Video(filmFile(root, f), demomedia.VideoSpec{
		Height:   f.Height,
		HDR:      f.HDR,
		Audio:    audio,
		Channels: channels,
		Size:     f.SizeMB << 20,
	})
}

func (s *seeder) movieLibrary(ctx context.Context) error {
	for _, f := range libraryFilms {
		if err := writeFilm(s.in.paths["movies"], f); err != nil {
			return err
		}
	}
	if _, err := s.importScan(ctx, s.in.paths["movies"], "movie", true); err != nil {
		return err
	}
	return s.indexMovies(ctx)
}

func (s *seeder) indexMovies(ctx context.Context) error {
	var page struct {
		Items []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := s.api.get(
		ctx,
		"/movies",
		url.Values{"limit": {"100"}},
		&page,
	); err != nil {
		return err
	}
	for _, m := range page.Items {
		s.movieIDs[m.Title] = m.ID
	}
	return nil
}

// addFilm looks a film up by title and year and adds it monitored.
func (s *seeder) addFilm(ctx context.Context, title string, year int) (int, error) {
	if id, ok := s.movieIDs[title]; ok {
		return id, nil
	}
	var hits []struct {
		TMDBID int    `json:"tmdb_id"`
		Title  string `json:"title"`
		Year   int    `json:"year"`
	}
	if err := s.api.get(
		ctx,
		"/search/movie",
		url.Values{"q": {title}, "year": {strconv.Itoa(year)}},
		&hits,
	); err != nil {
		return 0, err
	}
	for _, h := range hits {
		if h.Year == year || year == 0 {
			var m struct {
				ID int `json:"id"`
			}
			if err := s.api.post(
				ctx,
				"/movies",
				map[string]any{"tmdb_id": h.TMDBID},
				&m,
			); err != nil {
				return 0, err
			}
			s.movieIDs[title] = m.ID
			return m.ID, nil
		}
	}
	return 0, fmt.Errorf("no TMDB match for %s (%d)", title, year)
}

// ---- series ----

func (s *seeder) seriesLibrary(ctx context.Context) error {
	for _, sh := range libraryShows {
		dotted := strings.ReplaceAll(sh.Title, " ", ".")
		for season, eps := range sh.Seasons {
			for ep := 1; ep <= eps; ep++ {
				name := fmt.Sprintf(
					"%s.S%02dE%02d.1080p.BluRay.x265.AAC2.0-DEMO.mkv",
					dotted,
					season,
					ep,
				)
				path := filepath.Join(
					s.in.paths["series"],
					fmt.Sprintf("%s (%d)", sh.Title, sh.Year),
					fmt.Sprintf("Season %02d", season),
					name,
				)
				if err := demomedia.Video(path, demomedia.VideoSpec{
					Height:   1080,
					Audio:    "aac",
					Channels: 2,
					Duration: time.Hour,
					Size:     sh.EpMB << 20,
				}); err != nil {
					return err
				}
			}
		}
	}
	if _, err := s.importScan(
		ctx,
		s.in.paths["series"],
		"series",
		true,
	); err != nil {
		return err
	}
	return s.indexShows(ctx)
}

func (s *seeder) indexShows(ctx context.Context) error {
	var page struct {
		Items []struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := s.api.get(
		ctx,
		"/series",
		url.Values{"limit": {"100"}},
		&page,
	); err != nil {
		return err
	}
	for _, m := range page.Items {
		s.showIDs[m.Title] = m.ID
	}
	return nil
}

// ---- music ----

type albumTrack struct {
	Disc   int    `json:"disc"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type musicAlbum struct {
	ID          int          `json:"id"`
	MBID        string       `json:"mbid"`
	Title       string       `json:"title"`
	ReleaseDate string       `json:"release_date"`
	Status      string       `json:"status"`
	Tracks      []albumTrack `json:"tracks"`
	Pending     bool         `json:"tracks_pending"`
}

type musicArtist struct {
	ID        int          `json:"id"`
	Name      string       `json:"name"`
	Hydrating bool         `json:"hydrating"`
	Albums    []musicAlbum `json:"albums"`
}

func (s *seeder) findArtist(ctx context.Context, name string) (string, error) {
	var res struct {
		Items []struct {
			MBID string `json:"mbid"`
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := s.api.get(
		ctx,
		"/music/search",
		url.Values{"query": {name}},
		&res,
	); err != nil {
		return "", err
	}
	for _, it := range res.Items {
		if strings.EqualFold(it.Name, name) {
			return it.MBID, nil
		}
	}
	return "", fmt.Errorf("no MusicBrainz artist named %q", name)
}

func findAlbum(a *musicArtist, title string) *musicAlbum {
	for i := range a.Albums {
		if strings.EqualFold(a.Albums[i].Title, title) {
			return &a.Albums[i]
		}
	}
	return nil
}

// waitTracks makes sure the named albums have their track lists. Waiting on
// the background worker could take minutes per artist; choosing the automatic
// edition again re-fetches an album's tracks in the request, at interactive
// priority. A MusicBrainz refusal (429) is waited out.
func (s *seeder) waitTracks(
	ctx context.Context,
	id int,
	titles []string,
) (*musicArtist, error) {
	var a musicArtist
	err := waitFor(
		ctx,
		"album tracks",
		15*time.Minute,
		2*time.Second,
		func() (bool, error) {
			a = musicArtist{}
			if err := s.api.get(
				ctx,
				"/music/artists/"+strconv.Itoa(id),
				nil,
				&a,
			); err != nil {
				return false, err
			}
			for _, t := range titles {
				al := findAlbum(&a, t)
				if al == nil {
					return false, fmt.Errorf("%s has no album %q", a.Name, t)
				}
				if len(al.Tracks) > 0 && !al.Pending {
					continue
				}
				err := s.api.do(
					ctx,
					http.MethodPut,
					"/music/albums/"+strconv.Itoa(al.ID)+"/editions",
					map[string]any{"release_mbid": ""},
					nil,
				)
				var se *statusError
				if errors.As(err, &se) && se.Status == http.StatusTooManyRequests {
					slog.InfoContext(ctx, "MusicBrainz asks to wait", "album", t)
					time.Sleep(20 * time.Second)
					return false, nil
				}
				if err != nil {
					return false, err
				}
				return false, nil
			}
			return true, nil
		},
	)
	return &a, err
}

func (s *seeder) musicLibrary(ctx context.Context) error {
	// Follow every artist first: the add answers with stubs and the
	// background worker starts on all of them at once.
	for _, ar := range artists {
		mbid, err := s.findArtist(ctx, ar.Name)
		if err != nil {
			s.warn("artist "+ar.Name, err)
			continue
		}
		var a musicArtist
		if err := s.api.post(
			ctx,
			"/music/artists",
			map[string]any{"mbid": mbid, "monitor": "all"},
			&a,
		); err != nil {
			s.warn("artist "+ar.Name, err)
			continue
		}
		s.artistIDs[ar.Name] = a.ID
	}
	adopted := 0
	for _, ar := range artists {
		id, ok := s.artistIDs[ar.Name]
		if !ok {
			continue
		}
		full, err := s.waitTracks(
			ctx,
			id,
			append(append([]string{}, ar.Albums...), ar.Grab...),
		)
		if err != nil {
			s.warn("artist "+ar.Name, err)
			continue
		}
		for _, title := range ar.Albums {
			al := findAlbum(full, title)
			year, _, _ := strings.Cut(al.ReleaseDate, "-")
			dir := filepath.Join(
				s.in.paths["music"],
				safeFile(ar.Name),
				safeFile(fmt.Sprintf("%s (%s)", al.Title, year)),
			)
			for _, t := range al.Tracks {
				name := fmt.Sprintf("%02d - %s.mp3", t.Number, safeFile(t.Title))
				if t.Disc > 1 {
					name = fmt.Sprintf(
						"%d-%02d - %s.mp3",
						t.Disc,
						t.Number,
						safeFile(t.Title),
					)
				}
				if err := demomedia.Track(filepath.Join(dir, name), audiotags.Tags{
					audiotags.KeyArtist:      {ar.Name},
					audiotags.KeyAlbumArtist: {ar.Name},
					audiotags.KeyAlbum:       {al.Title},
					audiotags.KeyTitle:       {t.Title},
					audiotags.KeyTrackNumber: {strconv.Itoa(t.Number)},
					audiotags.KeyDiscNumber:  {strconv.Itoa(max(t.Disc, 1))},
					audiotags.KeyDate:        {year},
					// The release group pins the adoption to this album
					// whatever the title search would rank first.
					audiotags.KeyMBReleaseGroupID: {al.MBID},
				}); err != nil {
					return err
				}
			}
			adopted++
		}
	}
	if adopted == 0 {
		return errors.New("no album to adopt")
	}
	_, err := s.importScan(ctx, s.in.paths["music"], "music", true)
	return err
}

// ---- books ----

func (s *seeder) bookLibrary(ctx context.Context) error {
	for _, b := range libraryBooks {
		path := filepath.Join(
			s.in.paths["books"],
			safeFile(b.Author),
			safeFile(b.Title),
			safeFile(b.Title)+".epub",
		)
		if err := demomedia.Ebook(
			path,
			demomedia.EbookMeta{Title: b.Title, Author: b.Author},
		); err != nil {
			return err
		}
	}
	if _, err := s.importScan(ctx, s.in.paths["books"], "book", true); err != nil {
		return err
	}
	return s.indexBooks(ctx)
}

func (s *seeder) indexBooks(ctx context.Context) error {
	var page struct {
		Items []struct {
			Type  string `json:"type"`
			ID    int    `json:"id"`
			Title string `json:"title"`
			Book  *struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"book"`
			Series *struct {
				ID    int    `json:"id"`
				Title string `json:"title"`
			} `json:"series"`
		} `json:"items"`
	}
	if err := s.api.get(
		ctx,
		"/books",
		url.Values{"limit": {"100"}},
		&page,
	); err != nil {
		return err
	}
	for _, it := range page.Items {
		switch {
		case it.Book != nil:
			s.bookIDs[it.Book.Title] = it.Book.ID
		case it.Series != nil:
			s.seriesIDs[it.Series.Title] = it.Series.ID
		case it.Type == "series":
			s.seriesIDs[it.Title] = it.ID
		default:
			s.bookIDs[it.Title] = it.ID
		}
	}
	return nil
}

type bookHit struct {
	HardcoverID int    `json:"hardcover_id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	Year        int    `json:"year"`
}

func (s *seeder) findBook(
	ctx context.Context,
	kind string,
	b book,
) (*bookHit, error) {
	var res struct {
		Items []bookHit `json:"items"`
	}
	if err := s.api.get(
		ctx,
		"/books/search",
		url.Values{"query": {b.Title + " " + b.Author}, "type": {kind}},
		&res,
	); err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		if err := s.api.get(
			ctx,
			"/books/search",
			url.Values{"query": {b.Title}, "type": {kind}},
			&res,
		); err != nil {
			return nil, err
		}
	}
	var fallback *bookHit
	for i, h := range res.Items {
		if h.Type != "" && h.Type != kind {
			continue
		}
		if strings.Contains(
			strings.ToLower(h.Author),
			strings.ToLower(lastWord(b.Author)),
		) {
			if strings.EqualFold(h.Title, b.Title) {
				return &res.Items[i], nil
			}
			if fallback == nil {
				fallback = &res.Items[i]
			}
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("no Hardcover %s for %q", kind, b.Title)
}

func (s *seeder) followBookSeries(ctx context.Context) error {
	for _, b := range bookSeries {
		hit, err := s.findBook(ctx, "series", b)
		if err != nil {
			s.warn("book series "+b.Title, err)
			continue
		}
		var out struct {
			ID    int    `json:"id"`
			Title string `json:"title"`
		}
		if err := s.api.post(
			ctx,
			"/books/series",
			map[string]any{"hardcover_id": hit.HardcoverID, "monitor": "all"},
			&out,
		); err != nil {
			s.warn("book series "+b.Title, err)
			continue
		}
		s.seriesIDs[b.Title] = out.ID
	}
	// Phase two fills the volumes in the background; the shelf and the series
	// page read better once it has.
	_ = waitFor(
		ctx,
		"series hydration",
		10*time.Minute,
		3*time.Second,
		func() (bool, error) {
			for _, id := range s.seriesIDs {
				var sr struct {
					Hydrating bool `json:"hydrating"`
				}
				if err := s.api.get(
					ctx,
					"/books/series/"+strconv.Itoa(id),
					nil,
					&sr,
				); err != nil {
					return false, err
				}
				if sr.Hydrating {
					return false, nil
				}
			}
			return true, nil
		},
	)
	return s.indexBooks(ctx)
}

func (s *seeder) addBook(ctx context.Context, b book) (int, error) {
	if id, ok := s.bookIDs[b.Title]; ok {
		return id, nil
	}
	hit, err := s.findBook(ctx, "book", b)
	if err != nil {
		return 0, err
	}
	var out struct {
		ID int `json:"id"`
	}
	if err := s.api.post(
		ctx,
		"/books",
		map[string]any{"hardcover_id": hit.HardcoverID, "monitor": "both"},
		&out,
	); err != nil {
		return 0, err
	}
	s.bookIDs[b.Title] = out.ID
	return out.ID, nil
}

// ---- wanted ----

func (s *seeder) wanted(ctx context.Context) error {
	for _, f := range wantedFilms {
		if _, err := s.addFilm(ctx, f.Title, f.Year); err != nil {
			s.warn("wanted "+f.Title, err)
		}
	}
	for _, b := range wantedBooks {
		if _, err := s.addBook(ctx, b); err != nil {
			s.warn("wanted "+b.Title, err)
		}
	}
	return nil
}

// ---- imports ----

type importScan struct {
	ID        int    `json:"id"`
	Status    string `json:"status"`
	Reason    string `json:"failure_reason"`
	Total     int    `json:"total_count"`
	Committed int    `json:"commit_success_count"`
	Failed    int    `json:"commit_failed_count"`
}

// importScan scans root, accepts every confirmed row and, with commit,
// adopts them in place.
func (s *seeder) importScan(
	ctx context.Context,
	root, kind string,
	commit bool,
) (int, error) {
	var sc importScan
	if err := s.api.post(ctx, "/library/imports", map[string]any{
		"source_path": root, "kind": kind, "mode": "in_place",
	}, &sc); err != nil {
		return 0, err
	}
	path := "/library/imports/" + strconv.Itoa(sc.ID)
	wait := func(want ...string) error {
		return waitFor(
			ctx,
			kind+" import scan",
			20*time.Minute,
			time.Second,
			func() (bool, error) {
				if err := s.api.get(ctx, path, nil, &sc); err != nil {
					return false, err
				}
				switch sc.Status {
				case "failed", "cancelled":
					return false, fmt.Errorf("scan %s: %s", sc.Status, sc.Reason)
				}
				return slices.Contains(want, sc.Status), nil
			},
		)
	}
	if err := wait("awaiting_review"); err != nil {
		return sc.ID, err
	}
	if !commit {
		return sc.ID, nil
	}
	if err := s.api.post(
		ctx,
		path+"/decisions",
		map[string]any{"decision": "accept", "classification": "confirmed"},
		nil,
	); err != nil {
		return sc.ID, err
	}
	if err := s.api.post(ctx, path+"/commit", map[string]any{}, nil); err != nil {
		return sc.ID, err
	}
	if err := wait("completed"); err != nil {
		return sc.ID, err
	}
	slog.InfoContext(
		ctx,
		"import committed",
		"kind",
		kind,
		"rows",
		sc.Total,
		"adopted",
		sc.Committed,
		"failed",
		sc.Failed,
	)
	if sc.Committed == 0 {
		return sc.ID, fmt.Errorf(
			"the %s scan adopted nothing (%d rows): its provider answered no match",
			kind,
			sc.Total,
		)
	}
	return sc.ID, nil
}

// pendingImport drops more films into the movie root and scans it again,
// leaving the scan awaiting review: an in-place scan must stay inside the
// library, so the films already adopted show as existing beside the new ones.
func (s *seeder) pendingImport(ctx context.Context) error {
	root := s.in.paths["movies"]
	for _, f := range incomingFilms {
		if err := writeFilm(root, f); err != nil {
			return err
		}
	}
	// A file no provider knows, so the review has a decision to ask for.
	if err := demomedia.Video(
		filepath.Join(root, "Home Movies", "family.reunion.2019.mkv"),
		demomedia.VideoSpec{
			Height:   1080,
			Codec:    "h264",
			Audio:    "aac",
			Channels: 2,
			Size:     2400 << 20,
		},
	); err != nil {
		return err
	}
	id, err := s.importScan(ctx, root, "movie", false)
	s.pendingImp = id
	return err
}

// discographies waits for every followed artist's albums to be hydrated.
// Recording, MusicBrainz's pace keeps the background worker going for
// minutes after the seeding; shooting before it ends would leave albums
// nobody recorded, which a replay, unthrottled, then asks for and misses.
func (s *seeder) discographies(ctx context.Context) error {
	timeout := 2 * time.Minute
	if s.record {
		timeout = 30 * time.Minute
	}
	return waitFor(
		ctx,
		"discography hydration",
		timeout,
		5*time.Second,
		func() (bool, error) {
			for name, id := range s.artistIDs {
				var a musicArtist
				if err := s.api.get(
					ctx,
					"/music/artists/"+strconv.Itoa(id),
					nil,
					&a,
				); err != nil {
					return false, err
				}
				if a.Hydrating {
					slog.DebugContext(ctx, "still hydrating", "artist", name)
					return false, nil
				}
			}
			return true, nil
		},
	)
}

// artwork waits for the posters the background fetches bring in: a page shot
// before its poster lands shows the placeholder.
func (s *seeder) artwork(ctx context.Context) error {
	urls := make(
		[]string,
		0,
		len(s.movieIDs)+len(s.showIDs)+len(s.bookIDs)+len(s.artistIDs),
	)
	for _, id := range s.movieIDs {
		urls = append(urls, fmt.Sprintf("/posters/movies/%d/poster.jpg", id))
	}
	for _, id := range s.showIDs {
		urls = append(urls, fmt.Sprintf("/posters/tvshows/%d/poster.jpg", id))
	}
	for _, id := range s.bookIDs {
		urls = append(urls, fmt.Sprintf("/posters/books/%d/poster.jpg", id))
	}
	for _, id := range s.artistIDs {
		urls = append(urls, fmt.Sprintf("/posters/artists/%d/poster.jpg", id))
	}
	deadline := time.Now().Add(3 * time.Minute)
	missing := urls
	for len(missing) > 0 && time.Now().Before(deadline) {
		var still []string
		for _, u := range missing {
			if !s.posterReady(ctx, u) {
				still = append(still, u)
			}
		}
		missing = still
		if len(missing) > 0 {
			time.Sleep(2 * time.Second)
		}
	}
	if len(missing) > 0 {
		slog.InfoContext(
			ctx,
			"artwork never arrived",
			"count",
			len(missing),
			"first",
			missing[0],
		)
	}
	return nil
}

func (s *seeder) posterReady(ctx context.Context, path string) bool {
	req, err := newAuthed(ctx, s.api, s.in.base+path)
	if err != nil {
		return false
	}
	resp, err := s.api.hc.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ---- helpers ----

func safeFile(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, s)
}

func lastWord(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	return f[len(f)-1]
}
