package events

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/importscanfile"
	"github.com/datahearth/streamline/ent/importscanshow"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/track"
)

// owner is the resolved (scope, id) pair an event hangs off. A zero id means
// "nothing to attribute this to" — the hook returns without recording rather
// than writing an ownerless row.
type owner struct {
	scope Scope
	id    uint32
}

func (o owner) ok() bool { return o.id != 0 }

// auxFailure reports a failure in the event-recording half of a hook and lets
// the mutation stand.
//
// The hooks run after next.Mutate, so by the time anything here can fail the
// write they exist to describe has already happened. Returning the error made
// it the *mutation's* error: a locked events table or a bad payload marshal
// surfaced as "importing this file failed" or "download status update failed",
// with the real cause wrapped several layers down — an activity-feed problem
// dressed up as a library one. Record and PurgeOldEvents already log and count
// their own failures; this is for the resolve step in front of them.
func auxFailure(ctx context.Context, hook string, err error) {
	slog.ErrorContext(ctx, "could not attribute an event to its owner",
		"hook", hook, "error", err)
}

// recordAux writes a hook's event and never fails the mutation behind it, for
// the reason auxFailure gives. Record has already logged and counted the
// failure in detail; this only adds which hook was recording.
func recordAux(
	ctx context.Context,
	hook string,
	c *ent.Client,
	t Type,
	scope Scope,
	ownerID uint32,
	payload map[string]any,
) {
	if err := Record(ctx, c, t, scope, ownerID, payload); err != nil {
		slog.DebugContext(ctx, "event not recorded; mutation left in place",
			"hook", hook)
	}
}

// Register installs runtime mutation hooks on the supplied client and
// captures it as the package default for tx-less Record calls.
func Register(client *ent.Client) {
	defaultClient = client

	client.DownloadRecord.Use(downloadRecordHook())
	client.MediaFile.Use(mediaFileHook())
	client.MediaFile.Use(mediaFileDeleteHook())
	client.Movie.Use(movieHook())
	client.TVShow.Use(tvShowHook())
	client.Artist.Use(artistHook())
	client.Book.Use(bookHook())
	client.BookSeries.Use(bookSeriesHook())
	client.ImportScanFile.Use(importScanFileHook())
	client.ImportScanShow.Use(importScanShowHook())
}

func downloadRecordHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				dm, ok := m.(*ent.DownloadRecordMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				c := dm.Client()
				switch dm.Op() {
				case ent.OpCreate:
					// A record born completed is bookkeeping for a file the
					// library already holds; nothing was grabbed.
					if st, _ := dm.Status(); st == downloadrecord.StatusCompleted {
						return val, nil
					}
					o, err := downloadRecordOwner(ctx, c, dm)
					if err != nil {
						auxFailure(ctx, "download_record", err)
						return val, nil
					}
					if !o.ok() {
						return val, nil
					}
					recordAux(
						ctx,
						"download_record",
						c,
						TypeGrabbed,
						o.scope,
						o.id,
						downloadCreatePayload(dm),
					)
				default:
					if !dm.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
						return val, nil
					}
					status, changed := dm.Status()
					if !changed {
						return val, nil
					}
					o, err := downloadRecordOwner(ctx, c, dm)
					if err != nil {
						auxFailure(ctx, "download_record", err)
						return val, nil
					}
					if !o.ok() {
						return val, nil
					}
					var t Type
					switch status {
					// StatusImporting, not StatusCompleted: completed is
					// stamped by RecordImportSuccess, i.e. once the file is
					// already filed, where the MediaFile create hook is
					// separately recording TypeImported. Hanging
					// download_completed off it produced two rows for one
					// moment and left the hours between grabbed and imported
					// — the actual download — with nothing in the feed.
					case downloadrecord.StatusImporting:
						t = TypeDownloadCompleted
					case downloadrecord.StatusFailed:
						t = TypeDownloadFailed
					case downloadrecord.StatusHeld:
						t = TypeImportHeld
					default:
						return val, nil
					}
					recordAux(
						ctx, "download_record", c, t, o.scope, o.id,
						downloadStatusPayload(dm),
					)
				}
				return val, nil
			},
		)
	}
}

func mediaFileHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				mf, ok := m.(*ent.MediaFileMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !mf.Op().Is(ent.OpCreate) {
					return val, nil
				}
				if importedSuppressed(ctx) {
					return val, nil
				}
				// Track and book files are left to their importers, which
				// record one imported event per album or book slot: an album
				// is a dozen files and an audiobook a folder of them, and a
				// row per file read as a dozen unrelated imports.
				var o owner
				switch {
				case hasID(mf.MovieID):
					o = owner{ScopeMovie, mustID(mf.MovieID)}
				case hasID(mf.EpisodeID):
					o = owner{ScopeEpisode, mustID(mf.EpisodeID)}
				default:
					return val, nil
				}
				recordAux(
					ctx,
					"media_file",
					mf.Client(),
					TypeImported,
					o.scope,
					o.id,
					mediaFileCreatePayload(mf),
				)
				return val, nil
			},
		)
	}
}

func importScanFileHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				isf, ok := m.(*ent.ImportScanFileMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !isf.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
					return val, nil
				}
				outcome, changed := isf.Outcome()
				if !changed || outcome != importscanfile.OutcomeFailed {
					return val, nil
				}
				c := isf.Client()
				movieID, err := importScanFileMovieID(ctx, c, isf)
				if err != nil {
					auxFailure(ctx, "import_scan_file", err)
					return val, nil
				}
				if movieID == 0 {
					return val, nil
				}
				recordAux(
					ctx,
					"import_scan_file",
					c,
					TypeImportFailed,
					ScopeMovie,
					movieID,
					importScanFilePayload(isf),
				)
				return val, nil
			},
		)
	}
}

// importScanShowHook is the series twin of importScanFileHook. A failed show
// entry is recorded against the series it was meant to land in; a failure with
// no series resolved yet (an unmatched folder) has nowhere to hang and is
// left to the scan's own outcome_message.
func importScanShowHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				iss, ok := m.(*ent.ImportScanShowMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				if !iss.Op().Is(ent.OpUpdate | ent.OpUpdateOne) {
					return val, nil
				}
				outcome, changed := iss.Outcome()
				if !changed || outcome != importscanshow.OutcomeFailed {
					return val, nil
				}
				c := iss.Client()
				showID, err := importScanShowTVShowID(ctx, c, iss)
				if err != nil {
					auxFailure(ctx, "import_scan_show", err)
					return val, nil
				}
				if showID == 0 {
					return val, nil
				}
				recordAux(
					ctx,
					"import_scan_show",
					c,
					TypeImportFailed,
					ScopeSeries,
					showID,
					importScanShowPayload(iss),
				)
				return val, nil
			},
		)
	}
}

func downloadCreatePayload(m *ent.DownloadRecordMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.Title(); ok {
		p["release_title"] = v
	}
	if v, ok := m.Quality(); ok && v != "" {
		p["quality"] = v
	}
	if v, ok := m.Size(); ok {
		p["size_bytes"] = v
	}
	if v, ok := m.BookKind(); ok {
		p["book_kind"] = string(v)
	}
	return p
}

func downloadStatusPayload(m *ent.DownloadRecordMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.FailureReason(); ok && v != "" {
		p["reason"] = v
	}
	if v, ok := m.HoldReasons(); ok && len(v) > 0 {
		checks := make([]string, 0, len(v))
		for _, r := range v {
			checks = append(checks, r.Check)
		}
		p["held_checks"] = checks
		p["held_count"] = len(v)
	}
	return p
}

