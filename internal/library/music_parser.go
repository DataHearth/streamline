package library

import (
	"regexp"
	"slices"
	"strconv"

	"github.com/datahearth/streamline/internal/config"
)

var (
	musicFlac24Re = regexp.MustCompile(`(?i)24.?bit|FLAC.?24`)
	musicFlacRe   = regexp.MustCompile(`(?i)\bFLAC\b|\blossless\b`)
	musicV0Re     = regexp.MustCompile(`(?i)\bV0\b`)
	musicDiscoRe  = regexp.MustCompile(
		`(?i)\bdiscograph|complete collection|anthology\b|\(\d{4}-\d{4}\)`,
	)
	musicMP3320Re = musicBitrateRe("320")
	musicMP3256Re = musicBitrateRe("256")
	musicMP3192Re = musicBitrateRe("192")
)

func musicBitrateRe(rate string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?i)\b(?:mp3|cbr)\b\W*` + rate + `\b|\b` + rate + `\W*(?:kbps\W*)?(?:mp3|cbr)\b|[\[(]\s*` + rate + `\s*(?:kbps)?\s*[\])]`,
	)
}

type ParsedMusicRelease struct {
	Title       string
	Year        uint16
	Format      string
	Discography bool
}

// ParseMusicRelease classifies a release name into a music format tier.
// Order matters: flac-24 before flac, and a bare "320"/"256"/"192" only counts
// with MP3/CBR context or inside a bracket, so year-like and bitrate-like
// tokens elsewhere in the name never match.
func ParseMusicRelease(name string) ParsedMusicRelease {
	p := ParsedMusicRelease{
		Title:       name,
		Format:      "other",
		Discography: musicDiscoRe.MatchString(name),
	}
	if m := yearRe.FindString(name); m != "" {
		if y, err := strconv.ParseUint(m, 10, 16); err == nil {
			p.Year = uint16(y)
		}
	}
	switch {
	case musicFlac24Re.MatchString(name):
		p.Format = "flac-24"
	case musicFlacRe.MatchString(name):
		p.Format = "flac"
	case musicMP3320Re.MatchString(name):
		p.Format = "mp3-320"
	case musicV0Re.MatchString(name):
		p.Format = "mp3-v0"
	case musicMP3256Re.MatchString(name):
		p.Format = "mp3-256"
	case musicMP3192Re.MatchString(name):
		p.Format = "mp3-192"
	}
	return p
}

// ScoreMusicRelease returns a sortable score: higher is better, -1 when the
// release's format is not in the profile (rejected).
func ScoreMusicRelease(
	p ParsedMusicRelease,
	profile config.MusicQualityProfileEntry,
) int {
	idx := slices.Index(profile.Formats, p.Format)
	if idx < 0 {
		return -1
	}
	return (len(profile.Formats) - idx) * 100
}
