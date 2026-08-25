package qualityctx

import (
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
)

func ContextFromRelease(
	title string,
	size int64,
	seeders uint32,
) quality.ReleaseContext {
	p := library.Parse(title)
	return quality.ReleaseContext{
		Title: title, Size: size,
		Seeders: int(seeders), HasSeeders: true,
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
