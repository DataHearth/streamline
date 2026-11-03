package audiotags

import "go.senan.xyz/taglib"

// The property keys Streamline writes, MusicBrainz Picard's names, which
// TagLib maps onto each format's own frames, comments and atoms. Re-exported
// so nothing outside this package imports TagLib.
const (
	KeyTitle            = taglib.Title
	KeyArtist           = taglib.Artist
	KeyArtists          = taglib.Artists
	KeyArtistSort       = taglib.ArtistSort
	KeyAlbum            = taglib.Album
	KeyAlbumArtist      = taglib.AlbumArtist
	KeyAlbumArtistSort  = taglib.AlbumArtistSort
	KeyTrackNumber      = taglib.TrackNumber
	KeyTrackTotal       = TrackTotal
	KeyDiscNumber       = taglib.DiscNumber
	KeyDiscTotal        = DiscTotal
	KeyDate             = taglib.Date
	KeyOriginalDate     = taglib.OriginalDate
	KeyGenre            = taglib.Genre
	KeyLabel            = taglib.Label
	KeyCatalogNumber    = taglib.CatalogNumber
	KeyBarcode          = taglib.Barcode
	KeyReleaseCountry   = taglib.ReleaseCountry
	KeyMedia            = taglib.Media
	KeyReleaseType      = taglib.ReleaseType
	KeyCompilation      = taglib.Compilation
	KeyMBArtistID       = taglib.MusicBrainzArtistID
	KeyMBAlbumArtistID  = taglib.MusicBrainzAlbumArtistID
	KeyMBAlbumID        = taglib.MusicBrainzAlbumID
	KeyMBReleaseGroupID = taglib.MusicBrainzReleaseGroupID
	KeyMBRecordingID    = taglib.MusicBrainzTrackID
	KeyMBReleaseTrackID = taglib.MusicBrainzReleaseTrackID

	KeyComposer  = taglib.Composer
	KeyLyricist  = taglib.Lyricist
	KeyWriter    = "WRITER"
	KeyArranger  = taglib.Arranger
	KeyConductor = taglib.Conductor
	KeyRemixer   = taglib.Remixer
	KeyProducer  = taglib.Producer
	KeyEngineer  = taglib.Engineer
	KeyMixer     = taglib.Mixer
	// KeyPerformer takes "Name (instrument)" values, Picard's Vorbis
	// convention; Write folds them into one PERFORMER:<INSTRUMENT> key per
	// instrument for ID3, which stores performers in TMCL.
	KeyPerformer = taglib.Performer
)
