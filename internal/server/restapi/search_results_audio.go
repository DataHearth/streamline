package restapi

import (
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
)

// toMusicSearchResult maps an album or artist-pack release to the shared
// SearchResult. The table prints Source verbatim, so it is the parsed display
// label ("FLAC 24/96", "MP3 V0"), not the raw token; audio_tier is the parsed
// tier, omitted when the name does not state one. resolution and codec are the
// video parser's and mean nothing here, matched_formats is always empty. A
// score of -1 flags the release rejected and carries the reason instead of
// dropping it.
func toMusicSearchResult(
	r indexer.SearchResult,
	p library.ParsedMusicRelease,
	score int,
	reason string,
) SearchResult {
	item := toSearchResult(r)
	item.Resolution, item.Codec, item.Source = nil, nil, nil
	if p.Source != "" {
		src := p.Source
		item.Source = &src
	}
	if p.TierKnown {
		tier := MusicTier(p.Tier.String())
		item.AudioTier = &tier
	}
	return withVerdict(item, score, reason)
}

// toBookSearchResult maps an ebook or audiobook release to the shared
// SearchResult: Source is the upper-case container, Slot the slot that
// container fills, and bitrate_kbps the rate an audiobook name states.
func toBookSearchResult(
	r indexer.SearchResult,
	p library.ParsedBookRelease,
	score int,
	reason string,
) SearchResult {
	item := toSearchResult(r)
	item.Resolution, item.Codec, item.Source = nil, nil, nil
	if p.Format != "" {
		src := p.Format
		item.Source = &src
	}
	switch p.Kind {
	case "ebook":
		slot := BookFormatEbook
		item.Slot = &slot
	case "audiobook":
		slot := BookFormatAudiobook
		item.Slot = &slot
		if p.BitrateKbps > 0 {
			kbps := p.BitrateKbps
			item.BitrateKbps = &kbps
		}
	}
	return withVerdict(item, score, reason)
}

func withVerdict(item SearchResult, score int, reason string) SearchResult {
	matched := []string{}
	item.MatchedFormats = &matched
	if score >= 0 {
		item.Score = &score
		return item
	}
	rejected := true
	item.Rejected = &rejected
	zero := 0
	item.Score = &zero
	if reason != "" {
		item.RejectReason = &reason
	}
	return item
}
