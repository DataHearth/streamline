package music

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	entmediafile "github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/scheduler"
)

const (
	// tagSyncBatch bounds one scheduled pass; a manual run takes everything.
	tagSyncBatch = 500
	// maxTagFailures is how many writes of one file may fail before it is
	// failed and left for someone to ask again.
	maxTagFailures uint8 = 5

	tagWritten   = "written"
	tagUnchanged = "unchanged"
	tagDeferred  = "deferred"
	tagFailed    = "failed"
)

// ErrTagsOff is a tag request on an instance whose library.music_tags.mode
// is off.
var ErrTagsOff = errors.New("music tags are off (library.music_tags.mode)")

var tagWrites = otelx.Must(meter.Int64Counter(
	"streamline.music.tag_writes",
	metric.WithDescription("Music tag writes by outcome"),
))

// AlbumTagger is the slice of the service the importer tags an album's new
// files through, before it refreshes the media servers.
type AlbumTagger interface {
	TagAlbum(ctx context.Context, albumID uint32) error
}

var _ AlbumTagger = (*Service)(nil)

// TagPolicy is library.music_tags' strip and preserve lists as the writer
// applies them.
func TagPolicy(c config.MusicTagsConfig) audiotags.Policy {
	return audiotags.Policy{Strip: c.Strip, Preserve: c.Preserve}
}

// SyncTags is the music-tag-sync job: it writes every file waiting for its
// tags, deferring any a torrent still owns or shares, and asks the media
// servers to rescan once something was written. It is the only writer of a
// file whose tags changed after its import.
func (s *Service) SyncTags(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "music.tag_sync")
	defer span.End()

	cfg := config.Get().Library.MusicTags
	if cfg.Mode == config.MusicTagsOff {
		span.SetAttributes(attribute.Bool("tags.off", true))
		return nil
	}
	limit := tagSyncBatch
	if scheduler.Manual(ctx) {
		limit = 0
	}
	files, err := s.db.ListMediaFilesForTagSync(ctx, limit)
	if err != nil {
		return otelx.RecordSpanError(span, fmt.Errorf("list files to tag: %w", err))
	}
	span.SetAttributes(attribute.Int("tags.candidates", len(files)))
	if len(files) == 0 {
		return nil
	}
	seeding := s.seedingTorrents(ctx)
	res := s.tagFiles(ctx, files, seeding, cfg, func(done, total int) {
		scheduler.Progress(ctx, done, total)
	})
	span.SetAttributes(
		attribute.Int("tags.written", res.counts[tagWritten]),
		attribute.Int("tags.unchanged", res.counts[tagUnchanged]),
		attribute.Int("tags.deferred", res.counts[tagDeferred]),
		attribute.Int("tags.failed", res.counts[tagFailed]),
	)
	slog.InfoContext(ctx, "music tag sync complete",
		"written", res.counts[tagWritten],
		"unchanged", res.counts[tagUnchanged],
		"deferred", res.counts[tagDeferred],
		"failed", res.counts[tagFailed])
	if res.counts[tagWritten] > 0 && s.ms != nil {
		mediaserver.RefreshInBackground(
			ctx, s.ms, mediaserver.KindMusic, config.Get().Library.MusicPath,
		)
	}
	return nil
}

// TagAlbum writes the album's files waiting for their tags now, under the
// same rules as the job. The importer calls it once it has recorded the
// files, so a media server indexes them tagged; it refreshes nothing itself.
func (s *Service) TagAlbum(ctx context.Context, albumID uint32) error {
	ctx, span := tracer.Start(ctx, "music.tag_album",
		withAlbumID(albumID))
	defer span.End()

	cfg := config.Get().Library.MusicTags
	if cfg.Mode == config.MusicTagsOff {
		return nil
	}
	all, err := s.db.ListAlbumTagFiles(ctx, albumID)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	files := slices.DeleteFunc(all, func(mf *ent.MediaFile) bool {
		return mf.TagsState == nil ||
			(*mf.TagsState != entmediafile.TagsStatePending &&
				*mf.TagsState != entmediafile.TagsStateDeferred)
	})
	if len(files) == 0 {
		return nil
	}
	s.tagFiles(ctx, files, s.seedingTorrents(ctx), cfg, nil)
	return nil
}

// seedingTorrents lists the clients' torrents; with no download manager
// wired (a test, a tool) there are none.
func (s *Service) seedingTorrents(ctx context.Context) download.SeedingSet {
	if s.download == nil {
		return download.NewSeedingSet(true, nil, nil)
	}
	return s.download.SeedingTorrents(ctx)
}

