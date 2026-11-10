package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/datahearth/streamline/internal/library/audiotags"
)

// obj and list are the loosely-typed JSON the seeding reads: it needs a few
// fields of large responses, and the program must not drift from the spec's
// generated types by depending on them.
type (
	obj  = map[string]any
	list = []any
)

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func str(v any) string { s, _ := v.(string); return s }

func objs(v any) []obj {
	var out []obj
	if l, ok := v.(list); ok {
		for _, e := range l {
			if o, ok := e.(obj); ok {
				out = append(out, o)
			}
		}
	}
	return out
}

// items reads a list answer, bare or wrapped in {items}.
func items(v any) []obj {
	if o, ok := v.(obj); ok {
		return objs(o["items"])
	}
	return objs(v)
}

func fold(s string) string { return strings.Join(words(s), " ") }

func isConflict(err error) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.Status == http.StatusConflict
}

// addTitles adds every catalog title. The indexer and the client are not
// connected yet, so the searches an add sets off find nothing and every grab
// stays the seeding's own.
func (d *demo) addTitles(ctx context.Context) error {
	d.ids = ids{
		map[string]int{},
		map[string]int{},
		map[string]int{},
		map[string]int{},
		map[string]int{},
		map[string]int{},
	}

	var users any
	if err := d.admin.get(ctx, "/users", nil, &users); err != nil {
		return err
	}
	for _, u := range items(users) {
		if str(u["email"]) == adminEmail {
			if err := d.admin.do(
				ctx,
				http.MethodPatch,
				fmt.Sprintf("/users/%d", num(u["id"])),
				obj{"display_name": "Demo Admin"},
				nil,
			); err != nil {
				return err
			}
		}
	}

	for _, m := range movies {
		var out obj
		if err := retry(ctx, 2*time.Minute, func() error {
			return d.admin.post(ctx, "/movies", obj{"tmdb_id": m.TMDB}, &out)
		}); err != nil {
			return fmt.Errorf("movie %s: %w", m.Title, err)
		}
		d.ids.movies[m.Title] = num(out["id"])
	}
	for _, s := range series {
		var out obj
		if err := retry(ctx, 2*time.Minute, func() error {
			return d.admin.post(
				ctx,
				"/series",
				obj{"tvdb_id": s.TVDB, "preset": "all"},
				&out,
			)
		}); err != nil {
			return fmt.Errorf("series %s: %w", s.Title, err)
		}
		d.ids.series[s.Title] = num(out["id"])
	}
	for _, a := range artists {
		var out obj
		if err := retry(ctx, 3*time.Minute, func() error {
			return d.admin.post(
				ctx,
				"/music/artists",
				obj{"mbid": a.MBID, "monitor": "all"},
				&out,
			)
		}); err != nil {
			return fmt.Errorf("artist %s: %w", a.Name, err)
		}
		d.ids.artists[a.Name] = num(out["id"])
	}
	for _, b := range books {
		monitor := "ebook"
		if b.Audiobook != "" {
			monitor = "both"
		}
		var out obj
		if err := retry(ctx, 3*time.Minute, func() error {
			return d.admin.post(
				ctx,
				"/books",
				obj{"hardcover_id": b.Hardcover, "monitor": monitor},
				&out,
			)
		}); err != nil {
			return fmt.Errorf("book %s: %w", b.Title, err)
		}
		d.ids.books[b.Title] = num(out["id"])
	}
	for _, s := range bookSeries {
		var out obj
		if err := retry(ctx, 3*time.Minute, func() error {
			return d.admin.post(
				ctx,
				"/books/series",
				obj{"hardcover_id": s.Hardcover, "monitor": "all"},
				&out,
			)
		}); err != nil {
			return fmt.Errorf("book series %s: %w", s.Title, err)
		}
		d.ids.bseries[s.Title] = num(out["id"])
	}
	return nil
}

