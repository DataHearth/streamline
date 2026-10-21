package library

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/ent"
)

// The discography-pack folder rules, shared by the pack importer and the
// release-files preview so the preview names the albums the import would.
var (
	packYearRe  = regexp.MustCompile(`\b((?:19|20)\d{2})\b`)
	packGroupRe = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	packDiscRe  = regexp.MustCompile(`(?i)^(?:cd|dis[ck])\s*\d+$`)
	packDelimRe = regexp.MustCompile(`[._]+`)
)

const packPartsSep = " - "

// PackAlbumDir is the folder a file's album lives in: a disc subfolder (CD1,
// Disc 2) belongs to the folder holding the discs.
func PackAlbumDir(file string) string {
	dir := filepath.Dir(file)
	if packDiscRe.MatchString(strings.TrimSpace(filepath.Base(dir))) {
		return filepath.Dir(dir)
	}
	return dir
}

// MatchPackDir finds the album a pack folder holds among albums, by its folded
// title and, when the folder names a four-digit year, by that year. No
// provider is asked. An ambiguous folder (two albums of one title and no year
// to tell them apart) matches nothing.
func MatchPackDir(dir string, albums []*ent.Album) *ent.Album {
	base := filepath.Base(dir)
	var year int
	if m := packYearRe.FindString(base); m != "" {
		year, _ = strconv.Atoi(m)
	}
	clean := strings.TrimSpace(packGroupRe.ReplaceAllString(
		packDelimRe.ReplaceAllString(base, " "), " "))
	candidates := append([]string{clean}, strings.Split(clean, packPartsSep)...)

	var matches []*ent.Album
	for _, a := range albums {
		for _, c := range candidates {
			if TitleMatchesStrict(strings.TrimSpace(c), a.Title) {
				matches = append(matches, a)
				break
			}
		}
	}
	if len(matches) > 1 && year != 0 {
		matches = slices.DeleteFunc(matches, func(a *ent.Album) bool {
			return a.ReleaseDate == nil || a.ReleaseDate.Year() != year
		})
	}
	if len(matches) != 1 {
		return nil
	}
	return matches[0]
}