func mediaFileCreatePayload(m *ent.MediaFileMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.Path(); ok {
		p["path"] = v
	}
	if v, ok := m.Quality(); ok && v != "" {
		p["quality"] = v
	}
	if v, ok := m.Size(); ok {
		p["size_bytes"] = v
	}
	if v, ok := m.Source(); ok {
		p["source"] = string(v)
	}
	return p
}

func importScanFilePayload(m *ent.ImportScanFileMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.SourcePath(); ok {
		p["path"] = v
	}
	if v, ok := m.OutcomeMessage(); ok && v != "" {
		p["error"] = v
	}
	return p
}

func importScanShowPayload(m *ent.ImportScanShowMutation) map[string]any {
	p := map[string]any{}
	if v, ok := m.FolderPath(); ok {
		p["path"] = v
	}
	if v, ok := m.OutcomeMessage(); ok && v != "" {
		p["error"] = v
	}
	return p
}

func hasID(f func() (uint32, bool)) bool {
	id, ok := f()
	return ok && id != 0
}

func mustID(f func() (uint32, bool)) uint32 {
	id, _ := f()
	return id
}

// downloadRecordOwner resolves the movie, episode, album, book or artist a
// record belongs to. On a create the mutation carries the edge; on an update
// it usually does not, so the row is queried back — movie first, then episode,
// album, book and, for a discography pack, artist.
func downloadRecordOwner(
	ctx context.Context,
	c *ent.Client,
	m *ent.DownloadRecordMutation,
) (owner, error) {
	if hasID(m.MovieID) {
		return owner{ScopeMovie, mustID(m.MovieID)}, nil
	}
	if hasID(m.AnchorEpisodeID) {
		return owner{ScopeEpisode, mustID(m.AnchorEpisodeID)}, nil
	}
	if hasID(m.AlbumID) {
		return owner{ScopeAlbum, mustID(m.AlbumID)}, nil
	}
	if hasID(m.BookID) {
		return owner{ScopeBook, mustID(m.BookID)}, nil
	}
	// A discography pack: the artist edge, with the albums it expects on a
	// separate many-to-many. The pack is one grab, so it is one artist row.
	if hasID(m.ArtistID) {
		return owner{ScopeArtist, mustID(m.ArtistID)}, nil
	}
	// A create carries its edges in the mutation or not at all — there is no
	// stored row to fall back to, and ent refuses IDs() on OpCreate outright.
	// Reaching the query below therefore failed the whole insert, which is how
	// an adopted torrent matching nothing in the library ("unidentified — pick
	// a title", the one proposal shape with no owner) silently never got filed.
	if m.Op().Is(ent.OpCreate) {
		return owner{}, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return owner{}, err
	}
	if len(ids) == 0 {
		return owner{}, nil
	}
	row, err := c.DownloadRecord.Query().
		Where(downloadrecord.IDEQ(ids[0])).
		// Only the owner's id is read below, and this runs inside the
		// caller's transaction on every download-record mutation.
		WithMovie(func(q *ent.MovieQuery) { q.Select(movie.FieldID) }).
		WithAnchorEpisode(func(q *ent.EpisodeQuery) { q.Select(episode.FieldID) }).
		WithAlbum(func(q *ent.AlbumQuery) { q.Select(album.FieldID) }).
		WithBook(func(q *ent.BookQuery) { q.Select(book.FieldID) }).
		WithArtist(func(q *ent.ArtistQuery) { q.Select(artist.FieldID) }).
		Only(ctx)
	if ent.IsNotFound(err) {
		return owner{}, nil
	}
	if err != nil {
		return owner{}, err
	}
	switch {
	case row.Edges.Movie != nil:
		return owner{ScopeMovie, row.Edges.Movie.ID}, nil
	case row.Edges.AnchorEpisode != nil:
		return owner{ScopeEpisode, row.Edges.AnchorEpisode.ID}, nil
	case row.Edges.Album != nil:
		return owner{ScopeAlbum, row.Edges.Album.ID}, nil
	case row.Edges.Book != nil:
		return owner{ScopeBook, row.Edges.Book.ID}, nil
	case row.Edges.Artist != nil:
		return owner{ScopeArtist, row.Edges.Artist.ID}, nil
	}
	return owner{}, nil
}

