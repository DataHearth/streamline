package qualityctx

import (
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/internal/ffmpeg"

	"github.com/datahearth/streamline/ent"
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
	r := quality.ReleaseContext{
		Title: title, Size: size,
		Seeders: int(seeders), HasSeeders: seeders > 0,
		Resolution: p.Resolution, Source: p.Source,
		Group: p.Group, Codec: p.Codec,
		EpisodeCount: episodes,
	}
	applyStreamHints(&r, title)
	return r
}

// ContextFromPackFile scores one file *inside* a release against the release
// it arrived in. A basename alone routinely omits what the release name
// states — the source, the group, and anything a `release_title` condition
// matches — so the release title is folded in for whatever the basename does
// not already say, never replacing what it does. info comes from a probe when
// there is one, exactly as ContextFromFile; the release title additionally
// fills any stream field the probe left unknown, which is what keeps the
// pending-proposal preview — where the files are still in the client and
// nothing can be probed — answering vostfr and multi-audio the same way the
// import will.
func ContextFromPackFile(
	basename string,
	size int64,
	info *ffmpeg.Info,
	releaseTitle string,
) quality.ReleaseContext {
	r := ContextFromFile(basename, size, info)
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
	// Only fills what the probe left unknown, so a measurement always wins over
	// the name's claim. With no probe at all this is the only evidence there
	// is, which is the pending-proposal preview's whole situation.
	applyStreamHints(&r, releaseTitle)
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

// ContextFromRow scores a file already in the library, off the columns the
// importer stored rather than off its path.
//
// The path is the wrong input and was the one being used: the renamer wrote it
// from the naming template, and the default template keeps only the
// resolution, so re-parsing it reported no group and no source for every
// imported file. Against a profile scoring the idiomatic "release carries no
// group" format at -100, that made every file in the library score -100 for a
// group the row had recorded all along — and since ReplacesFile compares that
// number against a full release score, the RSS scanner read the whole library
// as upgradable.
//
// Probe data wins over the stored parse for the two things it can see
// directly, and is ignored at zero so an ffmpeg-disabled install falls back to
// what the release name claimed.
//
// Title stays the basename and EmptyIsUnknown makes every release_title
// condition read as unknown here, because nothing stores the release name: the
// naming template kept a fraction of its tokens. Blanking Title instead was
// tried and is worse — a file then scores none of remux/vostfr/multi-audio
// while the release replacing it scores all of them, so everything on disk
// becomes upgradable. Reporting them unknown is what lets ReplacesFile drop
// those formats from *both* sides instead. That alone once stopped upgrades
// happening at all, since the upgrade rules were written entirely in
// title-matched formats; it works now because the stream fields below answer
// the ones carrying the weight.
func ContextFromRow(f *ent.MediaFile) quality.ReleaseContext {
	r := quality.ReleaseContext{
		Title:          filepath.Base(f.Path),
		Size:           f.Size,
		Resolution:     f.ParsedResolution,
		Source:         f.ParsedSource,
		Group:          f.ReleaseGroup,
		Codec:          f.ParsedCodec,
		EmptyIsUnknown: true,
	}
	if w := quality.ResolutionFromWidth(int(f.Width)); w != "" {
		r.Resolution = w
	}
	if f.VideoCodec != "" {
		r.Codec = f.VideoCodec
	}
	// Gated on probed_at rather than on the values: an empty audio_langs is
	// both "the probe found no tagged track" and "nothing ever looked", and
	// only the first may be negated. probed_at is the one column that tells
	// them apart. A probe that failed stamps it too and stores nothing, which
	// is why the track count is read as a real zero only alongside it.
	if f.ProbedAt != nil {
		tracks := int(f.AudioTracks)
		r.AudioTracks = &tracks
		r.AudioLangs = splitLangs(f.AudioLangs)
		r.SubLangs = splitLangs(f.SubLangs)
	}
	return r
}

// splitLangs turns a stored comma list into the slice a language condition
// reads. Never nil: reaching here means the row was probed, so an empty column
// is the answer "no tagged track of any language", not an absence.
func splitLangs(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, ",")
}

// ContextFromFile scores a file by its name — a file inside a torrent, which
// has no row yet. For a file already in the library use ContextFromRow: its
// name has been through the renamer. Probe data (width, codec) wins over the
// filename parse when present; seeder conditions can never match a file.
func ContextFromFile(
	basename string,
	size int64,
	info *ffmpeg.Info,
) quality.ReleaseContext {
	p := library.Parse(basename)
	r := quality.ReleaseContext{
		Title: basename, Size: size,
		Resolution: p.Resolution, Source: p.Source,
		Group: p.Group, Codec: p.Codec,
	}
	if info == nil {
		return r
	}
	if w := quality.ResolutionFromWidth(int(info.Width)); w != "" {
		r.Resolution = w
	}
	if info.VideoCodec != "" {
		r.Codec = info.VideoCodec
	}
	tracks := int(info.AudioTracks)
	r.AudioTracks = &tracks
	r.AudioLangs = splitLangs(info.AudioLangs)
	r.SubLangs = splitLangs(info.SubLangs)
	return r
}
