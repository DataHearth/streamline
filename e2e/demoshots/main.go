// Command demoshots boots a throwaway streamline, seeds the demo library the
// README screenshots show, and shoots them into docs/assets.
//
//	task docs:screenshots
//
// The library is public-domain film and television, Creative Commons music
// and public-domain books, looked up for real: TMDB and TVDB need
// STREAMLINE_METADATA__TMDB_API_KEY and STREAMLINE_METADATA__TVDB_API_KEY,
// books need STREAMLINE_METADATA__HARDCOVER_API_KEY, and MusicBrainz needs
// nothing. Everything else is local: a fake Torznab indexer serves one
// release per title, a fake qBittorrent reports each one at the progress the
// catalog gives it, and the releases meant to finish are real files made with
// ffmpeg, so every import, the held one included, goes through streamline's
// own pipeline. Two things are staged rather than real, and nothing else:
// a handful of release dates are written into the current month, since a
// library of old titles has nothing on its calendar, and the browser is
// shown a 16 TiB disk and an https public URL in /system/info, since the
// real figures describe the machine taking the screenshots
// (stageSystemInfo).
//
// Everything lives under -root (a fixed path, so the paths the pages show
// stay the same from run to run), which is wiped at start except for the
// encoded clips in its cache. The instance's config.yaml, which holds the
// provider keys, is removed when the run ends unless -keep is given.
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
	"strings"
	"syscall"
	"time"
)

const (
	adminEmail    = "demo@streamline.local"
	adminPassword = "Demo-Passw0rd!"
	userPassword  = "Member-Passw0rd!"
	marker        = ".streamline-demo"
)

type demo struct {
	root     string
	binary   string
	out      string
	base     string
	only     map[string]bool
	keep     bool
	fx       *fixtures
	admin    *api
	qb       *qbittorrent
	tz       *torznab
	qbAddr   string
	tzAddr   string
	ids      ids
	scanID   int
	releases []*release
}

// ids are the library rows the seeding created, by catalog title.
type ids struct {
	movies  map[string]int
	series  map[string]int
	artists map[string]int
	albums  map[string]int // "Artist/Album"
	books   map[string]int
	bseries map[string]int
}

func main() {
	tmp := os.TempDir()
	d := &demo{}
	flag.StringVar(
		&d.binary,
		"binary",
		"",
		"streamline binary to boot (task docs:screenshots builds one)",
	)
	flag.StringVar(
		&d.root,
		"root",
		filepath.Join(tmp, "streamline-demo"),
		"scratch directory, wiped at start except its cache",
	)
	flag.StringVar(&d.out, "out", "docs/assets", "where the screenshots go")
	only := flag.String(
		"only",
		"",
		"comma-separated screenshot names to take (default: all)",
	)
	flag.BoolVar(
		&d.keep,
		"keep",
		false,
		"leave the instance running after the screenshots, until interrupted, and keep its config.yaml (provider keys included)",
	)
	flag.Parse()
	if d.binary == "" {
		fmt.Fprintln(os.Stderr, "demoshots: -binary is required")
		os.Exit(2)
	}
	if *only != "" {
		d.only = map[string]bool{}
		for n := range strings.SplitSeq(*only, ",") {
			d.only[strings.TrimSpace(n)] = true
		}
	}
	slog.SetDefault(
		slog.New(
			slog.NewTextHandler(
				os.Stderr,
				&slog.HandlerOptions{Level: slog.LevelInfo},
			),
		),
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	if err := d.run(ctx); err != nil {
		slog.ErrorContext(ctx, "demoshots failed", "error", err)
		stop()
		os.Exit(1) //nolint:gocritic // the deferred stop has already run
	}
}

func (d *demo) run(ctx context.Context) error {
	for _, k := range []string{"STREAMLINE_METADATA__TMDB_API_KEY", "STREAMLINE_METADATA__TVDB_API_KEY", "STREAMLINE_METADATA__HARDCOVER_API_KEY"} {
		if os.Getenv(k) == "" {
			return fmt.Errorf(
				"%s is not set: the demo library is looked up for real",
				k,
			)
		}
	}
	if err := d.prepareRoot(); err != nil {
		return err
	}
	// Registered before the instance is started, so it runs after the
	// instance has stopped and can no longer write the file back.
	defer d.dropConfig(ctx)
	d.fx = &fixtures{cache: filepath.Join(d.root, "cache")}
	d.releases = baseReleases()
	d.tz = &torznab{}
	d.qb = newQBittorrent(d.path("downloads"), nil)
	var err error
	if d.tzAddr, err = serve("127.0.0.1", d.tz); err != nil {
		return err
	}
	if d.qbAddr, err = serve("127.0.0.1", d.qb); err != nil {
		return err
	}
	slog.InfoContext(ctx, "encoding clips (cached after the first run)")
	for _, c := range []clip{clipMovie1080, clipMovie2160, clipEpisode} {
		if _, err := d.fx.clip(ctx, c); err != nil {
			return err
		}
	}

	inst, err := d.boot(ctx)
	if err != nil {
		return err
	}
	defer inst.stop()

	if d.admin, err = login(ctx, d.base, adminEmail, adminPassword); err != nil {
		return err
	}
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"add titles", d.addTitles},
		{"wait for metadata", d.waitForMetadata},
		{"write release files", d.writeReleases},
		{"connect indexer and client", d.connect},
		{"grab", d.grabAll},
		{"wait for imports", d.waitForImports},
		{"requests", d.seedRequests},
		{"library import", d.seedAdoption},
		{"settle", d.settle},
		{"calendar", d.seedCalendar},
	}
	for _, s := range steps {
		start := time.Now()
		slog.InfoContext(ctx, "step", "name", s.name)
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		slog.InfoContext(ctx,
			"step done",
			"name",
			s.name,
			"took",
			time.Since(start).Round(time.Second),
		)
	}
	if err := d.shoot(ctx); err != nil {
		return fmt.Errorf("screenshots: %w", err)
	}
	if d.keep {
		slog.InfoContext(ctx,
			"instance left running until interrupted",
			"url",
			d.base,
			"email",
			adminEmail,
			"password",
			adminPassword,
		)
		<-ctx.Done()
	}
	return nil
}

