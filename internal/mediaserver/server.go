package mediaserver

import "context"

type Server interface {
	TestConnection(ctx context.Context) error
	// RefreshLibrary triggers a rescan on the server: of req.Folders when the
	// server's own libraries contain every one of them, else of the whole
	// library. The scope it settled on is returned, error or not.
	RefreshLibrary(ctx context.Context, req RefreshRequest) (RefreshScope, error)
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

// RefreshRequest is one rescan sent to one server. Every path in it is already
// in the server's own view, through the entry's path_mappings.
type RefreshRequest struct {
	// Root is the library root the change happened under.
	Root string
	// Folders are the folders to rescan, none of them inside another. Empty
	// asks for the whole library.
	Folders []string
	// SectionKey is the Plex section configured for this kind; "" lets Plex
	// find it. Jellyfin and Emby ignore it.
	SectionKey string
	// SectionType limits a Plex fallback to the sections of one type
	// ("artist" for music) when no key is configured; "" means any.
	SectionType string
}

// RefreshScope is what a server was actually asked to rescan.
type RefreshScope string

const (
	// ScopeFolders is a partial scan of the request's folders.
	ScopeFolders RefreshScope = "folders"
	// ScopeLibrary is a scan of a whole section (Plex) or of every library
	// (Jellyfin, Emby): no folder was given, or one of them lies outside
	// everything the server reports.
	ScopeLibrary RefreshScope = "library"
)
