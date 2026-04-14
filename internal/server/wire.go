package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/importer"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/jobs"
	jobsstate "github.com/datahearth/streamline/internal/jobs/state"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/library/hygiene"
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
	dlManager := download.New(store)
	movieSvc := movie.NewService(store, tmdb, postersSvc, dlManager)
	tvSvc := tvshow.NewService(store, tvdb, postersSvc, dlManager)
	mediaServerSvc := mediaserver.New()
	libSvc := library.NewImportService(&cfg.Library)
	bulkImportSvc := bulkimport.NewService(
		store,
		tmdb,
		libSvc,
		movieSvc,
		tvSvc,
		postersSvc,
		cfg.Library.MoviePath,
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
	imp := importer.NewWorker(importer.Deps{
		DB:          store,
		Library:     libSvc,
		Download:    dlManager,
		MediaServer: dispatcher,
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
	authMW := middleware.NewAuth(authSvc, []string{
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

	missingSearcher, err := rss.NewMissingSearcher(store, indexerSvc, dlManager)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("create missing searcher: %w", err)
	}
	feedScanner, err := rss.NewFeedScanner(store, indexerSvc, dlManager)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("create rss feed scanner: %w", err)
	}

	reqSvc := request.NewService(store, movieSvc, tvSvc)
	tvMissing, err := rss.NewEpisodeMissingSearcher(store, indexerSvc, dlManager)
	if err != nil {
		dbClient.Close()
		return nil, fmt.Errorf("create tv missing searcher: %w", err)
	}

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
			"rss-sync",
			cfg.Schedule.RSSSync,
			func(time.Duration) scheduler.JobFunc { return jobs.RSSFeed(feedScanner) },
		},
		{
			"missing-search",
			cfg.Schedule.MissingSearch,
			func(time.Duration) scheduler.JobFunc { return jobs.MissingSearch(missingSearcher) },
		},
		{
			"metadata-refresh",
			cfg.Schedule.MetadataRefresh,
			func(time.Duration) scheduler.JobFunc { return jobs.MetadataRefresh(movieSvc) },
		},
		{
			"tv-missing-search",
			cfg.Schedule.MissingSearch,
			func(time.Duration) scheduler.JobFunc { return jobs.MissingSearch(tvMissing) },
		},
		{
			"tv-metadata-refresh",
			cfg.Schedule.MetadataRefresh,
			func(time.Duration) scheduler.JobFunc { return jobs.TVMetadataRefresh(tvSvc) },
		},
		{
			"cleanup",
			cfg.Schedule.Cleanup,
			func(time.Duration) scheduler.JobFunc { return jobs.Cleanup(dlManager.(download.Cleaner)) },
		},
		{
			"orphan-scan",
			cfg.Schedule.OrphanScan,
			func(time.Duration) scheduler.JobFunc { return jobs.OrphanScan(hygieneSvc) },
		},
		{
			"series-orphan-scan",
			cfg.Schedule.OrphanScan,
			func(time.Duration) scheduler.JobFunc { return jobs.SeriesOrphanScan(hygieneSvc) },
		},
		{
			"drift-check",
			cfg.Schedule.DriftCheck,
			func(d time.Duration) scheduler.JobFunc { return jobs.DriftCheck(hygieneSvc, d) },
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
	}, nil
}

// generateSessionSecret returns 64 bytes of crypto/rand encoded as base64.
func generateSessionSecret() (string, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
