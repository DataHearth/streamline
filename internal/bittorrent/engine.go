// Package bittorrent embeds a BitTorrent engine (anacrolix/torrent) exposed
// as the "builtin" download client. The torrent list survives restarts via
// the torrent_sessions table; piece completion lives in a bolt file under
// the download dir, so boot re-adds never re-hash or re-download.
package bittorrent

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	analog "github.com/anacrolix/log"
	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/types"
	"go.opentelemetry.io/otel"
	"golang.org/x/time/rate"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/bittorrent")

// sessionDirName holds engine-owned state (bolt piece completion) inside
// the download dir, so wiping the download dir also resets the engine.
const sessionDirName = ".streamline-session"

// torrentState mirrors the persisted per-torrent flags for cheap access on
// every status call.
type torrentState struct {
	paused      bool
	seedStopped bool
	completedAt time.Time
}

type speedSample struct {
	bytes int64
	at    time.Time
}

// Engine wraps one anacrolix torrent client. It implements download.Client
// (the "builtin" client_type) and Manager (the /api/v1/torrents surface).
type Engine struct {
	client      *antorrent.Client
	storageImpl storage.ClientImplCloser
	store       db.Store
	downloadDir string
	seedRatio   float64
	seedTime    time.Duration
	// bindAddr is the resolved interface/IP the engine actually bound to
	// (empty = all interfaces); surfaced in the download-client read view.
	bindAddr string

	mu     sync.Mutex
	state  map[string]*torrentState
	sample map[string]speedSample

	stop chan struct{}
	wg   sync.WaitGroup
}

// New starts the engine from the enabled builtin download-client config
// entry and re-adds every persisted torrent session.
func New(ctx context.Context, store db.Store) (*Engine, error) {
	entry, ok := config.BuiltinDownloadClient()
	if !ok {
		return nil, errors.New("no enabled builtin download client configured")
	}
	var seedTime time.Duration
	if entry.SeedTime != "" {
		var err error
		seedTime, err = time.ParseDuration(entry.SeedTime)
		if err != nil {
			return nil, fmt.Errorf("parse seed_time: %w", err)
		}
	}
	// Resolve bind_interface before touching disk or the network: a bad value
	// must stop the engine rather than silently leak peer traffic elsewhere.
	bindIP, err := resolveBindIP(entry.BindInterface)
	if err != nil {
		return nil, err
	}
	sessionDir := filepath.Join(entry.DownloadDir, sessionDirName)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}
	pc, err := storage.NewBoltPieceCompletion(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("open piece completion: %w", err)
	}
	st := storage.NewFileWithCompletion(entry.DownloadDir, pc)

	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = entry.DownloadDir
	cc.DefaultStorage = st
	cc.Seed = true
	cc.NoDHT = entry.DisableDHT
	cc.Logger = analog.Default.WithFilterLevel(analog.Error)
	if entry.ListenPort != 0 {
		cc.ListenPort = int(entry.ListenPort)
	}
	if entry.MaxUploadKbps > 0 {
		cc.UploadRateLimiter = rate.NewLimiter(
			rate.Limit(entry.MaxUploadKbps*1024), 256<<10,
		)
	}
	if entry.MaxDownloadKbps > 0 {
		cc.DownloadRateLimiter = rate.NewLimiter(
			rate.Limit(entry.MaxDownloadKbps*1024), 1<<20,
		)
	}
	if bindIP != nil {
		host := bindIP.String()
		cc.ListenHost = func(string) string { return host }
		// anacrolix pins uTP/DHT dials to the listen socket, but its TCP
		// dialer is not source-bound (dialTcpFromListenPort is compiled off).
		// Drop the default socket dialers and dial only through the
		// source-bound dialer added below, so peer traffic cannot leave the
		// bound interface. Constrain listeners to the bound address family so
		// the mismatched family doesn't fail to bind.
		cc.DialForPeerConns = false
		if bindIP.To4() != nil {
			cc.DisableIPv6 = true
		} else {
			cc.DisableIPv4 = true
		}
		// Peer sockets are pinned above, but tracker announces (HTTP + UDP) and
		// webseed/metainfo HTTP egress dial through anacrolix's own dialers,
		// which are unbound by default and would leave via the host default
		// route — leaking the real IP to trackers and webseeds. Source-bind them
		// to the same interface so the binding is fail-closed.
		srcDialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}}
		cc.TrackerDialContext = srcDialer.DialContext
		cc.HTTPDialContext = srcDialer.DialContext
		cc.TrackerListenPacket = func(network, _ string) (net.PacketConn, error) {
			return net.ListenUDP(network, &net.UDPAddr{IP: bindIP})
		}
	}

	client, err := antorrent.NewClient(cc)
	if err != nil {
		if cerr := st.Close(); cerr != nil {
			slog.WarnContext(ctx, "closing torrent storage failed", "error", cerr)
		}
		return nil, fmt.Errorf("start torrent client: %w", err)
	}
	if bindIP != nil {
		network := "tcp6"
		if bindIP.To4() != nil {
			network = "tcp4"
		}
		client.AddDialer(antorrent.NetworkDialer{
			Network: network,
			Dialer:  &net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}},
		})
	}
	var bindAddr string
	if bindIP != nil {
		bindAddr = bindIP.String()
	}
	e := &Engine{
		client:      client,
		storageImpl: st,
		store:       store,
		downloadDir: entry.DownloadDir,
		seedRatio:   entry.SeedRatio,
		seedTime:    seedTime,
		bindAddr:    bindAddr,
		state:       map[string]*torrentState{},
		sample:      map[string]speedSample{},
		stop:        make(chan struct{}),
	}
	if err := e.restore(ctx); err != nil {
		if cerr := e.Close(); cerr != nil {
			slog.WarnContext(ctx, "engine close after failed restore", "error", cerr)
		}
		return nil, fmt.Errorf("restore torrent sessions: %w", err)
	}
	e.wg.Go(e.enforceSeedLimits)
	return e, nil
}

