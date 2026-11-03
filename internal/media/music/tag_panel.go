package music

import (
	"context"
	"log/slog"
	"maps"

	"go.opentelemetry.io/otel/attribute"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

// desiredTags is what a track's file should carry: the library's tags, the
// album's overrides over them, and the track's over those. The policy's
// preserve list still wins at the write.
func desiredTags(ar *ent.Artist, al *ent.Album, tr *ent.Track) audiotags.Tags {
	tags := library.AlbumTrackTags(ar, al, tr)
	maps.Copy(tags, al.TagOverrides)
	maps.Copy(tags, tr.TagOverrides)
	return tags
}

// AlbumTagsView is the album's tag panel: the instance's mode, the album's
// own settings, and every file compared key by key with what a write would
// leave in it.
type AlbumTagsView struct {
	Mode      string
	Managed   bool
	Overrides map[string][]string
	Files     []TagFileView
}

// TagFileView is one file of the album against its desired tags.
type TagFileView struct {
	File  *ent.MediaFile
	Track *ent.Track
	// State is the writer's state, "untagged" for a file it was never asked
	// to write.
	State string
	Keys  []audiotags.KeyState
	// ReadError is why the file could not be compared, empty when it was.
	ReadError string
}

// AlbumTags reads every file of the album and compares it with what the tag
// writer would write. It reads files and writes nothing.
func (s *Service) AlbumTags(
	ctx context.Context,
	albumID uint32,
) (*AlbumTagsView, error) {
	ctx, span := tracer.Start(ctx, "music.album_tags", withAlbumID(albumID))
	defer span.End()

	alb, err := s.db.FindAlbumByID(ctx, albumID)
	if ent.IsNotFound(err) {
		return nil, otelx.RecordSpanError(span, ErrAlbumNotFound)
	}
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	cfg := config.Get().Library.MusicTags
	v := &AlbumTagsView{
		Mode: cfg.Mode, Managed: alb.TagsManaged, Overrides: alb.TagOverrides,
	}
	policy := TagPolicy(cfg)
	for _, tr := range alb.Edges.Tracks {
		for _, mf := range tr.Edges.MediaFiles {
			fv := TagFileView{File: mf, Track: tr, State: "untagged"}
			if mf.TagsState != nil {
				fv.State = string(*mf.TagsState)
			}
			fv.Keys, err = audiotags.Compare(
				mf.Path, desiredTags(alb.Edges.Artist, alb, tr), policy,
			)
			if err != nil {
				fv.ReadError = err.Error()
			}
			v.Files = append(v.Files, fv)
		}
	}
	span.SetAttributes(attribute.Int("tags.files", len(v.Files)))
	return v, nil
}

// ApplyAlbumTags queues every file of the album for a full rewrite, managed
// or not, and writes them in the background now, under the job's rules (a
// file a torrent holds is deferred). It returns how many files were queued.
func (s *Service) ApplyAlbumTags(ctx context.Context, albumID uint32) (int, error) {
	ctx, span := tracer.Start(ctx, "music.apply_album_tags", withAlbumID(albumID))
	defer span.End()

	if config.Get().Library.MusicTags.Mode == config.MusicTagsOff {
		return 0, otelx.RecordSpanError(span, ErrTagsOff)
	}
	if _, err := s.db.FindAlbumByID(ctx, albumID); err != nil {
		if ent.IsNotFound(err) {
			err = ErrAlbumNotFound
		}
		return 0, otelx.RecordSpanError(span, err)
	}
	n, err := s.db.QueueAlbumTags(ctx, albumID, db.MarkForce)
	if err != nil {
		return 0, otelx.RecordSpanError(span, err)
	}
	s.tagAlbumInBackground(ctx, albumID)
	return n, nil
}

// tagAlbumInBackground writes the album's waiting files off the request.
func (s *Service) tagAlbumInBackground(ctx context.Context, albumID uint32) {
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "music.tag_album", nil)
		if _, err := s.TagAlbum(bg, albumID); err != nil {
			slog.WarnContext(bg, "writing an album's tags failed, the job retries",
				"album.id", albumID, "error", err)
		}
	}()
}

// AlbumTagSettings is a patch of the album's own tag settings; a nil field
// is left as it is.
type AlbumTagSettings struct {
	Managed   *bool
	Overrides *map[string][]string
}

// SetAlbumTagSettings stores the album's tag settings. Overrides saved, or
// the album taken back under management, queue its files: a request about
// this album, so its own managed flag does not stand in the way.
func (s *Service) SetAlbumTagSettings(
	ctx context.Context,
	albumID uint32,
	set AlbumTagSettings,
) error {
	ctx, span := tracer.Start(
		ctx,
		"music.set_album_tag_settings",
		withAlbumID(albumID),
	)
	defer span.End()

	queue := false
	if set.Managed != nil {
		if err := s.db.SetAlbumTagsManaged(ctx, albumID, *set.Managed); err != nil {
			return otelx.RecordSpanError(span, orNotFound(err, ErrAlbumNotFound))
		}
		queue = *set.Managed
	}
	if set.Overrides != nil {
		if err := s.db.SetAlbumTagOverrides(
			ctx,
			albumID,
			*set.Overrides,
		); err != nil {
			return otelx.RecordSpanError(span, orNotFound(err, ErrAlbumNotFound))
		}
		queue = true
	}
	if queue {
		s.queueAlbum(ctx, albumID)
	}
	return nil
}

