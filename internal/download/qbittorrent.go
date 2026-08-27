package download

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/datahearth/streamline/internal/otelx"
)

type qbAuthMode uint8

const (
	qbAuthPassword qbAuthMode = iota
	qbAuthAPIKey
)

// managedCategory tags every torrent streamline adds so streamline only
// ever sees/acts on its own torrents, never the operator's personal ones
// in the same client.
const managedCategory = "streamline"

type QBittorrent struct {
	baseURL string
	mode    qbAuthMode

	username string
	password string
	sid      *http.Cookie

	apiKey string

	client *http.Client
}

func NewQBittorrentPassword(baseURL, username, password string) *QBittorrent {
	return &QBittorrent{
		baseURL:  strings.TrimRight(baseURL, "/"),
		mode:     qbAuthPassword,
		username: username,
		password: password,
		client:   otelx.HTTPClient,
	}
}

func NewQBittorrentAPIKey(baseURL, apiKey string) *QBittorrent {
	return &QBittorrent{
		baseURL: strings.TrimRight(baseURL, "/"),
		mode:    qbAuthAPIKey,
		apiKey:  apiKey,
		client:  otelx.HTTPClient,
	}
}

func (q *QBittorrent) login(ctx context.Context) error {
	form := url.Values{
		"username": {q.username},
		"password": {q.password},
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		q.baseURL+"/api/v2/auth/login",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", q.baseURL)

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf(
			"%w: IP banned due to too many failed login attempts",
			ErrUnauthorized,
		)
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent:
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" ||
			strings.HasPrefix(cookie.Name, "QBT_SID_") {
			q.sid = cookie
			return nil
		}
	}

	// qBittorrent returns 200 with body "Fails." on bad credentials —
	// no SID cookie is set, so distinguish from other malformed responses
	// by treating a missing SID as an auth failure.
	return fmt.Errorf("%w: no SID cookie in login response", ErrUnauthorized)
}

func (q *QBittorrent) doRequest(
	ctx context.Context,
	method, path string,
	form url.Values,
) (*http.Response, error) {
	if q.mode == qbAuthPassword && q.sid == nil {
		if err := q.login(ctx); err != nil {
			return nil, err
		}
	}

	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}

	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Referer", q.baseURL)

	switch q.mode {
	case qbAuthAPIKey:
		req.Header.Set("Authorization", "Bearer "+q.apiKey)
	default:
		req.AddCookie(q.sid)
	}

	resp, err := q.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusForbidden && q.mode == qbAuthPassword {
		resp.Body.Close()
		q.sid = nil
		if err := q.login(ctx); err != nil {
			return nil, err
		}
		return q.doRequest(ctx, method, path, form)
	}

	return resp, nil
}

