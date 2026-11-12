// Package sdk is the guest side of Streamline's extension contract, version 1.
// The types mirror internal/extension/contract.go field for field: the JSON tags
// are the wire.
package sdk

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/extism/go-pdk"
)

type Manifest struct {
	Name          string        `json:"name"`
	Version       string        `json:"version"`
	StreamlineAPI int           `json:"streamline_api"`
	Mounts        []Mount       `json:"mounts"`
	Views         []string      `json:"views"`
	Folders       []string      `json:"folders"`
	Services      []string      `json:"services"`
	AccountCards  []AccountCard `json:"account_cards"`
}

type Mount struct {
	Prefix string `json:"prefix"`
	Auth   string `json:"auth"`
}

type AccountCard struct {
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Title    string      `json:"title"`
	Mount    string      `json:"mount"`
	Username string      `json:"username"`
	Fields   []CardField `json:"fields,omitempty"`
}

const (
	CardCredential = "credential"
	CardSettings   = "settings"

	FieldSwitch = "switch"
	FieldNumber = "number"
)

// CardField is one control of a settings card. Min and Max bound a number and
// mean nothing on a switch.
type CardField struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Min   int64  `json:"min"`
	Max   int64  `json:"max"`
}

type Request struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Query   string              `json:"query"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
	// Mount is the prefix the request arrived under, empty for the
	// extension's own /api and /cards subtrees. Origin is the scheme and host
	// the client used, as Streamline works it out behind its trusted proxies.
	Mount   string   `json:"mount,omitempty"`
	Origin  string   `json:"origin,omitempty"`
	Caller  *Caller  `json:"caller,omitempty"`
	Folders []Folder `json:"folders,omitempty"`
	// TargetUser is the account a settings card is read or written for: an
	// admin editing another user, so it can differ from Caller.
	TargetUser uint32 `json:"target_user,omitempty"`
}

// Folder is a granted directory as the guest sees it: HostPath is what the
// views report, GuestPath is where this call has it mounted.
type Folder struct {
	Name      string `json:"name"`
	HostPath  string `json:"host_path"`
	GuestPath string `json:"guest_path"`
}

// Folder returns the granted folder of that name, when this call has it.
func (r Request) Folder(name string) (Folder, bool) {
	for _, f := range r.Folders {
		if f.Name == name {
			return f, true
		}
	}
	return Folder{}, false
}

// GuestPath maps a path a view reports (a file's host path) to where this
// call has it mounted.
func (r Request) GuestPath(hostPath string) (string, bool) {
	hostPath = path.Clean(hostPath)
	for _, f := range r.Folders {
		root := path.Clean(f.HostPath)
		if hostPath == root {
			return f.GuestPath, true
		}
		if rel, ok := strings.CutPrefix(hostPath, root+"/"); ok {
			return path.Join(f.GuestPath, rel), true
		}
	}
	return "", false
}

type Caller struct {
	ID   uint32 `json:"id"`
	Role string `json:"role"`
}

type Response struct {
	Status       int                 `json:"status"`
	Headers      map[string][]string `json:"headers"`
	Body         []byte              `json:"body"`
	ServeFile    *ServeFile          `json:"serve_file,omitempty"`
	ServePoster  *ServePoster        `json:"serve_poster,omitempty"`
	SendFile     *SendFile           `json:"send_file,omitempty"`
	StreamFFmpeg *StreamFFmpeg       `json:"stream_ffmpeg,omitempty"`
}

// SendFile asks the host to stream a file from a granted folder after the
// guest has returned. Path is a host path as the views and Folder.HostPath
// give it; the host finds the folder holding it.
type SendFile struct {
	Path        string `json:"path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type FFmpegCall struct {
	Tool string   `json:"tool"`
	Args []string `json:"args"`
}

// FFmpegResult is what a run left behind. Error is set, and the rest empty,
// when the host did not run the tool to completion.
type FFmpegResult struct {
	ExitCode int    `json:"exit_code"`
	Stdout   []byte `json:"stdout"`
	Stderr   []byte `json:"stderr"`
	Error    string `json:"error"`
}

// StreamFFmpeg asks the host to run ffmpeg after the guest has returned and
// stream its stdout as the response. Fallback is served instead when ffmpeg
// cannot start or fails before its first byte.
type StreamFFmpeg struct {
	Args        []string            `json:"args"`
	ContentType string              `json:"content_type"`
	Headers     map[string][]string `json:"headers"`
	Fallback    *SendFile           `json:"fallback,omitempty"`
}

type ServeFile struct {
	FileID      uint32 `json:"file_id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type ServePoster struct {
	Kind string `json:"kind"`
	ID   uint32 `json:"id"`
}

type SQLCall struct {
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

type SQLResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rows_affected"`
	LastInsertID int64    `json:"last_insert_id"`
	Error        string   `json:"error"`
}

// WriteManifest is the body of the guest's `manifest` export.
func WriteManifest(m Manifest) int32 {
	if err := pdk.OutputJSON(m); err != nil {
		pdk.SetError(err)
		return 1
	}
	return 0
}