// RetagLibrary queues every track file for a full rewrite, whatever it
// carries: the way to bring files written before a change in what Streamline
// writes up to date. It returns how many files were queued.
func (s *Service) RetagLibrary(ctx context.Context) (int, error) {
	if config.Get().Library.MusicTags.Mode == config.MusicTagsOff {
		return 0, ErrTagsOff
	}
	n, err := s.db.MarkAllMusicTags(ctx, db.MarkForce)
	if err != nil {
		return 0, err
	}
	slog.InfoContext(ctx, "music library queued for retagging", "files", n)
	return n, nil
}

// TagSettingsChanged reacts to a library.music_tags patch: under managed,
// any change to what a file should carry queues every file, which the hash
// then settles without a write where nothing differs.
func (s *Service) TagSettingsChanged(
	ctx context.Context,
	before, after config.MusicTagsConfig,
) {
	if after.Mode != config.MusicTagsManaged {
		return
	}
	same := before.Mode == after.Mode &&
		slices.Equal(before.Strip, after.Strip) &&
		slices.Equal(before.Preserve, after.Preserve) &&
		before.EmbedArt == after.EmbedArt &&
		before.ArtMaxPx == after.ArtMaxPx &&
		before.SidecarArt == after.SidecarArt
	if same {
		return
	}
	n, err := s.db.MarkAllMusicTags(ctx, db.MarkStale)
	if err != nil {
		slog.WarnContext(
			ctx,
			"queueing music files after a tag settings change failed",
			"error",
			err,
		)
		return
	}
	slog.InfoContext(ctx, "music files queued after a tag settings change",
		"files", n)
}

// tagsChanged queues the albums' files after the library's data for them
// changed. Under managed only: import writes a file once.
func (s *Service) tagsChanged(ctx context.Context, albumIDs ...uint32) {
	if config.Get().Library.MusicTags.Mode != config.MusicTagsManaged ||
		len(albumIDs) == 0 {
		return
	}
	if _, err := s.db.MarkAlbumTags(ctx, albumIDs, db.MarkStale); err != nil {
		slog.WarnContext(ctx, "queueing album files for their tags failed",
			"error", err)
	}
}

type tagResult struct {
	counts map[string]int
	albums map[uint32]bool
}

// tagFiles writes each file's tags, album by album so an album's tree is
// read once.
func (s *Service) tagFiles(
	ctx context.Context,
	files []*ent.MediaFile,
	seeding download.SeedingSet,
	cfg config.MusicTagsConfig,
	progress func(done, total int),
) tagResult {
	res := tagResult{counts: map[string]int{}, albums: map[uint32]bool{}}
	byAlbum := map[uint32][]*ent.MediaFile{}
	var order []uint32
	for _, mf := range files {
		tr := mf.Edges.Track
		if tr == nil || tr.Edges.Album == nil {
			continue
		}
		id := tr.Edges.Album.ID
		if _, ok := byAlbum[id]; !ok {
			order = append(order, id)
		}
		byAlbum[id] = append(byAlbum[id], mf)
	}
	done := 0
	for _, albumID := range order {
		alb, err := s.db.FindAlbumByID(ctx, albumID)
		if err != nil || alb.Edges.Artist == nil {
			slog.WarnContext(ctx, "tag sync: album not readable",
				"album.id", albumID, "error", err)
			done += len(byAlbum[albumID])
			continue
		}
		at := &albumTagging{svc: s, alb: alb, seeding: seeding, cfg: cfg}
		for _, mf := range byAlbum[albumID] {
			outcome := at.tag(ctx, mf)
			res.counts[outcome]++
			tagWrites.Add(ctx, 1, metric.WithAttributes(
				attribute.String("outcome", outcome)))
			if outcome == tagWritten {
				res.albums[albumID] = true
			}
			done++
			if progress != nil {
				progress(done, len(files))
			}
		}
	}
	return res
}

// albumTagging is one album's pass: its tree, read once, and the torrents
// behind it, read only when a hardlinked file needs them.
type albumTagging struct {
	svc     *Service
	alb     *ent.Album
	seeding download.SeedingSet
	cfg     config.MusicTagsConfig

	hashes     []string
	hashesRead bool
}