func importScanFileMovieID(
	ctx context.Context,
	c *ent.Client,
	m *ent.ImportScanFileMutation,
) (uint32, error) {
	if id, ok := m.CreatedMovieID(); ok && id != 0 {
		return id, nil
	}
	if id, ok := m.ExistingMovieID(); ok && id != 0 {
		return id, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	row, err := c.ImportScanFile.Query().
		Where(importscanfile.IDIn(ids...)).
		Select(importscanfile.FieldCreatedMovieID, importscanfile.FieldExistingMovieID).
		First(ctx)
	if err != nil {
		return 0, err
	}
	if row.CreatedMovieID != 0 {
		return row.CreatedMovieID, nil
	}
	return row.ExistingMovieID, nil
}

func importScanShowTVShowID(
	ctx context.Context,
	c *ent.Client,
	m *ent.ImportScanShowMutation,
) (uint32, error) {
	if id, ok := m.CreatedTvshowID(); ok && id != 0 {
		return id, nil
	}
	if id, ok := m.ExistingTvshowID(); ok && id != 0 {
		return id, nil
	}
	ids, err := m.IDs(ctx)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	row, err := c.ImportScanShow.Query().
		Where(importscanshow.IDIn(ids...)).
		Select(
			importscanshow.FieldCreatedTvshowID,
			importscanshow.FieldExistingTvshowID,
		).
		First(ctx)
	if err != nil {
		return 0, err
	}
	if row.CreatedTvshowID != nil && *row.CreatedTvshowID != 0 {
		return *row.CreatedTvshowID, nil
	}
	if row.ExistingTvshowID != nil {
		return *row.ExistingTvshowID, nil
	}
	return 0, nil
}

// suppressKey marks a context whose MediaFile deletions must not produce a
// file_removed event.
type suppressKey struct{}

// SuppressFileRemoved marks ctx so the media-file delete hook stays quiet for
// deletions made under it.
//
// The drift sweep records drift_detected/drift_confirmed itself, naming the
// file that vanished and the check that proved it. The delete hook cannot see
// which caller it runs under, so without this the same disappearance landed
// twice — once diagnosed, once bare — and the bare row read as a second,
// unexplained deletion.
func SuppressFileRemoved(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressKey{}, true)
}

func fileRemovedSuppressed(ctx context.Context) bool {
	v, _ := ctx.Value(suppressKey{}).(bool)
	return v
}

// suppressImportedKey marks a context whose MediaFile creates must not produce
// an imported event. It is deliberately its own key rather than a second
// meaning for suppressKey: a bulk commit that replaces files needs both
// suppressions at once, and sharing the key would make either one imply the
// other.
type suppressImportedKey struct{}

// SuppressImported marks ctx so the media-file create hook stays quiet for
// files filed under it.
//
// A bulk caller — a series scan commit, a season-pack import — files tens to
// hundreds of episodes for one operator action, and the hook cannot see that
// it runs inside one: a 442-file commit wrote 442 activity rows. Those callers
// record a single series-scoped imported event covering the whole batch
// instead, which is why the per-file row has to go away rather than be
// deduplicated afterwards.
func SuppressImported(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressImportedKey{}, true)
}

func importedSuppressed(ctx context.Context) bool {
	v, _ := ctx.Value(suppressImportedKey{}).(bool)
	return v
}

