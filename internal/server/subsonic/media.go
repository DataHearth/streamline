package subsonic

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

// maxTranscodes caps concurrent ffmpeg streams. Past it a request gets the
// original file rather than an error: a player can still play it.
const maxTranscodes = 8

// Bitrate bounds for a transcode, in kbps. The default applies when the
// client names a format but no maxBitRate.
const (
	minTranscodeKbps = 32
	maxTranscodeKbps = 320
)

var defaultTranscodeKbps = map[string]int{"mp3": 192, "opus": 128, "aac": 192}

// streamPlan decides whether stream converts a file, and to what. A client
// gets the original unless it asks for another format this server can make,
// or caps the bitrate below the file's (or a file whose bitrate is unknown).
// "raw" always means the original.
func streamPlan(
	suffix string,
	srcKbps uint32,
	format string,
	maxKbps int,
) (ffmpeg.AudioTranscode, bool) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "raw" {
		return ffmpeg.AudioTranscode{}, false
	}
	_, known := ffmpeg.AudioFormats[format]
	overCap := maxKbps > 0 && (srcKbps == 0 || int(srcKbps) > maxKbps)
	sameFormat := format == "" || format == suffix || !known
	if sameFormat && !overCap {
		return ffmpeg.AudioTranscode{}, false
	}
	target := format
	if !known {
		target = "mp3"
	}
	kbps := defaultTranscodeKbps[target]
	if maxKbps > 0 {
		kbps = min(maxKbps, maxTranscodeKbps)
	}
	return ffmpeg.AudioTranscode{
		Format:      target,
		BitrateKbps: max(kbps, minTranscodeKbps),
	}, true
}

func (h *Handler) transcodingAvailable() bool {
	if h.transcoder == nil || h.transcoder.FFmpegPath() == "" {
		return false
	}
	cfg := config.Get()
	return cfg == nil || cfg.FFmpeg.Enabled
}

func (h *Handler) trackFile(ctx context.Context, id uint32) (*ent.MediaFile, error) {
	mf, err := h.ent.MediaFile.Query().
		Where(
			mediafile.HasTrackWith(track.IDEQ(id)),
			mediafile.MissingSinceIsNil(),
		).
		Order(mediafile.ByID()).
		First(ctx)
	return mf, lookupErr(err)
}

// stream sends the original file with range support, or a transcode when
// streamPlan asks for one. A transcode cannot seek by range; timeOffset
// starts it that many seconds in instead (OpenSubsonic transcodeOffset).
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, _ *ent.User) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	mf, err := h.trackFile(r.Context(), id)
	if err != nil {
		return err
	}
	plan, convert := streamPlan(
		suffixOf(mf),
		kbpsOf(mf, mf.DurationSeconds),
		r.FormValue("format"),
		intParam(r, "maxBitRate", 0, 0, 1<<20),
	)
	if !convert || !h.transcodingAvailable() {
		return serveFile(w, r, mf, false)
	}
	select {
	case h.transcodes <- struct{}{}:
		defer func() { <-h.transcodes }()
	default:
		slog.InfoContext(
			r.Context(),
			"subsonic transcode limit reached, streaming original",
			"media_file.id",
			mf.ID,
		)
		return serveFile(w, r, mf, false)
	}
	plan.OffsetSec = intParam(r, "timeOffset", 0, 0, 1<<20)
	return h.transcode(w, r, mf, plan)
}

// sentWriter records whether anything reached the client, so a transcode
// that fails before its first byte can still answer a Subsonic error.
type sentWriter struct {
	http.ResponseWriter
	sent bool
}

func (s *sentWriter) Write(p []byte) (int, error) {
	if !s.sent {
		s.sent = true
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(p)
}

func (h *Handler) transcode(
	w http.ResponseWriter,
	r *http.Request,
	mf *ent.MediaFile,
	plan ffmpeg.AudioTranscode,
) error {
	if _, err := os.Stat(mf.Path); err != nil {
		return &apiError{Code: errNotFound, Message: "file missing"}
	}
	hdr := w.Header()
	if r.Method == http.MethodHead {
		hdr.Set("Content-Type", ffmpeg.AudioFormats[plan.Format].ContentType)
		w.WriteHeader(http.StatusOK)
		return nil
	}
	sw := &sentWriter{ResponseWriter: w}
	hdr.Set("Content-Type", ffmpeg.AudioFormats[plan.Format].ContentType)
	err := h.transcoder.TranscodeAudio(r.Context(), mf.Path, plan, sw)
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return nil
	case !sw.sent:
		hdr.Del("Content-Type")
		return err
	default:
		slog.WarnContext(r.Context(), "subsonic transcode failed mid-stream",
			"media_file.id", mf.ID, "error", err)
		return nil
	}
}