func (a *albumTagging) tag(ctx context.Context, mf *ent.MediaFile) string {
	ctx, span := tracer.Start(ctx, "music.write_tags",
		withAlbumID(a.alb.ID),
		withMediaFileID(mf.ID))
	defer span.End()

	idx := slices.IndexFunc(a.alb.Edges.Tracks, func(t *ent.Track) bool {
		return t.ID == mf.Edges.Track.ID
	})
	if idx < 0 {
		return a.fail(ctx, span, mf, errors.New("track not on the album"))
	}
	tr := a.alb.Edges.Tracks[idx]
	tags := library.AlbumTrackTags(a.alb.Edges.Artist, a.alb, tr)
	policy := TagPolicy(a.cfg)
	hash := audiotags.Hash(tags, policy)

	if mf.TagsHash == hash {
		// Nothing Streamline owns would change: settle it without opening
		// the file.
		st, err := os.Stat(mf.Path)
		if err != nil {
			return a.fail(ctx, span, mf, err)
		}
		if err := a.svc.db.SetMediaFileTagsWritten(
			ctx, mf.ID, hash, st.ModTime().UnixNano(), st.Size(), time.Now(),
		); err != nil {
			_ = otelx.RecordSpanError(span, err)
		}
		span.SetAttributes(attribute.String("outcome", tagUnchanged))
		return tagUnchanged
	}

	if reason := a.holdReason(ctx, mf); reason != "" {
		span.SetAttributes(
			attribute.String("outcome", tagDeferred),
			attribute.String("tags.deferred_reason", reason),
		)
		if err := a.svc.db.SetMediaFileTagsDeferred(ctx, mf.ID); err != nil {
			_ = otelx.RecordSpanError(span, err)
		}
		return tagDeferred
	}

	if err := audiotags.Write(mf.Path, tags, policy); err != nil {
		return a.fail(ctx, span, mf, err)
	}
	st, err := os.Stat(mf.Path)
	if err != nil {
		return a.fail(ctx, span, mf, err)
	}
	if err := a.svc.db.SetMediaFileTagsWritten(
		ctx, mf.ID, hash, st.ModTime().UnixNano(), st.Size(), time.Now(),
	); err != nil {
		_ = otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(attribute.String("outcome", tagWritten))
	return tagWritten
}

// holdReason is why a file must not be written yet, empty when it may be.
// Writing replaces the file at its path with a fresh copy, so a write never
// reaches another hard link's bytes; what it must not do is replace a file a
// torrent serves from that very path, and under seeding: defer it does not
// split a hardlink from a torrent still listed, which would cost a copy's
// disk.
func (a *albumTagging) holdReason(ctx context.Context, mf *ent.MediaFile) string {
	if a.seeding.Owns(mf.Path) {
		return "a torrent serves this file"
	}
	if !a.seeding.Complete &&
		library.PathUnderRoot(mf.Path, config.Get().Library.DownloadPath) {
		return "a download client could not be listed"
	}
	links, err := library.LinkCount(mf.Path)
	if err != nil || links <= 1 {
		// A missing file fails at the write, which records why.
		return ""
	}
	if !a.seeding.Complete {
		return "a download client could not be listed"
	}
	if a.cfg.Seeding != config.MusicTagsSeedingDefer {
		return ""
	}
	if !a.hashesRead {
		a.hashesRead = true
		hashes, err := a.svc.db.AlbumTorrentHashes(ctx, a.alb.ID)
		if err != nil {
			slog.WarnContext(ctx, "tag sync: reading the album's torrents failed",
				"album.id", a.alb.ID, "error", err)
			return "the album's torrents could not be read"
		}
		a.hashes = hashes
	}
	for _, h := range a.hashes {
		if h != "" && a.seeding.Live(h) {
			return "hardlinked to a torrent still seeding"
		}
	}
	return ""
}

func (a *albumTagging) fail(
	ctx context.Context,
	span trace.Span,
	mf *ent.MediaFile,
	err error,
) string {
	max := maxTagFailures
	if errors.Is(err, audiotags.ErrUnsupportedFormat) {
		max = 1
	}
	_ = otelx.RecordSpanError(span, err)
	slog.WarnContext(ctx, "music tag write failed",
		"media_file.id", mf.ID, "file", mf.Path, "error", err)
	if rerr := a.svc.db.RecordMediaFileTagFailure(
		ctx, mf.ID, err.Error(), max,
	); rerr != nil {
		slog.WarnContext(ctx, "recording a music tag failure failed",
			"media_file.id", mf.ID, "error", rerr)
	}
	span.SetAttributes(attribute.String("outcome", tagFailed))
	return tagFailed
}

func withAlbumID(id uint32) trace.SpanStartOption {
	return trace.WithAttributes(attribute.Int64("album.id", int64(id)))
}

func withMediaFileID(id uint32) trace.SpanStartOption {
	return trace.WithAttributes(attribute.Int64("media_file.id", int64(id)))
}
