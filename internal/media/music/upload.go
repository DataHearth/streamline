package music

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/otelx"
	"github.com/datahearth/streamline/internal/upload"
)

const defaultUploadTitle = "Upload"

// ReceiveFunc is the handler's half of an upload: it runs once the service has
// checked the target, and streams the request body into staging.
type ReceiveFunc func(ctx context.Context, kind upload.Kind) (*upload.Staged, error)

type UploadOptions struct {
	Replace bool
	Title   string
}

// AlbumUploadPreview reads the names of the files about to be uploaded
// against the album's tracks, the way AlbumReleaseFiles reads a torrent.
func (s *Service) AlbumUploadPreview(
	ctx context.Context,
	albumID uint32,
	files []download.ReleaseFile,
) (*ReleaseFilesView, error) {
	ctx, span := tracer.Start(ctx, "music.album_upload_preview",
		trace.WithAttributes(
			attribute.Int("album.id", int(albumID)),
			attribute.Int("files", len(files)),
		))
	defer span.End()

	wav := library.ConvertsWAV(s.prober)
	a, err := s.db.FindAlbumByID(ctx, albumID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, albumNotFound(err))
	}
	if err := s.checkAlbumsFree(ctx, albumID); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if err := upload.CheckNames(
		upload.KindAlbum,
		filePaths(files),
		wav,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if err := upload.CheckSpace(filesSize(files)); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	view := albumFilesView(a, files, wav)
	view.TracksPending = a.MetadataFetchedAt == nil
	return view, nil
}

// ArtistUploadPreview reads the names of the files about to be uploaded
// against every album of the artist: an upload fills any of them, monitored
// or not, so every one is a folder's candidate.
func (s *Service) ArtistUploadPreview(
	ctx context.Context,
	artistID uint32,
	files []download.ReleaseFile,
) (*DiscographyFilesView, error) {
	ctx, span := tracer.Start(ctx, "music.artist_upload_preview",
		trace.WithAttributes(
			attribute.Int("artist.id", int(artistID)),
			attribute.Int("files", len(files)),
		))
	defer span.End()

	wav := library.ConvertsWAV(s.prober)
	art, err := s.db.FindArtistByID(ctx, artistID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, notFound(err))
	}
	if err := upload.CheckNames(
		upload.KindArtist,
		filePaths(files),
		wav,
	); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	all := db.ArtistAlbums(art)
	view := packFilesView(all, all, files, wav)

	// Only what would be sent counts: the sheet leaves unmatched folders out.
	var sent int64
	for _, f := range view.Files {
		if f.AlbumID != 0 {
			sent += f.Size
		}
	}
	if err := upload.CheckSpace(sent); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	var matched []uint32
	for _, f := range view.Folders {
		if f.AlbumID != 0 {
			matched = append(matched, f.AlbumID)
		}
	}
	if err := s.checkAlbumsFree(ctx, matched...); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	return view, nil
}

func filePaths(files []download.ReleaseFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return out
}

func filesSize(files []download.ReleaseFile) int64 {
	var total int64
	for _, f := range files {
		total += f.Size
	}
	return total
}

// UploadAlbum receives files onto an album and files them as an uploaded
// download record, for the importer to place.
func (s *Service) UploadAlbum(
	ctx context.Context,
	albumID uint32,
	o UploadOptions,
	recv ReceiveFunc,
) (*ent.DownloadRecord, error) {
	ctx, span := tracer.Start(ctx, "music.upload_album",
		trace.WithAttributes(
			attribute.Int("album.id", int(albumID)),
			attribute.Bool("replace_existing", o.Replace),
		))
	defer span.End()

	if _, err := s.db.FindAlbumByID(ctx, albumID); err != nil {
		return nil, otelx.RecordSpanError(span, albumNotFound(err))
	}
	if err := s.checkAlbumsFree(ctx, albumID); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	staged, err := recv(ctx, upload.KindAlbum)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	defer staged.Done()

	rec, err := s.commitUpload(ctx, staged, o, db.CreateDownloadRecordParams{
		AlbumID: albumID,
	}, albumID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "album uploaded",
		"album.id", albumID, "download_record.id", rec.ID,
		"files", len(staged.Files))
	return rec, nil
}

// UploadTrack receives exactly one audio file, pinned to the track.
func (s *Service) UploadTrack(
	ctx context.Context,
	trackID uint32,
	o UploadOptions,
	recv ReceiveFunc,
) (*ent.DownloadRecord, error) {
	ctx, span := tracer.Start(ctx, "music.upload_track",
		trace.WithAttributes(
			attribute.Int("track.id", int(trackID)),
			attribute.Bool("replace_existing", o.Replace),
		))
	defer span.End()

	t, err := s.db.FindTrackByID(ctx, trackID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, trackNotFound(err))
	}
	albumID := t.Edges.Album.ID
	if err := s.checkAlbumsFree(ctx, albumID); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	staged, err := recv(ctx, upload.KindTrack)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	defer staged.Done()
	if len(staged.Files) != 1 ||
		!library.IsReleaseAudio(
			staged.Files[0].Path,
			library.ConvertsWAV(s.prober),
		) {
		staged.Discard()
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: a track takes exactly one audio file", upload.ErrRejected))
	}
	if len(db.PresentFiles(t.Edges.MediaFiles)) > 0 && !o.Replace {
		staged.Discard()
		return nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: track already has a file; replace it to upload",
			upload.ErrRejected))
	}

	rec, err := s.commitUpload(ctx, staged, o, db.CreateDownloadRecordParams{
		AlbumID: albumID,
		TrackID: trackID,
	}, albumID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "track uploaded",
		"track.id", trackID, "album.id", albumID, "download_record.id", rec.ID)
	return rec, nil
}

