package importer

import "errors"

var (
	// ErrPathNotAllowed is returned when a DownloadRecord.save_path is not
	// within any configured Library.AllowedDownloadRoots prefix.
	ErrPathNotAllowed = errors.New("save_path not in allowed download roots")
	// ErrMovieHasFile is returned when a grab imports into a movie that
	// already has a media file and the record did not request replacement.
	ErrMovieHasFile = errors.New("movie already has a media file")
	// ErrEpisodeHasFile is the episode-grab equivalent of ErrMovieHasFile.
	ErrEpisodeHasFile = errors.New("episode already has a media file")
	// ErrNoAlbumTracks is returned when no audio file under an album record's
	// save path matched a track the import was allowed to fill.
	ErrNoAlbumTracks = errors.New("no audio file matched an album track")
	// ErrNoBookProfile is returned when an ebook record has no quality profile
	// to choose its format by: none is configured for the author or as the
	// default.
	ErrNoBookProfile = errors.New("no quality profile configured for this book")
)
