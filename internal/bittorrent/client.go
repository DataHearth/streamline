package bittorrent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/types"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/otelx"
)

var _ download.Client = (*Engine)(nil)

// specFromSource builds a torrent spec plus the persistable source fields.
// Exactly one of Magnet/Bytes must be set (mirrors download.TorrentSource).
func specFromSource(
	src download.TorrentSource,
) (*antorrent.TorrentSpec, string, []byte, error) {
	switch {
	case src.Magnet != "":
		spec, err := antorrent.TorrentSpecFromMagnetUri(src.Magnet)
		if err != nil {
			return nil, "", nil, fmt.Errorf("parse magnet: %w", err)
		}
		return spec, src.Magnet, nil, nil
	case len(src.Bytes) > 0:
		// The same ceiling download.Manager applies to a .torrent it fetches
		// from an indexer. Bytes arriving through the API skip that path, so
		// without this their only bound is the transport body cap, which is
		// sized in base64 and so lands a couple of MiB higher.
		if len(src.Bytes) > download.MaxTorrentFileSize {
			return nil, "", nil, fmt.Errorf(
				"torrent file is %d bytes, over the %d byte cap",
				len(src.Bytes), download.MaxTorrentFileSize,
			)
		}
		mi, err := metainfo.Load(bytes.NewReader(src.Bytes))
		if err != nil {
			return nil, "", nil, fmt.Errorf("parse torrent file: %w", err)
		}
		spec, err := antorrent.TorrentSpecFromMetaInfoErr(mi)
		if err != nil {
			return nil, "", nil, fmt.Errorf("build torrent spec: %w", err)
		}
		return spec, "", src.Bytes, nil
	default:
		return nil, "", nil, errors.New(
			"torrent source has neither bytes nor magnet",
		)
	}
}

// AddTorrent persists the session row first (boot re-add is idempotent by
// infohash), then hands the spec to the engine.
func (e *Engine) AddTorrent(
	ctx context.Context,
	src download.TorrentSource,
) (string, error) {
	ctx, span := tracer.Start(ctx, "bittorrent.add_torrent")
	defer span.End()

	spec, sourceMagnet, sourceBytes, err := specFromSource(src)
	if err != nil {
		return "", otelx.RecordSpanError(span, err)
	}
	hash := spec.InfoHash.HexString()

	// Decided at add time, before AddTorrentSpec below can ever start pulling
	// pieces (spec §4.4 builtin). A selective magnet has no keep-set yet —
	// "pending" bumps nothing (applyFilePriorities) until the selection pass
	// or a SetWantedFiles confirmation resolves it, so metadata fetches but
	// zero data pieces are requested. A bytes source already carrying
	// WantedFiles (Flow A) records "explicit" here instead of relying on the
	// manager's post-add SetWantedFiles confirmation, which would otherwise
	// let a moment of "all" priorities land first — a bytes source has its
	// info immediately, so startWhenReady's default-priority pass can run
	// before that confirmation call ever reaches the engine.
	var selectionMode string
	var wantedFiles []int
	switch {
	case src.Selective && sourceMagnet != "":
		selectionMode = "pending"
	case src.WantedFiles != nil:
		selectionMode = "explicit"
		wantedFiles = src.WantedFiles
	}

	if _, err := e.store.CreateTorrentSession(ctx, db.CreateTorrentSessionParams{
		InfoHash:      hash,
		Name:          spec.DisplayName,
		SavePath:      e.downloadDir,
		SourceMagnet:  sourceMagnet,
		SourceTorrent: sourceBytes,
		SelectionMode: selectionMode,
		WantedFiles:   wantedFiles,
	}); err != nil && !ent.IsConstraintError(err) {
		return "", otelx.RecordSpanError(
			span, fmt.Errorf("persist torrent session: %w", err),
		)
	}
	t, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return "", otelx.RecordSpanError(span, fmt.Errorf("add torrent: %w", err))
	}
	// Gated on freshness like addedAt: a duplicate-hash add (the
	// swallowed-constraint-error path above, reached when a completed,
	// non-widen-eligible record falls through to a normal grab on the same
	// hash) must never let this call's selection overwrite what the first
	// add already persisted — CreateTorrentSession is create-only, so
	// applying it unconditionally here would diverge memory from the DB row
	// in both directions: bumping an already-resolved torrent's skipped
	// files back to Normal now, and reverting to the stale row on restart.
	e.setState(hash, func(st *torrentState) {
		if st.addedAt.IsZero() {
			st.addedAt = time.Now()
			st.selectionMode = selectionMode
			st.wantedFiles = wantedFiles
		}
	})
	e.startWhenReady(t, false)
	return hash, nil
}

