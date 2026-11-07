package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// release is a search result, sent back intact to grab it.
type release map[string]any

func (r release) title() string  { s, _ := r["title"].(string); return s }
func (r release) rejected() bool { b, _ := r["rejected"].(bool); return b }
func (r release) reason() string { s, _ := r["reject_reason"].(string); return s }

// grab searches path+"/search" and grabs the release whose name starts with
// want (the fake tracker's own naming), rejected or not: the demo decides
// what downloads, the profile only labels it.
func (s *seeder) grab(ctx context.Context, path, want string, query url.Values) error {
	search := path + "/search"
	if len(query) > 0 {
		search += "?" + query.Encode()
	}
	// A movie's search answers a bare list; the others wrap it in items.
	var raw json.RawMessage
	if err := s.api.do(ctx, http.MethodPost, search, nil, &raw); err != nil {
		return err
	}
	var hits []release
	if err := json.Unmarshal(raw, &hits); err != nil {
		var wrapped struct {
			Items []release `json:"items"`
		}
		if err := json.Unmarshal(raw, &wrapped); err != nil {
			return fmt.Errorf("%s: %w", search, err)
		}
		hits = wrapped.Items
	}
	for _, h := range hits {
		if strings.HasPrefix(strings.ToLower(h.title()), strings.ToLower(want)) {
			grab := path + "/grab"
			if len(query) > 0 {
				grab += "?" + query.Encode()
			}
			return s.api.post(ctx, grab, h, nil)
		}
	}
	names := make([]string, 0, len(hits))
	for _, h := range hits {
		names = append(names, h.title())
	}
	return fmt.Errorf("no release named %q among %d (%s)", want, len(hits), strings.Join(names, "; "))
}

func dotted(title string) string {
	return strings.NewReplacer(" ", ".", ":", "", "'", "", "!", "").Replace(title)
}

func filmRelease(f grabbedFilm) string {
	codec := "x265"
	if f.Height < 1080 {
		codec = "x264"
	}
	return fmt.Sprintf("%s.%d.%dp.WEB-DL.DDP5.1.%s-DEMO", dotted(f.Title), f.Year, f.Height, codec)
}

// downloads starts the tracker with every release the demo grabs or lists,
// grabs the quick ones and waits until they have imported or been held, then
// grabs the large ones so the queue has something in flight.
func (s *seeder) downloads(ctx context.Context) error {
	var args []string
	for _, f := range append(append([]grabbedFilm{}, quickFilms...), slowFilms...) {
		args = append(args, "-movie", fmt.Sprintf("%s:%d:%dp:%d:%dp", f.Title, f.Year, f.Height, f.SizeMB, f.Actual))
	}
	// Every resolution of the manual-search film.
	if len(wantedFilms) > 0 {
		d := wantedFilms[0]
		for _, spec := range []string{"2160p:18200", "1080p:9800", "720p:4400", "480p:1400"} {
			args = append(args, "-movie", fmt.Sprintf("%s:%d:%s", d.Title, d.Year, spec))
		}
	}
	for _, p := range slowPacks {
		args = append(args, "-pack", fmt.Sprintf("%s:%d:%d:%d", p.Show, p.Season, p.Episodes, p.EpMB))
	}
	type albumGrab struct{ artist, album string }
	var albums []albumGrab
	for _, ar := range artists {
		id, ok := s.artistIDs[ar.Name]
		if !ok {
			continue
		}
		var a musicArtist
		if err := s.api.get(ctx, "/music/artists/"+strconv.Itoa(id), nil, &a); err != nil {
			s.warn("album grab "+ar.Name, err)
			continue
		}
		for _, title := range ar.Grab {
			al := findAlbum(&a, title)
			if al == nil || len(al.Tracks) == 0 {
				s.warn("album grab "+title, fmt.Errorf("%s has no hydrated album %q", ar.Name, title))
				continue
			}
			names := make([]string, 0, len(al.Tracks))
			for _, t := range al.Tracks {
				names = append(names, strings.NewReplacer(":", "", "|", "").Replace(t.Title))
			}
			args = append(args, "-album", fmt.Sprintf("%s:%s:%s", ar.Name, strings.ReplaceAll(al.Title, ":", ""), strings.Join(names, "|")))
			albums = append(albums, albumGrab{ar.Name, al.Title})
		}
	}
	args = append(args,
		"-ebook", grabEbook.Author+":"+grabEbook.Title,
		"-audiobook", grabAudiobook.Author+":"+grabAudiobook.Title+":4",
	)
	if err := s.in.startTracker(ctx, args); err != nil {
		return err
	}

	// Quick: these finish and import (or hold) before anything else is shot.
	for _, f := range quickFilms {
		id, err := s.addFilm(ctx, f.Title, f.Year)
		if err == nil {
			err = s.grab(ctx, "/movies/"+strconv.Itoa(id), filmRelease(f), nil)
		}
		if err != nil {
			s.warn("grab "+f.Title, err)
		}
	}
	for _, g := range albums {
		var a musicArtist
		if err := s.api.get(ctx, "/music/artists/"+strconv.Itoa(s.artistIDs[g.artist]), nil, &a); err != nil {
			s.warn("grab "+g.album, err)
			continue
		}
		al := findAlbum(&a, g.album)
		if err := s.grab(ctx, "/music/albums/"+strconv.Itoa(al.ID), g.artist+" - "+strings.ReplaceAll(al.Title, ":", ""), nil); err != nil {
			s.warn("grab "+g.album, err)
		}
	}
	for _, b := range []struct {
		book
		kind string
	}{{grabEbook, "ebook"}, {grabAudiobook, "audiobook"}} {
		id, err := s.addBook(ctx, b.book)
		if err == nil {
			err = s.grab(ctx, "/books/"+strconv.Itoa(id), b.Author+" - "+b.Title, url.Values{"kind": {b.kind}})
		}
		if err != nil {
			s.warn("grab "+b.Title, err)
		}
	}
	if err := waitFor(ctx, "quick downloads", 8*time.Minute, 3*time.Second, func() (bool, error) {
		var q struct {
			Items []struct {
				Status string `json:"status"`
			} `json:"items"`
		}
		if err := s.api.get(ctx, "/activity/queue", nil, &q); err != nil {
			return false, err
		}
		for _, it := range q.Items {
			if it.Status != "held" && it.Status != "failed" {
				return false, nil
			}
		}
		return true, nil
	}); err != nil {
		s.warn("quick downloads", err)
	}

	// Slow: the queue's in-flight rows.
	for _, f := range slowFilms {
		id, err := s.addFilm(ctx, f.Title, f.Year)
		if err == nil {
			err = s.grab(ctx, "/movies/"+strconv.Itoa(id), filmRelease(f), nil)
		}
		if err != nil {
			s.warn("grab "+f.Title, err)
		}
	}
	for _, p := range slowPacks {
		id, ok := s.showIDs[p.Show]
		if !ok {
			s.warn("grab "+p.Show, fmt.Errorf("not in the library"))
			continue
		}
		want := fmt.Sprintf("%s.S%02d", dotted(p.Show), p.Season)
		if err := s.grab(ctx, fmt.Sprintf("/series/%d/seasons/%d", id, p.Season), want, nil); err != nil {
			s.warn("grab "+p.Show, err)
		}
	}
	// Give them a minute to show progress and a speed.
	slog.Info("letting the queue fill")
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(45 * time.Second):
	}
	return nil
}