// ensureCategory provisions managedCategory in qBittorrent. qBittorrent
// does not reliably auto-create a category on add, so streamline creates
// it explicitly; an already-existing category (409) is success.
func (q *QBittorrent) ensureCategory(ctx context.Context) error {
	form := url.Values{"category": {managedCategory}}
	resp, err := q.doRequest(
		ctx, http.MethodPost, "/api/v2/torrents/createCategory", form,
	)
	if err != nil {
		return fmt.Errorf("qbittorrent createCategory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict ||
		(resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf(
		"qbittorrent createCategory: unexpected status %d", resp.StatusCode,
	)
}

// qbAddEnvelope is the JSON body qBittorrent 5.x returns from
// /api/v2/torrents/add. For a multipart upload of a real .torrent file the
// daemon parses it synchronously and reports the resulting infohash in
// AddedTorrentIDs. Older qBittorrent versions return plain "Ok." with no
// body — handled via the magnet btih fallback.
type qbAddEnvelope struct {
	AddedTorrentIDs []string `json:"added_torrent_ids"`
	SuccessCount    int      `json:"success_count"`
}

// AddTorrent uploads the source to qBittorrent and returns the infohash.
// .torrent bytes are sent as a multipart file part so qBittorrent never has
// to reach the indexer itself — this is what unblocks deployments where the
// client lives in a VPN/Docker network the indexer is not on. The save path
// is intentionally left to qBittorrent's own configuration; streamline only
// needs to know where to read completed downloads from (library.download_path).
func (q *QBittorrent) AddTorrent(
	ctx context.Context,
	src TorrentSource,
) (string, error) {
	if len(src.Bytes) == 0 && src.Magnet == "" {
		return "", fmt.Errorf("qbittorrent add: empty torrent source")
	}
	if err := q.ensureCategory(ctx); err != nil {
		return "", err
	}

	buildBody := func() (*bytes.Buffer, string, error) {
		buf := &bytes.Buffer{}
		mw := multipart.NewWriter(buf)
		if err := mw.WriteField("category", managedCategory); err != nil {
			return nil, "", err
		}
		if src.Magnet != "" {
			if err := mw.WriteField("urls", src.Magnet); err != nil {
				return nil, "", err
			}
		}
		if len(src.Bytes) > 0 {
			fw, err := mw.CreateFormFile("torrents", "release.torrent")
			if err != nil {
				return nil, "", err
			}
			if _, err := fw.Write(src.Bytes); err != nil {
				return nil, "", err
			}
		}
		if len(src.WantedFiles) > 0 && len(src.Bytes) > 0 {
			priorities, err := filePrioritiesField(src.Bytes, src.WantedFiles)
			if err != nil {
				return nil, "", err
			}
			if err := mw.WriteField("filePriorities", priorities); err != nil {
				return nil, "", err
			}
		}
		if err := mw.Close(); err != nil {
			return nil, "", err
		}
		return buf, mw.FormDataContentType(), nil
	}

	doOnce := func() (*http.Response, error) {
		if q.mode == qbAuthPassword && q.sid == nil {
			if err := q.login(ctx); err != nil {
				return nil, err
			}
		}
		body, contentType, err := buildBody()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(
			ctx, http.MethodPost, q.baseURL+"/api/v2/torrents/add", body,
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Referer", q.baseURL)
		switch q.mode {
		case qbAuthAPIKey:
			req.Header.Set("Authorization", "Bearer "+q.apiKey)
		default:
			req.AddCookie(q.sid)
		}
		return q.client.Do(req)
	}

	resp, err := doOnce()
	if err != nil {
		return "", fmt.Errorf("qbittorrent add: %w", err)
	}
	if resp.StatusCode == http.StatusForbidden && q.mode == qbAuthPassword {
		resp.Body.Close()
		q.sid = nil
		if lerr := q.login(ctx); lerr != nil {
			return "", lerr
		}
		resp, err = doOnce()
		if err != nil {
			return "", fmt.Errorf("qbittorrent add: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnsupportedMediaType {
		return "", fmt.Errorf("qbittorrent: invalid torrent")
	}
	if resp.StatusCode == http.StatusConflict {
		return "", ErrTorrentAlreadyExists
	}
	// qBittorrent 5.x replies 202 when the upload is still being processed
	// (the daemon hasn't finished parsing yet). 200 is the synchronous case.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf(
			"qbittorrent add: unexpected status %d",
			resp.StatusCode,
		)
	}

	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var env qbAddEnvelope
		if jerr := json.Unmarshal(body, &env); jerr == nil &&
			len(env.AddedTorrentIDs) > 0 {
			hash := strings.ToLower(env.AddedTorrentIDs[0])
			slog.DebugContext(ctx,
				"qbittorrent torrent added",
				"hash", hash,
				"success_count", env.SuccessCount,
			)
			return hash, nil
		}
	}
	if src.Magnet != "" {
		if h := extractBtihFromMagnet(src.Magnet); h != "" {
			slog.DebugContext(ctx,
				"qbittorrent torrent added (magnet btih fallback)",
				"hash", h,
			)
			return h, nil
		}
	}
	// qBittorrent before WebAPI 2.15 (≤5.0.x) answers a bare "Ok." with no
	// envelope, so for .torrent uploads the infohash is derived from the same
	// bytes that were just sent. Guarded on a parseable info dict: hashing an
	// absent one would mint a plausible-looking hash for garbage input.
	if len(src.Bytes) > 0 {
		if mi, merr := metainfo.Load(bytes.NewReader(src.Bytes)); merr == nil &&
			len(mi.InfoBytes) > 0 {
			hash := mi.HashInfoBytes().HexString()
			slog.DebugContext(ctx,
				"qbittorrent torrent added (locally derived infohash)",
				"hash", hash,
			)
			return hash, nil
		}
	}
	return "", fmt.Errorf(
		"qbittorrent add: no hash returned (success=0, body=%q)",
		string(body),
	)
}

// filePrioritiesField builds the qBittorrent add-time filePriorities value:
// one comma-joined entry per file in metainfo order, 1 (Normal) for a wanted
// index and 0 (Ignored) for everything else.
func filePrioritiesField(raw []byte, wantedFiles []int) (string, error) {
	files, err := decodeTorrentFiles(raw)
	if err != nil {
		return "", err
	}
	wanted := make(map[int]bool, len(wantedFiles))
	for _, idx := range wantedFiles {
		wanted[idx] = true
	}
	priorities := make([]string, len(files))
	for _, f := range files {
		if wanted[f.Index] {
			priorities[f.Index] = "1"
		} else {
			priorities[f.Index] = "0"
		}
	}
	return strings.Join(priorities, ","), nil
}

func (q *QBittorrent) GetTorrent(
	ctx context.Context,
	hash string,
) (*Torrent, error) {
	torrents, err := q.listWithFilter(ctx, url.Values{"hashes": {hash}})
	if err != nil {
		return nil, err
	}
	if len(torrents) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrTorrentNotFound, hash)
	}
	return &torrents[0], nil
}

func (q *QBittorrent) ListTorrents(ctx context.Context) ([]Torrent, error) {
	return q.listWithFilter(ctx, nil)
}

func (q *QBittorrent) listWithFilter(
	ctx context.Context,
	params url.Values,
) ([]Torrent, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("category", managedCategory)
	path := "/api/v2/torrents/info?" + params.Encode()

	resp, err := q.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("qbittorrent list: %w", err)
	}
	defer resp.Body.Close()

	var qbTorrents []qbTorrent
	if err := json.NewDecoder(resp.Body).Decode(&qbTorrents); err != nil {
		return nil, fmt.Errorf("qbittorrent list decode: %w", err)
	}

	torrents := make([]Torrent, 0, len(qbTorrents))
	for _, t := range qbTorrents {
		eta := t.Eta
		if eta >= qbEtaInfinity {
			eta = 0
		}
		torrents = append(torrents, Torrent{
			Hash:          t.Hash,
			Name:          t.Name,
			Status:        mapQBState(t.State),
			Progress:      t.Progress,
			Size:          t.Size,
			SavePath:      t.SavePath,
			DownloadSpeed: t.Dlspeed,
			ETA:           eta,
		})
	}

	return torrents, nil
}

