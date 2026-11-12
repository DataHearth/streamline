package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/extensions/sdk"
)

func main() {}

// extra is set by the echo-full build (-X main.extra=1) so one source holds
// both the plain fixture and the one that asks for every grant.
var extra string

//go:wasmexport manifest
func manifest() int32 {
	mf := sdk.Manifest{
		Name:          "echo",
		Version:       "0.0.1",
		StreamlineAPI: 1,
		Mounts:        []sdk.Mount{{Prefix: "/echo", Auth: "self"}},
		Views:         []string{"books"},
		Services:      []string{"serve_file"},
		AccountCards: []sdk.AccountCard{{
			ID: "secret", Kind: "credential", Title: "Echo",
			Mount: "/echo", Username: "email",
		}, {
			ID: "prefs", Kind: "settings", Title: "Echo settings",
			Fields: []sdk.CardField{
				{ID: "allow", Kind: "switch", Label: "Allow"},
				{ID: "limit", Kind: "number", Label: "Limit", Min: 0, Max: 320},
			},
		}},
	}
	if extra != "" {
		mf.Folders = []string{"music", "artwork", "cache"}
		mf.Services = append(mf.Services, "ffmpeg", "library_scan")
	}
	return sdk.WriteManifest(mf)
}

var sink [][]byte

//go:wasmexport handle
func handle() int32 {
	return sdk.Dispatch(func(r sdk.Request) sdk.Response {
		switch r.Path {
		case "/trap":
			panic("trap")
		case "/spin":
			for {
			}
		case "/grow":
			for {
				sink = append(sink, make([]byte, 1<<20))
			}
		case "/sql":
			res, err := sdk.Query("SELECT 1 AS n")
			if err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
			b, _ := json.Marshal(res)
			return sdk.Response{Status: 200, Body: b}
		case "/random":
			b := make([]byte, 16)
			if _, err := rand.Read(b); err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: 200, Body: []byte(hex.EncodeToString(b))}
		case "/file":
			return sdk.Response{ServeFile: &sdk.ServeFile{FileID: 7, Filename: "a.epub", ContentType: "application/epub+zip"}}
		case "/query":
			return sdk.Response{Status: 200, Body: []byte(r.Query)}
		case "/cache":
			q, _ := url.ParseQuery(r.Query)
			status, _ := strconv.Atoi(q.Get("status"))
			return sdk.Response{
				Status:  status,
				Headers: map[string][]string{"Cache-Control": {q.Get("cc")}},
				Body:    []byte("x"),
			}
		case "/request":
			b, _ := json.Marshal(map[string]string{"mount": r.Mount, "origin": r.Origin})
			return sdk.Response{Status: 200, Body: b}
		case "/headers", "/api/headers":
			b, _ := json.Marshal(r.Headers)
			return sdk.Response{Status: 200, Body: b}
		case "/cards/secret":
			return card(r)
		case "/cards/prefs":
			return prefs(r)
		case "/fs/read":
			q, _ := url.ParseQuery(r.Query)
			b, err := os.ReadFile(q.Get("path"))
			if err != nil {
				return sdk.Response{Status: 404, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: 200, Body: b}
		case "/fs/write":
			q, _ := url.ParseQuery(r.Query)
			if err := os.WriteFile(q.Get("path"), r.Body, 0o600); err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: 204}
		case "/fs/symlink":
			q, _ := url.ParseQuery(r.Query)
			if err := os.Symlink(q.Get("old"), q.Get("new")); err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: 204}
		case "/send":
			q, _ := url.ParseQuery(r.Query)
			return sdk.Response{SendFile: &sdk.SendFile{
				Path: q.Get("path"), Filename: q.Get("name"), ContentType: q.Get("type"),
			}}
		case "/ffmpeg/run":
			var call sdk.FFmpegCall
			if err := json.Unmarshal(r.Body, &call); err != nil {
				return sdk.Response{Status: 400, Body: []byte(err.Error())}
			}
			res, err := sdk.FFmpeg(call.Tool, call.Args...)
			if err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
			b, _ := json.Marshal(res)
			return sdk.Response{Status: 200, Body: b}
		case "/ffmpeg/stream":
			q, _ := url.ParseQuery(r.Query)
			st := &sdk.StreamFFmpeg{Args: q["arg"], ContentType: q.Get("type")}
			if fb := q.Get("fallback"); fb != "" {
				st.Fallback = &sdk.SendFile{Path: fb, ContentType: "text/plain"}
			}
			return sdk.Response{StreamFFmpeg: st}
		case "/scan/status":
			st, err := sdk.ScanStatus()
			if err != nil {
				return sdk.Response{Status: 502, Body: []byte(err.Error())}
			}
			b, _ := json.Marshal(st)
			return sdk.Response{Status: 200, Body: b}
		case "/scan/start":
			if err := sdk.StartScan(); err != nil {
				return sdk.Response{Status: 502, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: 204}
		case "/fetch":
			q, _ := url.ParseQuery(r.Query)
			res, err := sdk.Fetch(sdk.FetchRequest{
				Method:  r.Method,
				URL:     q.Get("url"),
				Headers: map[string][]string{"X-Echo": {"1"}},
				Body:    r.Body,
			})
			if err != nil {
				return sdk.Response{Status: 502, Body: []byte(err.Error())}
			}
			return sdk.Response{Status: res.Status, Headers: res.Headers, Body: res.Body}
		case "/fs/folders":
			folders := r.Folders
			if folders == nil {
				folders = []sdk.Folder{}
			}
			b, _ := json.Marshal(folders)
			return sdk.Response{Status: 200, Body: b}
		case "/fs/guest":
			q, _ := url.ParseQuery(r.Query)
			p, ok := r.GuestPath(q.Get("path"))
			if !ok {
				return sdk.Response{Status: 404}
			}
			return sdk.Response{Status: 200, Body: []byte(p)}
		case "/oversize":
			return sdk.Response{Status: 200, Body: make([]byte, 17<<20)}
		}
		if id, ok := strings.CutPrefix(r.Path, "/file/"); ok {
			n, _ := strconv.ParseUint(id, 10, 32)
			return sdk.Response{ServeFile: &sdk.ServeFile{FileID: uint32(n), Filename: "a\"/b\n.epub", ContentType: "application/epub+zip"}}
		}
		if rest, ok := strings.CutPrefix(r.Path, "/poster/"); ok {
			kind, id, _ := strings.Cut(rest, "/")
			n, _ := strconv.ParseUint(id, 10, 32)
			return sdk.Response{ServePoster: &sdk.ServePoster{Kind: kind, ID: uint32(n)}}
		}
		if strings.HasPrefix(r.Path, "/api/") {
			b, _ := json.Marshal(r.Caller)
			return sdk.Response{Status: 200, Body: b}
		}
		return sdk.Response{Status: 200, Body: []byte(r.Path)}
	})
}

