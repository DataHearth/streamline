package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	oasspec "github.com/datahearth/streamline/api"
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/bittorrent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/importer"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/library/hygiene"
	"github.com/datahearth/streamline/internal/library/pathmigrate"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/media/music"
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
	"github.com/datahearth/streamline/internal/transcoding"
	"github.com/datahearth/streamline/internal/utils/httputil"
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
	art     LookupArt
	db      db.Store
}

// LookupArt serves the art of titles that are not in the library yet.
type LookupArt interface {
	Serve(w http.ResponseWriter, r *http.Request, kind, key string)
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
	RefreshStatus   mediaserver.RefreshStatuses
	DeepLinker      *mediaserver.DeepLinker
	Renamer         library.Renamer
	SeriesRenamer   library.Renamer
	MusicRenamer    library.Renamer
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
	Music           music.Manager
	Books           book.Manager
	MetadataBook    metadata.BookProvider
	MetadataMusic   metadata.MusicProvider
	Hardcover       interface{ AuthRejected() bool }
	Posters         posters.Manager
	LookupArt       LookupArt
	Torrents        bittorrent.Manager
	PathMigrations  *pathmigrate.Service
	Hygiene         *hygiene.Service
	Importer        importer.Enqueuer
	TrackPlans      importer.TrackPlanner
	Prober          ffmpeg.Prober
	Transcoder      *transcoding.Worker
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
		RefreshStatus:   cfg.RefreshStatus,
		DeepLinker:      cfg.DeepLinker,
		Renamer:         cfg.Renamer,
		SeriesRenamer:   cfg.SeriesRenamer,
		MusicRenamer:    cfg.MusicRenamer,
		Scheduler:       cfg.Scheduler,
		BulkImports:     cfg.BulkImports,
		MissingSearcher: cfg.MissingSearcher,
		TVShows:         cfg.TVShows,
		Requests:        cfg.Requests,
		TVSearcher:      cfg.TVSearcher,
		MetadataTV:      cfg.MetadataTV,
		Music:           cfg.Music,
		Books:           cfg.Books,
		MetadataBook:    cfg.MetadataBook,
		MetadataMusic:   cfg.MetadataMusic,
		Hardcover:       cfg.Hardcover,
		Torrents:        cfg.Torrents,
		PathMigrations:  cfg.PathMigrations,
		Hygiene:         cfg.Hygiene,
		Importer:        cfg.Importer,
		TrackPlans:      cfg.TrackPlans,
		Store:           cfg.DB,
		Ent:             cfg.Ent,
		PublicURL:       config.PublicURL(),
		Prober:          cfg.Prober,
		Transcoder:      cfg.Transcoder,
	})

	s := &Server{
		router:  chi.NewRouter(),
		web:     webH,
		api:     api,
		posters: cfg.Posters,
		art:     cfg.LookupArt,
		db:      cfg.DB,
	}

	// RequestID first so every line the rest of the chain logs — including the
	// access log and the resolver's own warnings — can be tied to a request.
	s.router.Use(chimw.RequestID)
	// Must precede everything that can write a response, so the headers land on
	// auth redirects, 404s and panic 500s too — not just handler output.
	s.router.Use(middleware.SecurityHeaders)
	// Must precede everything that logs, rate-limits or authorises by IP.
	s.router.Use(httputil.ClientIPResolver())
	if cfg.HTTPLog != nil {
		s.router.Use(cfg.HTTPLog)
	}
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.RouteTagger)
	if cfg.AuthMiddleware != nil {
		s.router.Use(cfg.AuthMiddleware)
	}
	// Last, so an anonymous caller still gets the 401/302 it gets today rather
	// than learning the body ceiling before authenticating. Auth reads no body,
	// so nothing is lost by deferring. /auth/login and /auth/register are only
	// *excluded* from auth, not from the chain, so they are still capped.
	s.router.Use(middleware.BodyLimit)

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
			id := uint32(idU)
			if kind == "artists" {
				kind, id = s.artistPosterSource(r.Context(), id)
			}
			s.posters.Serve(w, r, kind, id)
		},
	)

	// The one proxy for art of a title not in the library: the browser sees only
	// same-origin bytes. Behind the session middleware like /posters above.
	s.router.Get(
		"/posters/lookup/{kind}/{key}/poster.jpg",
		func(w http.ResponseWriter, r *http.Request) {
			s.art.Serve(w, r, chi.URLParam(r, "kind"), chi.URLParam(r, "key"))
		},
	)

	// Every non-API, non-static path falls through to the SPA shell. Routify
	// owns routing client-side, including its own 404 state.
	s.router.NotFound(s.web.SPAShell)

	return s
}

// artistPosterSource falls an artist with no photo back on the cover of its
// newest released album that has one; with neither, the artist's own 404 is what
// tells the SPA to draw its monogram.
func (s *Server) artistPosterSource(
	ctx context.Context,
	id uint32,
) (string, uint32) {
	if _, err := os.Stat(
		s.posters.Path("artists", id),
	); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		return "artists", id
	}
	albums, err := s.db.ArtistCoverAlbums(ctx, id)
	if err != nil {
		slog.WarnContext(
			ctx,
			"could not list an artist's albums for its photo fallback",
			"artist.id",
			id,
			"error",
			err,
		)
		return "artists", id
	}
	for _, album := range albums {
		if _, err := os.Stat(s.posters.Path("albums", album)); err == nil {
			return "albums", album
		}
	}
	return "artists", id
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