// albumInfo is what writing an album's files needs from the library.
type albumInfo struct {
	id     int
	year   string
	tracks []obj
}

// volumeInfo is one book-series volume to import.
type volumeInfo struct {
	series, title, author string
	bookID                int
}

var (
	albumFacts  = map[string]albumInfo{}
	volumeFacts []volumeInfo
)

// waitForMetadata waits for the albums the seeding grabs to have their
// tracks, and the book series to have every volume.
func (d *demo) waitForMetadata(ctx context.Context) error {
	for _, a := range artists {
		var detail obj
		if err := d.admin.get(
			ctx,
			fmt.Sprintf("/music/artists/%d", d.ids.artists[a.Name]),
			nil,
			&detail,
		); err != nil {
			return err
		}
		for _, al := range a.Albums {
			var id int
			for _, row := range objs(detail["albums"]) {
				if fold(str(row["title"])) == fold(al.Title) {
					id = num(row["id"])
					break
				}
			}
			if id == 0 {
				return fmt.Errorf("%s has no album %q", a.Name, al.Title)
			}
			key := a.Name + "/" + al.Title
			d.ids.albums[key] = id
			var album obj
			err := poll(
				ctx,
				15*time.Minute,
				3*time.Second,
				"tracks of "+key,
				func() (bool, error) {
					if err := d.admin.get(
						ctx,
						fmt.Sprintf("/music/albums/%d", id),
						nil,
						&album,
					); err != nil {
						return false, err
					}
					return len(objs(album["tracks"])) > 0, nil
				},
			)
			if err != nil {
				return err
			}
			year := str(album["release_date"])
			if len(year) >= 4 {
				year = year[:4]
			}
			albumFacts[key] = albumInfo{
				id:     id,
				year:   year,
				tracks: objs(album["tracks"]),
			}
		}
	}
	for _, s := range bookSeries {
		var detail obj
		err := poll(
			ctx,
			10*time.Minute,
			3*time.Second,
			"volumes of "+s.Title,
			func() (bool, error) {
				if err := d.admin.get(
					ctx,
					fmt.Sprintf("/books/series/%d", d.ids.bseries[s.Title]),
					nil,
					&detail,
				); err != nil {
					return false, err
				}
				hydrating, _ := detail["hydrating"].(bool)
				return !hydrating, nil
			},
		)
		if err != nil {
			return err
		}
		for _, n := range s.Volumes {
			var found obj
			for _, v := range objs(detail["volumes"]) {
				if num(v["number"]) == n || str(v["number"]) == strconv.Itoa(n) ||
					num(v["position"]) == n {
					found = v
					break
				}
			}
			if found == nil {
				return fmt.Errorf("%s has no volume %d", s.Title, n)
			}
			// A volume names its book; the title and makers are the book's.
			var book obj
			if err := d.admin.get(
				ctx,
				fmt.Sprintf("/books/%d", num(found["id"])),
				nil,
				&book,
			); err != nil {
				return err
			}
			author := str(book["author"])
			if author == "" {
				author = str(detail["author"])
			}
			volumeFacts = append(
				volumeFacts,
				volumeInfo{
					series: s.Title,
					title:  str(book["title"]),
					author: author,
					bookID: num(book["id"]),
				},
			)
		}
	}
	return nil
}

