package subsonic

import (
	"errors"
	"net/http"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/scheduler"
)

// scanJob is what startScan runs: the drift check re-stats every tracked
// file and marks the missing ones, which is what a Subsonic client means by
// a rescan. New music arrives through imports, never a folder walk.
const scanJob = "drift-check"

func (h *Handler) scanCount(r *http.Request) (int, error) {
	return h.ent.Track.Query().Where(playable()).Count(r.Context())
}

func (h *Handler) scanning() bool {
	if h.jobs == nil {
		return false
	}
	info, err := h.jobs.Get(scanJob)
	return err == nil && info.Running
}

func (h *Handler) getScanStatus(
	w http.ResponseWriter,
	r *http.Request,
	_ *ent.User,
) error {
	n, err := h.scanCount(r)
	if err != nil {
		return err
	}
	writeOK(w, r, &body{ScanStatus: &scanStatus{Scanning: h.scanning(), Count: n}})
	return nil
}

func (h *Handler) startScan(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	if !isAdmin(u) {
		return errNotAuthorizedErr
	}
	if h.jobs == nil {
		return &apiError{Code: errGeneric, Message: "Scanning is not available"}
	}
	scanning := true
	switch err := h.jobs.RunNow(scanJob); {
	case err == nil, errors.Is(err, scheduler.ErrJobBusy):
	case errors.Is(err, scheduler.ErrNotStarted):
		scanning = false
	default:
		return err
	}
	n, err := h.scanCount(r)
	if err != nil {
		return err
	}
	writeOK(w, r, &body{ScanStatus: &scanStatus{Scanning: scanning, Count: n}})
	return nil
}