// resolveBindIP turns a bind_interface config value into the local IP the
// engine binds its listeners and dials to. Empty means "all interfaces"
// (nil, nil). A literal IP is used verbatim; otherwise the value is an
// interface name whose first usable unicast address (IPv4 preferred) is
// chosen. A missing interface, or one with no usable address, is an error so
// the engine refuses to start rather than leak traffic onto another interface.
func resolveBindIP(iface string) (net.IP, error) {
	if iface == "" {
		return nil, nil
	}
	if ip := net.ParseIP(iface); ip != nil {
		return ip, nil
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("resolve bind_interface %q: %w", iface, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, fmt.Errorf("read bind_interface %q addresses: %w", iface, err)
	}
	var v4, v6 net.IP
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.To4() != nil {
			if v4 == nil {
				v4 = ip
			}
		} else if v6 == nil {
			v6 = ip
		}
	}
	switch {
	case v4 != nil:
		return v4, nil
	case v6 != nil:
		return v6, nil
	default:
		return nil, fmt.Errorf(
			"bind_interface %q has no usable unicast address",
			iface,
		)
	}
}

// Close stops background loops and shuts the torrent client down. Resume
// state is already on disk (bolt piece completion + ent rows).
func (e *Engine) Close() error {
	close(e.stop)
	e.wg.Wait()
	errs := e.client.Close()
	// A custom DefaultStorage is not closed by client.Close (only the
	// fallback file storage is); close it explicitly.
	if err := e.storageImpl.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// restore re-adds every persisted torrent session. Unrestorable rows are
// skipped with a warning rather than failing boot.
func (e *Engine) restore(ctx context.Context) error {
	sessions, err := e.store.ListTorrentSessions(ctx)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		spec, _, _, err := specFromSource(download.TorrentSource{
			Magnet: s.SourceMagnet,
			Bytes:  s.SourceTorrent,
		})
		if err != nil {
			slog.WarnContext(ctx, "skipping unrestorable torrent session",
				"info_hash", s.InfoHash, "error", err)
			continue
		}
		t, _, err := e.client.AddTorrentSpec(spec)
		if err != nil {
			slog.WarnContext(ctx, "failed to re-add torrent",
				"info_hash", s.InfoHash, "error", err)
			continue
		}
		st := &torrentState{paused: s.Paused, seedStopped: s.SeedStopped}
		if s.CompletedAt != nil {
			st.completedAt = *s.CompletedAt
		}
		e.mu.Lock()
		e.state[s.InfoHash] = st
		e.mu.Unlock()
		if s.Paused {
			t.DisallowDataDownload()
			t.DisallowDataUpload()
		}
		// Arm the metadata watcher even for paused sessions: a magnet re-adds
		// with Info()==nil, and the default file prioritization must fire once
		// metadata resolves so a subsequent resume isn't stuck with no wanted
		// pieces. Data stays gated by the Disallow calls above until resumed.
		e.startWhenReady(t, s.SeedStopped)
	}
	return nil
}

// startWhenReady begins downloading once torrent metadata is known. Magnet
// adds resolve info asynchronously, so this must not block the caller.
func (e *Engine) startWhenReady(t *antorrent.Torrent, seedStopped bool) {
	e.wg.Go(func() {
		select {
		case <-t.GotInfo():
		case <-t.Closed(): // events.Done is <-chan struct{}
			return
		case <-e.stop:
			return
		}
		if seedStopped {
			t.DisallowDataUpload()
		}
		wantFilesByDefault(t)
		hash := t.InfoHash().HexString()
		if err := e.store.SetTorrentSessionName(
			context.Background(), hash, t.Name(),
		); err != nil {
			slog.WarnContext(context.Background(),
				"persisting resolved torrent name failed",
				"info_hash", hash, "error", err)
		}
	})
}

// wantFilesByDefault bumps every still-unprioritized file from anacrolix's
// None default to Normal. File priorities are the engine's single demand
// source — never DownloadAll, whose piece-level demand is max()-merged and
// can't be retracted by a later per-file skip — so fresh files must be
// bumped or nothing downloads. Files the user already prioritized are left
// untouched.
func wantFilesByDefault(t *antorrent.Torrent) {
	for _, f := range t.Files() {
		if f.Priority() == types.PiecePriorityNone {
			f.SetPriority(types.PiecePriorityNormal)
		}
	}
}

func (e *Engine) setState(hash string, mut func(*torrentState)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.state[hash]
	if !ok {
		st = &torrentState{}
		e.state[hash] = st
	}
	mut(st)
}

func (e *Engine) getState(hash string) torrentState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if st, ok := e.state[hash]; ok {
		return *st
	}
	return torrentState{}
}

// parseHash validates a 40-char hex infohash. Bad input maps to
// download.ErrTorrentNotFound so callers get uniform not-found semantics.
func parseHash(s string) (metainfo.Hash, error) {
	var h metainfo.Hash
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(h) {
		return h, fmt.Errorf("%w: %q", download.ErrTorrentNotFound, s)
	}
	copy(h[:], b)
	return h, nil
}

// torrent resolves a hex infohash to the live torrent.
func (e *Engine) torrent(hash string) (*antorrent.Torrent, error) {
	h, err := parseHash(hash)
	if err != nil {
		return nil, err
	}
	t, ok := e.client.Torrent(h)
	if !ok {
		return nil, fmt.Errorf("%w: %s", download.ErrTorrentNotFound, hash)
	}
	return t, nil
}