func (e *Engine) GetTorrent(
	ctx context.Context,
	hash string,
) (*download.Torrent, error) {
	t, err := e.torrent(hash)
	if err != nil {
		return nil, err
	}
	v := e.view(t)
	return &v, nil
}

func (e *Engine) ListTorrents(ctx context.Context) ([]download.Torrent, error) {
	live := e.client.Torrents()
	out := make([]download.Torrent, 0, len(live))
	for _, t := range live {
		out = append(out, e.view(t))
	}
	return out, nil
}

func (e *Engine) RemoveTorrent(
	ctx context.Context,
	hash string,
	deleteFiles bool,
) error {
	ctx, span := tracer.Start(ctx, "bittorrent.remove_torrent")
	defer span.End()

	t, err := e.torrent(hash)
	if err != nil {
		return otelx.RecordSpanError(span, err)
	}
	var contentPath string
	if info := t.Info(); info != nil {
		// Guard against deleting the whole download dir when the torrent
		// has no usable name.
		if name := info.BestName(); name != "" && name != "." && name != ".." {
			contentPath = filepath.Join(e.downloadDir, name)
		}
	}
	t.Drop()
	if deleteFiles && contentPath != "" {
		// An incomplete single-file torrent stores its partial data at
		// "<name>.part" (anacrolix UsePartFiles), a sibling of contentPath that
		// os.RemoveAll(contentPath) would miss. Remove both; the .part path is a
		// harmless no-op for a completed or multi-file torrent.
		for _, p := range []string{contentPath, contentPath + ".part"} {
			if err := os.RemoveAll(p); err != nil {
				return otelx.RecordSpanError(
					span, fmt.Errorf("delete torrent data: %w", err),
				)
			}
		}
	}
	if err := e.store.DeleteTorrentSessionByHash(ctx, hash); err != nil {
		return otelx.RecordSpanError(
			span, fmt.Errorf("delete torrent session: %w", err),
		)
	}
	e.mu.Lock()
	delete(e.state, hash)
	delete(e.sample, hash)
	e.mu.Unlock()
	return nil
}

func (e *Engine) PauseTorrent(ctx context.Context, hash string) error {
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	e.setState(hash, func(st *torrentState) { st.paused = true })
	return e.store.SetTorrentSessionPaused(ctx, hash, true)
}

func (e *Engine) ResumeTorrent(ctx context.Context, hash string) error {
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	st := e.getState(hash)
	t.AllowDataDownload()
	if !st.seedStopped {
		t.AllowDataUpload()
	}
	// Mode "pending" bumps no file at all, so resuming it without this would
	// leave every piece at PiecePriorityNone forever — the torrent runs and
	// asks for nothing. Resume is therefore the give-up exit from a pending
	// selection: take it whole. Both callers want exactly that — the selection
	// pass's finalizePending arms (grace expired, unsupported, everything
	// matched) and a human clicking resume on the Torrents page.
	//
	// Persist before touching priorities: metadata may not have resolved yet,
	// in which case startWhenReady's pass is what applies mode "all", and it
	// reads the state this sets.
	if st.selectionMode == "pending" {
		if err := e.store.SetTorrentSessionSelection(
			ctx, hash, "all", nil,
		); err != nil {
			return fmt.Errorf("persist torrent session selection: %w", err)
		}
		e.setState(hash, func(s *torrentState) {
			s.selectionMode = "all"
			s.wantedFiles = nil
		})
		if t.Info() != nil {
			applyFilePriorities(t, "all", nil)
		}
	}
	e.setState(hash, func(st *torrentState) { st.paused = false })
	return e.store.SetTorrentSessionPaused(ctx, hash, false)
}

// TestConnection is a no-op: a constructed engine is by definition running.
func (e *Engine) TestConnection(ctx context.Context) error { return nil }

