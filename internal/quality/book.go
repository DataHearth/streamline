package quality

import "slices"

// The canonical format ladders, best first, upper case everywhere: config,
// wire and MediaFile.quality.
var (
	EbookLadder     = []string{"EPUB", "AZW3", "MOBI", "PDF", "CBZ", "CBR"}
	AudiobookLadder = []string{"M4B", "MP3", "M4A", "FLAC"}
)

// EbookProfile is the ebook slot of a book profile over plain values.
type EbookProfile struct {
	Formats        []string
	Preferred      string
	UpgradeAllowed bool
}

// AudiobookProfile is the audiobook slot. MinBitrate is in kbps, 0 for no
// floor.
type AudiobookProfile struct {
	Formats        []string
	Preferred      string
	MinBitrate     uint16
	UpgradeAllowed bool
}

// ladderScore ranks format best-first over its ladder, restricted to the
// ticked formats; -1 when it is not ticked or not on the ladder.
func ladderScore(format string, formats, ladder []string) int {
	idx := slices.Index(ladder, format)
	if idx < 0 || !slices.Contains(formats, format) {
		return -1
	}
	return (len(ladder) - idx) * 100
}

// ScoreEbook is -1 for a format the profile does not tick.
func ScoreEbook(format string, p EbookProfile) int {
	return ladderScore(format, p.Formats, EbookLadder)
}

// maxAudiobookBonus keeps the bit-rate bonus below the 100 points that
// separate two formats.
const maxAudiobookBonus = 99

// ScoreAudiobook is -1 for a format the profile does not tick and for a
// stated bit rate under the floor. An unstated rate (0) is accepted: a release
// name cannot always say, and the importer measures the real one.
func ScoreAudiobook(format string, kbps uint32, p AudiobookProfile) int {
	s := ladderScore(format, p.Formats, AudiobookLadder)
	if s < 0 {
		return -1
	}
	if kbps > 0 && kbps < uint32(p.MinBitrate) {
		return -1
	}
	return s + int(min(kbps/10, maxAudiobookBonus))
}

// EbookReplaces is the single predicate deciding whether an incoming ebook
// replaces the held one: never when the profile forbids upgrades or the held
// format is off the ladder, never once it has reached Preferred, otherwise
// when the incoming format is ticked and better.
func EbookReplaces(p EbookProfile, have, incoming string) bool {
	return ladderReplaces(
		p.UpgradeAllowed, p.Formats, p.Preferred, have, incoming, EbookLadder,
	)
}

// AudiobookReplaces is EbookReplaces over the audiobook ladder, and also true
// when the held folder's measured rate is known and under the floor while the
// incoming release is acceptable.
func AudiobookReplaces(
	p AudiobookProfile,
	have string,
	haveKbps uint32,
	incoming string,
	incomingKbps uint32,
) bool {
	if !p.UpgradeAllowed {
		return false
	}
	if ScoreAudiobook(incoming, incomingKbps, p) < 0 {
		return false
	}
	if haveKbps > 0 && haveKbps < uint32(p.MinBitrate) {
		return true
	}
	return ladderReplaces(
		p.UpgradeAllowed, p.Formats, p.Preferred, have, incoming, AudiobookLadder,
	)
}

func ladderReplaces(
	upgradeAllowed bool,
	formats []string,
	preferred, have, incoming string,
	ladder []string,
) bool {
	haveIdx := slices.Index(ladder, have)
	if !upgradeAllowed || haveIdx < 0 {
		return false
	}
	if haveIdx <= slices.Index(ladder, preferred) {
		return false
	}
	inIdx := slices.Index(ladder, incoming)
	return inIdx >= 0 && slices.Contains(formats, incoming) && inIdx < haveIdx
}
