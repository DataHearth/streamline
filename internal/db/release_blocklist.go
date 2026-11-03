package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/downloadrecord"
)

// ReleaseBlocklist is every release a user rejected from the held-download
// dialog, indexed by the item it was grabbed for: the movie, the show (an
// episode or a season pack), the album and the book. The automatic grab paths
// — missing searches and feed scans — drop a release it blocks before they
// pick one, so a rejected release is not grabbed again for that item. A
// manual grab is the user's own call and never consults it.
//
// A nil blocklist blocks nothing.
type ReleaseBlocklist struct {
	movies, shows, albums, books map[uint32]*releaseSet
}

// releaseSet matches like the search results' previously_grabbed_at: a
// release carrying an info hash is blocked by a record with that hash, or by a
// hashless record of the same title (a record with a different hash under the
// same title is a different upload); a release with no hash is blocked by any
// record of its title. Both compare lowercase.
type releaseSet struct {
	hash, title, unhashedTitle map[string]struct{}
}

func (r *releaseSet) add(hash, title string) {
	title = strings.ToLower(title)
	r.title[title] = struct{}{}
	if hash == "" {
		r.unhashedTitle[title] = struct{}{}
		return
	}
	r.hash[strings.ToLower(hash)] = struct{}{}
}

func (r *releaseSet) blocks(hash, title string) bool {
	if r == nil {
		return false
	}
	title = strings.ToLower(title)
	if hash == "" {
		_, ok := r.title[title]
		return ok
	}
	if _, ok := r.hash[strings.ToLower(hash)]; ok {
		return true
	}
	_, ok := r.unhashedTitle[title]
	return ok
}

func releaseSetFor(m map[uint32]*releaseSet, id uint32) *releaseSet {
	s, ok := m[id]
	if !ok {
		s = &releaseSet{
			hash:          map[string]struct{}{},
			title:         map[string]struct{}{},
			unhashedTitle: map[string]struct{}{},
		}
		m[id] = s
	}
	return s
}

// Movie reports whether the release was rejected for the movie.
func (b *ReleaseBlocklist) Movie(id uint32, infoHash, title string) bool {
	return b != nil && b.movies[id].blocks(infoHash, title)
}

// Show reports whether the release was rejected for any episode of the show.
// A release names its episodes, so a show-wide match is the same release.
func (b *ReleaseBlocklist) Show(id uint32, infoHash, title string) bool {
	return b != nil && b.shows[id].blocks(infoHash, title)
}

// Album reports whether the release was rejected for the album, grabbed for
// it alone or as part of a discography pack.
func (b *ReleaseBlocklist) Album(id uint32, infoHash, title string) bool {
	return b != nil && b.albums[id].blocks(infoHash, title)
}

// Book reports whether the release was rejected for the book, either slot.
func (b *ReleaseBlocklist) Book(id uint32, infoHash, title string) bool {
	return b != nil && b.books[id].blocks(infoHash, title)
}

// Len is how many items carry a rejected release.
func (b *ReleaseBlocklist) Len() int {
	if b == nil {
		return 0
	}
	return len(b.movies) + len(b.shows) + len(b.albums) + len(b.books)
}

// RejectedReleases loads the blocklist: every record FailHeldDownloadRecord
// marked rejected, keyed by the items it was grabbed for. Rejected records are
// a handful, so the whole set is read once per pass rather than per item.
func (db *DB) RejectedReleases(ctx context.Context) (*ReleaseBlocklist, error) {
	recs, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.Rejected(true)).
		WithMovie().
		WithAnchorEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(sq *ent.SeasonQuery) { sq.WithTvShow() })
		}).
		WithAlbum().
		WithAlbums().
		WithBook().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list rejected releases: %w", err)
	}
	b := &ReleaseBlocklist{
		movies: map[uint32]*releaseSet{},
		shows:  map[uint32]*releaseSet{},
		albums: map[uint32]*releaseSet{},
		books:  map[uint32]*releaseSet{},
	}
	for _, r := range recs {
		if m := r.Edges.Movie; m != nil {
			releaseSetFor(b.movies, m.ID).add(r.TorrentHash, r.Title)
		}
		if e := r.Edges.AnchorEpisode; e != nil {
			if se := e.Edges.Season; se != nil && se.Edges.TvShow != nil {
				releaseSetFor(
					b.shows,
					se.Edges.TvShow.ID,
				).add(r.TorrentHash, r.Title)
			}
		}
		if a := r.Edges.Album; a != nil {
			releaseSetFor(b.albums, a.ID).add(r.TorrentHash, r.Title)
		}
		for _, a := range r.Edges.Albums {
			releaseSetFor(b.albums, a.ID).add(r.TorrentHash, r.Title)
		}
		if bk := r.Edges.Book; bk != nil {
			releaseSetFor(b.books, bk.ID).add(r.TorrentHash, r.Title)
		}
	}
	return b, nil
}