// download sends the original file as an attachment, never transcoded.
func (h *Handler) download(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	mf, err := h.trackFile(r.Context(), id)
	if err != nil {
		return err
	}
	return serveFile(w, r, mf, true)
}

func serveFile(
	w http.ResponseWriter,
	r *http.Request,
	mf *ent.MediaFile,
	attachment bool,
) error {
	if mf == nil {
		return errNotFoundErr
	}
	f, err := os.Open(mf.Path) //nolint:gosec // library row, not request input
	if err != nil {
		return &apiError{Code: errNotFound, Message: "file missing"}
	}
	defer f.Close()

	name := filepath.Base(mf.Path)
	w.Header().Set("Content-Type", contentTypeFor(suffixOf(mf)))
	if attachment {
		w.Header().Set("Content-Disposition",
			mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, name, mf.UpdateTime, f)
	return nil
}

// getCoverArt serves an album's cover for an album, a song or a playlist
// (its first playable entry's), and an artist's photo.
func (h *Handler) getCoverArt(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	kind, id, err := anyIDParam(r)
	if err != nil {
		return err
	}
	switch kind {
	case kindAlbum:
		h.posters.Serve(w, r, "albums", id)
	case kindArtist:
		h.posters.Serve(w, r, "artists", id)
	case kindTrack:
		t, err := h.ent.Track.Query().Where(track.IDEQ(id)).WithAlbum().Only(ctx)
		if err != nil {
			return lookupErr(err)
		}
		h.posters.Serve(w, r, "albums", t.Edges.Album.ID)
	case kindPlaylist:
		p, err := h.loadPlaylist(ctx, u, id)
		if err != nil {
			return err
		}
		ts, err := h.playableTracks(ctx, p.TrackIds)
		if err != nil {
			return err
		}
		if len(ts) == 0 {
			return errNotFoundErr
		}
		h.posters.Serve(w, r, "albums", ts[0].Edges.Album.ID)
	default:
		return errNotFoundErr
	}
	return nil
}

// getAvatar has nothing to serve: Streamline keeps no user pictures.
func (h *Handler) getAvatar(
	_ http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	if r.FormValue("username") == "" {
		return missingParam("username")
	}
	return errNotFoundErr
}

// lyricsFor reads the lyrics of t's first playable file.
func lyricsFor(t *ent.Track) (parsedLyrics, bool) {
	mf := firstFile(t)
	if mf == nil {
		return parsedLyrics{}, false
	}
	return readLyrics(mf.Path)
}

// getLyrics finds a song by artist and title and answers its lyrics as plain
// text; an empty element when there are none.
func (h *Handler) getLyrics(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	ctx := r.Context()
	artistName := strings.TrimSpace(r.FormValue("artist"))
	title := strings.TrimSpace(r.FormValue("title"))
	out := &lyrics{}
	if title == "" {
		writeOK(w, r, &body{Lyrics: out})
		return nil
	}
	q := h.ent.Track.Query().Where(track.TitleEqualFold(title), playable())
	if artistName != "" {
		q.Where(
			track.HasAlbumWith(
				album.HasArtistWith(artist.NameEqualFold(artistName)),
			),
		)
	}
	ts, err := songEdges(q).Order(track.ByID()).Limit(maxPageSize).All(ctx)
	if err != nil {
		return err
	}
	for _, t := range ts {
		if lyr, ok := lyricsFor(t); ok {
			song := songOf(t)
			out = &lyrics{Artist: song.Artist, Title: song.Title, Value: lyr.plain()}
			break
		}
	}
	writeOK(w, r, &body{Lyrics: out})
	return nil
}

// getLyricsBySongId is the OpenSubsonic songLyrics endpoint: the song's
// lyrics as lines, timed when the source is LRC.
func (h *Handler) getLyricsBySongID(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	t, err := songEdges(h.ent.Track.Query().Where(track.IDEQ(id))).Only(r.Context())
	if err != nil {
		return lookupErr(err)
	}
	out := &lyricsList{StructuredLyrics: []structuredLyrics{}}
	if lyr, ok := lyricsFor(t); ok {
		song := songOf(t)
		out.StructuredLyrics = append(out.StructuredLyrics, structuredLyrics{
			DisplayArtist: song.Artist,
			DisplayTitle:  song.Title,
			Lang:          "xxx",
			Synced:        lyr.synced,
			Line:          lyr.lines,
		})
	}
	writeOK(w, r, &body{LyricsList: out})
	return nil
}
