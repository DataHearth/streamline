package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
)

// transmissionRPCPath is Transmission's default RPC endpoint. A custom
// rpc-url is rare; ponytail: hard-coded default, add a config path field if
// someone actually reconfigures it.
const transmissionRPCPath = "/transmission/rpc"

// Transmission talks the Transmission RPC protocol (hyphenated method names,
// camelCase torrent fields). Auth is optional HTTP Basic; the daemon also
// requires the X-Transmission-Session-Id CSRF handshake (a 409 challenge).
type Transmission struct {
	baseURL   string
	username  string
	password  string
	sessionID string
	client    *http.Client
}

func NewTransmission(baseURL, username, password string) *Transmission {
	return &Transmission{
		baseURL:  strings.TrimRight(baseURL, "/"),
		username: username,
		password: password,
		client:   otelx.HTTPClient,
	}
}

type trResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

// call posts a single RPC method and returns its arguments object. A 409
// response carries a fresh session id, which is captured before retrying once.
func (t *Transmission) call(
	ctx context.Context,
	method string,
	args any,
) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{"method": method, "arguments": args})
	if err != nil {
		return nil, err
	}

	do := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, t.baseURL+transmissionRPCPath,
			bytes.NewReader(payload),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if t.sessionID != "" {
			req.Header.Set("X-Transmission-Session-Id", t.sessionID)
		}
		if t.username != "" || t.password != "" {
			req.SetBasicAuth(t.username, t.password)
		}
		return t.client.Do(req)
	}

	resp, err := do()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	if resp.StatusCode == http.StatusConflict {
		t.sessionID = resp.Header.Get("X-Transmission-Session-Id")
		resp.Body.Close()
		if resp, err = do(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	var out trResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	if out.Result != "success" {
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedStatus, out.Result)
	}
	return out.Arguments, nil
}

func (t *Transmission) AddTorrent(
	ctx context.Context,
	src TorrentSource,
) (string, error) {
	if len(src.Bytes) == 0 && src.Magnet == "" {
		return "", fmt.Errorf("transmission add: empty torrent source")
	}
	args := map[string]any{"labels": []string{managedCategory}}
	if src.Magnet != "" {
		args["filename"] = src.Magnet
	} else {
		args["metainfo"] = base64.StdEncoding.EncodeToString(src.Bytes)
	}

	raw, err := t.call(ctx, "torrent-add", args)
	if err != nil {
		return "", fmt.Errorf("transmission add: %w", err)
	}
	var added struct {
		Added *struct {
			HashString string `json:"hashString"`
		} `json:"torrent-added"`
		Duplicate *struct {
			HashString string `json:"hashString"`
		} `json:"torrent-duplicate"`
	}
	if err := json.Unmarshal(raw, &added); err != nil {
		return "", fmt.Errorf("transmission add: %w: %w", ErrBadResponse, err)
	}
	if added.Duplicate != nil {
		return "", ErrTorrentAlreadyExists
	}
	if added.Added == nil || added.Added.HashString == "" {
		return "", fmt.Errorf("transmission add: no hash returned")
	}
	return strings.ToLower(added.Added.HashString), nil
}

// trFields are the torrent-get fields streamline reads. Names are camelCase
// per the Transmission RPC spec (unlike the hyphenated method names).
var trFields = []string{
	"hashString", "name", "percentDone", "totalSize",
	"downloadDir", "rateDownload", "eta", "status", "errorString",
}

type trTorrent struct {
	HashString   string  `json:"hashString"`
	Name         string  `json:"name"`
	PercentDone  float64 `json:"percentDone"`
	TotalSize    int64   `json:"totalSize"`
	DownloadDir  string  `json:"downloadDir"`
	RateDownload int64   `json:"rateDownload"`
	Eta          int64   `json:"eta"`
	Status       int     `json:"status"`
	ErrorString  string  `json:"errorString"`
}

// get runs torrent-get; ids nil fetches all torrents.
func (t *Transmission) get(ctx context.Context, ids any) ([]Torrent, error) {
	args := map[string]any{"fields": trFields}
	if ids != nil {
		args["ids"] = ids
	}
	raw, err := t.call(ctx, "torrent-get", args)
	if err != nil {
		return nil, fmt.Errorf("transmission get: %w", err)
	}
	var out struct {
		Torrents []trTorrent `json:"torrents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("transmission get: %w: %w", ErrBadResponse, err)
	}
	torrents := make([]Torrent, 0, len(out.Torrents))
	for _, tr := range out.Torrents {
		eta := max(tr.Eta, 0) // -1/-2 mean unknown; UI treats 0 as unknown.
		torrents = append(torrents, Torrent{
			Hash: strings.ToLower(tr.HashString),
			Name: tr.Name,
			Status: mapTransmissionState(
				tr.Status,
				tr.PercentDone,
				tr.ErrorString,
			),
			Progress:      tr.PercentDone,
			Size:          tr.TotalSize,
			SavePath:      tr.DownloadDir,
			DownloadSpeed: tr.RateDownload,
			ETA:           eta,
		})
	}
	return torrents, nil
}

func (t *Transmission) GetTorrent(
	ctx context.Context,
	hash string,
) (*Torrent, error) {
	torrents, err := t.get(ctx, []string{hash})
	if err != nil {
		return nil, err
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTorrentNotFound, hash)
	}
	return &torrents[0], nil
}

func (t *Transmission) ListTorrents(ctx context.Context) ([]Torrent, error) {
	return t.get(ctx, nil)
}

func (t *Transmission) RemoveTorrent(
	ctx context.Context,
	hash string,
	deleteFiles bool,
) error {
	_, err := t.call(ctx, "torrent-remove", map[string]any{
		"ids":               []string{hash},
		"delete-local-data": deleteFiles,
	})
	if err != nil {
		return fmt.Errorf("transmission remove: %w", err)
	}
	return nil
}

func (t *Transmission) PauseTorrent(ctx context.Context, hash string) error {
	if _, err := t.call(ctx, "torrent-stop",
		map[string]any{"ids": []string{hash}}); err != nil {
		return fmt.Errorf("transmission stop: %w", err)
	}
	return nil
}

func (t *Transmission) ResumeTorrent(ctx context.Context, hash string) error {
	if _, err := t.call(ctx, "torrent-start",
		map[string]any{"ids": []string{hash}}); err != nil {
		return fmt.Errorf("transmission start: %w", err)
	}
	return nil
}

func (t *Transmission) TestConnection(ctx context.Context) error {
	_, err := t.call(ctx, "session-get", nil)
	return err
}

// mapTransmissionState maps the numeric torrent status to a TorrentStatus.
// A non-empty errorString wins; a stopped-but-complete torrent (seeding
// disabled) reports Completed so the monitor still imports it.
func mapTransmissionState(
	status int,
	percentDone float64,
	errString string,
) TorrentStatus {
	if errString != "" {
		return StatusError
	}
	switch status {
	case 5, 6: // queued to seed, seeding
		return StatusSeeding
	case 0: // stopped
		if percentDone >= 1 {
			return StatusCompleted
		}
		return StatusPaused
	default: // 1 verify-wait, 2 verifying, 3 dl-wait, 4 downloading
		return StatusDownloading
	}
}