// ListFiles returns the torrent's files, empty (nil error) when metadata
// hasn't resolved yet — the same "not an error" shape GetTorrent's
// StatusFetching reports for the same condition.
func (e *Engine) ListFiles(
	ctx context.Context,
	hash string,
) ([]download.TorrentFile, error) {
	t, err := e.torrent(hash)
	if err != nil {
		return nil, err
	}
	files := []download.TorrentFile{}
	if t.Info() != nil {
		for i, f := range t.Files() {
			files = append(files, download.TorrentFile{
				Index:  i,
				Path:   f.DisplayPath(),
				Size:   f.Length(),
				Wanted: f.Priority() != types.PiecePriorityNone,
			})
		}
	}
	return files, nil
}

// SetWantedFiles applies an explicit keep-set and persists it so a restart's
// metadata re-resolve (startWhenReady) reproduces the same skip instead of
// reverting to mode "all". Skip-everything is refused up front — a selective
// record never wants zero files — before any state or priority is touched,
// matching the Transmission/Deluge/qBittorrent behavior.
func (e *Engine) SetWantedFiles(
	ctx context.Context,
	hash string,
	wanted []int,
) error {
	if len(wanted) == 0 {
		return errors.New(
			"builtin set wanted files: refusing to skip every file",
		)
	}
	t, err := e.torrent(hash)
	if err != nil {
		return err
	}
	if t.Info() == nil {
		return errors.New("torrent metadata not yet available")
	}
	st := e.getState(hash)
	newlyWanted := newlyWantedIndexes(st.selectionMode, st.wantedFiles, wanted)

	applyFilePriorities(t, "explicit", wanted)
	if err := e.store.SetTorrentSessionSelection(
		ctx, hash, "explicit", wanted,
	); err != nil {
		return fmt.Errorf("persist torrent session selection: %w", err)
	}
	e.setState(hash, func(s *torrentState) {
		s.selectionMode = "explicit"
		s.wantedFiles = wanted
	})

	// A widened selection (spec §4.6: re-grabbing the same hash for more
	// episodes) can hand back files that were never downloaded while this
	// torrent was seed-stopped. Re-arm it, and reset completed_at — left
	// stale, it would make the seed-time limit re-stop the torrent the
	// instant the new files finish, since enforceOnce never re-stamps a
	// completedAt that isn't already zero.
	if st.seedStopped && filesHaveMissingBytes(t, newlyWanted) {
		t.AllowDataUpload()
		if err := e.store.SetTorrentSessionCompleted(
			ctx, hash, time.Time{},
		); err != nil {
			slog.WarnContext(ctx, "resetting torrent completion failed",
				"info_hash", hash, "error", err)
		}
		// Persisted, not just in-memory: restore() re-applies whatever
		// seed_stopped last saved, and a restart landing between this call and
		// the newly-wanted files completing would otherwise re-stamp the stale
		// "true" and strand the torrent seed-stopped with no enforcer pass left
		// to re-check it (enforceOnce skips its bookkeeping entirely while
		// wantedMissing(t) != 0).
		if err := e.store.SetTorrentSessionSeedStopped(
			ctx, hash, false,
		); err != nil {
			slog.WarnContext(ctx, "persisting seed re-arm failed",
				"info_hash", hash, "error", err)
		}
		e.setState(hash, func(s *torrentState) {
			s.seedStopped = false
			s.completedAt = time.Time{}
		})
	}
	return nil
}

// newlyWantedIndexes reports which of wanted were not already wanted under
// the previous selection. Only mode "explicit" recorded a keep-set to diff
// against; "all"/"pending" (or a session predating selection) already cover
// every index, so nothing in a narrower wanted list can be "new".
func newlyWantedIndexes(prevMode string, prevWanted, wanted []int) []int {
	if prevMode != "explicit" {
		return nil
	}
	prev := make(map[int]struct{}, len(prevWanted))
	for _, i := range prevWanted {
		prev[i] = struct{}{}
	}
	var out []int
	for _, i := range wanted {
		if _, ok := prev[i]; !ok {
			out = append(out, i)
		}
	}
	return out
}

// filesHaveMissingBytes reports whether any of indexes still has undownloaded
// data, i.e. it needs the torrent uploading-disallowed state re-armed.
func filesHaveMissingBytes(t *antorrent.Torrent, indexes []int) bool {
	files := t.Files()
	for _, i := range indexes {
		if i < 0 || i >= len(files) {
			continue
		}
		if files[i].BytesCompleted() < files[i].Length() {
			return true
		}
	}
	return false
}

