package music

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/otelx"
)

// DiscographyFilesView is a discography pack's torrent read against the
// artist before any grab, the way the pack import will read it: audio files
// grouped by album folder, each folder matched by title (and year) to one of
// the albums the grab would link, then each file to a track by its name.
type DiscographyFilesView struct {
	Files []PackFileView
	// Folders is every folder holding audio, in the torrent's order.
	Folders []PackFolderView
	// Albums is what the grab would link: monitored, released, hydrated and
	// still waiting (wanted, paused or failed). Empty means the grab would be
	// refused, since a pack fills nothing else.
	Albums []PackAlbumView
}

type PackFileView struct {
	Path    string
	Size    int64
	AlbumID uint32
	TrackID uint32
}

type PackFolderView struct {
	Path string
	// AlbumID is the artist's album the folder names, 0 for none.
	AlbumID uint32
	// Waiting says the import will fill it: the album is one the grab links.
	// A folder naming an album that is complete or not monitored is skipped.
	Waiting bool
}

type PackAlbumView struct {
	ID     uint32
	Title  string
	Year   int
	Tracks []ReleaseTrackView
	// InRelease says a folder of the pack names the album.
	InRelease bool
}

// ArtistPackFiles lists a discography pack's files against the artist's
// albums. One request to the indexer, for the .torrent; a magnet answers
// download.ErrFileListUnavailable. Folder to album is the importer's own
// rule (library.MatchPackDir), so it says which albums the import fills;
// file to track reads names where the import reads tags, so that half is a
// preview.
func (s *Service) ArtistPackFiles(
	ctx context.Context,
	artistID uint32,
	result indexer.SearchResult,
) (*DiscographyFilesView, error) {
	ctx, span := tracer.Start(ctx, "music.artist_pack_files",
		trace.WithAttributes(
			attribute.Int("artist.id", int(artistID)),
			attribute.String("release.title", result.Title),
		))
	defer span.End()

	art, err := s.db.FindArtistByID(ctx, artistID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	waiting, err := s.db.ListPackAlbums(ctx, artistID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	files, err := s.download.ReleaseFiles(ctx, result)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}

	// The tree carries tracks and files; the waiting list says which albums
	// the grab links, and so which the import matches folders against.
	all := art.Edges.Albums
	byID := make(map[uint32]*ent.Album, len(all))
	for _, a := range all {
		byID[a.ID] = a
	}
	linked := make([]*ent.Album, 0, len(waiting))
	for _, w := range waiting {
		if a, ok := byID[w.ID]; ok {
			linked = append(linked, a)
		}
	}

	// Each folder is one album import, which decides over its own files
	// whether their numbers name the album's tracks.
	byDir := map[string][]string{}
	for _, f := range files {
		if audiotags.IsAudioFile(f.Path) {
			dir := library.PackAlbumDir(f.Path)
			byDir[dir] = append(byDir[dir], f.Path)
		}
	}
	matchers := map[string]library.TrackMatcher{}

	view := &DiscographyFilesView{Files: make([]PackFileView, 0, len(files))}
	folders := map[string]int{}
	named := map[uint32]bool{}
	in := map[uint32]bool{}
	for _, f := range files {
		fv := PackFileView{Path: f.Path, Size: f.Size}
		if audiotags.IsAudioFile(f.Path) {
			dir := library.PackAlbumDir(f.Path)
			i, seen := folders[dir]
			if !seen {
				pf := PackFolderView{Path: dir}
				if a := library.MatchPackDir(dir, linked); a != nil {
					pf.AlbumID, pf.Waiting = a.ID, true
					named[a.ID] = true
				} else if a := library.MatchPackDir(dir, all); a != nil {
					pf.AlbumID = a.ID
				}
				i = len(view.Folders)
				folders[dir] = i
				view.Folders = append(view.Folders, pf)
			}
			if pf := view.Folders[i]; pf.Waiting {
				fv.AlbumID = pf.AlbumID
				m, ok := matchers[dir]
				if !ok {
					m = library.NewTrackMatcherByName(
						byID[pf.AlbumID].Edges.Tracks,
						byDir[dir],
					)
					matchers[dir] = m
				}
				if t := m.MatchByName(f.Path); t != nil {
					fv.TrackID = t.ID
					in[t.ID] = true
				}
			}
		}
		view.Files = append(view.Files, fv)
	}

	view.Albums = make([]PackAlbumView, 0, len(linked))
	for _, a := range linked {
		av := PackAlbumView{ID: a.ID, Title: a.Title, InRelease: named[a.ID]}
		if a.ReleaseDate != nil {
			av.Year = a.ReleaseDate.Year()
		}
		for _, t := range a.Edges.Tracks {
			av.Tracks = append(av.Tracks, ReleaseTrackView{
				ID:        t.ID,
				Disc:      t.Disc,
				Position:  t.Position,
				Title:     t.Title,
				HasFile:   len(t.Edges.MediaFiles) > 0,
				InRelease: in[t.ID],
			})
		}
		view.Albums = append(view.Albums, av)
	}
	span.SetAttributes(
		attribute.Int("files", len(view.Files)),
		attribute.Int("folders", len(view.Folders)),
		attribute.Int("albums.linked", len(linked)),
		attribute.Int("albums.named", len(named)),
	)
	return view, nil
}
