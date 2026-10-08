package library

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/config"
)

type ParsedBookRelease struct {
	Title      string
	Year       uint16
	Format     string // one of config.EbookFormats ∪ config.AudiobookFormats
	Kind       string // "ebook" | "audiobook" | "" when undetectable
	Collection bool
}

var (
	ebookFormatRe = regexp.MustCompile(`(?i)\b(EPUB|AZW3?|MOBI|PDF)\b`)
	m4bRe         = regexp.MustCompile(`(?i)\bM4B\b`)
	mp3Re         = regexp.MustCompile(`(?i)\bMP3\b`)
	// A bare MP3 is far more often music than a book, so mp3 only counts
	// as an audiobook when the name says so.
	audiobookContextRe = regexp.MustCompile(
		`(?i)\b(audiobook|unabridged|narrated)\b`,
	)
	// \b binds each word alternative separately; the year range has no word
	// boundary inside its parentheses to anchor on.
	collectionRe = regexp.MustCompile(
		`(?i)\b(?:collection|anthology|complete works|boxset|box set)\b|\(\d{4}-\d{4}\)`,
	)
)

// ParseBookRelease classifies a release name into a book format tier. A name
// with no recognisable format stays Format "other" with Kind "".
func ParseBookRelease(name string) ParsedBookRelease {
	p := ParsedBookRelease{
		Title:      name,
		Format:     "other",
		Collection: collectionRe.MatchString(name),
	}
	if m := yearRe.FindString(name); m != "" {
		if y, err := strconv.ParseUint(m, 10, 16); err == nil {
			p.Year = uint16(y)
		}
	}

	switch {
	case ebookFormatRe.MatchString(name):
		f := strings.ToLower(ebookFormatRe.FindString(name))
		if f == "azw" {
			f = "azw3"
		}
		p.Format, p.Kind = f, "ebook"
	case m4bRe.MatchString(name):
		p.Format, p.Kind = "m4b", "audiobook"
	case mp3Re.MatchString(name) && audiobookContextRe.MatchString(name):
		p.Format, p.Kind = "mp3", "audiobook"
	}
	return p
}

// scoreFormat is -1 for a release of the other family or a format outside
// the profile; an undetectable release (Kind "") therefore never scores,
// even where "other" is listed.
func scoreFormat(p ParsedBookRelease, kind string, formats []string) int {
	if p.Kind != kind {
		return -1
	}
	i := slices.Index(formats, p.Format)
	if i < 0 {
		return -1
	}
	return (len(formats) - i) * 100
}

func ScoreEbookRelease(
	p ParsedBookRelease,
	profile config.EbookQualityProfileEntry,
) int {
	return scoreFormat(p, "ebook", profile.Formats)
}

func ScoreAudiobookRelease(
	p ParsedBookRelease,
	profile config.AudiobookQualityProfileEntry,
) int {
	return scoreFormat(p, "audiobook", profile.Formats)
}