// SetListenPort moves the engine's peer sockets to port.
//
// It exists for a VPN forwarded port, which rotates on every tunnel
// reconnect and is authored by the tunnel rather than by config — so this
// deliberately persists nothing. A restart falls back to
// torrent_listen_port from the environment, which is where the value comes
// from in the first place.
//
// The sockets move immediately, but announceRequest reads the port live per
// request, so trackers only learn the new port at the *next* scheduled
// announce. There is no public forced-announce for regular trackers in this
// version of anacrolix (ModifyTrackers used to force one; it no longer does
// — torrentRegularTrackerAnnouncer.Stop is now a no-op, and both
// modifyTrackers and the client-level dispatcher key their state by
// infohash, so restarting an announcer that's already running is a no-op
// too). On a long tracker interval this leaves a real window where the
// tracker still advertises the old port and inbound connections to it fail.
func (e *Engine) SetListenPort(ctx context.Context, port uint16) error {
	ctx, span := tracer.Start(ctx, "bittorrent.set_listen_port")
	defer span.End()

	if port == 0 {
		return otelx.RecordSpanError(
			span,
			errors.New("listen port must be non-zero"),
		)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// The question is whether the sockets being moved are already on port,
	// not whether the client thinks it's listening there — a bare *Engine{}
	// built for teardown/tests has no client at all.
	listenerPortRaw, ok := portOf(e.listener.Addr())
	if !ok {
		return otelx.RecordSpanError(span, fmt.Errorf(
			"tcp listener address %v is not a TCP/UDP address", e.listener.Addr(),
		))
	}
	packetConnPortRaw, ok := portOf(e.packetConn.LocalAddr())
	if !ok {
		return otelx.RecordSpanError(span, fmt.Errorf(
			"packet conn address %v is not a TCP/UDP address",
			e.packetConn.LocalAddr(),
		))
	}
	listenerPort := uint16(listenerPortRaw)
	packetConnPort := uint16(packetConnPortRaw)
	if listenerPort == port && packetConnPort == port {
		return nil
	}

	// Rebinding a socket already on port would open a second listener on an
	// address the first one still holds and fail with "already in use" —
	// each socket only rebinds when it isn't already where it needs to be,
	// rather than the pair moving as an all-or-nothing unit.
	if listenerPort != port {
		if err := e.listener.rebind(port); err != nil {
			return otelx.RecordSpanError(
				span,
				fmt.Errorf("rebind tcp listener: %w", err),
			)
		}
	}
	if packetConnPort != port {
		if err := e.packetConn.rebind(port); err != nil {
			// The listener move above already landed (or wasn't needed), but
			// the packet conn didn't: announces still carry packetConnPort —
			// that's the port every tracker learns, not the listener's (see
			// newPeerSockets) — while inbound TCP now only answers on the new
			// port, so peers can reach neither. This does not "converge on
			// retry": the only caller is gluetun's
			// VPN_PORT_FORWARDING_UP_COMMAND, fired once per rotation with no
			// retry of its own, so the mismatch persists until the next
			// rotation or a restart.
			// listener.Addr() is re-read rather than reusing listenerPort:
			// the listener may have just been rebound above, and this must
			// report where it actually ended up.
			currentTCPPort, _ := portOf(e.listener.Addr())
			//nolint:sloglint // LogAttrs takes slog.Attr by API design
			slog.LogAttrs(
				ctx,
				observability.LevelCritical,
				"torrent listen port partially moved: tcp listener and packet conn disagree",
				slog.Int("tcp_port", currentTCPPort),
				slog.Int("packet_conn_port", int(packetConnPort)),
			)
			return otelx.RecordSpanError(
				span,
				fmt.Errorf("rebind packet conn: %w", err),
			)
		}
	}

	slog.InfoContext(ctx, "torrent listen port moved", "port", port)
	return nil
}

// portOf reads the port off a net.Addr as returned by a rebindableListener
// or rebindablePacketConn — always a *net.TCPAddr or *net.UDPAddr. The bool
// is false for any other type; returning it explicitly (rather than a -1
// sentinel a uint16 cast would silently wrap to 65535) is what makes an
// unexpected address type a reported error instead of an indistinguishable
// "already on port 65535".
func portOf(addr net.Addr) (int, bool) {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.Port, true
	case *net.UDPAddr:
		return a.Port, true
	default:
		return 0, false
	}
}

