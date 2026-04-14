// Package restapi holds the OpenAPI StrictServer implementation for the
// /api/v1/* surface. gen.go provides the generated router + types; the
// handler_*.go files implement StrictServerInterface methods on *Server.
package restapi

import (
	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/library/bulkimport"
	"github.com/datahearth/streamline/internal/media/movie"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/request"
	"github.com/datahearth/streamline/internal/rss"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/go-chi/chi/v5"
)

// Server implements StrictServerInterface. Holds the service-layer deps
// used by the handler_*.go files in this package.
type Server struct {
	auth            auth.Manager
	movies          movie.Manager
	metadata        metadata.Provider
	indexers        indexer.Manager
	downloads       download.Downloader
	mediaServers    mediaserver.Manager
	scheduler       scheduler.Controller
	bulkImports     bulkimport.Manager
	missingSearcher *rss.MissingSearcher
	tvshows         tvshow.Manager
	tvSearcher      *rss.EpisodeMissingSearcher
	metadataTV      metadata.TVProvider
	deepLinker      *mediaserver.DeepLinker
	renamer         library.Renamer
	seriesRenamer   library.Renamer
	requests        request.Manager
	store           db.Store
	ent             *ent.Client
	publicURL       string
}

// Deps is the dependency set required by restapi handlers.
type Deps struct {
	Auth            auth.Manager
	Movies          movie.Manager
	Metadata        metadata.Provider
	Indexers        indexer.Manager
	Downloads       download.Downloader
	MediaServers    mediaserver.Manager
	Scheduler       scheduler.Controller
	BulkImports     bulkimport.Manager
	MissingSearcher *rss.MissingSearcher
	TVShows         tvshow.Manager
	TVSearcher      *rss.EpisodeMissingSearcher
	MetadataTV      metadata.TVProvider
	DeepLinker      *mediaserver.DeepLinker
	Renamer         library.Renamer
	SeriesRenamer   library.Renamer
	Requests        request.Manager
	Store           db.Store
	Ent             *ent.Client
	PublicURL       string
}

// New constructs a Server from the given Deps.
func New(d Deps) *Server {
	return &Server{
		auth:            d.Auth,
		movies:          d.Movies,
		metadata:        d.Metadata,
		indexers:        d.Indexers,
		downloads:       d.Downloads,
		mediaServers:    d.MediaServers,
		scheduler:       d.Scheduler,
		bulkImports:     d.BulkImports,
		missingSearcher: d.MissingSearcher,
		tvshows:         d.TVShows,
		tvSearcher:      d.TVSearcher,
		metadataTV:      d.MetadataTV,
		deepLinker:      d.DeepLinker,
		renamer:         d.Renamer,
		requests:        d.Requests,
		store:           d.Store,
		ent:             d.Ent,
		publicURL:       d.PublicURL,
	}
}

// Mount wires the /api/v1/* routes onto r using the generated strict
// handler adapter.
func Mount(r chi.Router, s *Server) {
	HandlerFromMuxWithBaseURL(NewStrictHandler(s, nil), r, "/api/v1")
}
