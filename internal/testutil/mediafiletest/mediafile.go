package mediafiletest

import (
	"path/filepath"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library"
)

// StoredParse fills a MediaFile fixture's parsed_* and release_group columns
// from releaseName, the way db.applyParsed fills them at import.
//
// A fixture that only sets Path describes a row no import produces: quality
// scoring reads the columns, because the path has been through the renamer and
// the naming template keeps a fraction of the release name's tokens. Fixtures
// that name a release in Path still need this — the row is what the scorer
// sees, and it has to be filled the way an import fills it.
func StoredParse(f *ent.MediaFile, releaseName string) *ent.MediaFile {
	p := library.Parse(filepath.Base(releaseName))
	f.ParsedSource = p.Source
	f.ParsedResolution = p.Resolution
	f.ParsedCodec = p.Codec
	if f.ReleaseGroup == "" {
		f.ReleaseGroup = p.Group
	}
	return f
}
