// Command demo builds the demo library the README screenshots show and
// shoots them. It boots a throwaway instance, seeds it over the REST API, and
// writes every page in docs/assets at the README's sizes:
//
//	task docs:screenshots                  # replay the cassette, no keys, no network
//	task docs:screenshots -- -record       # fill the cassette from the real providers
//
// The library is the same every run. Every metadata provider and image CDN
// the instance calls goes through a recording proxy (proxy.go): -record sends
// what the cassette lacks upstream and keeps it, which needs
// STREAMLINE_METADATA__TMDB_API_KEY, __TVDB_API_KEY and __HARDCOVER_API_KEY
// in the environment; a plain run answers everything from the cassette, with
// recent dates moved forward so the calendar still has a month to show.
// Downloads come from the fake tracker, media files are written by demomedia,
// and nothing outside the scratch directory is touched but -out.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/datahearth/streamline/internal/testutil/nettest"
)

const (
	adminEmail     = "admin@streamline.demo"
	memberEmail    = "mina@streamline.demo"
	demoPassword   = "Demo-Passw0rd!"
	replayKey      = "demo-replay-key"
	trackerAPIKey  = "demo-tracker-key"
	sessionSecret  = "demo-session-secret-0123456789abcdef0123"
	trackerRateKiB = 10240
)

type options struct {
	binary   string
	tracker  string
	out      string
	cassette string
	record   bool
	keep     bool
	only     map[string]bool
	scale    float64
}

