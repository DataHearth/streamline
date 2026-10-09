package book

import (
	"time"

	entbook "github.com/datahearth/streamline/ent/book"

	"github.com/datahearth/streamline/ent"
)

// The derived states below are the same rules the shelf's SQL applies; the two
// must agree or an item appears under a filter its page contradicts.

// Slot states, as the wire spells them.
const (
	StateAvailable   = "available"
	StateWanted      = "wanted"
	StateDownloading = "downloading"
	StateUnmonitored = "unmonitored"

	VolumeUpcoming = "upcoming"
)

// SlotState derives a slot's state. A slot is unmonitored when it is not
// monitored and neither available nor downloading; otherwise available and
// downloading map across (a paused record is still in flight) and a monitored
// wanted or skipped slot is wanted.
func SlotState(b *ent.Book, kind string) string {
	status, monitored := slotStatus(b, kind), slotMonitored(b, kind)
	switch {
	case status == string(entbook.EbookStatusDownloading) ||
		(status == string(entbook.EbookStatusPaused) && monitored):
		return StateDownloading
	case monitored && (status == string(entbook.EbookStatusWanted) ||
		status == string(entbook.EbookStatusSkipped)):
		return StateWanted
	case status == string(entbook.EbookStatusAvailable):
		return StateAvailable
	}
	return StateUnmonitored
}

// Status derives a book's status over the slots whose state is not
// unmonitored: any downloading gives downloading, else any wanted gives
// wanted, else available, which is also what a book with every slot
// unmonitored, or not yet released, reports.
func Status(b *ent.Book, now time.Time) string {
	if unreleased(b, now) {
		return StateAvailable
	}
	ebook, audiobook := SlotState(b, slotEbook), SlotState(b, slotAudiobook)
	switch {
	case ebook == StateDownloading || audiobook == StateDownloading:
		return StateDownloading
	case ebook == StateWanted || audiobook == StateWanted:
		return StateWanted
	}
	return StateAvailable
}

// VolumeStatus is a volume's ebook-slot status, or upcoming while its release
// date is ahead. A stub reports wanted when its ebook slot is monitored.
func VolumeStatus(v *ent.Book, now time.Time) string {
	if unreleased(v, now) {
		return VolumeUpcoming
	}
	switch SlotState(v, slotEbook) {
	case StateDownloading:
		return StateDownloading
	case StateWanted:
		return StateWanted
	}
	return StateAvailable
}

// SeriesStatus derives a series' status over its hydrated, released volumes:
// any downloading gives downloading, else wanted when the series is monitored
// and any is wanted, else available.
func SeriesStatus(s *ent.BookSeries, now time.Time) string {
	wanted := false
	for _, v := range s.Edges.Volumes {
		if v.LastRefreshedAt == nil || unreleased(v, now) {
			continue
		}
		switch SlotState(v, slotEbook) {
		case StateDownloading:
			return StateDownloading
		case StateWanted:
			wanted = true
		}
	}
	if wanted && s.Monitor != "none" {
		return StateWanted
	}
	return StateAvailable
}

// Hydrating reports a series with a volume the hydration worker has not
// reached.
func Hydrating(s *ent.BookSeries) bool {
	for _, v := range s.Edges.Volumes {
		if v.LastRefreshedAt == nil {
			return true
		}
	}
	return false
}

// Monitor derives the book monitor value from its two slot flags.
func Monitor(b *ent.Book) string { return monitorOf(b) }