// writeReleases puts on disk the files of every release meant to finish, and
// completes the catalog with the music and book releases, which need the
// facts read above.
func (d *demo) writeReleases(ctx context.Context) error {
	dl := func(parts ...string) string { return d.path(append([]string{"downloads"}, parts...)...) }
	clipFor := func(res string) clip {
		if res == "2160p" {
			return clipMovie2160
		}
		return clipMovie1080
	}
	for _, m := range movies {
		if m.Plan != planImport && m.Plan != planHeld {
			continue
		}
		c := clipFor(m.Res)
		if m.Plan == planHeld {
			c = clipMovie1080 // claims 2160p, holds 1080p: verification holds it
		}
		src, err := d.fx.clip(ctx, c)
		if err != nil {
			return err
		}
		if err := link(src, dl(m.release(), m.release()+".mkv")); err != nil {
			return err
		}
	}

	episodeClip, err := d.fx.clip(ctx, clipEpisode)
	if err != nil {
		return err
	}
	for _, s := range series {
		var show obj
		if err := d.admin.get(
			ctx,
			fmt.Sprintf("/series/%d", d.ids.series[s.Title]),
			nil,
			&show,
		); err != nil {
			return err
		}
		for _, n := range s.Seasons {
			if n.Plan != planImport {
				continue
			}
			var eps []obj
			for _, season := range objs(show["seasons"]) {
				if num(season["number"]) == n.N {
					eps = objs(season["episodes"])
				}
			}
			if len(eps) == 0 {
				return fmt.Errorf("%s has no episodes in season %d", s.Title, n.N)
			}
			for _, ep := range eps {
				if err := link(
					episodeClip,
					dl(s.release(n), s.episodeFile(n, num(ep["number"]))),
				); err != nil {
					return err
				}
			}
		}
	}

	for _, a := range artists {
		for _, al := range a.Albums {
			facts := albumFacts[a.Name+"/"+al.Title]
			name := a.release(al, facts.year)
			r := &release{
				Name:     name,
				Category: "3000",
				Match:    append(words(a.Name), words(al.Title)...),
				Age:      9 * 24 * time.Hour,
			}
			r.Size = al.Size
			if r.Size == 0 {
				r.Size = int64(len(facts.tracks)) * 32_000_000
			}
			applyProgress(r, al.Prog)
			d.releases = append(d.releases, r)
			if al.Plan != planImport {
				continue
			}
			for _, t := range facts.tracks {
				secs := num(t["duration"])
				if secs <= 0 {
					secs = 200
				}
				src, err := d.fx.flacTrack(ctx, secs)
				if err != nil {
					return err
				}
				disc := max(num(t["disc"]), 1)
				file := fmt.Sprintf(
					"%02d - %s.flac",
					num(t["number"]),
					safeName(str(t["title"])),
				)
				if err := writeTagged(src, dl(name, file), audiotags.Tags{
					audiotags.KeyArtist:      {a.Name},
					audiotags.KeyAlbumArtist: {a.Name},
					audiotags.KeyAlbum:       {al.Title},
					audiotags.KeyTitle:       {str(t["title"])},
					audiotags.KeyTrackNumber: {strconv.Itoa(num(t["number"]))},
					audiotags.KeyDiscNumber:  {strconv.Itoa(disc)},
				}); err != nil {
					return err
				}
			}
		}
	}

	part, err := d.fx.mp3Part(ctx)
	if err != nil {
		return err
	}
	addBook := func(author, title, ebookPlan, audioPlan string, prog progress) error {
		if ebookPlan != "" && ebookPlan != planWanted {
			name := ebookRelease(author, title)
			r := &release{
				Name:     name,
				Category: "7020",
				Size:     1_400_000,
				Match:    words(title),
				Age:      60 * 24 * time.Hour,
			}
			d.releases = append(d.releases, r)
			if ebookPlan == planImport {
				if err := writeEPUB(
					dl(name, author+" - "+safeName(title)+".epub"),
					title,
					author,
				); err != nil {
					return err
				}
			}
		}
		if audioPlan != "" && audioPlan != planWanted {
			name := audiobookRelease(author, title)
			r := &release{
				Name:     name,
				Category: "3030",
				Size:     640_000_000,
				Match:    words(title),
				Age:      40 * 24 * time.Hour,
			}
			applyProgress(r, prog)
			d.releases = append(d.releases, r)
			if audioPlan == planImport {
				for i := 1; i <= 6; i++ {
					if err := link(
						part,
						dl(
							name,
							fmt.Sprintf("%s - Part %02d.mp3", safeName(title), i),
						),
					); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, b := range books {
		if err := addBook(
			b.Author,
			b.Title,
			b.Ebook,
			b.Audiobook,
			b.Prog,
		); err != nil {
			return err
		}
	}
	for _, v := range volumeFacts {
		if err := addBook(
			firstAuthor(v.author),
			v.title,
			planImport,
			"",
			progress{},
		); err != nil {
			return err
		}
	}

	d.tz.releases = d.releases
	for _, r := range d.releases {
		d.qb.byName[r.Name] = r
	}
	return nil
}

func firstAuthor(s string) string {
	if i := strings.Index(s, " & "); i > 0 {
		return s[:i]
	}
	return s
}

func safeName(s string) string {
	return strings.NewReplacer("/", "-", ":", " -", "?", "", "\"", "'").Replace(s)
}

// connect registers the fake client and indexer, the way a person would
// through Settings.
func (d *demo) connect(ctx context.Context) error {
	host, port, _ := net.SplitHostPort(d.qbAddr)
	p, _ := strconv.Atoi(port)
	if err := d.admin.post(ctx, "/download-clients", obj{
		"name":        "qBittorrent",
		"client_type": "qbittorrent",
		"host":        host,
		"port":        p,
		"username":    "admin",
		"password":    "demo-password",
		"enabled":     true,
		"priority":    1,
	}, nil); err != nil && !isConflict(err) {
		return err
	}
	host, port, _ = net.SplitHostPort(d.tzAddr)
	p, _ = strconv.Atoi(port)
	if err := d.admin.post(ctx, "/indexers", obj{
		"name": "Public trackers", "protocol": "torznab", "host": host, "port": p,
		"api_key": "demo", "enabled": true, "priority": 1,
	}, nil); err != nil && !isConflict(err) {
		return err
	}
	return nil
}

// pick finds the release named name among search results.
func pick(results []obj, name string) obj {
	for _, r := range results {
		if str(r["title"]) == name {
			return r
		}
	}
	return nil
}

// grab searches path+"/search" and grabs the release named name through
// path+"/grab". A release the client already holds is a grab an earlier,
// automatic pass made, which is the same outcome.
func (d *demo) grab(ctx context.Context, path, name string) error {
	var res any
	if err := retry(
		ctx,
		2*time.Minute,
		func() error { return d.admin.post(ctx, path+"/search", nil, &res) },
	); err != nil {
		return err
	}
	r := pick(items(res), name)
	if r == nil {
		var got []string
		for _, i := range items(res) {
			got = append(got, str(i["title"])+" ("+str(i["reject_reason"])+")")
		}
		return fmt.Errorf("search %s did not list %q; got %v", path, name, got)
	}
	err := d.admin.post(ctx, path+"/grab", r, nil)
	var ae *apiError
	if errors.As(err, &ae) && ae.Status == http.StatusUnprocessableEntity &&
		strings.Contains(ae.Body, "already") {
		return nil
	}
	return err
}

func (d *demo) grabAll(ctx context.Context) error {
	for _, m := range movies {
		if m.Plan == planWanted {
			continue
		}
		if err := d.grab(
			ctx,
			fmt.Sprintf("/movies/%d", d.ids.movies[m.Title]),
			m.release(),
		); err != nil {
			return fmt.Errorf("%s: %w", m.Title, err)
		}
	}
	for _, s := range series {
		for _, n := range s.Seasons {
			if err := d.grab(
				ctx,
				fmt.Sprintf("/series/%d/seasons/%d", d.ids.series[s.Title], n.N),
				s.release(n),
			); err != nil {
				return fmt.Errorf("%s season %d: %w", s.Title, n.N, err)
			}
		}
	}
	for _, a := range artists {
		for _, al := range a.Albums {
			key := a.Name + "/" + al.Title
			if err := d.grab(
				ctx,
				fmt.Sprintf("/music/albums/%d", d.ids.albums[key]),
				a.release(al, albumFacts[key].year),
			); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	for _, b := range books {
		path := fmt.Sprintf("/books/%d", d.ids.books[b.Title])
		if b.Ebook == planImport || b.Ebook == planDownload {
			if err := d.grab(
				ctx,
				path,
				ebookRelease(b.Author, b.Title),
			); err != nil {
				return fmt.Errorf("%s ebook: %w", b.Title, err)
			}
		}
		if b.Audiobook == planImport || b.Audiobook == planDownload {
			if err := d.grab(
				ctx,
				path,
				audiobookRelease(b.Author, b.Title),
			); err != nil {
				return fmt.Errorf("%s audiobook: %w", b.Title, err)
			}
		}
	}
	for _, v := range volumeFacts {
		if err := d.grab(
			ctx,
			fmt.Sprintf("/books/%d", v.bookID),
			ebookRelease(firstAuthor(v.author), v.title),
		); err != nil {
			return fmt.Errorf("%s: %w", v.title, err)
		}
	}
	return nil
}

func (d *demo) runJob(ctx context.Context, name string) error {
	return d.admin.post(ctx, "/schedules/"+name+"/run", nil, nil)
}

// waitForImports runs the download monitor until every release with files
// on disk is in the history, imported, or in the queue, held for a decision.
// The queue is a snapshot the server refreshes on its own, so a release
// missing from it right after the grab is not a finished one.
func (d *demo) waitForImports(ctx context.Context) error {
	onDisk := map[string]string{}
	for _, r := range d.releases {
		if _, err := os.Stat(d.path("downloads", r.Name)); err == nil {
			onDisk[r.hash()] = r.Name
		}
	}
	return poll(
		ctx,
		20*time.Minute,
		5*time.Second,
		"imports to finish",
		func() (bool, error) {
			if err := d.runJob(ctx, "download-monitor"); err != nil {
				return false, err
			}
			done := map[string]bool{}
			var history, queue any
			if err := d.admin.get(
				ctx,
				"/activity/history",
				url.Values{"limit": {"200"}},
				&history,
			); err != nil {
				return false, err
			}
			for _, e := range items(history) {
				h := strings.ToLower(str(e["torrent_hash"]))
				if name, ok := onDisk[h]; ok && str(e["status"]) == "failed" {
					return false, fmt.Errorf("import of %s failed", name)
				}
				done[h] = true
			}
			if err := d.admin.get(ctx, "/activity/queue", nil, &queue); err != nil {
				return false, err
			}
			for _, e := range items(queue) {
				if str(e["status"]) == "held" {
					done[strings.ToLower(str(e["torrent_hash"]))] = true
				}
			}
			var left []string
			for h, name := range onDisk {
				if !done[h] {
					left = append(left, name)
				}
			}
			slog.InfoContext(
				ctx,
				"waiting for imports",
				"left",
				len(left),
				"of",
				len(onDisk),
			)
			if len(left) > 0 && len(left) <= 3 {
				slog.InfoContext(ctx, "still importing", "releases", left)
			}
			return len(left) == 0, nil
		},
	)
}

func (d *demo) seedRequests(ctx context.Context) error {
	as := map[string]*api{}
	for _, p := range people {
		if err := d.admin.post(
			ctx,
			"/users",
			obj{
				"email":        p.Email,
				"password":     userPassword,
				"role":         p.Role,
				"display_name": p.Name,
			},
			nil,
		); err != nil &&
			!isConflict(err) {
			return err
		}
		c, err := login(ctx, d.base, p.Email, userPassword)
		if err != nil {
			return err
		}
		as[p.Email] = c
	}
	for _, r := range requests {
		body := obj{"media_type": r.MediaType, "title": r.Title}
		if r.MBID != "" {
			body["media_mbid"] = r.MBID
		} else {
			body["media_id"] = r.ID
		}
		var out obj
		if err := as[r.By].post(ctx, "/requests", body, &out); err != nil {
			return fmt.Errorf("request %s: %w", r.Title, err)
		}
		id := num(out["id"])
		switch r.Decision {
		case "approve":
			if err := retry(ctx, 2*time.Minute, func() error {
				return d.admin.post(
					ctx,
					fmt.Sprintf("/requests/%d/approve", id),
					obj{},
					nil,
				)
			}); err != nil {
				return fmt.Errorf("approve %s: %w", r.Title, err)
			}
		case "deny":
			if err := d.admin.post(
				ctx,
				fmt.Sprintf("/requests/%d/deny", id),
				obj{"reason": r.Reason},
				nil,
			); err != nil {
				return fmt.Errorf("deny %s: %w", r.Title, err)
			}
		}
	}
	return nil
}

// seedAdoption starts a library import over a folder of films that are not
// in the library, and leaves it waiting for review.
func (d *demo) seedAdoption(ctx context.Context) error {
	src, err := d.fx.clip(ctx, clipMovie1080)
	if err != nil {
		return err
	}
	for _, a := range adoptions {
		if err := link(
			src,
			d.path("media", "movies", a.Folder, a.File),
		); err != nil {
			return err
		}
	}
	var scan obj
	if err := d.admin.post(ctx, "/library/imports", obj{
		"source_path": d.path(
			"media",
			"movies",
		),
		"kind":        "movie",
		"mode":        "rename",
		"import_mode": "hardlink",
	}, &scan); err != nil {
		return err
	}
	id := num(scan["id"])
	d.scanID = id
	return poll(
		ctx,
		10*time.Minute,
		3*time.Second,
		"the library import to reach review",
		func() (bool, error) {
			if err := d.admin.get(
				ctx,
				fmt.Sprintf("/library/imports/%d", id),
				nil,
				&scan,
			); err != nil {
				return false, err
			}
			st := str(scan["status"])
			if st == "failed" || st == "cancelled" {
				return false, fmt.Errorf(
					"library import failed: %s",
					str(scan["failure_reason"]),
				)
			}
			return st == "awaiting_review" || st == "completed", nil
		},
	)
}

// settle probes the imported files and waits for the music worker to finish
// filling in the discographies, so the pages show tracks and covers. A
// release group MusicBrainz keeps refusing is not worth failing the run
// over: past the wait, the screenshots are taken with it still pending.
func (d *demo) settle(ctx context.Context) error {
	if err := d.runJob(ctx, "media-probe"); err != nil {
		return err
	}
	err := poll(
		ctx,
		10*time.Minute,
		10*time.Second,
		"music hydration",
		func() (bool, error) {
			var res any
			if err := d.admin.get(
				ctx,
				"/music/artists",
				url.Values{"limit": {"100"}},
				&res,
			); err != nil {
				return false, err
			}
			left := 0
			for _, a := range items(res) {
				var detail obj
				if err := d.admin.get(
					ctx,
					fmt.Sprintf("/music/artists/%d", num(a["id"])),
					nil,
					&detail,
				); err != nil {
					return false, err
				}
				for _, al := range objs(detail["albums"]) {
					if p, _ := al["tracks_pending"].(bool); p {
						left++
					}
				}
			}
			slog.InfoContext(ctx, "waiting for music hydration", "albums_left", left)
			return left == 0, nil
		},
	)
	if _, ok := errors.AsType[*pollTimeout](err); ok {
		slog.WarnContext(
			ctx,
			"music hydration did not finish; shooting with what is there",
			"error",
			err,
		)
		return nil
	}
	return err
}

// seedCalendar moves a few release dates into the current month. A library
// of public-domain titles has nothing coming out, and the calendar page is
// about what is coming out; this is the one write that goes around the API.
func (d *demo) seedCalendar(ctx context.Context) error {
	db, err := sql.Open(
		"sqlite",
		"file:"+d.path(
			"data",
			"streamline.db",
		)+"?_pragma=busy_timeout(10000)&_timezone=UTC",
	)
	if err != nil {
		return err
	}
	defer db.Close()
	now := time.Now().UTC()
	first := time.Date(now.Year(), now.Month(), 1, 20, 0, 0, 0, time.UTC)

	// One weekly show per weekday, its last season's episodes from the
	// first of the month on.
	plan := map[string]int{
		"Bonanza":                 6,
		"Adventures of Superman":  1,
		"The Beverly Hillbillies": 3,
		"The Cisco Kid":           4,
	}
	for title, weekday := range plan {
		var show obj
		if err := d.admin.get(
			ctx,
			fmt.Sprintf("/series/%d", d.ids.series[title]),
			nil,
			&show,
		); err != nil {
			return err
		}
		pick, err := calendarEpisodes(show)
		if err != nil {
			return fmt.Errorf("%s: %w", title, err)
		}
		if len(pick.empty) > 0 {
			slog.WarnContext(
				ctx,
				"calendar: later seasons have no episodes; dating an earlier one",
				"series",
				title,
				"empty_seasons",
				pick.empty,
				"season",
				pick.season,
			)
		}
		day := first
		for day.Weekday() != time.Weekday(weekday) {
			day = day.AddDate(0, 0, 1)
		}
		for i, id := range pick.episodes {
			if err := setDate(
				ctx,
				db,
				`UPDATE episodes SET air_date = ? WHERE id = ?`,
				day.AddDate(0, 0, 7*i),
				id,
			); err != nil {
				return fmt.Errorf(
					"%s season %d episode id %d: %w",
					title,
					pick.season,
					id,
					err,
				)
			}
		}
		slog.InfoContext(
			ctx,
			"calendar: episodes dated",
			"series",
			title,
			"season",
			pick.season,
			"episodes",
			len(pick.episodes),
		)
	}
	for i, title := range []string{"Safety Last!", "The Gold Rush", "Dementia 13"} {
		if err := setDate(
			ctx,
			db,
			`UPDATE movies SET digital_release_date = ? WHERE id = ?`,
			first.AddDate(0, 0, 9+7*i),
			d.ids.movies[title],
		); err != nil {
			return fmt.Errorf("%s: %w", title, err)
		}
	}
	return nil
}

// calendarPick is the episodes of one show seedCalendar gives a date.
type calendarPick struct {
	season   int
	episodes []int
	// empty lists the later seasons passed over for having no episode.
	empty []int
}

// calendarEpisodes picks the first five episodes of a show's last season
// that has any. The season list and the episode list are two separate
// answers from the provider, so a show can end on a season row with nothing
// in it; taking that one would date no episode and leave the show off the
// calendar without a word. A show with no episode at all is an error.
func calendarEpisodes(show obj) (calendarPick, error) {
	var pick calendarPick
	seasons := objs(show["seasons"])
	for _, se := range slices.Backward(seasons) {
		number := num(se["number"])
		episodes := objs(se["episodes"])
		if len(episodes) == 0 {
			pick.empty = append(pick.empty, number)
			continue
		}
		pick.season = number
		for _, ep := range episodes[:min(5, len(episodes))] {
			pick.episodes = append(pick.episodes, num(ep["id"]))
		}
		return pick, nil
	}
	return pick, fmt.Errorf(
		"none of its %d seasons has an episode to put on the calendar",
		len(seasons),
	)
}

// setDate runs one of seedCalendar's updates and insists it changed exactly
// one row: these writes go around the API, so a row that is not there would
// otherwise only show as a gap in the screenshot.
func setDate(
	ctx context.Context,
	db *sql.DB,
	query string,
	when time.Time,
	id int,
) error {
	res, err := db.ExecContext(ctx, query, when, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("the date update matched %d rows, not 1", n)
	}
	return nil
}

// writeFile is a small helper for the composite and other outputs.
func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