func main() {
	var o options
	var only string
	flag.StringVar(&o.binary, "binary", "", "streamline binary to run (task docs:screenshots builds one)")
	flag.StringVar(&o.tracker, "tracker", "", "faketracker binary (default: built from ./e2e/faketracker)")
	flag.StringVar(&o.out, "out", "docs/assets", "directory the screenshots are written to")
	flag.StringVar(&o.cassette, "cassette", "e2e/demo/cassette", "directory of recorded provider responses")
	flag.BoolVar(&o.record, "record", false, "send what the cassette lacks to the real providers and keep it (needs the provider keys)")
	flag.BoolVar(&o.keep, "keep", false, "leave the instance running after the shots, until Ctrl-C")
	flag.StringVar(&only, "only", "", "comma-separated screenshot names to take (default: all)")
	flag.Float64Var(&o.scale, "scale", 1, "device scale factor of the screenshots")
	flag.Parse()
	if o.binary == "" {
		fmt.Fprintln(os.Stderr, "-binary is required")
		os.Exit(2)
	}
	if only != "" {
		o.only = map[string]bool{}
		for n := range strings.SplitSeq(only, ",") {
			o.only[strings.TrimSpace(n)] = true
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, o); err != nil {
		slog.Error("demo failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	keys := map[string]string{
		"tmdb":      os.Getenv("STREAMLINE_METADATA__TMDB_API_KEY"),
		"tvdb":      os.Getenv("STREAMLINE_METADATA__TVDB_API_KEY"),
		"hardcover": os.Getenv("STREAMLINE_METADATA__HARDCOVER_API_KEY"),
	}
	secrets := []string{replayKey}
	for name, v := range keys {
		if v == "" {
			if o.record {
				slog.Warn("no key in the environment: that provider's calls fail and are not recorded",
					"provider", name, "env", "STREAMLINE_METADATA__"+strings.ToUpper(name)+"_API_KEY")
			}
			keys[name] = replayKey
			continue
		}
		secrets = append(secrets, v)
	}
	if !o.record {
		// Replaying sends no key anywhere: the cassette answers every call.
		for name := range keys {
			keys[name] = replayKey
		}
	}

	scratch, err := os.MkdirTemp("", "streamline-demo-")
	if err != nil {
		return err
	}
	if !o.keep {
		defer os.RemoveAll(scratch)
	}
	// The data directory (the database, posters, a few MB) goes in memory
	// where there is some, as the e2e suites do: the dashboard's health pill
	// reads its disk, and a nearly full build disk would shoot it red. The
	// downloads stay on disk: the engine writes gigabytes there.
	dataDir := filepath.Join(scratch, "data")
	if st, err := os.Stat("/dev/shm"); err == nil && st.IsDir() {
		if d, err := os.MkdirTemp("/dev/shm", "streamline-demo-data-"); err == nil {
			dataDir = d
			if !o.keep {
				defer os.RemoveAll(d)
			}
		}
	}
	slog.Info("scratch directory", "path", scratch)

	px, err := newProxy(o.cassette, o.record, secrets)
	if err != nil {
		return err
	}
	pxLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	pxSrv := &http.Server{Handler: px, ReadHeaderTimeout: 30 * time.Second}
	go func() { _ = pxSrv.Serve(pxLn) }()
	defer pxSrv.Close()
	defer func() { slog.Info(px.report()) }()

	in, err := newInstance(scratch, dataDir, keys, o.record)
	if err != nil {
		return err
	}
	if err := in.start(ctx, o.binary, "http://"+pxLn.Addr().String(), px.caPEM); err != nil {
		return err
	}
	defer in.stop()

	tracker := o.tracker
	if tracker == "" {
		tracker = filepath.Join(scratch, "faketracker")
		slog.Info("building the fake tracker")
		if out, err := exec.CommandContext(ctx, "go", "build", "-o", tracker, "./e2e/faketracker").CombinedOutput(); err != nil {
			return fmt.Errorf("build faketracker: %w: %s", err, out)
		}
	}
	in.trackerBin = tracker

	s := &seeder{in: in, api: in.client(), record: o.record}
	if err := s.seed(ctx); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if err := shoot(ctx, in, s, o); err != nil {
		return fmt.Errorf("shoot: %w", err)
	}
	if o.keep {
		slog.Info("instance left running; Ctrl-C to stop", "url", in.base, "email", adminEmail, "password", demoPassword)
		<-ctx.Done()
	}
	return nil
}

// instance is the throwaway streamline the demo runs against.
type instance struct {
	dir                 string
	port                int
	base                string
	trackerHost         string
	trackerPort         int
	trackerBin          string
	cmd, trackerCmd     *exec.Cmd
	keys                map[string]string
	record              bool
	paths               map[string]string
	logFile, trackerLog *os.File
	trackerArgs         []string
}

func newInstance(dir, dataDir string, keys map[string]string, record bool) (*instance, error) {
	in := &instance{dir: dir, keys: keys, record: record, paths: map[string]string{"data": dataDir}}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	for _, k := range []string{"downloads", "movies", "series", "music", "books", "audiobooks", "tracker"} {
		p := filepath.Join(dir, k)
		switch k {
		case "movies", "series", "music", "books", "audiobooks":
			p = filepath.Join(dir, "media", k)
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
		in.paths[k] = p
	}
	var err error
	if in.port, err = freePort("127.0.0.1"); err != nil {
		return nil, err
	}
	ip, err := nettest.AdvertiseIP()
	if err != nil {
		return nil, fmt.Errorf("the fake tracker needs a non-loopback address: %w", err)
	}
	in.trackerHost = ip.String()
	if in.trackerPort, err = freePort(in.trackerHost); err != nil {
		return nil, err
	}
	in.base = "http://127.0.0.1:" + strconv.Itoa(in.port)
	return in, nil
}

func freePort(host string) (int, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (in *instance) config() string {
	interval := "0s"
	if in.record {
		interval = "1s"
	}
	return fmt.Sprintf(`data_dir: %[1]s
server: {host: 127.0.0.1, port: %[2]d}
auth:
  session_secret: %[3]s
  registration_mode: invite
  api_failure_limit: "0"
  seed_admin: {email: %[4]s, password: "%[5]s"}
library:
  download_path: %[6]s
  movie_path: %[7]s
  series_path: %[8]s
  music_path: %[9]s
  ebook_path: %[10]s
  audiobook_path: %[11]s
metadata:
  tmdb_api_key: "%[12]s"
  tvdb_api_key: "%[13]s"
  hardcover_api_key: "%[14]s"
  musicbrainz_interval: %[15]s
download:
  # The large releases are sparse on the tracker's side and never finish; the
  # check would refuse them against this machine's free space.
  space_check: false
download_clients:
  - {name: builtin, client_type: builtin, enabled: true, download_dir: %[6]s}
indexers:
  - {name: DemoTracker, protocol: torznab, host: %[16]s, port: %[17]d, api_key: %[18]s, enabled: true}
log:
  app: {level: warn}
`,
		in.paths["data"], in.port, sessionSecret, adminEmail, demoPassword,
		in.paths["downloads"], in.paths["movies"], in.paths["series"], in.paths["music"],
		in.paths["books"], in.paths["audiobooks"],
		in.keys["tmdb"], in.keys["tvdb"], in.keys["hardcover"], interval,
		in.trackerHost, in.trackerPort, trackerAPIKey,
	)
}

// childEnv is the environment minus every STREAMLINE_* variable (the
// environment overrides the config file, and a stray data dir would point
// the instance at real data) and minus the caller's proxy, with ours in its
// place.
func childEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case strings.HasPrefix(k, "STREAMLINE_"),
			strings.EqualFold(k, "HTTPS_PROXY"), strings.EqualFold(k, "HTTP_PROXY"),
			strings.EqualFold(k, "NO_PROXY"), strings.EqualFold(k, "ALL_PROXY"),
			k == "SSL_CERT_FILE":
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func (in *instance) start(ctx context.Context, binary, proxyURL string, caPEM []byte) error {
	cfg := filepath.Join(in.dir, "config.yaml")
	if err := os.WriteFile(cfg, []byte(in.config()), 0o600); err != nil {
		return err
	}
	ca := filepath.Join(in.dir, "demo-ca.pem")
	if err := os.WriteFile(ca, caPEM, 0o644); err != nil {
		return err
	}
	var err error
	if in.logFile, err = os.Create(filepath.Join(in.dir, "server.log")); err != nil {
		return err
	}
	in.cmd = exec.Command(binary, "--config", cfg)
	in.cmd.Env = childEnv(
		"HTTPS_PROXY="+proxyURL, "HTTP_PROXY="+proxyURL,
		"NO_PROXY=127.0.0.1,localhost,"+in.trackerHost,
		// An https public URL, or the health pill warns about plain http.
		"STREAMLINE_PUBLIC_URL=https://streamline.demo",
		"SSL_CERT_FILE="+ca,
	)
	in.cmd.Stdout, in.cmd.Stderr = in.logFile, in.logFile
	if err := in.cmd.Start(); err != nil {
		return fmt.Errorf("start streamline: %w", err)
	}
	slog.Info("instance starting", "url", in.base, "log", in.logFile.Name())
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if resp, err := http.Get(in.base + "/health"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if in.cmd.ProcessState != nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("streamline did not come up; see %s", in.logFile.Name())
}

// startTracker runs the fake tracker with the releases the seeder settled on.
func (in *instance) startTracker(ctx context.Context, args []string) error {
	var err error
	if in.trackerLog, err = os.Create(filepath.Join(in.dir, "tracker.log")); err != nil {
		return err
	}
	all := append([]string{
		"-listen", net.JoinHostPort(in.trackerHost, strconv.Itoa(in.trackerPort)),
		"-data", in.paths["tracker"],
		"-apikey", trackerAPIKey,
		"-rate", strconv.Itoa(trackerRateKiB),
	}, args...)
	in.trackerArgs = all
	in.trackerCmd = exec.Command(in.trackerBin, all...)
	in.trackerCmd.Env = childEnv()
	in.trackerCmd.Stdout, in.trackerCmd.Stderr = in.trackerLog, in.trackerLog
	if err := in.trackerCmd.Start(); err != nil {
		return fmt.Errorf("start faketracker: %w", err)
	}
	addr := net.JoinHostPort(in.trackerHost, strconv.Itoa(in.trackerPort))
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if resp, err := http.Get("http://" + addr + "/?t=caps&apikey=" + trackerAPIKey); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("faketracker did not come up; see " + in.trackerLog.Name())
}

func (in *instance) stop() {
	for _, c := range []*exec.Cmd{in.trackerCmd, in.cmd} {
		if c == nil || c.Process == nil {
			continue
		}
		_ = c.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = c.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = c.Process.Kill()
		}
	}
}
