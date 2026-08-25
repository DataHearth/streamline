package qualityctx

import (
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
)

// ContextFromRelease scores an indexer result. Torznab leaves Seeders at 0
// when the attribute is absent, which is indistinguishable from a real zero,
// so unknown is reported as HasSeeders=false: a negated seeders condition
// would otherwise score every silent indexer's results as confirmed-low.
func ContextFromRelease(
	title string,
	size int64,
	seeders uint32,
) quality.ReleaseContext {
	p := library.Parse(title)
	return quality.ReleaseContext{
		Title: title, Size: size,
		Seeders: int(seeders), HasSeeders: seeders > 0,
		Resolution: p.Resolution, Source: p.Source,
		Group: p.Group, Codec: p.Codec,
	}
}

// ContextFromFile scores what is already on disk. Probe data (width,
// codec) wins over the filename parse when present; seeder conditions
// can never match a file.
func ContextFromFile(
	basename string,
	size int64,
	width int,
	codec string,
) quality.ReleaseContext {
	p := library.Parse(basename)
	r := quality.ReleaseContext{
		Title: basename, Size: size,
		Resolution: p.Resolution, Source: p.Source,
		Group: p.Group, Codec: p.Codec,
	}
	if w := quality.ResolutionFromWidth(width); w != "" {
		r.Resolution = w
	}
	if codec != "" {
		r.Codec = codec
	}
	return r
}