// requests signs in as a member and asks for a few titles; the admin then
// decides some of them.
func (s *seeder) requests(ctx context.Context) error {
	if err := s.api.post(ctx, "/users", map[string]any{
		"email": memberEmail, "password": demoPassword, "role": "member", "display_name": "Mina Okafor",
	}, nil); err != nil {
		return err
	}
	s.member = s.in.client()
	if err := s.member.login(ctx, memberEmail, demoPassword); err != nil {
		return err
	}
	for _, r := range requests {
		body, err := s.requestBody(ctx, r)
		if err != nil {
			s.warn("request "+r.Title, err)
			continue
		}
		var out struct {
			ID int `json:"id"`
		}
		if err := s.member.post(ctx, "/requests", body, &out); err != nil {
			s.warn("request "+r.Title, err)
			continue
		}
		switch r.Decide {
		case "approve":
			err = s.api.post(ctx, fmt.Sprintf("/requests/%d/approve", out.ID), map[string]any{}, nil)
		case "deny":
			err = s.api.post(ctx, fmt.Sprintf("/requests/%d/deny", out.ID), map[string]any{"reason": "Already on the shelf in another edition."}, nil)
		}
		if err != nil {
			s.warn("decide "+r.Title, err)
		}
	}
	return nil
}

func (s *seeder) requestBody(ctx context.Context, r request) (map[string]any, error) {
	switch r.Kind {
	case "movie":
		var hits []struct {
			TMDBID int    `json:"tmdb_id"`
			Title  string `json:"title"`
			Year   int    `json:"year"`
		}
		if err := s.member.get(ctx, "/search/movie", url.Values{"q": {r.Title}, "year": {strconv.Itoa(r.Year)}}, &hits); err != nil {
			return nil, err
		}
		for _, h := range hits {
			if h.Year == r.Year {
				return map[string]any{"media_type": "movie", "media_id": h.TMDBID, "title": h.Title}, nil
			}
		}
	case "series":
		var res struct {
			Items []struct {
				TVDBID int    `json:"tvdb_id"`
				Title  string `json:"title"`
				Year   int    `json:"year"`
			} `json:"items"`
		}
		if err := s.member.get(ctx, "/series/lookup", url.Values{"query": {r.Title}}, &res); err != nil {
			return nil, err
		}
		for _, h := range res.Items {
			if h.Year == r.Year {
				return map[string]any{"media_type": "tvshow", "media_id": h.TVDBID, "title": h.Title}, nil
			}
		}
	case "album":
		var res struct {
			Items []struct {
				MBID       string `json:"mbid"`
				Title      string `json:"title"`
				Artist     string `json:"artist"`
				ArtistMBID string `json:"artist_mbid"`
			} `json:"items"`
		}
		if err := s.member.get(ctx, "/music/search/releases", url.Values{"query": {r.Artist + " " + r.Title}}, &res); err != nil {
			return nil, err
		}
		for _, h := range res.Items {
			if strings.EqualFold(h.Title, r.Title) && strings.EqualFold(h.Artist, r.Artist) {
				return map[string]any{
					"media_type": "album", "media_mbid": h.MBID, "artist_mbid": h.ArtistMBID,
					"artist_name": h.Artist, "title": h.Title,
				}, nil
			}
		}
	case "book":
		var res struct {
			Items []bookHit `json:"items"`
		}
		if err := s.member.get(ctx, "/books/search", url.Values{"query": {r.Title}, "type": {"book"}}, &res); err != nil {
			return nil, err
		}
		for _, h := range res.Items {
			if strings.EqualFold(h.Title, r.Title) {
				return map[string]any{"media_type": "book", "media_id": h.HardcoverID, "title": h.Title}, nil
			}
		}
		if len(res.Items) > 0 {
			h := res.Items[0]
			return map[string]any{"media_type": "book", "media_id": h.HardcoverID, "title": h.Title}, nil
		}
	}
	return nil, fmt.Errorf("no match for %s %q", r.Kind, r.Title)
}

func newAuthed(ctx context.Context, a *api, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{Name: "streamline_session", Value: a.token})
	return req, nil
}
