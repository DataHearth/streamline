package qualityctx

import (
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
)

// ContextFromRelease scores an indexer result. Torznab leaves Seeders at 0
// when the attribute is absent, which is indistinguishable from a real zero,
// so unknown is reported as HasSeeders=false: a negated seeders condition
// would otherwise score every silent indexer's results as confirmed-low.
//
// episodes is how many episodes the release carries and scales every size
// bound. Pass 1 for a movie or a single episode, and the season's (or the
// show's) episode count for a pack; 0 when the caller genuinely cannot say,
// which the engine reads as an unscaled bound.
func ContextFromRelease(
	title string,
	size int64,
	seeders uint32,
	episodes int,
) quality.ReleaseContext {
	p := library.Parse(title)
	return quality.ReleaseContext{
		Title: title, Size: size,
		Seeders: int(seeders), HasSeeders: seeders > 0,
		Resolution: p.Resolution, Source: p.Source,
		Group: p.Group, Codec: p.Codec,
		EpisodeCount: episodes,
	}
}

// ContextFromPackFile scores one file *inside* a release against the release
// it arrived in. A basename alone routinely omits what the release name
// states — the source, the group, and anything a `release_title` condition
// matches — so the release title is folded in for whatever the basename does
// not already say, never replacing what it does. width/codec come from a probe
// when there is one and are ignored at zero, exactly as ContextFromFile.
func ContextFromPackFile(
	basename string,
	size int64,
	width int,
	codec string,
	releaseTitle string,
) quality.ReleaseContext {
	r := ContextFromFile(basename, size, width, codec)
	fromTitle := library.Parse(releaseTitle)
	if r.Source == "" {
		r.Source = fromTitle.Source
	}
	if r.Group == "" {
		r.Group = fromTitle.Group
	}
	// release_title conditions match the whole raw string, so there is no
	// single field to test for "does the basename carry it" — appending the
	// release title adds whatever the basename lacks without ever dropping
	// what the basename itself already states.
	r.Title += " " + releaseTitle
	return r
}

// Replaces is the whole per-file verdict behind replace_mode "upgrades":
// whether incoming should take the place of existing under p. Rejecting the
// incoming release first is the part quality.ReplacesFile cannot do on its
// own — Evaluate reports 0 both for "matched no format" and for "rejected
// outright" (above preferred_resolution, say, which a probe can reveal even
// when the release claimed lower), and a bare 0 beats any "never grab this"
// negative score. The scanner never reaches that case because it pre-rejects,
// so the check lives here for the callers that do not.
//
// Shared by the importer, which decides this per file at import time, and by
// the pending-proposal preview, which has to promise the same answer before
// the operator commits. Two copies of it would drift.
func Replaces(p quality.Profile, existing, incoming quality.ReleaseContext) bool {
	if quality.Evaluate(p, incoming).Rejected {
		return false
	}
	return quality.ReplacesFile(p, existing, incoming)
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
