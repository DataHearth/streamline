package extension

import (
	"context"
	"time"
)

const APIVersion = 1

const (
	MemoryPages     = 1024 // 64 KiB each: 64 MiB per guest instance
	RequestTimeout  = 30 * time.Second
	SQLTimeout      = 5 * time.Second
	MaxResponseBody = 16 << 20
	MaxWasmBytes    = 32 << 20
	CacheBytes      = 1 << 30
	MaxFFmpegProcs  = 8
	MaxFFmpegStderr = 64 << 10
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

// Caller is the signed-in user. KeyID and KeyName are the API key the request
// came with, both zero for a browser session: one key per device makes the
// key the device, and a revoked key stops arriving.
type Caller struct {
	ID      uint32 `json:"id"`
	Role    string `json:"role"`
	KeyID   uint32 `json:"key_id,omitempty"`
	KeyName string `json:"key_name,omitempty"`
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

// FetchCall is one HTTP request a guest asks the host to make, through the
// http grant, to a host the entry's hosts list names.
type FetchCall struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

// FetchResult is the answer as it came back, a redirect included. Error is
// set, and the rest empty, when the host made no request or got no answer.
type FetchResult struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
	Error   string              `json:"error,omitempty"`
}

// Scanner is the host's library scan, reached through the library_scan
// grant.
type Scanner interface {
	ScanStatus(ctx context.Context) (ScanStatus, error)
	// StartScan returns once the scan is running; one already running is not
	// an error.
	StartScan(ctx context.Context) error
}

type ScanStatus struct {
	Scanning bool       `json:"scanning"`
	Count    int64      `json:"count"`
	LastScan *time.Time `json:"last_scan"`
}

// ScanResult answers scan_status and scan_start. Error is set, and the status
// zero, when the host did not answer the call.
type ScanResult struct {
	ScanStatus
	Error string `json:"error,omitempty"`
}
