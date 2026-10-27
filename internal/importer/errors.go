package importer

import (
	"errors"

	"github.com/datahearth/streamline/internal/library"
)

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
	// ErrCueImage is returned when an album release is disc images, one file
	// per disc or vinyl side, cut by a CUE sheet beside them.
	ErrCueImage = errors.New("release is a " + library.CueImageReason)
	// ErrNoBookProfile is returned when an ebook record has no quality profile
	// to choose its format by: none is configured for the author or as the
	// default.
	ErrNoBookProfile = errors.New("no quality profile configured for this book")
)

// ErrTracksHeld is returned when every file of an album release matched a
// track that already holds one and the record replaces none. It is an
// library.ErrDestExists for classify and the pack importer, with a message
// that says so: the record used to read "destination already exists", which
// names no destination and sends a person looking for a file collision.
var ErrTracksHeld error = tracksHeldError{}

type tracksHeldError struct{}

func (tracksHeldError) Error() string {
	return "every track this release matched already holds a file"
}

func (tracksHeldError) Is(
	target error,
) bool {
	return target == library.ErrDestExists
}
