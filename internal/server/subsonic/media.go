package subsonic

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

// maxTranscodes caps concurrent ffmpeg streams.
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
		WithTrack().
		Order(mediafile.ByID()).
		First(ctx)
	return mf, lookupErr(err)
}

// streamLimits is the format and bitrate cap a stream honours: the client's,
// tightened by the account's own cap, which also overrides a request for the
// raw file. The cap holds only while a transcode can run; without ffmpeg, or
// with every transcode slot busy, the original is sent anyway.
func streamLimits(r *http.Request, u *ent.User) (string, int) {
	format := r.FormValue("format")
	maxKbps := intParam(r, "maxBitRate", 0, 0, 1<<20)
	if limit := int(u.SubsonicMaxBitRate); limit > 0 {
		if maxKbps == 0 || maxKbps > limit {
			maxKbps = limit
		}
		if strings.EqualFold(strings.TrimSpace(format), "raw") {
			format = ""
		}
	}
	return format, maxKbps
}

// stream sends the original file with range support, or a transcode when
// streamPlan asks for one. A transcode cannot seek by range; timeOffset
// starts it that many seconds in instead (OpenSubsonic transcodeOffset), and
// only a transcode can, so an offset always takes one.
func (h *Handler) stream(w http.ResponseWriter, r *http.Request, u *ent.User) error {
	if err := require(u, permStream); err != nil {
		return err
	}
	id, err := idParam(r, kindTrack)
	if err != nil {
		return err
	}
	mf, err := h.trackFile(r.Context(), id)
	if err != nil {
		return err
	}
	format, maxKbps := streamLimits(r, u)
	// The same estimate the song listing reported, so an unprobed file is
	// not taken for one of unknown bitrate and transcoded needlessly.
	srcKbps := kbpsOf(mf, durationOf(mf, mf.Edges.Track))
	plan, convert := streamPlan(suffixOf(mf), srcKbps, format, maxKbps)
	offset := intParam(r, "timeOffset", 0, 0, 1<<20)
	if offset == 0 {
		return h.sendAudio(w, r, mf, plan, convert)
	}
	// The client counts from the offset, so the original from 0 would play
	// at the wrong position: transcode or refuse, never fall back.
	if !convert {
		plan = offsetPlan(suffixOf(mf), srcKbps)
	}
	if !h.transcodingAvailable() ||
		!h.acquireTranscode(r.Context(), offsetSlotWait) {
		return errOffsetUnavailable
	}
	defer h.releaseTranscode()
	plan.OffsetSec = offset
	return h.transcode(w, r, mf, plan)
}

// offsetSlotWait is how long a stream with timeOffset waits for a transcode
// slot. It has no original to fall back on, so it queues briefly instead.
// A variable only so the specs need not wait it out.
var offsetSlotWait = 5 * time.Second

var errOffsetUnavailable = &apiError{
	Code:    errGeneric,
	Message: "timeOffset needs a transcode and none can run now",
}

// offsetPlan is the transcode a timeOffset needs when streamPlan asked for
// none: the file's own format when this server can make it, else mp3, at the
// file's bitrate within the encoder's bounds.
func offsetPlan(suffix string, srcKbps uint32) ffmpeg.AudioTranscode {
	format := suffix
	if _, ok := ffmpeg.AudioFormats[format]; !ok {
		format = "mp3"
	}
	kbps := defaultTranscodeKbps[format]
	if srcKbps > 0 {
		kbps = min(int(srcKbps), maxTranscodeKbps)
	}
	return ffmpeg.AudioTranscode{
		Format:      format,
		BitrateKbps: max(kbps, minTranscodeKbps),
	}
}

// acquireTranscode takes a transcode slot, waiting up to wait for one.
func (h *Handler) acquireTranscode(ctx context.Context, wait time.Duration) bool {
	select {
	case h.transcodes <- struct{}{}:
		return true
	default:
	}
	if wait <= 0 {
		return false
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case h.transcodes <- struct{}{}:
		return true
	case <-t.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (h *Handler) releaseTranscode() { <-h.transcodes }

// sendAudio sends mf as planned: a transcode when convert asks for one and a
// slot is free, else the original with range support. Past maxTranscodes the
// original goes out rather than an error: a player can still play it.
func (h *Handler) sendAudio(
	w http.ResponseWriter,
	r *http.Request,
	mf *ent.MediaFile,
	plan ffmpeg.AudioTranscode,
	convert bool,
) error {
	if !convert || !h.transcodingAvailable() {
		return serveFile(w, r, mf, false)
	}
	if !h.acquireTranscode(r.Context(), 0) {
		slog.InfoContext(
			r.Context(),
			"subsonic transcode limit reached, streaming original",
			"media_file.id",
			mf.ID,
		)
		return serveFile(w, r, mf, false)
	}
	defer h.releaseTranscode()
	return h.transcode(w, r, mf, plan)
}

// streamIdle is how long a stream or download may go without the client
// taking a byte. It replaces the listener's WriteTimeout, which cut a long
// track or a slow link off two minutes in, while still dropping a stalled
// client, and the transcode slot it holds.
const streamIdle = 2 * time.Minute

// idleChunk bounds one sendfile step, so the deadline moves as bytes go out.
const idleChunk = 4 << 20

// idleWriter pushes the connection's write deadline streamIdle ahead of
// every write.
type idleWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func newIdleWriter(w http.ResponseWriter) *idleWriter {
	iw := &idleWriter{ResponseWriter: w, rc: http.NewResponseController(w)}
	iw.touch()
	return iw
}

func (w *idleWriter) touch() {
	// Unsupported only off a real connection (a test recorder).
	_ = w.rc.SetWriteDeadline(time.Now().Add(streamIdle))
}

func (w *idleWriter) Write(p []byte) (int, error) {
	w.touch()
	return w.ResponseWriter.Write(p)
}

// ReadFrom keeps http.ServeContent's sendfile path, in idleChunk steps.
func (w *idleWriter) ReadFrom(src io.Reader) (int64, error) {
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	lr, limited := src.(*io.LimitedReader)
	if !ok || !limited {
		return io.Copy(struct{ io.Writer }{w}, src)
	}
	var total int64
	for lr.N > 0 {
		w.touch()
		n, err := rf.ReadFrom(&io.LimitedReader{R: lr.R, N: min(lr.N, idleChunk)})
		total += n
		lr.N -= n
		if err != nil || n == 0 {
			return total, err
		}
	}
	return total, nil
}

func (w *idleWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
	path := mf.Path                          // a library row, never request input
	if _, err := os.Stat(path); err != nil { //nolint:gosec // see path
		return &apiError{Code: errNotFound, Message: "file missing"}
	}
	hdr := w.Header()
	if r.Method == http.MethodHead {
		hdr.Set("Content-Type", ffmpeg.AudioFormats[plan.Format].ContentType)
		w.WriteHeader(http.StatusOK)
		return nil
	}
	sw := &sentWriter{ResponseWriter: newIdleWriter(w)}
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
	u *ent.User,
) error {
	if err := require(u, permDownload); err != nil {
		return err
	}
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
	http.ServeContent(newIdleWriter(w), r, name, mf.UpdateTime, f)
	return nil
}

// getCoverArt serves an album's cover for an album or a song, a grid of its
// albums' covers for a playlist, and an artist's photo.
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
		return h.servePlaylistCover(w, r, ts)
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
