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

// checkingStored reports whether the torrent is hashing data it found on disk
// rather than data a peer just sent. Opening a torrent sets every complete
// piece of an unfinished file back to unknown (anacrolix's
// setCompletionFromPartFiles), so a partial download hashes everything it
// holds again on each boot. A piece received from a peer is hashed with its
// completion already known (Ok, not Complete), which is what tells the two
// apart.
func checkingStored(t *antorrent.Torrent) bool {
	for _, run := range t.PieceStateRuns() {
		if run.Checking && !run.Ok {
			return true
		}
	}
	return false
}

// wantedVerified reports whether every piece a wanted file spans has passed
// its hash check. wantedMissing reaching zero does not say that: anacrolix's
// File.BytesCompleted counts dirty chunks — written, not yet hashed — as
// completed (fileBytesLeft in file.go), so the last pieces can still be queued
// for hash, mid-hash, or failing it. A piece only reads Complete once
// MarkComplete has returned, which is also what persists it and promotes a
// finished file off its .part name; a client closed before then drops the
// hash result, and the next boot finds that piece still stored incomplete.
func wantedVerified(t *antorrent.Torrent) bool {
	type span struct{ begin, end int }
	var wanted []span
	for _, f := range t.Files() {
		if f.Priority() != types.PiecePriorityNone {
			wanted = append(wanted, span{f.BeginPieceIndex(), f.EndPieceIndex()})
		}
	}
	begin := 0
	for _, run := range t.PieceStateRuns() {
		end := begin + run.Length
		if !run.Complete {
			for _, w := range wanted {
				if w.begin < end && begin < w.end {
					return false
				}
			}
		}
		begin = end
	}
	return true
}
