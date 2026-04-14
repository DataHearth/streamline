package server

import (
	"log/slog"
	"net/http"
	"strconv"

	oasspec "github.com/datahearth/streamline/api"
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/posters"
	"github.com/datahearth/streamline/internal/request"
	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/server/middleware"
	"github.com/datahearth/streamline/internal/server/restapi"
	"github.com/datahearth/streamline/internal/server/web"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Server is the composition root: chi.Mux wired with auth + observability
// middleware, mounted with the web.Handler and restapi.Server. The exported
// Router method returns the fully-wrapped HTTP handler ready to serve.
type Server struct {
	router  *chi.Mux
	web     *web.Handler
	api     *restapi.Server
	posters posters.Manager
}

// Config carries the service-layer dependencies the composition root needs to
// build the web + restapi handlers and apply auth middleware.
type Config struct {
	DB              db.Store
	Ent             *ent.Client
	Movies          movie.Manager
	Metadata        metadata.Provider
	Indexers        indexer.Manager
	Downloads       download.Downloader
	MediaServers    mediaserver.Manager
	DeepLinker      *mediaserver.DeepLinker
	Renamer         library.Renamer
	SeriesRenamer   library.Renamer
	Auth            auth.Manager
	Limiter         auth.Limiter
	OIDC            auth.OIDCManager
	Scheduler       scheduler.Controller
	BulkImports     bulkimport.Manager
	MissingSearcher *rss.MissingSearcher
	TVShows         tvshow.Manager
	Requests        request.Manager
	TVSearcher      *rss.EpisodeMissingSearcher
	MetadataTV      metadata.TVProvider
	Posters         posters.Manager
	AuthMiddleware  func(http.Handler) http.Handler
	HTTPLog         func(http.Handler) http.Handler
}

// New assembles the composition root.
func New(cfg Config) *Server {
	webH := web.New(web.Deps{
		Auth:         cfg.Auth,
		OIDC:         cfg.OIDC,
		Limiter:      cfg.Limiter,
		MediaServers: cfg.MediaServers,
	})

	api := restapi.New(restapi.Deps{
		Auth:            cfg.Auth,
		Movies:          cfg.Movies,
		Metadata:        cfg.Metadata,
		Indexers:        cfg.Indexers,
		Downloads:       cfg.Downloads,
		MediaServers:    cfg.MediaServers,
		DeepLinker:      cfg.DeepLinker,
		Renamer:         cfg.Renamer,
		SeriesRenamer:   cfg.SeriesRenamer,
		Scheduler:       cfg.Scheduler,
		BulkImports:     cfg.BulkImports,
		MissingSearcher: cfg.MissingSearcher,
		TVShows:         cfg.TVShows,
		Requests:        cfg.Requests,
		TVSearcher:      cfg.TVSearcher,
		MetadataTV:      cfg.MetadataTV,
		Store:           cfg.DB,
		Ent:             cfg.Ent,
		PublicURL:       config.PublicURL(),
	})

	s := &Server{
		router:  chi.NewRouter(),
		web:     webH,
		api:     api,
		posters: cfg.Posters,
	}

	// Resolve the client IP from X-Forwarded-For assuming exactly one trusted
	// reverse proxy in front, before anything that logs or rate-limits by IP.
	// ponytail: single-proxy default; make the strategy configurable if
	// deployments run with 0 or N proxies.
	s.router.Use(chimw.ClientIPFromXFFTrustedProxies(1))
	if cfg.HTTPLog != nil {
		s.router.Use(cfg.HTTPLog)
	}
	s.router.Use(chimw.RequestID)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RouteTagger)
	if cfg.AuthMiddleware != nil {
		s.router.Use(cfg.AuthMiddleware)
	}

	s.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"status":"healthy"}`)); err != nil {
			slog.ErrorContext(r.Context(), "health write failed", "error", err)
		}
	})

	s.router.Get(
		"/api/v1/openapi.yaml",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-yaml")
			if _, err := w.Write(oasspec.OpenAPISpec); err != nil {
				slog.ErrorContext(
					r.Context(),
					"openapi spec write failed",
					"error",
					err,
				)
			}
		},
	)
	s.router.Get("/api/docs", s.web.APIDocs)

	restapi.Mount(s.router, s.api)
	web.Mount(s.router, s.web)

	s.router.Get(
		"/posters/{kind}/{id}/poster.jpg",
		func(w http.ResponseWriter, r *http.Request) {
			kind := chi.URLParam(r, "kind")
			idU, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 32)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			s.posters.Serve(w, r, kind, uint32(idU))
		},
	)

	// Every non-API, non-static path falls through to the SPA shell. Routify
	// owns routing client-side, including its own 404 state.
	s.router.NotFound(s.web.SPAShell)

	return s
}

// Router returns the fully-wrapped HTTP handler. otelhttp is applied
// outermost so every request — including those rejected by chi's 404/405
// handlers and those that panic before chi routes them — produces a span.
func (s *Server) Router() http.Handler {
	return otelhttp.NewHandler(
		s.router,
		"http.request",
		otelhttp.WithSpanNameFormatter(spanNameFromRequest),
	)
}

func spanNameFromRequest(_ string, r *http.Request) string {
	return r.Method + " " + r.URL.Path
}
