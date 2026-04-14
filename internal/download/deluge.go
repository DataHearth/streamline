package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/datahearth/streamline/internal/otelx"
)

// delugeRPCPath is the Deluge Web UI JSON-RPC endpoint. streamline drives the
// Web UI (port 8112 by default), not the native daemon RPC — it matches the
// host/port/password config and is what *arr clients have always used.
const delugeRPCPath = "/json"

// Deluge talks the Deluge Web UI JSON-RPC protocol. Auth is the Web UI
// password (auth.login → session cookie); the Web UI must then be attached to
// a daemon (web.connect), which streamline ensures lazily.
type Deluge struct {
	baseURL   string
	password  string
	cookies   []*http.Cookie
	connected bool
	client    *http.Client
}

func NewDeluge(baseURL, password string) *Deluge {
	return &Deluge{
		baseURL:  strings.TrimRight(baseURL, "/"),
		password: password,
		client:   otelx.HTTPClient,
	}
}

type delugeError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *delugeError) Error() string { return e.Message }

type delugeResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *delugeError    `json:"error"`
}

// rpc posts a raw JSON-RPC call. It does not bootstrap the session (login and
// connect use it directly); callers needing an authenticated daemon use call.
func (d *Deluge) rpc(
	ctx context.Context,
	method string,
	params ...any,
) (json.RawMessage, error) {
	if params == nil {
		params = []any{}
	}
	// id is constant: requests are sequential and the response id is unused.
	payload, err := json.Marshal(map[string]any{
		"method": method, "params": params, "id": 1,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, d.baseURL+delugeRPCPath, bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range d.cookies {
		req.AddCookie(c)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if cs := resp.Cookies(); len(cs) > 0 {
		d.cookies = cs
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}
	var out delugeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	if out.Error != nil {
		return nil, out.Error
	}
	return out.Result, nil
}

func (d *Deluge) login(ctx context.Context) error {
	raw, err := d.rpc(ctx, "auth.login", d.password)
	if err != nil {
		return err
	}
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	if !ok {
		return fmt.Errorf("%w: deluge rejected password", ErrUnauthorized)
	}
	return nil
}

// ensureConnected attaches the Web UI to a daemon if it isn't already. A fresh
// Web UI session may not be connected; streamline connects to the first host.
func (d *Deluge) ensureConnected(ctx context.Context) error {
	raw, err := d.rpc(ctx, "web.connected")
	if err != nil {
		return err
	}
	var connected bool
	if err := json.Unmarshal(raw, &connected); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	if connected {
		return nil
	}
	hraw, err := d.rpc(ctx, "web.get_hosts")
	if err != nil {
		return err
	}
	// web.get_hosts → [[host_id, host, port, status], ...]
	var hosts [][]any
	if err := json.Unmarshal(hraw, &hosts); err != nil {
		return fmt.Errorf("%w: %w", ErrBadResponse, err)
	}
	if len(hosts) == 0 || len(hosts[0]) == 0 {
		return fmt.Errorf("%w: no deluge daemon hosts", ErrUnexpectedStatus)
	}
	hostID, ok := hosts[0][0].(string)
	if !ok {
		return fmt.Errorf("%w: malformed deluge host id", ErrBadResponse)
	}
	_, err = d.rpc(ctx, "web.connect", hostID)
	return err
}

func (d *Deluge) ensureSession(ctx context.Context) error {
	if len(d.cookies) == 0 {
		if err := d.login(ctx); err != nil {
			return err
		}
	}
	if !d.connected {
		if err := d.ensureConnected(ctx); err != nil {
			return err
		}
		d.connected = true
	}
	return nil
}

// call wraps rpc with session bootstrap and a single re-auth retry when the
// daemon reports the session expired.
func (d *Deluge) call(
	ctx context.Context,
	method string,
	params ...any,
) (json.RawMessage, error) {
	if err := d.ensureSession(ctx); err != nil {
		return nil, err
	}
	raw, err := d.rpc(ctx, method, params...)
	if err != nil {
		var de *delugeError
		if errors.As(err, &de) &&
			strings.Contains(strings.ToLower(de.Message), "not authenticated") {
			d.cookies, d.connected = nil, false
			if serr := d.ensureSession(ctx); serr != nil {
				return nil, serr
			}
			return d.rpc(ctx, method, params...)
		}
		return nil, err
	}
	return raw, nil
}

func (d *Deluge) AddTorrent(
	ctx context.Context,
	src TorrentSource,
) (string, error) {
	if len(src.Bytes) == 0 && src.Magnet == "" {
		return "", fmt.Errorf("deluge add: empty torrent source")
	}
	var (
		raw json.RawMessage
		err error
	)
	if src.Magnet != "" {
		raw, err = d.call(
			ctx,
			"core.add_torrent_magnet",
			src.Magnet,
			map[string]any{},
		)
	} else {
		dump := base64.StdEncoding.EncodeToString(src.Bytes)
		raw, err = d.call(
			ctx,
			"core.add_torrent_file",
			"release.torrent",
			dump,
			map[string]any{},
		)
	}
	if err != nil {
		var de *delugeError
		if errors.As(err, &de) &&
			strings.Contains(strings.ToLower(de.Message), "already") {
			return "", ErrTorrentAlreadyExists
		}
		return "", fmt.Errorf("deluge add: %w", err)
	}
	var hash string
	if uerr := json.Unmarshal(raw, &hash); uerr != nil || hash == "" {
		return "", fmt.Errorf("deluge add: no hash returned")
	}
	return strings.ToLower(hash), nil
}

// delugeKeys are the get_torrent_status fields streamline reads.
var delugeKeys = []string{
	"name", "progress", "total_size", "save_path",
	"download_payload_rate", "eta", "state", "is_finished",
}

type delugeTorrent struct {
	Name                string  `json:"name"`
	Progress            float64 `json:"progress"` // 0-100
	TotalSize           int64   `json:"total_size"`
	SavePath            string  `json:"save_path"`
	DownloadPayloadRate float64 `json:"download_payload_rate"`
	Eta                 float64 `json:"eta"`
	State               string  `json:"state"`
	IsFinished          bool    `json:"is_finished"`
}

func (st delugeTorrent) toTorrent(hash string) Torrent {
	return Torrent{
		Hash:          hash,
		Name:          st.Name,
		Status:        mapDelugeState(st.State, st.IsFinished),
		Progress:      st.Progress / 100,
		Size:          st.TotalSize,
		SavePath:      st.SavePath,
		DownloadSpeed: int64(st.DownloadPayloadRate),
		ETA:           max(int64(st.Eta), 0),
	}
}

func (d *Deluge) GetTorrent(
	ctx context.Context,
	hash string,
) (*Torrent, error) {
	raw, err := d.call(ctx, "core.get_torrent_status", hash, delugeKeys)
	if err != nil {
		return nil, fmt.Errorf("deluge status: %w", err)
	}
	var st delugeTorrent
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("deluge status: %w: %w", ErrBadResponse, err)
	}
	// Deluge returns an empty object {} for an unknown hash.
	if st.Name == "" && st.State == "" {
		return nil, fmt.Errorf("%w: %s", ErrTorrentNotFound, hash)
	}
	t := st.toTorrent(hash)
	return &t, nil
}

func (d *Deluge) ListTorrents(ctx context.Context) ([]Torrent, error) {
	raw, err := d.call(ctx, "core.get_torrents_status", map[string]any{}, delugeKeys)
	if err != nil {
		return nil, fmt.Errorf("deluge list: %w", err)
	}
	var m map[string]delugeTorrent
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("deluge list: %w: %w", ErrBadResponse, err)
	}
	torrents := make([]Torrent, 0, len(m))
	for hash, st := range m {
		torrents = append(torrents, st.toTorrent(hash))
	}
	return torrents, nil
}

func (d *Deluge) RemoveTorrent(
	ctx context.Context,
	hash string,
	deleteFiles bool,
) error {
	if _, err := d.call(ctx, "core.remove_torrent", hash, deleteFiles); err != nil {
		return fmt.Errorf("deluge remove: %w", err)
	}
	return nil
}

// pause/resume pass a single-element list: Deluge 1.x requires a list and
// 2.x normalizes a non-list to one, so the list form works on both.
func (d *Deluge) PauseTorrent(ctx context.Context, hash string) error {
	if _, err := d.call(ctx, "core.pause_torrent", []string{hash}); err != nil {
		return fmt.Errorf("deluge pause: %w", err)
	}
	return nil
}

func (d *Deluge) ResumeTorrent(ctx context.Context, hash string) error {
	if _, err := d.call(ctx, "core.resume_torrent", []string{hash}); err != nil {
		return fmt.Errorf("deluge resume: %w", err)
	}
	return nil
}

func (d *Deluge) TestConnection(ctx context.Context) error {
	return d.ensureSession(ctx)
}

// mapDelugeState maps a Deluge state string to a TorrentStatus. A finished
// torrent that is paused (seeding disabled) reports Completed so the monitor
// still imports it.
func mapDelugeState(state string, isFinished bool) TorrentStatus {
	switch state {
	case "Seeding":
		return StatusSeeding
	case "Paused":
		if isFinished {
			return StatusCompleted
		}
		return StatusPaused
	case "Error":
		return StatusError
	default: // Downloading, Checking, Allocating, Queued, Moving
		return StatusDownloading
	}
}
