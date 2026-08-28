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
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

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
	addedAt     time.Time
	// uploadedBase is what prior processes uploaded. anacrolix's own counter
	// starts at zero every boot, so ratio has to be read off base + counter or
	// a restart silently forgives whatever the torrent already gave back.
	uploadedBase int64
	// uploadedPersisted is the last total written to TorrentSession. The
	// minute tick would otherwise rewrite the row for every torrent whether
	// or not it moved, and a torrent nobody leeches from uploads nothing —
	// 20 idle torrents cost ~28,800 no-op commits a day through the single
	// SQLite connection every API request also queues behind.
	uploadedPersisted int64
	// selectionMode and wantedFiles mirror the persisted file-selection
	// columns, consulted by applyFilePriorities every time startWhenReady
	// runs — including the metadata-resolve re-run after a restore, so a
	// restart can't erase a skip the way the old unconditional bump did.
	selectionMode string
	wantedFiles   []int
}

type speedSample struct {
	down int64
	up   int64
	at   time.Time
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
	// listener and packetConn are the engine's own peer sockets. Owning them
	// is what makes SetListenPort possible: anacrolix can neither retire a
	// listener nor change the port it announces.
	listener   *rebindableListener
	packetConn *rebindablePacketConn

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
	st := newDrainingStorage(
		storage.NewFileWithCompletion(entry.DownloadDir, pc),
	)

	packetConn, listener, err := newPeerSockets(ctx, bindIP, entry.ListenPort)
	if err != nil {
		closeAndWarn(ctx, "torrent storage", st)
		return nil, err
	}

	cc := newClientConfig(entry, bindIP, st, packetConn)

	client, err := antorrent.NewClient(cc)
	if err != nil {
		closeAndWarn(ctx, "torrent storage", st)
		closeAndWarn(ctx, "listener after client failure", listener)
		closeAndWarn(ctx, "packet conn after client failure", packetConn)
		return nil, fmt.Errorf("start torrent client: %w", err)
	}
	client.AddListener(listener)
	var bindAddr string
	// cc.DisableTCP above stops anacrolix from creating any TCP socket of its
	// own, and cl.dialers is populated only from sockets it created — so
	// without a dialer added here, nothing ever dials TCP and every outbound
	// peer connection is uTP-only. Both branches exist to supply the TCP
	// dialer anacrolix no longer will; only the *binding* differs.
	if bindIP != nil {
		network := "tcp6"
		if bindIP.To4() != nil {
			network = "tcp4"
		}
		client.AddDialer(antorrent.NetworkDialer{
			Network: network,
			Dialer:  &net.Dialer{LocalAddr: &net.TCPAddr{IP: bindIP}},
		})
		bindAddr = bindIP.String()
	} else {
		client.AddDialer(antorrent.NetworkDialer{
			Network: "tcp4",
			Dialer:  &net.Dialer{},
		})
		client.AddDialer(antorrent.NetworkDialer{
			Network: "tcp6",
			Dialer:  &net.Dialer{},
		})
	}
	e := &Engine{
		client:      client,
		storageImpl: st,
		store:       store,
		downloadDir: entry.DownloadDir,
		seedRatio:   entry.SeedRatio,
		seedTime:    seedTime,
		bindAddr:    bindAddr,
		listener:    listener,
		packetConn:  packetConn,
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

// closeAndWarn closes a socket whose close is not the caller's outcome — a
// replacement being retired, or a partially built pair being unwound after
// some other failure. what names it in the message ("retired listener").
// Every such site logs and carries on, because the error the caller is
// already returning (or the success it is already committed to) is the one
// that matters.
func closeAndWarn(ctx context.Context, what string, c io.Closer) {
	if err := c.Close(); err != nil {
		slog.WarnContext(ctx, "closing "+what+" failed", "error", err)
	}
}

// maxPeerSocketBindAttempts bounds the retry newPeerSockets runs when the
// requested port is 0: the UDP port the OS hands out can already be taken on
// the independent TCP port space, so a collision is retried rather than
// failing the engine outright.
const maxPeerSocketBindAttempts = 5

// newPeerSockets binds the engine's UDP (uTP/DHT) and TCP (peer) sockets.
//
// Client.LocalPort reads only the first entry in cl.listeners — the uTP
// socket riding the packet conn — and that is the port every tracker
// announce carries. An inbound TCP peer dials whatever port the announce
// named, so the TCP listener has to answer on that same number or it is
// unreachable despite "working" from the engine's own point of view. A
// configured port binds both directly; port 0 asks the OS for a UDP port
// first and then binds TCP to the number it got, retrying the pair if that
// second bind loses a race for the port.
func newPeerSockets(
	ctx context.Context, bindIP net.IP, port uint16,
) (*rebindablePacketConn, *rebindableListener, error) {
	if port != 0 {
		pc, err := newRebindablePacketConn(bindIP, port)
		if err != nil {
			return nil, nil, err
		}
		ln, err := newRebindableListener(bindIP, port)
		if err != nil {
			closeAndWarn(ctx, "packet conn after listener failure", pc)
			return nil, nil, err
		}
		return pc, ln, nil
	}

	var lastErr error
	for range maxPeerSocketBindAttempts {
		pc, err := newRebindablePacketConn(bindIP, 0)
		if err != nil {
			lastErr = err
			continue
		}
		addr, ok := pc.LocalAddr().(*net.UDPAddr)
		if !ok {
			lastErr = fmt.Errorf(
				"unexpected packet conn local address type %T", pc.LocalAddr())
			closeAndWarn(ctx, "packet conn after address lookup failure", pc)
			continue
		}
		ln, err := newRebindableListener(bindIP, uint16(addr.Port))
		if err != nil {
			lastErr = err
			closeAndWarn(ctx, "packet conn after listener retry failure", pc)
			continue
		}
		return pc, ln, nil
	}
	return nil, nil, fmt.Errorf(
		"bind matching peer sockets after %d attempts: %w",
		maxPeerSocketBindAttempts, lastErr)
}

// newClientConfig builds the anacrolix client config. A nil bindIP means "all
// interfaces"; a non-nil one only half-binds the client, and New must attach
// the matching source-bound dialer for it to reach peers at all.
func newClientConfig(
	entry config.DownloadClientEntry,
	bindIP net.IP,
	st storage.ClientImpl,
	pc *rebindablePacketConn,
) *antorrent.ClientConfig {
	cc := antorrent.NewDefaultClientConfig()
	cc.DataDir = entry.DownloadDir
	cc.DefaultStorage = st
	cc.Seed = true
	cc.NoDHT = entry.DisableDHT
	// The pion WebRTC/DTLS/ICE stack anacrolix links for webtorrent carries
	// real CVEs and a plain BitTorrent-over-TCP/uTP client never needs it. No
	// config key re-enables it; the only cost is that ws:// and wss:// trackers
	// in an announce list are skipped.
	cc.DisableWebtorrent = true
	cc.Slogger = engineSlogger()
	applyMemoryBounds(cc)
	// cc.ListenPort is deliberately left alone. It only reaches
	// listenAllWithListenFunc, whose one remaining network is served by the
	// ListenPacket closure below — which hands back a conn newPeerSockets
	// already bound to entry.ListenPort and ignores the address it is asked
	// for. Setting it read as though it decided a port; it decides nothing.
	//
	// The engine registers its own rebindable TCP listener and supplies the
	// uTP/DHT packet conn, so both report a port it can move. A socket
	// anacrolix created would sit at cl.listeners[0] — the entry LocalPort
	// reads, and the port every tracker announce carries — with no way to
	// retire it: AddListener only appends.
	cc.DisableTCP = true
	if pc != nil {
		cc.ListenPacket = func(string, string) (net.PacketConn, error) {
			return pc, nil
		}
		// The engine hands anacrolix one packet conn, so the client must be told
		// about exactly one UDP network, or listenAllRetry calls ListenPacket
		// once per network and builds two utp.Socket instances racing each other
		// to read the same pc. DisableIPv6 is how; its cost is that anacrolix
		// rejects every inbound IPv6 peer and skips udp6:// trackers, while the
		// tcp6 dialer added in New still dials IPv6 peers outbound.
		// IPv4-mapped remotes keep To4() != nil, so v4 traffic is unaffected.
		if bindIP == nil {
			cc.DisableIPv6 = true
		}
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
		// Drop the default socket dialers so peer traffic can only leave
		// through the source-bound dialer New attaches to the client — this
		// config on its own leaves the client unable to dial peers at all, so
		// the two halves must stay together. Constrain listeners to the bound
		// address family so the mismatched family doesn't fail to bind.
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
		// UPnP discovery ignores every binding above: anacrolix multicasts an
		// SSDP M-SEARCH out of *each* multicast-capable interface and then asks
		// whichever IGD answers to open an inbound mapping for the host's real
		// WAN address. On a VPN-bound setup that both announces our presence on
		// the LAN and forwards a port around the tunnel, so the binding stops
		// being fail-closed. Peers reach a bound engine through the tunnel's own
		// forwarding, not the LAN router's.
		cc.NoDefaultPortForwarding = true
	}
	return cc
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
	// Snapshot before closing: client.Close empties the torrent set.
	torrents := e.client.Torrents()
	e.flushUploaded(context.Background())
	errs := e.client.Close()
	waitPieceMarks(torrents)
	// A custom DefaultStorage is not closed by client.Close (only the
	// fallback file storage is); close it explicitly.
	if err := e.storageImpl.Close(); err != nil {
		errs = append(errs, err)
	}
	// client.Close only closes sockets it created itself; a listener
	// registered via AddListener is ours to close (anacrolix's own doc on
	// AddListener says so).
	if err := e.listener.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := e.packetConn.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// pieceMarkPollInterval paces the shutdown wait below. anacrolix publishes
// piece-state changes over a pubsub that Torrent.close tears down
// (torrent.go:1136), so a mark finishing after the client closed can only be
// observed by polling.
const pieceMarkPollInterval = 500 * time.Microsecond

// waitPieceMarks blocks until every piece-completion mark anacrolix committed
// to before the client closed has finished writing.
//
// A mark runs with the client lock released and outside the per-torrent
// storage lock, on a hasher goroutine Client.Close does not await — see
// drainingStorage for the full trace — so closing the completion store as soon
// as Client.Close returns can cut a mark off mid-write and cost the piece its
// persisted completion. PieceState.Marking is the fence: pieceHashed raises it
// under the client lock (torrent.go:2635) right after the same critical
// section decided the torrent was still open (:2619), and lowers it from a
// defer once the mark has returned (:2637-2640). Client.Close sets that closed
// flag under the same lock (torrent.go:1103 via client.go:542-553), so once it
// has returned every mark that will ever run is already flagged, and waiting
// the flags out is a terminating wait rather than a grace period.
func waitPieceMarks(torrents []*antorrent.Torrent) {
	for _, t := range torrents {
		if t.Info() == nil {
			continue
		}
		for pieceMarkInFlight(t) {
			time.Sleep(pieceMarkPollInterval)
		}
	}
}

func pieceMarkInFlight(t *antorrent.Torrent) bool {
	for _, run := range t.PieceStateRuns() {
		if run.Marking {
			return true
		}
	}
	return false
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
		st := &torrentState{
			paused:            s.Paused,
			seedStopped:       s.SeedStopped,
			addedAt:           s.CreateTime,
			uploadedBase:      s.Uploaded,
			uploadedPersisted: s.Uploaded,
			selectionMode:     string(s.SelectionMode),
			wantedFiles:       s.WantedFiles,
		}
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
		hash := t.InfoHash().HexString()
		st := e.getState(hash)
		applyFilePriorities(t, st.selectionMode, st.wantedFiles)
		if err := e.store.SetTorrentSessionName(
			context.Background(), hash, t.Name(),
		); err != nil {
			slog.WarnContext(context.Background(),
				"persisting resolved torrent name failed",
				"info_hash", hash, "error", err)
		}
	})
}

// applyFilePriorities sets every file's priority from the persisted
// selection, and runs every time startWhenReady does — including the
// metadata-resolve re-run after a restore. File priorities are the engine's
// single demand source — never DownloadAll, whose piece-level demand is
// max()-merged and can't be retracted by a later per-file skip — so this is
// the only place demand is decided.
//
// mode "all" (also the zero value, so an unrecorded state behaves exactly
// like a session created before selection existed) bumps every
// still-unprioritized file from anacrolix's None default to Normal, and
// leaves a file the user already prioritized untouched. mode "pending" is a
// selective grab whose keep-set isn't known yet: bump nothing, so a magnet
// fetches metadata (priority-independent) but no data piece. mode "explicit"
// wants exactly wanted's indexes and skips the rest — unconditionally,
// unlike "all", so a repeat run (the restart case this fixes) reproduces the
// same skip instead of the old code's unconditional bump erasing it.
func applyFilePriorities(t *antorrent.Torrent, mode string, wanted []int) {
	switch mode {
	case "pending":
	case "explicit":
		keep := make(map[int]struct{}, len(wanted))
		for _, i := range wanted {
			keep[i] = struct{}{}
		}
		for i, f := range t.Files() {
			if _, ok := keep[i]; ok {
				if f.Priority() == types.PiecePriorityNone {
					f.SetPriority(types.PiecePriorityNormal)
				}
			} else {
				f.SetPriority(types.PiecePriorityNone)
			}
		}
	default: // "all"
		for _, f := range t.Files() {
			if f.Priority() == types.PiecePriorityNone {
				f.SetPriority(types.PiecePriorityNormal)
			}
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