func (d *demo) path(parts ...string) string {
	return filepath.Join(append([]string{d.root}, parts...)...)
}

// prepareRoot empties the scratch directory, refusing one this program did
// not create.
func (d *demo) prepareRoot() error {
	entries, err := os.ReadDir(d.root)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	case len(entries) > 0:
		if _, err := os.Stat(d.path(marker)); err != nil {
			return fmt.Errorf(
				"%s is not empty and was not made by demoshots; pick another -root",
				d.root,
			)
		}
		for _, e := range entries {
			if e.Name() == "cache" || e.Name() == marker {
				continue
			}
			if err := os.RemoveAll(d.path(e.Name())); err != nil {
				return err
			}
		}
	}
	for _, dir := range []string{"cache", "data", "downloads", "library/movies", "library/series", "library/music", "library/books", "library/audiobooks", "media/movies"} {
		if err := os.MkdirAll(d.path(dir), 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(d.path(marker), nil, 0o644)
}

// dropConfig removes the instance's config once the run is over, however it
// ended: boot writes the provider keys into it in clear, and -root is a
// fixed, guessable path that is otherwise only emptied by the next run.
// -keep leaves it, so the instance can be started again by hand.
func (d *demo) dropConfig(ctx context.Context) {
	file := d.path("config.yaml")
	if d.keep {
		if _, err := os.Stat(file); err == nil {
			slog.WarnContext(
				ctx,
				"config kept; it holds the provider keys",
				"path",
				file,
			)
		}
		return
	}
	if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.WarnContext(
			ctx,
			"config not removed; it holds the provider keys",
			"path",
			file,
			"error",
			err,
		)
	}
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// instance is the streamline process the screenshots are taken of.
type instance struct {
	cmd *exec.Cmd
	log string
	// exited closes once the process has been waited on. Only that Wait sets
	// ProcessState, and a second Wait would only error, so the channel is how
	// everything else learns the process is gone.
	exited chan struct{}
}

// startInstance starts cmd with its output in the file at logPath and waits
// on it from the start, so an instance that dies is noticed when it dies.
func startInstance(cmd *exec.Cmd, logPath string) (*instance, error) {
	logf, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return nil, err
	}
	in := &instance{cmd: cmd, log: logPath, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		logf.Close()
		close(in.exited)
	}()
	return in, nil
}

// waitHealthy waits for base to answer /health, and gives up at once, with
// the exit status, when the process exits first: an instance that refused
// its config will not answer however long it is polled.
func (in *instance) waitHealthy(
	ctx context.Context,
	base string,
	budget time.Duration,
) error {
	return poll(
		ctx,
		budget,
		500*time.Millisecond,
		"the instance to answer /health",
		func() (bool, error) {
			resp, err := http.Get(
				base + "/health",
			) //nolint:noctx // a loopback probe
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return true, nil
				}
			}
			select {
			case <-in.exited:
				return false, fmt.Errorf(
					"the instance exited before it answered /health (%s); see %s",
					in.cmd.ProcessState,
					in.log,
				)
			default:
				return false, nil
			}
		},
	)
}

// stop asks the process to terminate and waits for it, through the one Wait
// startInstance began. One that ignores the request for ten seconds is
// killed.
func (in *instance) stop() {
	_ = in.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-in.exited:
		return
	case <-time.After(10 * time.Second):
	}
	_ = in.cmd.Process.Kill()
	<-in.exited
}

// boot writes the config and starts the instance with no STREAMLINE_*
// variable of this shell, which would override the file.
func (d *demo) boot(ctx context.Context) (*instance, error) {
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	d.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	cfg := fmt.Sprintf(`data_dir: %[1]s/data
server: {host: 127.0.0.1, port: %[2]d}
auth:
  session_secret: demoshots-session-secret-0123456789abcdef
  seed_admin: {email: %[3]s, password: %[4]q}
library:
  download_path: %[1]s/downloads
  movie_path: %[1]s/library/movies
  series_path: %[1]s/library/series
  music_path: %[1]s/library/music
  ebook_path: %[1]s/library/books
  audiobook_path: %[1]s/library/audiobooks
  import_mode: hardlink
metadata:
  tmdb_api_key: %[5]q
  tvdb_api_key: %[6]q
  hardcover_api_key: %[7]q
`, d.root, port, adminEmail, adminPassword,
		os.Getenv("STREAMLINE_METADATA__TMDB_API_KEY"),
		os.Getenv("STREAMLINE_METADATA__TVDB_API_KEY"),
		os.Getenv("STREAMLINE_METADATA__HARDCOVER_API_KEY"))
	//nolint:gosec // d.root is this program's own scratch directory
	if err := os.WriteFile(d.path("config.yaml"), []byte(cfg), 0o600); err != nil {
		return nil, err
	}
	//nolint:gosec // the binary the task target just built
	cmd := exec.CommandContext(ctx, d.binary, "--config", d.path("config.yaml"))
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "STREAMLINE_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	inst, err := startInstance(cmd, d.path("server.log"))
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "instance starting", "url", d.base, "log", inst.log)
	if err := inst.waitHealthy(ctx, d.base, 2*time.Minute); err != nil {
		inst.stop()
		return nil, err
	}
	return inst, nil
}
