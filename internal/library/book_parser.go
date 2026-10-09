package library

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
)

type ParsedBookRelease struct {
	Title string
	Year  uint16
	// Format is upper case and one of config.EbookFormats or
	// config.AudiobookFormats; empty when the name states none.
	Format string
	Kind   string // "ebook" | "audiobook" | "" when undetectable
	// BitrateKbps is the audiobook rate the name states ("64k", "128kbps"), 0
	// when it states none.
	BitrateKbps uint32
	Collection  bool
}

var (
	ebookFormatRe = regexp.MustCompile(`(?i)\b(EPUB|AZW3?|MOBI|PDF|CBZ)\b`)
	cbrRe         = regexp.MustCompile(`(?i)\bCBR\b`)
	// CBR is also "constant bit rate"; either of these beside it says so.
	audioTokenRe = regexp.MustCompile(`(?i)\b(MP3|M4B|M4A|FLAC)\b`)
	cbrRateRe    = regexp.MustCompile(`(?i)\bCBR\W*\d{2,3}`)
	m4bRe        = regexp.MustCompile(`(?i)\bM4B\b`)
	// A bare MP3, M4A or FLAC is far more often music than a book, so they
	// only count as an audiobook when the name says so.
	audioFormatRe      = regexp.MustCompile(`(?i)\b(MP3|M4A|FLAC)\b`)
	audiobookContextRe = regexp.MustCompile(
		`(?i)\b(audiobook|unabridged|narrated)\b`,
	)
	bookBitrateRe = regexp.MustCompile(`(?i)\b(\d{2,3})\s?k(?:bps)?\b`)
	// \b binds each word alternative separately; the year range has no word
	// boundary inside its parentheses to anchor on.
	collectionRe = regexp.MustCompile(
		`(?i)\b(?:collection|anthology|complete works|boxset|box set)\b|\(\d{4}-\d{4}\)`,
	)
)

// ParseBookRelease classifies a release name into a book format. A name with
// no recognisable format stays Format "" with Kind "".
func ParseBookRelease(name string) ParsedBookRelease {
	p := ParsedBookRelease{
		Title:      name,
		Collection: collectionRe.MatchString(name),
	}
	if m := yearRe.FindString(name); m != "" {
		if y, err := strconv.ParseUint(m, 10, 16); err == nil {
			p.Year = uint16(y)
		}
	}

	switch {
	case ebookFormatRe.MatchString(name):
		f := strings.ToUpper(ebookFormatRe.FindString(name))
		if f == "AZW" {
			f = "AZW3"
		}
		p.Format, p.Kind = f, "ebook"
	case m4bRe.MatchString(name):
		p.Format, p.Kind = "M4B", "audiobook"
	case audioFormatRe.MatchString(name) && audiobookContextRe.MatchString(name):
		p.Format = strings.ToUpper(audioFormatRe.FindString(name))
		p.Kind = "audiobook"
	case cbrRe.MatchString(name) && !audioTokenRe.MatchString(name) &&
		!cbrRateRe.MatchString(name):
		p.Format, p.Kind = "CBR", "ebook"
	}
	if p.Kind == "audiobook" {
		if m := bookBitrateRe.FindStringSubmatch(name); m != nil {
			if n, err := strconv.ParseUint(m[1], 10, 32); err == nil {
				p.BitrateKbps = uint32(n)
			}
		}
	}
	return p
}

// JudgeEbookRelease scores a release against a profile's ebook slot and, when
// it scores -1, says why.
func JudgeEbookRelease(
	p ParsedBookRelease,
	profile config.BookQualityProfileEntry,
) (int, string) {
	if p.Kind != "ebook" {
		return -1, "not an ebook"
	}
	score := quality.ScoreEbook(p.Format, profile.EbookProfile())
	if score < 0 {
		return -1, fmt.Sprintf("%s is not in the profile", p.Format)
	}
	return score, ""
}

// JudgeAudiobookRelease scores a release against a profile's audiobook slot.
// A stated rate under the floor is rejected; an unstated one is accepted,
// since the name cannot always say and the importer measures the real one.
func JudgeAudiobookRelease(
	p ParsedBookRelease,
	profile config.BookQualityProfileEntry,
) (int, string) {
	if p.Kind != "audiobook" {
		return -1, "not an audiobook"
	}
	slot := profile.AudiobookProfile()
	score := quality.ScoreAudiobook(p.Format, p.BitrateKbps, slot)
	if score >= 0 {
		return score, ""
	}
	if quality.ScoreAudiobook(p.Format, 0, slot) >= 0 {
		return -1, fmt.Sprintf(
			"%d kbps is below the profile's minimum of %d",
			p.BitrateKbps, slot.MinBitrate,
		)
	}
	return -1, fmt.Sprintf("%s is not in the profile", p.Format)
}

// ScoreEbookRelease is -1 for a release of the other family or a format
// outside the profile; an undetectable release (Kind "") therefore never
// scores.
func ScoreEbookRelease(
	p ParsedBookRelease,
	profile config.BookQualityProfileEntry,
) int {
	score, _ := JudgeEbookRelease(p, profile)
	return score
}

func ScoreAudiobookRelease(
	p ParsedBookRelease,
	profile config.BookQualityProfileEntry,
) int {
	score, _ := JudgeAudiobookRelease(p, profile)
	return score
}
