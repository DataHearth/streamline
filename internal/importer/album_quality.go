package importer

import (
	"context"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/quality"
)

// albumFileQuality is what the importer could establish about one source file.
// probed says the facts were measured rather than read off the extension or the
// release name: only a measurement may hold an import, since a claim cannot
// contradict itself.
type albumFileQuality struct {
	tier   quality.AudioTier
	known  bool
	probed bool
}

// assessAlbumFile classifies one source file: measured when ffprobe is
// available and answers, otherwise read off the extension (a lossless one is
// lossless, never hi-res) or, for a lossy file, off what the grabbed release's
// name claimed.
func (w *Worker) assessAlbumFile(
	ctx context.Context,
	path string,
	claim library.ParsedMusicRelease,
) albumFileQuality {
	if config.Get().FFmpeg.Enabled && w.probe != nil && w.probe.Available() {
		info, err := w.probe.ProbeAudio(ctx, path)
		if err == nil {
			t, ok := quality.AudioTierOf(library.AudioFactsFromProbe(info))
			return albumFileQuality{tier: t, known: ok, probed: true}
		}
		slog.WarnContext(ctx, "album import: probe failed, tier not verified",
			"file", filepath.Base(path), "error", err)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".flac":
		return albumFileQuality{tier: quality.TierLossless, known: true}
	}
	if claim.TierKnown && !claim.Facts.Lossless {
		return albumFileQuality{tier: claim.Tier, known: true}
	}
	return albumFileQuality{}
}

// albumHoldReasons lists the files tagged as another album. Tracks match by
// tagged number first, so a download of the wrong album fills the right
// album's track slots one for one and would be placed without this. An
// untagged album is no evidence either way.
func albumHoldReasons(plan []albumFile, alb *ent.Album) []schema.HoldReason {
	var out []schema.HoldReason
	for _, f := range plan {
		if f.info.Album == "" ||
			library.AlbumTitleNamed(f.info.Album, alb) {
			continue
		}
		out = append(out, schema.HoldReason{
			File:     f.path,
			Check:    "album",
			Expected: alb.Title,
			Actual:   f.info.Album,
		})
	}
	return out
}

// trackHoldReasons lists the files whose title contradicts the track they were
// matched to: another edition's bonus tracks land on the album's by number, and
// the tags would then call one recording another's name. everyFile lists every
// title that differs, a script the comparison cannot read included, for a
// person to answer. Without it, it speaks only when at least half the comparable
// titles agree, since a mostly different list is a translated release whose
// numbers are all there is to go on.
func trackHoldReasons(plan []albumFile, everyFile bool) []schema.HoldReason {
	var (
		out             []schema.HoldReason
		agree, compared int
	)
	for _, f := range plan {
		if !library.TitleComparable(f.info.Title) ||
			!library.TitleComparable(f.tr.Title) {
			title := strings.TrimSpace(f.info.Title)
			if everyFile && title != "" &&
				!strings.EqualFold(title, strings.TrimSpace(f.tr.Title)) {
				out = append(out, schema.HoldReason{
					File:     f.path,
					Check:    "track",
					Expected: f.tr.Title,
					Actual:   f.info.Title,
				})
			}
			continue
		}
		compared++
		if library.TrackTitleAgrees(f.tr, f.info.Title) {
			agree++
			continue
		}
		out = append(out, schema.HoldReason{
			File:     f.path,
			Check:    "track",
			Expected: f.tr.Title,
			Actual:   f.info.Title,
		})
	}
	if !everyFile && 2*agree < compared {
		return nil
	}
	return out
}

// missingHoldReasons says when the import would leave some of the album's tracks
// with no file. The reason belongs to the album, so it names no file: the
// release may be another edition, and only a person can tell if that is fine.
func missingHoldReasons(plan []albumFile, alb *ent.Album) []schema.HoldReason {
	filled := make(map[uint32]struct{}, len(plan))
	for _, f := range plan {
		filled[f.tr.ID] = struct{}{}
	}
	empty := 0
	for _, t := range alb.Edges.Tracks {
		if _, ok := filled[t.ID]; !ok && len(t.Edges.MediaFiles) == 0 {
			empty++
		}
	}
	if empty == 0 {
		return nil
	}
	total := len(alb.Edges.Tracks)
	return []schema.HoldReason{{
		Check:    "missing",
		Expected: strconv.Itoa(total),
		Actual:   strconv.Itoa(total - empty),
	}}
}

// tierHoldReasons lists the measured files whose tier the artist's profile
// does not tick: a "FLAC" that is a 128k transcode, or a 24-bit pack under a
// profile that left hi-res unticked.
func tierHoldReasons(
	plan []albumFile,
	profile config.MusicQualityProfileEntry,
) []schema.HoldReason {
	mp := profile.Profile()
	var out []schema.HoldReason
	for _, f := range plan {
		if !f.q.probed || !f.q.known || mp.Accepts(f.q.tier) {
			continue
		}
		out = append(out, schema.HoldReason{
			File:     f.path,
			Check:    "tier",
			Expected: strings.Join(profile.Tiers, "/"),
			Actual:   f.q.tier.String(),
		})
	}
	return out
}

// heldTrackTier is the tier of a track's current file, known only when every
// file the track holds names a tier: one unreadable file makes the track
// neither an upgrade target nor an obstacle.
func heldTrackTier(tr *ent.Track) (quality.AudioTier, bool) {
	var (
		worst quality.AudioTier
		first = true
	)
	for _, mf := range tr.Edges.MediaFiles {
		t, ok := quality.ParseAudioTier(mf.Quality)
		if !ok {
			return 0, false
		}
		if first || t < worst {
			worst, first = t, false
		}
	}
	return worst, !first
}
