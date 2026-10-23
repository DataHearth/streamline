package library

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
)

// MusicScope is what a music search is for: a discography pack is rejected
// for one album and accepted for the artist-level search.
type MusicScope uint8

const (
	MusicScopeAlbum MusicScope = iota
	MusicScopeArtist
)

var (
	musicBitDepthRe = regexp.MustCompile(
		`(?i)\b24[\s._-]?bit|FLAC[\s._-]?24\b|\b24/\d{2,3}(?:\.\d)?\b|hi-?res`,
	)
	musicRateSlashRe = regexp.MustCompile(`(?i)\b24/(\d{2,3}(?:\.\d)?)\b`)
	musicRateKHzRe   = regexp.MustCompile(`(?i)(\d{2,3}(?:\.\d)?)\s?kHz`)
	musicLosslessRe  = regexp.MustCompile(
		`(?i)\b(FLAC|ALAC|WAV|AIFF|WV|APE)\b`,
	)
	musicLosslessWordRe = regexp.MustCompile(`(?i)\blossless\b`)
	musicLossyRe        = regexp.MustCompile(`(?i)\b(MP3|AAC|M4A|OGG|OPUS)\b`)
	musicVBRRe          = regexp.MustCompile(`(?i)\bV([0-2])\b`)
	musicDiscoRe        = regexp.MustCompile(
		`(?i)\bdiscograph|complete collection|\banthology\b|\b(?:19|20)\d{2}\s?[-–]\s?(?:19|20)\d{2}\b`,
	)
	// A bare number is never a bit rate; it needs a codec or CBR next to it, a
	// kbps unit, or brackets, so year-like and group-name tokens never match.
	musicRateRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:mp3|aac|m4a|ogg|opus|cbr)\b\W*(\d{2,3})\b`),
		regexp.MustCompile(
			`(?i)\b(\d{2,3})\W*(?:kbps\W*)?(?:mp3|aac|m4a|ogg|opus|cbr)\b`,
		),
		regexp.MustCompile(`(?i)[\[(]\s*(\d{2,3})\s*(?:kbps)?\s*[\])]`),
		regexp.MustCompile(`(?i)\b(\d{2,3})\s?kbps\b`),
	}
)

var musicLossyRates = []uint32{320, 256, 224, 192, 160, 128, 96, 64}

// ParsedMusicRelease is what a release name states about a music release.
// Tier is meaningful only when TierKnown; Source is the display label the
// manual-search table prints verbatim ("FLAC 24/96", "MP3 V0"), empty when
// nothing could be read.
type ParsedMusicRelease struct {
	Title       string
	Year        uint16
	Facts       quality.AudioFacts
	Tier        quality.AudioTier
	TierKnown   bool
	Source      string
	Discography bool
}

// ParseMusicRelease reads codec, bit depth, sample rate, bit rate and VBR
// preset from a release name and classifies them with quality.AudioTierOf.
func ParseMusicRelease(name string) ParsedMusicRelease {
	p := ParsedMusicRelease{
		Title:       name,
		Discography: musicDiscoRe.MatchString(name),
	}
	if m := yearRe.FindString(name); m != "" {
		if y, err := strconv.ParseUint(m, 10, 16); err == nil {
			p.Year = uint16(y)
		}
	}

	f := &p.Facts
	if m := musicLosslessRe.FindString(name); m != "" {
		f.Codec, f.Lossless = strings.ToLower(m), true
	} else if musicLosslessWordRe.MatchString(name) {
		f.Codec, f.Lossless = "flac", true
	}
	if f.Lossless {
		if musicBitDepthRe.MatchString(name) {
			f.BitDepth = 24
			f.SampleRateHz = musicSampleRate(name)
		}
	} else {
		parseLossy(name, f)
	}

	if t, ok := quality.AudioTierOf(*f); ok {
		p.Tier, p.TierKnown = t, true
		p.Source = musicSourceLabel(*f)
	}
	return p
}

func parseLossy(name string, f *quality.AudioFacts) {
	if m := musicLossyRe.FindString(name); m != "" {
		f.Codec = strings.ToLower(m)
		if f.Codec == "m4a" {
			f.Codec = "aac"
		}
	}
	if m := musicVBRRe.FindStringSubmatch(name); m != nil {
		f.VBR = "V" + m[1]
		if f.Codec == "" {
			f.Codec = "mp3"
		}
		return
	}
	for _, re := range musicRateRes {
		for _, m := range re.FindAllStringSubmatch(name, -1) {
			n, err := strconv.ParseUint(m[1], 10, 32)
			if err != nil || !slices.Contains(musicLossyRates, uint32(n)) {
				continue
			}
			f.BitrateKbps = uint32(n)
			if f.Codec == "" {
				f.Codec = "mp3"
			}
			return
		}
	}
}

func musicSampleRate(name string) uint32 {
	for _, re := range []*regexp.Regexp{musicRateSlashRe, musicRateKHzRe} {
		m := re.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		khz, err := strconv.ParseFloat(m[1], 64)
		if err != nil || khz <= 0 {
			continue
		}
		return uint32(khz * 1000)
	}
	return 0
}

func musicSourceLabel(f quality.AudioFacts) string {
	codec := strings.ToUpper(f.Codec)
	if f.Lossless {
		if f.BitDepth < 24 {
			return codec
		}
		if f.SampleRateHz == 0 {
			return codec + " 24-bit"
		}
		return fmt.Sprintf("%s 24/%s", codec, formatKHz(f.SampleRateHz))
	}
	if f.VBR != "" {
		return codec + " " + f.VBR
	}
	return fmt.Sprintf("%s %d", codec, f.BitrateKbps)
}

func formatKHz(hz uint32) string {
	if hz%1000 == 0 {
		return strconv.FormatUint(uint64(hz/1000), 10)
	}
	return strconv.FormatFloat(float64(hz)/1000, 'f', 1, 64)
}

// JudgeMusicRelease scores a release against a profile and, when it scores -1,
// says why. A release whose quality the name does not state is rejected
// rather than guessed at: there is no catch-all tier.
func JudgeMusicRelease(
	p ParsedMusicRelease,
	profile config.MusicQualityProfileEntry,
	scope MusicScope,
) (int, string) {
	if !p.TierKnown {
		return -1, "quality is not stated in the release name"
	}
	mp := profile.Profile()
	score := mp.Score(p.Tier, quality.AudioFine(p.Facts))
	if score < 0 {
		return -1, "tier not in the profile"
	}
	if p.Discography && scope == MusicScopeAlbum {
		return -1, "discography pack, not a single album"
	}
	return score, ""
}

// ScoreMusicRelease returns a sortable score: higher is better, -1 when the
// release is rejected. JudgeMusicRelease also returns the reason.
func ScoreMusicRelease(
	p ParsedMusicRelease,
	profile config.MusicQualityProfileEntry,
	scope MusicScope,
) int {
	score, _ := JudgeMusicRelease(p, profile, scope)
	return score
}