// liveStats is one consistent snapshot of a torrent's transfer state,
// shared by the download.Client view and the /torrents management views so
// the rate sampler is hit exactly once per observation.
type liveStats struct {
	hash           string
	name           string
	status         download.TorrentStatus
	progress       float64
	size           int64
	downloadSpeed  int64
	uploadSpeed    int64
	uploaded       int64
	eta            int64
	seeds          int
	activePeers    int
	knownPeers     int
	addedAt        time.Time
	seedingStopped bool
}

func (e *Engine) live(t *antorrent.Torrent) liveStats {
	hash := t.InfoHash().HexString()
	var size, completed int64
	if t.Info() != nil {
		size = wantedBytes(t)
		completed = wantedCompleted(t)
	}
	var progress float64
	if size > 0 {
		progress = float64(completed) / float64(size)
	}
	stats := t.Stats()
	// This process's counter only — rates works on deltas, and folding a
	// constant base into it would change nothing but the first sample.
	sessionUp := stats.BytesWrittenData.Int64()
	down, up := e.rates(hash, completed, sessionUp)
	var eta int64
	if down > 0 && size > completed {
		eta = (size - completed) / down
	}
	st := e.getState(hash)
	return liveStats{
		hash:           hash,
		name:           t.Name(),
		status:         e.status(t, hash),
		progress:       progress,
		size:           size,
		downloadSpeed:  down,
		uploadSpeed:    up,
		uploaded:       st.uploadedBase + sessionUp,
		eta:            eta,
		seeds:          stats.ConnectedSeeders,
		activePeers:    stats.ActivePeers,
		knownPeers:     stats.TotalPeers,
		addedAt:        st.addedAt,
		seedingStopped: st.seedStopped,
	}
}

// view builds the download.Client-facing snapshot of one torrent.
func (e *Engine) view(t *antorrent.Torrent) download.Torrent {
	l := e.live(t)
	return download.Torrent{
		Hash:          l.hash,
		Name:          l.name,
		Status:        l.status,
		Progress:      l.progress,
		Size:          l.size,
		SavePath:      e.downloadDir,
		DownloadSpeed: l.downloadSpeed,
		ETA:           l.eta,
	}
}

func (e *Engine) status(
	t *antorrent.Torrent,
	hash string,
) download.TorrentStatus {
	st := e.getState(hash)
	if st.paused {
		return download.StatusPaused
	}
	if t.Info() == nil {
		return download.StatusFetching
	}
	// A pending selection wants no file yet, so wantedBytes — and with it
	// wantedMissing — is 0 and the completed branch below would report the
	// torrent as seeding the instant its metadata resolved, which the download
	// monitor reads as "ready to import". It is still fetching, just fetching
	// the answer to which files it wants rather than the metainfo.
	if st.selectionMode == "pending" {
		return download.StatusFetching
	}
	if wantedMissing(t) == 0 {
		if st.seedStopped {
			return download.StatusCompleted
		}
		return download.StatusSeeding
	}
	// Downloading but with nothing to download from: no data can move, so
	// surface it as stalled rather than a healthy-looking "downloading".
	if t.Stats().ActivePeers == 0 {
		return download.StatusStalled
	}
	return download.StatusDownloading
}

// rates derives download/upload bytes/sec from the deltas since the
// previous observation of this torrent. First observation (or byte
// regression after a failed piece check) reports 0.
func (e *Engine) rates(hash string, completed, uploaded int64) (int64, int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	prev, ok := e.sample[hash]
	now := time.Now()
	e.sample[hash] = speedSample{down: completed, up: uploaded, at: now}
	elapsed := now.Sub(prev.at).Seconds()
	if !ok || elapsed <= 0 {
		return 0, 0
	}
	var down, up int64
	if completed >= prev.down {
		down = int64(float64(completed-prev.down) / elapsed)
	}
	if uploaded >= prev.up {
		up = int64(float64(uploaded-prev.up) / elapsed)
	}
	return down, up
}