func (q *QBittorrent) RemoveTorrent(
	ctx context.Context,
	hash string,
	deleteFiles bool,
) error {
	form := url.Values{
		"hashes":      {hash},
		"deleteFiles": {fmt.Sprintf("%t", deleteFiles)},
	}

	resp, err := q.doRequest(ctx, http.MethodPost, "/api/v2/torrents/delete", form)
	if err != nil {
		return fmt.Errorf("qbittorrent delete: %w", err)
	}
	defer resp.Body.Close()

	return nil
}

func (q *QBittorrent) PauseTorrent(ctx context.Context, hash string) error {
	return q.stopStart(ctx, hash, true)
}

func (q *QBittorrent) ResumeTorrent(ctx context.Context, hash string) error {
	return q.stopStart(ctx, hash, false)
}

// stopStart issues the qBittorrent 5.x stop/start verb, falling back to the
// 4.x pause/resume path when the host returns 404/405 (older qBittorrent).
func (q *QBittorrent) stopStart(
	ctx context.Context,
	hash string,
	pause bool,
) error {
	newPath, oldPath := "/api/v2/torrents/start", "/api/v2/torrents/resume"
	if pause {
		newPath, oldPath = "/api/v2/torrents/stop", "/api/v2/torrents/pause"
	}
	form := url.Values{"hashes": {hash}}

	resp, err := q.doRequest(ctx, http.MethodPost, newPath, form)
	if err != nil {
		return fmt.Errorf("qbittorrent %s: %w", newPath, err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusMethodNotAllowed {
		resp2, err := q.doRequest(ctx, http.MethodPost, oldPath, form)
		if err != nil {
			return fmt.Errorf("qbittorrent %s: %w", oldPath, err)
		}
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusOK {
			return fmt.Errorf(
				"qbittorrent %s: unexpected status %d",
				oldPath, resp2.StatusCode)
		}
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"qbittorrent %s: unexpected status %d", newPath, resp.StatusCode)
	}
	return nil
}

func (q *QBittorrent) TestConnection(ctx context.Context) error {
	switch q.mode {
	case qbAuthAPIKey:
		return q.testAPIKey(ctx)
	default:
		return q.login(ctx)
	}
}

// qbFile is one entry of qBittorrent's /api/v2/torrents/files response.
type qbFile struct {
	Index    int    `json:"index"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Priority int    `json:"priority"`
}

func (q *QBittorrent) ListFiles(
	ctx context.Context,
	hash string,
) ([]TorrentFile, error) {
	path := "/api/v2/torrents/files?hash=" + url.QueryEscape(hash)
	resp, err := q.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("qbittorrent files: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"qbittorrent files: unexpected status %d", resp.StatusCode,
		)
	}

	var qbFiles []qbFile
	if err := json.NewDecoder(resp.Body).Decode(&qbFiles); err != nil {
		return nil, fmt.Errorf("qbittorrent files decode: %w", err)
	}

	// Metadata not yet available (e.g. a magnet still resolving) answers an
	// empty array; make(..., 0, 0) already yields a non-nil empty slice, so
	// no special-casing is needed to satisfy the interface contract.
	files := make([]TorrentFile, 0, len(qbFiles))
	for _, f := range qbFiles {
		files = append(files, TorrentFile{
			Index:  f.Index,
			Path:   f.Name,
			Size:   f.Size,
			Wanted: f.Priority != 0,
		})
	}
	return files, nil
}

// SetWantedFiles makes wanted downloaded and everything else skipped.
// qBittorrent's filePrio verb only accepts an explicit id list per call, and
// this method is only handed the keep-set, so it lists the torrent's actual
// files first to compute the complement — "every other file" per the
// interface contract.
func (q *QBittorrent) SetWantedFiles(
	ctx context.Context,
	hash string,
	wanted []int,
) error {
	files, err := q.ListFiles(ctx, hash)
	if err != nil {
		return fmt.Errorf("qbittorrent set wanted files: %w", err)
	}

	keep := make(map[int]bool, len(wanted))
	for _, idx := range wanted {
		keep[idx] = true
	}
	var skipped []int
	for _, f := range files {
		if !keep[f.Index] {
			skipped = append(skipped, f.Index)
		}
	}

	if err := q.setFilePriority(ctx, hash, skipped, "0"); err != nil {
		return err
	}
	return q.setFilePriority(ctx, hash, wanted, "1")
}

// setFilePriority issues one qBittorrent filePrio call for ids, or no call at
// all when ids is empty — the no-op is what collapses SetWantedFiles to a
// single request when nothing needs skipping.
func (q *QBittorrent) setFilePriority(
	ctx context.Context,
	hash string,
	ids []int,
	priority string,
) error {
	if len(ids) == 0 {
		return nil
	}
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = strconv.Itoa(id)
	}
	form := url.Values{
		"hash":     {hash},
		"id":       {strings.Join(idStrs, "|")},
		"priority": {priority},
	}
	resp, err := q.doRequest(
		ctx, http.MethodPost, "/api/v2/torrents/filePrio", form,
	)
	if err != nil {
		return fmt.Errorf("qbittorrent filePrio: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"qbittorrent filePrio: unexpected status %d", resp.StatusCode,
		)
	}
	return nil
}

func (q *QBittorrent) testAPIKey(ctx context.Context) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		q.baseURL+"/api/v2/app/version",
		nil,
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+q.apiKey)

	resp, err := q.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("%w: status %d", ErrUnexpectedStatus, resp.StatusCode)
	}
	return nil
}

// mapQBState maps a qBittorrent state string to a TorrentStatus. The `UP`
// suffix means the torrent already finished downloading and is in its
// upload phase, so idle `UP` states report Completed (not Paused) and the
// monitor still imports them — matching transmission/deluge. Only the `DL`
// side is genuinely mid-transfer and therefore Paused.
func mapQBState(state string) TorrentStatus {
	switch state {
	case "downloading",
		"metaDL",
		"forcedDL",
		"allocating",
		"stalledDL",
		"checkingDL",
		"checkingResumeData":
		return StatusDownloading
	case "uploading", "forcedUP", "stalledUP", "checkingUP":
		return StatusSeeding
	case "pausedDL", "stoppedDL", "queuedDL":
		return StatusPaused
	case "pausedUP", "stoppedUP", "queuedUP", "moving":
		return StatusCompleted
	case "error", "missingFiles", "unknown":
		return StatusError
	default:
		return StatusError
	}
}

type qbTorrent struct {
	Hash     string  `json:"hash"`
	Name     string  `json:"name"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
	Size     int64   `json:"size"`
	SavePath string  `json:"save_path"`
	Dlspeed  int64   `json:"dlspeed"`
	Eta      int64   `json:"eta"`
}

// qbEtaInfinity is qBittorrent's "no ETA" sentinel (8640000s). Normalized
// to 0 so the UI treats 0 as "unknown".
const qbEtaInfinity = 8640000