// SetTrackTagOverrides stores a track's hand-set tags and queues its album.
func (s *Service) SetTrackTagOverrides(
	ctx context.Context,
	trackID uint32,
	overrides map[string][]string,
) error {
	ctx, span := tracer.Start(ctx, "music.set_track_tag_overrides")
	defer span.End()
	span.SetAttributes(attribute.Int64("track.id", int64(trackID)))

	albumID, err := s.db.SetTrackTagOverrides(ctx, trackID, overrides)
	if err != nil {
		return otelx.RecordSpanError(span, orNotFound(err, ErrTrackNotFound))
	}
	s.queueAlbum(ctx, albumID)
	return nil
}

// queueAlbum queues an album's files after a change someone made to it,
// unless tagging is off; the hash settles the files nothing changed for.
func (s *Service) queueAlbum(ctx context.Context, albumID uint32) {
	if config.Get().Library.MusicTags.Mode == config.MusicTagsOff {
		return
	}
	if _, err := s.db.QueueAlbumTags(ctx, albumID, db.MarkStale); err != nil {
		slog.WarnContext(ctx, "queueing an album's files for their tags failed",
			"album.id", albumID, "error", err)
	}
}

func orNotFound(err, sentinel error) error {
	if ent.IsNotFound(err) {
		return sentinel
	}
	return err
}

// checkEdits is the outside-edit check, under managed: the written files of
// managed albums checked longest ago are stat-ed, and one whose size or
// modification time moved since the writer last recorded it is read and
// compared with what it should carry. A change to a key Streamline owns
// queues the file for a full rewrite; anything else (ReplayGain added by a
// player, a key the policy preserves) is recorded as seen. It returns how
// many files it queued.
func (s *Service) checkEdits(
	ctx context.Context,
	cfg config.MusicTagsConfig,
	limit int,
) int {
	ctx, span := tracer.Start(ctx, "music.check_tag_edits")
	defer span.End()

	files, err := s.db.ListMediaFilesForEditCheck(ctx, limit)
	if err != nil {
		_ = otelx.RecordSpanError(span, err)
		slog.WarnContext(ctx, "listing music files to check for edits failed",
			"error", err)
		return 0
	}
	policy := TagPolicy(cfg)
	albums := map[uint32]*ent.Album{}
	edited, checked := 0, 0
	for _, mf := range files {
		now := timeNow()
		st, err := statFile(mf.Path)
		if err != nil {
			// A missing file is the library scan's business, not the tags'.
			_ = s.db.SetMediaFileTagsChecked(
				ctx,
				mf.ID,
				mf.TagsMtime,
				mf.TagsSize,
				now,
			)
			continue
		}
		checked++
		mtime, size := st.ModTime().UnixNano(), st.Size()
		if mtime == mf.TagsMtime && size == mf.TagsSize {
			_ = s.db.SetMediaFileTagsChecked(ctx, mf.ID, mtime, size, now)
			continue
		}
		// A file that cannot be compared still had its turn: stamped as
		// checked against what the writer recorded, so the change is seen
		// again on its next turn, and the rotation (oldest check first) is
		// not headed by the same unreadable rows on every pass.
		skip := func() {
			_ = s.db.SetMediaFileTagsChecked(
				ctx, mf.ID, mf.TagsMtime, mf.TagsSize, now,
			)
		}
		tr := mf.Edges.Track
		if tr == nil || tr.Edges.Album == nil {
			skip()
			continue
		}
		alb, ok := albums[tr.Edges.Album.ID]
		if !ok {
			alb, err = s.db.FindAlbumByID(ctx, tr.Edges.Album.ID)
			if err != nil {
				slog.WarnContext(ctx, "music edit check could not read an album",
					"album.id", tr.Edges.Album.ID, "error", err)
				alb = nil
			}
			albums[tr.Edges.Album.ID] = alb
		}
		var full *ent.Track
		if alb != nil {
			full = trackOf(alb, tr.ID)
		}
		if full == nil || alb.Edges.Artist == nil {
			skip()
			continue
		}
		states, err := audiotags.Compare(
			mf.Path, desiredTags(alb.Edges.Artist, alb, full), policy,
		)
		switch {
		case err != nil:
			// Not read, so not seen: a transient failure (another tagger
			// mid-write) must not accept the change.
			slog.WarnContext(ctx, "music edit check could not read a file",
				"media_file.id", mf.ID, "error", err)
			skip()
		case audiotags.Edited(states):
			slog.InfoContext(
				ctx,
				"music file edited elsewhere, queued to be rewritten",
				"media_file.id",
				mf.ID,
				"album.id",
				alb.ID,
			)
			if err := s.db.SetMediaFileTagsEdited(ctx, mf.ID, now); err == nil {
				edited++
			}
		default:
			_ = s.db.SetMediaFileTagsChecked(ctx, mf.ID, mtime, size, now)
		}
	}
	span.SetAttributes(
		attribute.Int("tags.checked", checked),
		attribute.Int("tags.edited", edited),
	)
	return edited
}

func trackOf(alb *ent.Album, id uint32) *ent.Track {
	for _, t := range alb.Edges.Tracks {
		if t.ID == id {
			return t
		}
	}
	return nil
}
