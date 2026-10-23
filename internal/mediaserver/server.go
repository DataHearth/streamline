package mediaserver

import "context"

type Server interface {
	TestConnection(ctx context.Context) error
	// RefreshLibrary triggers a re-scan on the server. sectionKey is the
	// Plex-internal section identifier when targeting a specific library;
	// pass "" to fall back to legacy behavior (path-match for Plex, full
	// refresh for Jellyfin/Emby).
	RefreshLibrary(ctx context.Context, libraryPath, sectionKey string) error
	// MovieDeepLink returns a web URL pointing at the movie's details page on
	// this server. Returns ErrMovieNotFound when no item matches. hintSection
	// is the configured Plex library section (key) to check first; Jellyfin
	// and Emby ignore it.
	MovieDeepLink(
		ctx context.Context,
		hintSection string,
		tmdbID uint32,
		title string,
		year uint16,
	) (string, error)
	// TVShowDeepLink returns a web URL pointing at the series' details page on
	// this server. Returns ErrShowNotFound when no item matches. hintSection
	// is the configured Plex TV library section (key) to check first; Jellyfin
	// and Emby ignore it.
	TVShowDeepLink(
		ctx context.Context,
		hintSection string,
		tvdbID uint32,
		title string,
		year uint16,
	) (string, error)
}