// movieHook records a movie entering the library and its monitored flag being
// flipped. Both ride the same mutation type, and CreateMovie/UpdateMovie each
// have a single caller, so nothing else can trip them.
func movieHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				mm, ok := m.(*ent.MovieMutation)
				if !ok {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				c := mm.Client()
				switch {
				case mm.Op().Is(ent.OpCreate):
					row, ok := val.(*ent.Movie)
					if !ok || row.ID == 0 {
						return val, nil
					}
					recordAux(ctx, "movie", c, TypeAdded, ScopeMovie, row.ID,
						moviePayload(row))
				case mm.Op().Is(ent.OpUpdate | ent.OpUpdateOne):
					monitored, changed := mm.Monitored()
					if !changed {
						return val, nil
					}
					id, ok := mm.ID()
					if !ok || id == 0 {
						return val, nil
					}
					recordAux(ctx, "movie", c, TypeMonitoringChanged,
						ScopeMovie, id, map[string]any{
							"monitored": monitored,
						})
				}
				return val, nil
			},
		)
	}
}

// tvShowHook is the series twin of movieHook, minus the monitoring half: a
// series toggle cascades to every season and episode, and the activity row for
// it carries the episode count. The hook sees only the show row's own update
// and so cannot know that count — tvshow.Service.Update records it instead,
// where the cascade returns it.
func tvShowHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				tm, ok := m.(*ent.TVShowMutation)
				if !ok || !tm.Op().Is(ent.OpCreate) {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				row, ok := val.(*ent.TVShow)
				if !ok || row.ID == 0 {
					return val, nil
				}
				recordAux(ctx, "tv_show", tm.Client(), TypeAdded, ScopeSeries,
					row.ID, tvShowPayload(row))
				return val, nil
			},
		)
	}
}

// mediaFileDeleteHook records a file leaving the library, whatever removed it
// — a manual delete, an upgrade replacing it, or a bulk-import commit
// adopting a better copy. It resolves the owner *before* the delete runs,
// since the row and its edges are gone afterwards.
func mediaFileDeleteHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				mf, ok := m.(*ent.MediaFileMutation)
				if !ok || !mf.Op().Is(ent.OpDelete|ent.OpDeleteOne) {
					return next.Mutate(ctx, m)
				}
				if fileRemovedSuppressed(ctx) {
					return next.Mutate(ctx, m)
				}
				c := mf.Client()
				doomed, err := doomedMediaFiles(ctx, c, mf)
				if err != nil {
					auxFailure(ctx, "media_file_delete", err)
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				for _, d := range doomed {
					p := map[string]any{"path": d.path}
					if d.files > 1 {
						p = map[string]any{"files": d.files}
					}
					if d.bookKind != "" {
						p["book_kind"] = d.bookKind
					}
					recordAux(ctx, "media_file_delete", c, TypeFileRemoved,
						d.scope, d.id, p)
				}
				return val, nil
			},
		)
	}
}

// removed is a media file resolved to its owner before deletion. Track and
// book files are folded per owner: replacing an album or an audiobook deletes
// every file of it in one mutation, which is one removal, not a dozen — files
// counts them and path is the first one's.
type removed struct {
	scope    Scope
	id       uint32
	path     string
	files    int
	bookKind string
}

// doomedMediaFiles reads the rows a delete mutation is about to remove,
// resolving each to its owning movie, episode, album (through its track) or
// book. Files whose owner is itself being deleted come back ownerless and are
// skipped.
func doomedMediaFiles(
	ctx context.Context,
	c *ent.Client,
	m *ent.MediaFileMutation,
) ([]removed, error) {
	ids, err := m.IDs(ctx)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := c.MediaFile.Query().
		Where(mediafile.IDIn(ids...)).
		WithMovie(func(q *ent.MovieQuery) { q.Select(movie.FieldID) }).
		WithEpisode(func(q *ent.EpisodeQuery) { q.Select(episode.FieldID) }).
		WithTrack(func(q *ent.TrackQuery) {
			q.Select(track.FieldID).
				WithAlbum(func(q *ent.AlbumQuery) { q.Select(album.FieldID) })
		}).
		WithBook(func(q *ent.BookQuery) { q.Select(book.FieldID) }).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]removed, 0, len(rows))
	folded := map[owner]int{}
	fold := func(o owner, path, kind string) {
		if i, ok := folded[o]; ok {
			out[i].files++
			return
		}
		folded[o] = len(out)
		out = append(out, removed{
			scope: o.scope, id: o.id, path: path, files: 1, bookKind: kind,
		})
	}
	for _, row := range rows {
		switch {
		case row.Edges.Movie != nil:
			out = append(out, removed{
				scope: ScopeMovie, id: row.Edges.Movie.ID, path: row.Path,
			})
		case row.Edges.Episode != nil:
			out = append(out, removed{
				scope: ScopeEpisode, id: row.Edges.Episode.ID, path: row.Path,
			})
		case row.Edges.Track != nil && row.Edges.Track.Edges.Album != nil:
			fold(owner{ScopeAlbum, row.Edges.Track.Edges.Album.ID}, row.Path, "")
		case row.Edges.Book != nil:
			fold(owner{ScopeBook, row.Edges.Book.ID}, row.Path,
				string(row.BookKind))
		}
	}
	return out, nil
}