// Dispatch is the body of the guest's `handle` export.
func Dispatch(h func(Request) Response) int32 {
	var req Request
	if err := pdk.InputJSON(&req); err != nil {
		pdk.SetError(err)
		return 1
	}
	if err := pdk.OutputJSON(h(req)); err != nil {
		pdk.SetError(err)
		return 1
	}
	return 0
}

// Query runs a read statement. A statement the database rejects comes back in
// SQLResult.Error with a nil error; the error is for a failed host call.
func Query(
	sql string,
	args ...any,
) (SQLResult, error) {
	return call(hostSQLQuery, sql, args)
}

// Exec runs a write statement against the extension's own database.
func Exec(
	sql string,
	args ...any,
) (SQLResult, error) {
	return call(hostSQLExec, sql, args)
}

// Log writes one line to the host's log, tagged with the extension's name.
// kv is alternating keys and values.
func Log(level, msg string, kv ...any) {
	b, err := json.Marshal(struct {
		Level   string `json:"level"`
		Message string `json:"msg"`
		KV      []any  `json:"kv"`
	}{level, msg, kv})
	if err != nil {
		return
	}
	mem := pdk.AllocateBytes(b)
	defer mem.Free()
	hostLog(mem.Offset())
}

func call(fn func(uint64) uint64, sql string, args []any) (SQLResult, error) {
	in, err := json.Marshal(SQLCall{SQL: sql, Args: args})
	if err != nil {
		return SQLResult{}, err
	}
	mem := pdk.AllocateBytes(in)
	defer mem.Free()
	off := fn(mem.Offset())
	if off == 0 {
		return SQLResult{}, fmt.Errorf("sdk: host call failed")
	}
	out := pdk.FindMemory(off)
	defer out.Free()
	var res SQLResult
	if err := json.Unmarshal(out.ReadBytes(), &res); err != nil {
		return SQLResult{}, err
	}
	return res, nil
}

// FFmpeg runs ffmpeg or ffprobe (tool) with args as given, no shell, in the
// extension's cache folder. A run the host refused or cut short comes back in
// FFmpegResult.Error with a nil error; the error is for a failed host call.
func FFmpeg(tool string, args ...string) (FFmpegResult, error) {
	in, err := json.Marshal(FFmpegCall{Tool: tool, Args: args})
	if err != nil {
		return FFmpegResult{}, err
	}
	mem := pdk.AllocateBytes(in)
	defer mem.Free()
	off := hostFFmpegRun(mem.Offset())
	if off == 0 {
		return FFmpegResult{}, fmt.Errorf("sdk: host call failed")
	}
	out := pdk.FindMemory(off)
	defer out.Free()
	var res FFmpegResult
	if err := json.Unmarshal(out.ReadBytes(), &res); err != nil {
		return FFmpegResult{}, err
	}
	return res, nil
}

// ScanInfo is the host's library scan as ScanStatus reports it; the host
// names the type ScanStatus.
type ScanInfo struct {
	Scanning bool       `json:"scanning"`
	Count    int64      `json:"count"`
	LastScan *time.Time `json:"last_scan"`
}

type scanResult struct {
	ScanInfo
	Error string `json:"error,omitempty"`
}

// ScanStatus reports the library scan: whether one is running, how many
// tracks hold a file on disk and when the last one finished. It needs the
// library_scan service.
func ScanStatus() (ScanInfo, error) { return scan(hostScanStatus) }

// StartScan starts a library scan and returns at once; one already running is
// not an error. It needs the library_scan service.
func StartScan() error {
	_, err := scan(hostScanStart)
	return err
}

func scan(fn func(uint64) uint64) (ScanInfo, error) {
	mem := pdk.AllocateBytes([]byte("{}"))
	defer mem.Free()
	off := fn(mem.Offset())
	if off == 0 {
		return ScanInfo{}, fmt.Errorf("sdk: host call failed")
	}
	out := pdk.FindMemory(off)
	defer out.Free()
	var res scanResult
	if err := json.Unmarshal(out.ReadBytes(), &res); err != nil {
		return ScanInfo{}, err
	}
	if res.Error != "" {
		return ScanInfo{}, errors.New(res.Error)
	}
	return res.ScanInfo, nil
}

// FetchRequest is one HTTP request the host makes for the extension.
type FetchRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

type FetchResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
	Error   string              `json:"error,omitempty"`
}

// Fetch sends req through the host and returns the answer as it came back: a
// redirect is not followed and a status of 400 or more is not an error. It
// needs the http service, and the URL's host:port must be in the hosts list
// the admin wrote for this extension. The answer is at most 16 MiB.
func Fetch(req FetchRequest) (FetchResponse, error) {
	in, err := json.Marshal(req)
	if err != nil {
		return FetchResponse{}, err
	}
	mem := pdk.AllocateBytes(in)
	defer mem.Free()
	off := hostHTTPRequest(mem.Offset())
	if off == 0 {
		return FetchResponse{}, fmt.Errorf("sdk: host call failed")
	}
	out := pdk.FindMemory(off)
	defer out.Free()
	var res FetchResponse
	if err := json.Unmarshal(out.ReadBytes(), &res); err != nil {
		return FetchResponse{}, err
	}
	if res.Error != "" {
		return FetchResponse{}, errors.New(res.Error)
	}
	return res, nil
}