// UploadArtist receives a discography: each album folder is matched against
// every album of the artist, the folders that match none are dropped from
// staging and returned.
func (s *Service) UploadArtist(
	ctx context.Context,
	artistID uint32,
	o UploadOptions,
	recv ReceiveFunc,
) (*ent.DownloadRecord, []string, error) {
	ctx, span := tracer.Start(ctx, "music.upload_artist",
		trace.WithAttributes(
			attribute.Int("artist.id", int(artistID)),
			attribute.Bool("replace_existing", o.Replace),
		))
	defer span.End()

	art, err := s.db.FindArtistByID(ctx, artistID)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, notFound(err))
	}
	all := db.ArtistAlbums(art)
	staged, err := recv(ctx, upload.KindArtist)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	defer staged.Done()

	// Matching reads the last folder names, so a top folder wrapping the
	// whole discography changes nothing.
	var matched []uint32
	var skipped, dropped []string
	dirs := map[string]*ent.Album{}
	for _, f := range staged.Files {
		dir := library.PackAlbumDir(f.Path)
		a, seen := dirs[dir]
		if !seen {
			a = library.MatchPackDir(dir, all)
			dirs[dir] = a
			if a == nil {
				skipped = append(skipped, dir)
			} else if !slices.Contains(matched, a.ID) {
				matched = append(matched, a.ID)
			}
		}
		if a == nil {
			dropped = append(dropped, f.Path)
		}
	}
	if len(matched) == 0 {
		staged.Discard()
		return nil, nil, otelx.RecordSpanError(span, fmt.Errorf(
			"%w: no folder matches an album of this artist", upload.ErrRejected))
	}
	if err := staged.Drop(dropped); err != nil {
		staged.Discard()
		return nil, nil, otelx.RecordSpanError(span, err)
	}

	// Only the albums the upload names can clash, and only the received names
	// say which they are; the preview answers this before the bytes are sent.
	rec, err := s.commitUpload(ctx, staged, o, db.CreateDownloadRecordParams{
		ArtistID: artistID,
		AlbumIDs: matched,
	}, matched...)
	if err != nil {
		return nil, nil, otelx.RecordSpanError(span, err)
	}
	span.SetAttributes(
		attribute.Int("albums.matched", len(matched)),
		attribute.Int("folders.skipped", len(skipped)),
	)
	slog.InfoContext(ctx, "discography uploaded",
		"artist.id", artistID, "download_record.id", rec.ID,
		"albums", len(matched), "skipped", len(skipped))
	return rec, skipped, nil
}

// checkAlbumsFree refuses with upload.ErrConflict when any of the albums has
// a download in flight.
func (s *Service) checkAlbumsFree(ctx context.Context, ids ...uint32) error {
	for _, id := range ids {
		live, err := s.db.AlbumHasLiveRecord(ctx, id)
		if err != nil {
			return err
		}
		if live {
			return fmt.Errorf(
				"%w: cancel the download in progress first", upload.ErrConflict)
		}
	}
	return nil
}

// commitUpload files the staged upload as a client-less record in importing,
// unless one of the albums took a download while it was being received. Check,
// mark and insert hold uploadMu, or two uploads finishing together both pass the
// check. Any failure removes the staging folder: nothing refers to it.
func (s *Service) commitUpload(
	ctx context.Context,
	staged *upload.Staged,
	o UploadOptions,
	p db.CreateDownloadRecordParams,
	albumIDs ...uint32,
) (*ent.DownloadRecord, error) {
	s.uploadMu.Lock()
	defer s.uploadMu.Unlock()

	if err := s.checkAlbumsFree(ctx, albumIDs...); err != nil {
		staged.Discard()
		return nil, err
	}
	s.markAlbumsDownloading(ctx, albumIDs...)

	var size int64
	for _, f := range staged.Files {
		size += f.Size
	}
	p.Title = cmp.Or(o.Title, defaultUploadTitle)
	p.Size = size
	p.Status = downloadrecord.StatusImporting
	p.SavePath = staged.Dir
	p.Uploaded = true
	if o.Replace {
		p.ReplaceMode = downloadrecord.ReplaceModeAll
	}
	rec, err := s.db.CreateDownloadRecord(ctx, p)
	if err != nil {
		staged.Discard()
		return nil, fmt.Errorf("create upload record: %w", err)
	}
	return rec, nil
}

func (s *Service) markAlbumsDownloading(ctx context.Context, ids ...uint32) {
	for _, id := range ids {
		_, err := s.db.SetAlbumStatus(
			ctx,
			id,
			[]album.Status{
				album.StatusWanted,
				album.StatusPaused,
				album.StatusFailed,
			},
			album.StatusDownloading,
		)
		if err != nil {
			slog.WarnContext(ctx, "mark album downloading failed",
				"album.id", id, "error", err)
		}
	}
}

func trackNotFound(err error) error {
	if ent.IsNotFound(err) {
		return ErrTrackNotFound
	}
	return err
}
