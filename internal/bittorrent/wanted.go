package bittorrent

import (
	antorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/types"
)

// wantedBytes is the completion/progress/ratio denominator: the torrent's
// length restricted to files being downloaded. With nothing skipped it equals
// t.Length(), so full-torrent behaviour needs no special case (spec §3.2 —
// anacrolix's BytesMissing counts skipped pieces and never reaches zero).
func wantedBytes(t *antorrent.Torrent) int64 {
	var n int64
	for _, f := range t.Files() {
		if f.Priority() != types.PiecePriorityNone {
			n += f.Length()
		}
	}
	return n
}

// wantedCompleted is wantedBytes' completed counterpart: the same file set,
// summing what has actually been downloaded rather than each file's length.
func wantedCompleted(t *antorrent.Torrent) int64 {
	var n int64
	for _, f := range t.Files() {
		if f.Priority() != types.PiecePriorityNone {
			n += f.BytesCompleted()
		}
	}
	return n
}

// wantedMissing is anacrolix's BytesMissing scoped to wanted files: unlike
// BytesMissing itself it reaches zero once every wanted file is complete,
// regardless of what a skip left undownloaded.
func wantedMissing(t *antorrent.Torrent) int64 {
	return wantedBytes(t) - wantedCompleted(t)
}
