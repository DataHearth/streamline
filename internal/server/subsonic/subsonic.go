// Package subsonic serves the Subsonic REST API (1.16.1, with the OpenSubsonic
// extensions it advertises) over the adopted music library. It sits outside
// the OpenAPI surface, like /health.
package subsonic

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/scheduler"
)

// Transcoder converts one audio file for stream. *ffmpeg.CLI implements it.
type Transcoder interface {
	FFmpegPath() string
	TranscodeAudio(
		ctx context.Context,
		src string,
		opts ffmpeg.AudioTranscode,
		w io.Writer,
	) error
}

// Jobs is the slice of the scheduler startScan and getScanStatus drive.
type Jobs interface {
	Get(name string) (scheduler.JobInfo, error)
	RunNow(name string) error
}

type Handler struct {
	ent        *ent.Client
	posters    posters.Manager
	tracker    *appaccess.Tracker
	transcoder Transcoder
	jobs       Jobs
	playing    *nowPlaying
	transcodes chan struct{}
}

type Deps struct {
	Ent     *ent.Client
	Posters posters.Manager
	Tracker *appaccess.Tracker
	// Transcoder is optional: without one, stream always sends the original.
	Transcoder Transcoder
	// Jobs is optional: without it startScan is refused and getScanStatus
	// never reports a scan.
	Jobs Jobs
}

func New(d Deps) *Handler {
	return &Handler{
		ent:        d.Ent,
		posters:    d.Posters,
		tracker:    d.Tracker,
		transcoder: d.Transcoder,
		jobs:       d.Jobs,
		playing:    newNowPlaying(),
		transcodes: make(chan struct{}, maxTranscodes),
	}
}

type endpoint func(w http.ResponseWriter, r *http.Request, u *ent.User) error

func (h *Handler) endpoints() map[string]endpoint {
	return map[string]endpoint{
		// System
		"ping":       h.ping,
		"getLicense": h.getLicense,

		// Browsing
		"getMusicFolders":   h.getMusicFolders,
		"getIndexes":        h.getIndexes,
		"getMusicDirectory": h.getMusicDirectory,
		"getGenres":         h.getGenres,
		"getArtists":        h.getArtists,
		"getArtist":         h.getArtist,
		"getAlbum":          h.getAlbum,
		"getSong":           h.getSong,
		"getArtistInfo":     h.getArtistInfo,
		"getArtistInfo2":    h.getArtistInfo2,
		"getAlbumInfo":      h.getAlbumInfo,
		"getAlbumInfo2":     h.getAlbumInfo,
		"getSimilarSongs":   h.getSimilarSongs,
		"getSimilarSongs2":  h.getSimilarSongs2,
		"getTopSongs":       h.getTopSongs,

		// Album and song lists
		"getAlbumList":    h.getAlbumList,
		"getAlbumList2":   h.getAlbumList2,
		"getRandomSongs":  h.getRandomSongs,
		"getSongsByGenre": h.getSongsByGenre,
		"getNowPlaying":   h.getNowPlaying,
		"getStarred":      h.getStarred,
		"getStarred2":     h.getStarred2,

		// Searching
		"search":  h.search,
		"search2": h.search2,
		"search3": h.search3,

		// Playlists
		"getPlaylists":   h.getPlaylists,
		"getPlaylist":    h.getPlaylist,
		"createPlaylist": h.createPlaylist,
		"updatePlaylist": h.updatePlaylist,
		"deletePlaylist": h.deletePlaylist,

		// Media retrieval
		"stream":            h.stream,
		"download":          h.download,
		"getCoverArt":       h.getCoverArt,
		"getLyrics":         h.getLyrics,
		"getLyricsBySongId": h.getLyricsBySongID,
		"getAvatar":         h.getAvatar,

		// Media annotation
		"star":      h.star,
		"unstar":    h.unstar,
		"setRating": h.setRating,
		"scrobble":  h.scrobble,

		// Sharing
		"getShares":   h.getShares,
		"createShare": h.createShare,
		"updateShare": h.updateShare,
		"deleteShare": h.deleteShare,

		// Internet radio
		"getInternetRadioStations":   h.getInternetRadioStations,
		"createInternetRadioStation": h.createInternetRadioStation,
		"updateInternetRadioStation": h.updateInternetRadioStation,
		"deleteInternetRadioStation": h.deleteInternetRadioStation,

		// User management: the caller's own account only.
		"getUser": h.getUser,

		// Bookmarks and the play queue
		"getBookmarks":         h.getBookmarks,
		"createBookmark":       h.createBookmark,
		"deleteBookmark":       h.deleteBookmark,
		"getPlayQueue":         h.getPlayQueue,
		"savePlayQueue":        h.savePlayQueue,
		"getPlayQueueByIndex":  h.getPlayQueueByIndex,
		"savePlayQueueByIndex": h.savePlayQueueByIndex,

		// Library scanning
		"getScanStatus": h.getScanStatus,
		"startScan":     h.startScan,
	}
}

// Routes is mounted at /rest by the composition root.
func (h *Handler) Routes() http.Handler {
	endpoints := h.endpoints()

	r := chi.NewRouter()
	r.HandleFunc("/{endpoint}", func(w http.ResponseWriter, r *http.Request) {
		// Clients such as DSub and Symfonium send the parameters as a POST
		// form body; ParseForm merges that with the query into r.Form.
		if err := r.ParseForm(); err != nil {
			writeError(w, r, errGeneric, "Malformed request")
			return
		}
		name := strings.TrimSuffix(chi.URLParam(r, "endpoint"), ".view")
		// OpenSubsonic requires this one to answer without credentials, so a
		// client can learn what the server supports before it logs in.
		if name == "getOpenSubsonicExtensions" {
			writeOK(w, r, &body{OpenSubsonicExtensions: extensions})
			return
		}
		u, err := h.authenticate(r)
		if err != nil {
			fail(w, r, err)
			return
		}
		fn, ok := endpoints[name]
		if !ok {
			writeError(w, r, errGeneric, "Not implemented")
			return
		}
		if err := fn(w, r, u); err != nil {
			fail(w, r, err)
		}
	})
	return r
}

// extensions are the OpenSubsonic extensions this server implements.
var extensions = []openSubsonicExtension{
	{Name: "formPost", Versions: []int{1}},
	{Name: "songLyrics", Versions: []int{1}},
	{Name: "transcodeOffset", Versions: []int{1}},
	{Name: "indexBasedQueue", Versions: []int{1}},
}

func fail(w http.ResponseWriter, r *http.Request, err error) {
	if apiErr, ok := errors.AsType[*apiError](err); ok {
		writeError(w, r, apiErr.Code, apiErr.Message)
		return
	}
	slog.ErrorContext(r.Context(), "subsonic request failed", "error", err)
	writeError(w, r, errGeneric, "Internal error")
}

// respond decorates every song, album and artist in b with the caller's own
// star, rating and play count, then writes it.
func (h *Handler) respond(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
	b *body,
) error {
	if err := h.decorate(r.Context(), u, b); err != nil {
		return err
	}
	writeOK(w, r, b)
	return nil
}
