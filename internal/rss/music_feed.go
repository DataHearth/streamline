package rss

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
)

const (
	musicCategoryMin  = 3000
	musicCategoryMax  = 4000
	audiobookCategory = 3030
)

// musicPass carries one tick's state for the album branch: wanted albums
// indexed by artist key, the music profile resolved per artist profile name,
// and the albums already attempted.
type musicPass struct {
	wanted   map[string][]*ent.Album
	profiles map[string]config.MusicQualityProfileEntry
	grabbed  map[uint32]struct{}
}

// musicCategory reports whether a Torznab category is music. 3030 is the
// audiobook subcategory and belongs to the book pass.
func musicCategory(cat string) bool {
	n, err := strconv.Atoi(cat)
	return err == nil && n >= musicCategoryMin && n < musicCategoryMax &&
		n != audiobookCategory
}

func (s *FeedScanner) newMusicPass(ctx context.Context) (*musicPass, error) {
	albums, err := s.store.ListWantedAlbums(
		ctx, config.Get().Library.MaxGrabFailures,
	)
	if err != nil {
		return nil, err
	}
	pass := &musicPass{
		wanted:   make(map[string][]*ent.Album),
		profiles: make(map[string]config.MusicQualityProfileEntry),
		grabbed:  make(map[uint32]struct{}),
	}
	unresolved := make(map[string]struct{})
	for _, a := range albums {
		ar := a.Edges.Artist
		if ar == nil {
			continue
		}
		if _, bad := unresolved[ar.QualityProfile]; bad {
			continue
		}
		if _, ok := pass.profiles[ar.QualityProfile]; !ok {
			p, ok := config.ResolveMusicQualityProfile(ar.QualityProfile)
			if !ok {
				unresolved[ar.QualityProfile] = struct{}{}
				slog.WarnContext(ctx, "feed-scan: music quality profile unresolved",
					"artist", ar.Name, "profile", ar.QualityProfile)
				continue
			}
			pass.profiles[ar.QualityProfile] = p
		}
		key := showKey(ar.Name)
		pass.wanted[key] = append(pass.wanted[key], a)
	}
	return pass, nil
}

func (s *FeedScanner) processMusicItems(
	ctx context.Context,
	items []indexer.SearchResult,
	pass *musicPass,
) int {
	if len(pass.wanted) == 0 {
		return 0
	}
	matched := 0
	for _, item := range items {
		if !musicCategory(item.Category) {
			continue
		}
		parsed := library.ParseMusicRelease(item.Title)
		if parsed.Discography {
			continue
		}
		artistPart, albumPart, ok := splitCreatorTitle(item.Title)
		if !ok {
			continue
		}
		var a *ent.Album
		for _, cand := range pass.wanted[showKey(artistPart)] {
			if library.TitleNamesSameWork(albumPart, cand.Title) {
				a = cand
				break
			}
		}
		if a == nil {
			continue
		}
		if _, already := pass.grabbed[a.ID]; already {
			continue
		}
		profile := pass.profiles[a.Edges.Artist.QualityProfile]
		if library.ScoreMusicRelease(parsed, profile) < 0 {
			slog.DebugContext(ctx, "feed-scan: music format rejected",
				"album", a.Title, "release", item.Title, "format", parsed.Format)
			continue
		}
		pass.grabbed[a.ID] = struct{}{}
		if err := s.albums.GrabAlbumRelease(ctx, a.ID, item); err != nil {
			slog.WarnContext(ctx, "feed-scan: album grab failed",
				"album", a.Title, "release", item.Title, "error", err)
			if !transportFailure(err) {
				if bumpErr := s.store.IncrementAlbumGrabFailures(
					ctx, a.ID,
				); bumpErr != nil {
					slog.WarnContext(
						ctx,
						"feed-scan: bump album grab_failures failed",
						"album",
						a.Title,
						"error",
						bumpErr,
					)
				}
			}
			continue
		}
		matched++
		if err := s.store.ResetAlbumGrabFailures(ctx, a.ID); err != nil {
			slog.WarnContext(ctx, "feed-scan: reset album grab_failures failed",
				"album", a.Title, "error", err)
		}
		if err := s.store.SetAlbumLastSearchAt(ctx, a.ID, time.Now()); err != nil {
			slog.WarnContext(ctx, "feed-scan: set album last_search_at failed",
				"album", a.Title, "error", err)
		}
		slog.InfoContext(ctx, "feed-scan: grabbed album",
			"album", a.Title, "release", item.Title)
	}
	return matched
}
