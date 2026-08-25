package library

import "regexp"

// MediaExts is the set of file extensions treated as candidate media files.
// Extension comparison should be lowercase: callers must `strings.ToLower(filepath.Ext(path))` before lookup.
var MediaExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".wmv": true,
	".mov": true, ".m4v": true, ".ts": true, ".webm": true,
}

// SampleRe matches scene-release sample/preview clips so they can be filtered out.
// Word-boundary match skips `sample.mkv`, `Movie.Sample.mkv` without catching `samplesheet.mkv`.
var SampleRe = regexp.MustCompile(`(?i)\bsample\b`)

// MinMediaSize is the minimum file size to consider a candidate media file (50 MiB).
const MinMediaSize = 50 * 1024 * 1024

// MinEpisodeSize is MinMediaSize for episodes (5 MiB). A feature under 50 MiB
// is junk, but an episode need not be: Kaamelott's first four seasons are
// 3-minute shorts, and the movie floor hid 228 of a 248-file folder from the
// scanner — reported as a 20-file folder that then failed to match a single
// episode.
const MinEpisodeSize = 5 * 1024 * 1024
