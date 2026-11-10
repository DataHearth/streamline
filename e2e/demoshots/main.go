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
// own pipeline. The only state written behind the API's back is a handful of
// release dates moved into the current month, since a library of old titles
// has nothing on its calendar.
//
// Everything lives under -root (a fixed path, so the paths the pages show
// stay the same from run to run), which is wiped at start except for the
// encoded clips in its cache.
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
		"leave the instance running after the screenshots, until interrupted",
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
		slog.Error("demoshots failed", "error", err)
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
	slog.Info("encoding clips (cached after the first run)")
	for _, c := range []clip{clipMovie1080, clipMovie2160, clipEpisode} {
		if _, err := d.fx.clip(ctx, c); err != nil {
			return err
		}
	}

	proc, err := d.boot(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = proc.Process.Signal(syscall.SIGTERM)
		_ = proc.Wait()
	}()

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
		slog.Info("step", "name", s.name)
		if err := s.fn(ctx); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		slog.Info(
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
		slog.Info(
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

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// boot writes the config and starts the instance with no STREAMLINE_*
// variable of this shell, which would override the file.
func (d *demo) boot(ctx context.Context) (*exec.Cmd, error) {
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
	if err := os.WriteFile(d.path("config.yaml"), []byte(cfg), 0o600); err != nil {
		return nil, err
	}
	logf, err := os.Create(d.path("server.log"))
	if err != nil {
		return nil, err
	}
	//nolint:gosec // the binary the task target just built
	cmd := exec.CommandContext(ctx, d.binary, "--config", d.path("config.yaml"))
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "STREAMLINE_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	slog.Info("instance starting", "url", d.base, "log", d.path("server.log"))
	err = poll(
		ctx,
		2*time.Minute,
		500*time.Millisecond,
		"the instance to answer /health",
		func() (bool, error) {
			resp, err := http.Get(
				d.base + "/health",
			) //nolint:noctx // a loopback probe
			if err != nil {
				return false, nil
			}
			resp.Body.Close()
			return resp.StatusCode == http.StatusOK, nil
		},
	)
	return cmd, err
}
