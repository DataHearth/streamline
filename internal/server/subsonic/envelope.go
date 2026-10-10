package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
)

const apiVersion = "1.16.1"

const (
	errGeneric          = 0
	errMissingParam     = 10
	errWrongCredentials = 40
	errNotFound         = 70
)

type apiError struct {
	Code    int    `xml:"code,attr"    json:"code"`
	Message string `xml:"message,attr" json:"message"`
}

func (e *apiError) Error() string { return e.Message }

// body is the payload set by each handler, exactly one field non-nil,
// serialized inside <subsonic-response> / "subsonic-response".
type body struct {
	License      *license       `xml:"license,omitempty"       json:"license,omitempty"`
	MusicFolders *musicFolders  `xml:"musicFolders,omitempty"  json:"musicFolders,omitempty"`
	Artists      *indexes       `xml:"artists,omitempty"       json:"artists,omitempty"`
	Indexes      *folderIndexes `xml:"indexes,omitempty"       json:"indexes,omitempty"`
	Artist       *artistWith    `xml:"artist,omitempty"        json:"artist,omitempty"`
	Album        *albumWith     `xml:"album,omitempty"         json:"album,omitempty"`
	Song         *child         `xml:"song,omitempty"          json:"song,omitempty"`
	AlbumList2   *albumList2    `xml:"albumList2,omitempty"    json:"albumList2,omitempty"`
	SearchResult *search3       `xml:"searchResult3,omitempty" json:"searchResult3,omitempty"`
	Error        *apiError      `xml:"error,omitempty"         json:"error,omitempty"`
}

type response struct {
	XMLName xml.Name `xml:"subsonic-response" json:"-"`
	Xmlns   string   `xml:"xmlns,attr"        json:"-"`
	Status  string   `xml:"status,attr"       json:"status"`
	Version string   `xml:"version,attr"      json:"version"`
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

func write(w http.ResponseWriter, r *http.Request, resp response) {
	resp.Xmlns = "http://subsonic.org/restapi"
	resp.Version = apiVersion

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