// card is a credential card in the shape core's account endpoints read.
func card(r sdk.Request) sdk.Response {
	if r.Caller == nil {
		return sdk.Response{Status: 401}
	}
	id := int64(r.Caller.ID)
	if _, err := sdk.Exec(
		"CREATE TABLE IF NOT EXISTS secrets(user_id INTEGER PRIMARY KEY)",
	); err != nil {
		return sdk.Response{Status: 500, Body: []byte(err.Error())}
	}
	switch r.Method {
	case "POST":
		_, _ = sdk.Exec("INSERT OR REPLACE INTO secrets VALUES (?)", id)
		return sdk.Response{Status: 200, Body: []byte(`{"secret":"echo-secret"}`)}
	case "DELETE":
		_, _ = sdk.Exec("DELETE FROM secrets WHERE user_id = ?", id)
		return sdk.Response{Status: 204}
	}
	res, _ := sdk.Query("SELECT 1 FROM secrets WHERE user_id = ?", id)
	b, _ := json.Marshal(map[string]bool{"set": len(res.Rows) > 0})
	return sdk.Response{Status: 200, Body: b}
}

// prefs is a settings card whose values are kept per target user.
func prefs(r sdk.Request) sdk.Response {
	if _, err := sdk.Exec(
		"CREATE TABLE IF NOT EXISTS prefs(user_id INTEGER, id TEXT, value TEXT, PRIMARY KEY(user_id, id))",
	); err != nil {
		return sdk.Response{Status: 500, Body: []byte(err.Error())}
	}
	user := int64(r.TargetUser)
	if r.Method == "PUT" {
		var body struct {
			Values map[string]json.RawMessage `json:"values"`
		}
		if err := json.Unmarshal(r.Body, &body); err != nil {
			return sdk.Response{Status: 400, Body: []byte(err.Error())}
		}
		for id, v := range body.Values {
			if _, err := sdk.Exec(
				"INSERT OR REPLACE INTO prefs VALUES (?, ?, ?)", user, id, string(v),
			); err != nil {
				return sdk.Response{Status: 500, Body: []byte(err.Error())}
			}
		}
		return sdk.Response{Status: 204}
	}
	values := map[string]json.RawMessage{
		"allow": json.RawMessage("false"),
		"limit": json.RawMessage("0"),
	}
	res, err := sdk.Query("SELECT id, value FROM prefs WHERE user_id = ?", user)
	if err != nil {
		return sdk.Response{Status: 500, Body: []byte(err.Error())}
	}
	for _, row := range res.Rows {
		id, _ := row[0].(string)
		v, _ := row[1].(string)
		values[id] = json.RawMessage(v)
	}
	b, _ := json.Marshal(map[string]any{"values": values})
	return sdk.Response{Status: 200, Body: b}
}
