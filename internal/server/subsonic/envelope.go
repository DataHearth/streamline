package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/internal/buildinfo"
)

const apiVersion = "1.16.1"

// serverType is the OpenSubsonic "type" every response carries.
const serverType = "streamline"

const (
	errGeneric          = 0
	errMissingParam     = 10
	errWrongCredentials = 40
	errNotAuthorized    = 50
	errNotFound         = 70
)

type apiError struct {
	Code    int    `xml:"code,attr"    json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

func (e *apiError) Error() string { return e.Message }

// body is the payload set by each handler, at most one field non-nil,
// serialized inside <subsonic-response> / "subsonic-response".
type body struct {
	License                *license                `xml:"license,omitempty"                json:"license,omitempty"`
	MusicFolders           *musicFolders           `xml:"musicFolders,omitempty"           json:"musicFolders,omitempty"`
	Artists                *indexes                `xml:"artists,omitempty"                json:"artists,omitempty"`
	Indexes                *folderIndexes          `xml:"indexes,omitempty"                json:"indexes,omitempty"`
	Directory              *directory              `xml:"directory,omitempty"              json:"directory,omitempty"`
	Genres                 *genres                 `xml:"genres,omitempty"                 json:"genres,omitempty"`
	Artist                 *artistWith             `xml:"artist,omitempty"                 json:"artist,omitempty"`
	Album                  *albumWith              `xml:"album,omitempty"                  json:"album,omitempty"`
	Song                   *child                  `xml:"song,omitempty"                   json:"song,omitempty"`
	ArtistInfo             *artistInfo             `xml:"artistInfo,omitempty"             json:"artistInfo,omitempty"`
	ArtistInfo2            *artistInfo2            `xml:"artistInfo2,omitempty"            json:"artistInfo2,omitempty"`
	AlbumInfo              *albumInfo              `xml:"albumInfo,omitempty"              json:"albumInfo,omitempty"`
	SimilarSongs           *songs                  `xml:"similarSongs,omitempty"           json:"similarSongs,omitempty"`
	SimilarSongs2          *songs                  `xml:"similarSongs2,omitempty"          json:"similarSongs2,omitempty"`
	TopSongs               *songs                  `xml:"topSongs,omitempty"               json:"topSongs,omitempty"`
	AlbumList              *albumList              `xml:"albumList,omitempty"              json:"albumList,omitempty"`
	AlbumList2             *albumList2             `xml:"albumList2,omitempty"             json:"albumList2,omitempty"`
	RandomSongs            *songs                  `xml:"randomSongs,omitempty"            json:"randomSongs,omitempty"`
	SongsByGenre           *songs                  `xml:"songsByGenre,omitempty"           json:"songsByGenre,omitempty"`
	NowPlaying             *nowPlayingList         `xml:"nowPlaying,omitempty"             json:"nowPlaying,omitempty"`
	Starred                *starred                `xml:"starred,omitempty"                json:"starred,omitempty"`
	Starred2               *starred2               `xml:"starred2,omitempty"               json:"starred2,omitempty"`
	SearchResult           *searchResult           `xml:"searchResult,omitempty"           json:"searchResult,omitempty"`
	SearchResult2          *searchResult2          `xml:"searchResult2,omitempty"          json:"searchResult2,omitempty"`
	SearchResult3          *search3                `xml:"searchResult3,omitempty"          json:"searchResult3,omitempty"`
	Playlists              *playlists              `xml:"playlists,omitempty"              json:"playlists,omitempty"`
	Playlist               *playlistWith           `xml:"playlist,omitempty"               json:"playlist,omitempty"`
	Shares                 *shares                 `xml:"shares,omitempty"                 json:"shares,omitempty"`
	Bookmarks              *bookmarks              `xml:"bookmarks,omitempty"              json:"bookmarks,omitempty"`
	PlayQueue              *playQueue              `xml:"playQueue,omitempty"              json:"playQueue,omitempty"`
	PlayQueueByIndex       *playQueueByIndex       `xml:"playQueueByIndex,omitempty"       json:"playQueueByIndex,omitempty"`
	InternetRadioStations  *radioStations          `xml:"internetRadioStations,omitempty"  json:"internetRadioStations,omitempty"`
	ScanStatus             *scanStatus             `xml:"scanStatus,omitempty"             json:"scanStatus,omitempty"`
	User                   *subsonicUser           `xml:"user,omitempty"                   json:"user,omitempty"`
	Users                  *subsonicUsers          `xml:"users,omitempty"                  json:"users,omitempty"`
	Lyrics                 *lyrics                 `xml:"lyrics,omitempty"                 json:"lyrics,omitempty"`
	LyricsList             *lyricsList             `xml:"lyricsList,omitempty"             json:"lyricsList,omitempty"`
	OpenSubsonicExtensions []openSubsonicExtension `xml:"openSubsonicExtensions,omitempty" json:"openSubsonicExtensions,omitempty"`
	Error                  *apiError               `xml:"error,omitempty"                  json:"error,omitempty"`
}

type response struct {
	XMLName       xml.Name `xml:"subsonic-response"  json:"-"`
	Xmlns         string   `xml:"xmlns,attr"         json:"-"`
	Status        string   `xml:"status,attr"        json:"status"`
	Version       string   `xml:"version,attr"       json:"version"`
	Type          string   `xml:"type,attr"          json:"type"`
	ServerVersion string   `xml:"serverVersion,attr" json:"serverVersion"`
	OpenSubsonic  bool     `xml:"openSubsonic,attr"  json:"openSubsonic"`
	body
}

func writeOK(w http.ResponseWriter, r *http.Request, b *body) {
	if b == nil {
		b = &body{}
	}
	write(w, r, response{Status: "ok", body: *b})
}

func writeError(w http.ResponseWriter, r *http.Request, code int, msg string) {
	write(w, r, response{
		Status: "failed",
		Error:  &apiError{Code: code, Message: msg},
	})
}

// serverVersion is the build's version without a leading "v", "dev" for a
// plain go build.
func serverVersion() string {
	if v := strings.TrimPrefix(buildinfo.Version, "v"); v != "" {
		return v
	}
	return "dev"
}

func write(w http.ResponseWriter, r *http.Request, resp response) {
	resp.Xmlns = "http://subsonic.org/restapi"
	resp.Version = apiVersion
	resp.Type = serverType
	resp.ServerVersion = serverVersion()
	resp.OpenSubsonic = true

	var (
		out         []byte
		err         error
		contentType string
	)
	if r.FormValue("f") == "json" {
		contentType = "application/json; charset=utf-8"
		out, err = json.Marshal(map[string]response{"subsonic-response": resp})
	} else {
		contentType = "text/xml; charset=utf-8"
		out, err = xml.Marshal(resp)
		out = append([]byte(xml.Header), out...)
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "encode subsonic response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(out); err != nil {
		slog.DebugContext(r.Context(), "write subsonic response", "error", err)
	}
}
