package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/bittorrent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/importer"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/jobs"
	jobsstate "github.com/datahearth/streamline/internal/jobs/state"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/library/hygiene"
	"github.com/datahearth/streamline/internal/library/pathmigrate"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/request"
	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/server/middleware"
	"go.opentelemetry.io/otel"
)

// App holds the assembled application components.
// The caller is responsible for starting the scheduler and HTTP server,
// and for closing the database when done.
type App struct {
	Server     *Server
	Scheduler  *scheduler.Scheduler
	DB         *ent.Client
	Store      db.Store
	Auth       auth.Manager
	Downloads  download.Downloader
	Importer   *importer.Worker
	HTTPLogger *observability.HTTPLogger
	Torrents   *bittorrent.Engine
}

// httpAccessSkip filters which requests bypass the HTTP access log.
// Health probes and static assets/Scalar docs would otherwise drown out
// the signal in real traffic.
func httpAccessSkip(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case p == "/health":
		return true
	case strings.HasPrefix(p, "/static/"):
		return true
	case p == "/api/docs":
		return true
	case strings.HasPrefix(p, "/api/docs/"):
		return true
	}
	return false
}

// NewFromConfig wires all application dependencies from the config singleton.
// Logging uses the process-wide slog.Default installed by cmd/main.go after
// observability.Setup, so no logger is plumbed through component constructors.
func NewFromConfig(ctx context.Context) (*App, error) {
	cfg := config.Get()

	// 1. Ensure a session secret exists. Try to persist it to the backing
	//    config file so sessions survive restarts. If persistence fails
	//    (no backing file in tests, read-only mount in compose/k8s), fall
	//    back to an in-memory secret and warn — sessions will then reset
	//    every restart, which is preferable to refusing to boot.
	if config.SecretValue(cfg.Auth.SessionSecret, cfg.Auth.SessionSecretFile) == "" {
		secret, err := generateSessionSecret()
		if err != nil {
			return nil, fmt.Errorf("generate session secret: %w", err)
		}
		if err := config.Update(ctx, func(c *config.Config) error {
			c.Auth.SessionSecret = secret
			return nil
		}); err != nil {
			if errors.Is(err, config.ErrNoPath) ||
				errors.Is(err, config.ErrReadOnly) {
				slog.WarnContext(
					ctx,
					"session secret not persisted (read-only or no backing file) — set auth.session_secret to keep sessions across restarts",
				)
			} else {
				slog.WarnContext(
					ctx,
					"could not persist generated session secret — sessions will not survive restart",
					"error",
					err,
				)
			}
			cfg.Auth.SessionSecret = secret
		} else {
			cfg = config.Get()
			slog.InfoContext(ctx, "generated and persisted new session secret")
		}
	}

	// Plex client identifier: a stable per-install ID required by Plex's PIN
	// OAuth flow. Generated lazily — only once a Plex server is configured —
	// and persisted; adding a Plex server via the API mints it inline. Here we
	// cover a config that already lists a Plex server on boot.
	if err := config.EnsurePlexClientID(ctx); errors.Is(err, config.ErrReadOnly) {
		slog.WarnContext(
			ctx,
			"plex client id not persisted (read-only config) — set media_server.plex_client_id to keep it stable",
		)
	} else if err != nil {
		slog.WarnContext(
			ctx,
			"could not persist plex client id — id rotates on restart",
			"error",
			err,
		)
	} else if cfg.MediaServer.PlexClientID == "" {
		cfg = config.Get()
	}

	// 2. Open database
	dbClient, err := db.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.RegisterEntityMetrics(
		otel.Meter("streamline"),
		dbClient,
	); err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("register entity metrics: %w", err)
	}

	events.Register(dbClient)

	store := db.New(dbClient)

	// 3. Create metadata / media / indexer / download services
	tmdb := metadata.NewTMDB()
	tvdb := metadata.NewTVDB()
	postersSvc, err := posters.New(cfg.DataDir)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("create posters service: %w", err)
	}
	indexerSvc := indexer.New()
	var torrentEngine *bittorrent.Engine
	var builtinClient download.Client
	if _, ok := config.BuiltinDownloadClient(); ok {
		var err error
		torrentEngine, err = bittorrent.New(ctx, store)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("builtin torrent engine: %w", err)
		}
		builtinClient = torrentEngine
	}
	dlManager := download.New(store, builtinClient)
	movieSvc := movie.NewService(store, tmdb, postersSvc, dlManager)
	tvSvc := tvshow.NewService(store, tvdb, postersSvc, dlManager)
	mediaServerSvc := mediaserver.New()
	// Nothing else creates the library roots — the importer only makes per-title
	// subfolders, so on a fresh install they'd first appear after an import that
	// bulk-import refuses to run until they exist.
	for _, p := range []string{cfg.Library.MoviePath, cfg.Library.SeriesPath} {
		if p == "" {
			continue
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("create library path %s: %w", p, err)
		}
	}
	libSvc := library.NewImportService(&cfg.Library)
	bulkImportSvc := bulkimport.NewService(
		store,
		tmdb,
		tvdb,
		libSvc,
		movieSvc,
		tvSvc,
		cfg.Library.MoviePath,
		cfg.Library.SeriesPath,
	)
	hygieneSvc := hygiene.New(store, tmdb, tvdb, libSvc, &cfg.Library)
	if n, err := bulkImportSvc.AbortInflight(ctx); err != nil {
		slog.WarnContext(
			ctx,
			"bulk import: failed to abort inflight scans on boot",
			"error",
			err,
		)
	} else if n > 0 {
		slog.InfoContext(
			ctx,
			"bulk import: cleared inflight scans on boot",
			"aborted_count",
			n,
		)
	}
	dispatcher := mediaserver.NewDispatcher()
	deepLinker := mediaserver.NewDeepLinker(nil)
	renamer := movie.NewRenameService(
		store, cfg.Library.MoviePath, cfg.Library.MovieNaming,
	)
	seriesRenamer := tvshow.NewRenameService(
		store, cfg.Library.SeriesPath, cfg.Library.SeriesNaming,
	)
	pathMigrations := pathmigrate.NewService(store)
	// Constructed once and kept: the media-probe backfill job and the
	// health/system-info endpoint reuse this same prober.
	prober := ffmpeg.NewCLI(cfg.FFmpeg.Path)
	hygieneSvc.Probe = prober
	imp := importer.NewWorker(importer.Deps{
		DB:          store,
		Library:     libSvc,
		Download:    dlManager,
		MediaServer: dispatcher,
		Prober:      prober,
	})
	go imp.Start(ctx)

	// 4. Create auth service
	authSvc, err := auth.New(store)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("create auth service: %w", err)
	}

	// 5. Bootstrap seed admin (no-op if email empty or users already exist).
	if err := authSvc.BootstrapSeedAdmin(ctx); err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("bootstrap seed admin: %w", err)
	}

	// 6. Initialize OIDC providers (silent skip on discovery failures).
	oidcMgr := auth.NewOIDCManager()
	oidcMgr.Init(ctx, config.PublicURL())

	// 7. Login/register rate limiter: 5 attempts per 15 minutes per IP.
	limiter := auth.NewLimiter(5, 15*time.Minute)

	// 8. Middleware
	authMW := middleware.NewAuth(authSvc, apiFailureLimiter(), []string{
		"/health",
		"/api/docs",
		"/api/v1/openapi.yaml",
		"/static/",
		"/login",
		"/register",
		"/auth/login",
		"/auth/register",
		"/auth/config",
		"/auth/invite/",
		"/auth/oidc/",
	})

	// 9. Scheduler
	sched := scheduler.New(scheduler.WithStateHook(jobsstate.NewHook(dbClient)))

	missingSearcher := rss.NewMissingSearcher(store, indexerSvc, dlManager)
	feedScanner := rss.NewFeedScanner(store, indexerSvc, dlManager)

	reqSvc := request.NewService(store, movieSvc, tvSvc)
	tvMissing := rss.NewEpisodeMissingSearcher(store, indexerSvc, dlManager)
	tvFeedScanner := rss.NewTVFeedScanner(store, indexerSvc, dlManager)

	jobsToRegister := []struct {
		name     string
		interval string
		fn       func(d time.Duration) scheduler.JobFunc
	}{
		{
			"download-monitor",
			cfg.Schedule.DownloadMonitor,
			func(time.Duration) scheduler.JobFunc {
				return jobs.DownloadMonitor(
					dlManager,
					dlManager.(download.Adopter),
					imp,
				)
			},
		},
		{
			"import-scan",
			cfg.Schedule.ImportScan,
			func(time.Duration) scheduler.JobFunc { return imp.Scan },
		},
		{
			"movie-rss-sync",
			cfg.Schedule.MovieRSSSync,
			func(time.Duration) scheduler.JobFunc { return jobs.RSSFeed(feedScanner) },
		},
		{
			"movie-missing-search",
			cfg.Schedule.MovieMissingSearch,
			func(time.Duration) scheduler.JobFunc { return jobs.MissingSearch(missingSearcher) },
		},
		{
			"movie-metadata-refresh",
			cfg.Schedule.MovieMetadataRefresh,
			func(time.Duration) scheduler.JobFunc { return jobs.MetadataRefresh(movieSvc) },
		},
		{
			"tv-missing-search",
			cfg.Schedule.TVMissingSearch,
			func(time.Duration) scheduler.JobFunc { return jobs.MissingSearch(tvMissing) },
		},
		{
			"tv-rss-sync",
			cfg.Schedule.TVRSSSync,
			func(time.Duration) scheduler.JobFunc { return jobs.RSSFeed(tvFeedScanner) },
		},
		{
			"tv-metadata-refresh",
			cfg.Schedule.TVMetadataRefresh,
			func(time.Duration) scheduler.JobFunc { return jobs.TVMetadataRefresh(tvSvc) },
		},
		{
			"cleanup",
			cfg.Schedule.Cleanup,
			func(time.Duration) scheduler.JobFunc { return jobs.Cleanup(dlManager.(download.Cleaner)) },
		},
		{
			"movie-orphan-scan",
			cfg.Schedule.MovieOrphanScan,
			func(time.Duration) scheduler.JobFunc { return jobs.OrphanScan(hygieneSvc) },
		},
		{
			"tv-orphan-scan",
			cfg.Schedule.TVOrphanScan,
			func(time.Duration) scheduler.JobFunc { return jobs.SeriesOrphanScan(hygieneSvc) },
		},
		{
			"drift-check",
			cfg.Schedule.DriftCheck,
			func(d time.Duration) scheduler.JobFunc { return jobs.DriftCheck(hygieneSvc, d) },
		},
		{
			"media-probe",
			cfg.Schedule.MediaProbe,
			func(d time.Duration) scheduler.JobFunc { return jobs.MediaProbe(hygieneSvc) },
		},
	}
	for _, j := range jobsToRegister {
		d, err := time.ParseDuration(j.interval)
		if err != nil {
			dbClient.Close()
			return nil, fmt.Errorf("parse %s interval: %w", j.name, err)
		}
		sched.Register(j.name, d, j.fn(d))
	}

	sched.Register(
		"purge-sessions",
		time.Hour,
		jobs.PurgeSessions(authSvc),
		scheduler.WithSystem(),
	)

	if err := jobsstate.Seed(ctx, dbClient, sched.List()); err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("seed scheduled_job rows: %w", err)
	}
	paused, err := jobsstate.PausedNames(ctx, dbClient)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("load paused job names: %w", err)
	}
	for _, name := range paused {
		if err := sched.Pause(name); err != nil {
			slog.WarnContext(ctx, "could not re-pause job from DB state",
				"job", name, "error", err)
		}
	}

	// 10. HTTP access logger (nil when disabled). Mounted as outermost
	//     middleware so every request — including 404s and panics — is
	//     accounted for.
	httpLogger, err := observability.NewHTTPLogger(cfg.Log.HTTP)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("http access logger: %w", err)
	}

	var torrentsAPI bittorrent.Manager
	if torrentEngine != nil {
		torrentsAPI = torrentEngine
	}

	// 11. HTTP server
	srv := New(Config{
		DB:              store,
		Ent:             dbClient,
		Movies:          movieSvc,
		Metadata:        tmdb,
		Indexers:        indexerSvc,
		Downloads:       dlManager,
		MediaServers:    mediaServerSvc,
		DeepLinker:      deepLinker,
		Renamer:         renamer,
		SeriesRenamer:   seriesRenamer,
		Auth:            authSvc,
		Limiter:         limiter,
		OIDC:            oidcMgr,
		Scheduler:       sched,
		BulkImports:     bulkImportSvc,
		MissingSearcher: missingSearcher,
		TVShows:         tvSvc,
		Requests:        reqSvc,
		TVSearcher:      tvMissing,
		MetadataTV:      tvdb,
		Posters:         postersSvc,
		Torrents:        torrentsAPI,
		PathMigrations:  pathMigrations,
		AuthMiddleware:  authMW,
		HTTPLog:         httpLogger.Middleware(httpAccessSkip),
	})

	return &App{
		Server:     srv,
		Scheduler:  sched,
		DB:         dbClient,
		Store:      store,
		Auth:       authSvc,
		Downloads:  dlManager,
		Importer:   imp,
		HTTPLogger: httpLogger,
		Torrents:   torrentEngine,
	}, nil
}

// apiFailureLimiter returns the budget /api/v1 credential failures are metered
// against, per client address. It is separate from the login limiter so a
// browser fumbling a password cannot throttle the same operator's API client,
// and wider than it because one expired session fires every query on the page
// at once and each of those 401s is charged.
//
// The hidden auth.api_failure_limit key overrides the count, and 0 turns
// metering off entirely (e2e seam). The sweep that asserts all 131 API routes
// refuse an anonymous caller is, by construction, exactly the traffic this
// limiter exists to stop: metered, it would prove the limiter works and stop
// proving what it was written to prove. No default and no schema entry, so an
// install can only run on the value below.
func apiFailureLimiter() auth.Limiter {
	limit := uint64(20)
	if raw := config.HiddenString("auth.api_failure_limit"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 8)
		if err == nil {
			limit = parsed
		}
	}
	if limit == 0 {
		return nil
	}
	return auth.NewLimiter(uint8(limit), 15*time.Minute)
}

// generateSessionSecret returns 64 bytes of crypto/rand encoded as base64.
func generateSessionSecret() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