// artistHook records an artist entering the library. Create-only, like
// tvShowHook: the monitor policy is re-applied over every album in one
// statement, and the stub albums an add seeds are not each an arrival — the
// artist row stands for all of them.
func artistHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				am, ok := m.(*ent.ArtistMutation)
				if !ok || !am.Op().Is(ent.OpCreate) {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				row, ok := val.(*ent.Artist)
				if !ok || row.ID == 0 {
					return val, nil
				}
				recordAux(ctx, "artist", am.Client(), TypeAdded, ScopeArtist,
					row.ID, map[string]any{"title": row.Name})
				return val, nil
			},
		)
	}
}

// bookHook records a standalone book entering the library. A series volume is
// skipped: adding a series writes a row — most of them stubs — for every
// position, and bookSeriesHook's one row stands for all of them.
func bookHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				bm, ok := m.(*ent.BookMutation)
				if !ok || !bm.Op().Is(ent.OpCreate) || hasID(bm.SeriesID) {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				row, ok := val.(*ent.Book)
				if !ok || row.ID == 0 {
					return val, nil
				}
				recordAux(ctx, "book", bm.Client(), TypeAdded, ScopeBook,
					row.ID, bookPayload(row))
				return val, nil
			},
		)
	}
}

// bookSeriesHook records a book series entering the library.
func bookSeriesHook() ent.Hook {
	return func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(
			func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
				sm, ok := m.(*ent.BookSeriesMutation)
				if !ok || !sm.Op().Is(ent.OpCreate) {
					return next.Mutate(ctx, m)
				}
				val, err := next.Mutate(ctx, m)
				if err != nil {
					return val, err
				}
				row, ok := val.(*ent.BookSeries)
				if !ok || row.ID == 0 {
					return val, nil
				}
				p := map[string]any{"title": row.Title}
				if row.QualityProfile != "" {
					p["quality_profile"] = row.QualityProfile
				}
				recordAux(ctx, "book_series", sm.Client(), TypeAdded,
					ScopeBookSeries, row.ID, p)
				return val, nil
			},
		)
	}
}

func bookPayload(b *ent.Book) map[string]any {
	p := map[string]any{"title": b.Title}
	if b.AuthorName != "" {
		p["author"] = b.AuthorName
	}
	if b.QualityProfile != "" {
		p["quality_profile"] = b.QualityProfile
	}
	return p
}

func moviePayload(m *ent.Movie) map[string]any {
	p := map[string]any{"title": m.Title}
	if m.Year != 0 {
		p["year"] = m.Year
	}
	if m.QualityProfile != "" {
		p["quality_profile"] = m.QualityProfile
	}
	return p
}

func tvShowPayload(s *ent.TVShow) map[string]any {
	p := map[string]any{"title": s.Title}
	if s.Year != 0 {
		p["year"] = s.Year
	}
	if s.QualityProfile != "" {
		p["quality_profile"] = s.QualityProfile
	}
	return p
}
