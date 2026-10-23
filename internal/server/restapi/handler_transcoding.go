package restapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/transcoding"
)

// queueLimit caps the queue response, matching the spec.
const queueLimit = 200

// transcodingOff reports whether the four /transcoding/* endpoints should
// refuse. The worker is nil in any composition that never wired one, and the
// SPA keys its "feature off" screen on the 409 rather than on an empty list.
func (s *Server) transcodingOff() bool {
	c := config.Get()
	return s.transcoder == nil || c == nil || !c.Transcoding.Enabled
}

const transcodingDisabledMsg = "transcoding is disabled"

func (s *Server) ListTranscodeQueue(
	ctx context.Context,
	_ ListTranscodeQueueRequestObject,
) (ListTranscodeQueueResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListTranscodeQueue403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if s.transcodingOff() {
		return ListTranscodeQueue409JSONResponse{
			ConflictJSONResponse: errConflict(transcodingDisabledMsg),
		}, nil
	}

	rows, err := s.store.ListTranscodeJobs(ctx, queueLimit)
	if err != nil {
		return nil, err
	}
	out := make([]TranscodeJob, 0, len(rows))
	for _, j := range rows {
		out = append(out, transcodeJobToAPI(j, s.transcoder))
	}
	return ListTranscodeQueue200JSONResponse(out), nil
}

func (s *Server) CancelTranscodeJob(
	ctx context.Context,
	request CancelTranscodeJobRequestObject,
) (CancelTranscodeJobResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CancelTranscodeJob403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if s.transcodingOff() {
		return CancelTranscodeJob409JSONResponse{
			ConflictJSONResponse: errConflict(transcodingDisabledMsg),
		}, nil
	}

	// An id naming no job lands here too: the store's update is one
	// conditional statement, so a missing row and a finished one are the same
	// zero rows affected. There is no 404 to be had.
	switch err := s.transcoder.Cancel(ctx, request.Id); {
	case errors.Is(err, db.ErrTranscodeJobNotCancelable):
		return CancelTranscodeJob409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	return CancelTranscodeJob204Response{}, nil
}

func (s *Server) RetryTranscodeJob(
	ctx context.Context,
	request RetryTranscodeJobRequestObject,
) (RetryTranscodeJobResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return RetryTranscodeJob403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if s.transcodingOff() {
		return RetryTranscodeJob409JSONResponse{
			ConflictJSONResponse: errConflict(transcodingDisabledMsg),
		}, nil
	}

	// Same zero-rows conflation as cancel: an unknown id is a 409, not a 404.
	switch err := s.store.RetryTranscodeJob(ctx, request.Id); {
	case errors.Is(err, db.ErrTranscodeJobNotRetryable):
		return RetryTranscodeJob409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	return RetryTranscodeJob204Response{}, nil
}

func (s *Server) StartTranscodeScan(
	ctx context.Context,
	_ StartTranscodeScanRequestObject,
) (StartTranscodeScanResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return StartTranscodeScan403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if s.transcodingOff() {
		return StartTranscodeScan409JSONResponse{
			ConflictJSONResponse: errConflict(transcodingDisabledMsg),
		}, nil
	}
	// The other three endpoints act on rows that already exist; a scan
	// creates them. Queueing work no worker can claim leaves rows nothing
	// drains and nothing explains, so this one refuses unless ffmpeg is
	// actually usable.
	if !s.transcoder.Ready() {
		return StartTranscodeScan409JSONResponse{
			ConflictJSONResponse: errWorkerUnavailable(
				"transcoding worker cannot run: ffmpeg disabled or not found",
			),
		}, nil
	}

	switch err := s.transcoder.Scan(ctx); {
	case errors.Is(err, transcoding.ErrScanRunning):
		return StartTranscodeScan409JSONResponse{
			ConflictJSONResponse: errConflict(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	return StartTranscodeScan202Response{}, nil
}

// transcodeJobToAPI maps a job row into the queue view. The owner chain
// (media file → movie, or → episode → season → show) must be eager-loaded;
// db.ListTranscodeJobs does that. w supplies the live progress of a job this
// process is actually encoding — a row left running by a restart has none,
// which is why the fields are optional rather than zero.
func transcodeJobToAPI(j *ent.TranscodeJob, w *transcoding.Worker) TranscodeJob {
	out := TranscodeJob{
		Id:        j.ID,
		Status:    TranscodeJobStatus(j.Status),
		Attempts:  j.Attempts,
		CreatedAt: j.CreateTime,
	}
	if j.Error != "" {
		e := j.Error
		out.Error = &e
	}
	if j.SizeBefore != 0 {
		sb := j.SizeBefore
		out.SizeBefore = &sb
	}
	if j.SizeAfter != 0 {
		sa := j.SizeAfter
		out.SizeAfter = &sa
	}
	out.StartedAt = j.StartedAt
	out.FinishedAt = j.FinishedAt
	out.DeferredUntil = j.DeferredUntil

	if mf := j.Edges.MediaFile; mf != nil {
		out.FilePath = mf.Path
		out.MediaTitle = mediaFileTitle(mf)
		if m := mf.Edges.Movie; m != nil {
			id := m.ID
			out.MovieId = &id
		}
		if ep := mf.Edges.Episode; ep != nil {
			epID := ep.ID
			out.EpisodeId = &epID
			if season := ep.Edges.Season; season != nil {
				if show := season.Edges.TvShow; show != nil {
					showID := show.ID
					out.SeriesId = &showID
				}
			}
		}
	}

	if w != nil && j.Status == transcodejob.StatusRunning {
		if snap, ok := w.Progress(j.ID); ok {
			percent := snap.Percent
			out.Percent = &percent
			speed := snap.Speed
			out.Speed = &speed
			eta := int(snap.ETA.Seconds())
			out.EtaSeconds = &eta
		}
	}
	return out
}

// mediaFileTitle labels a queue row: "Heat (1995)" for a movie,
// "Kaamelott S02E07" for an episode, and the bare filename when the owner
// chain was not loaded.
func mediaFileTitle(mf *ent.MediaFile) string {
	if m := mf.Edges.Movie; m != nil {
		if m.Year == 0 {
			return m.Title
		}
		return fmt.Sprintf("%s (%d)", m.Title, m.Year)
	}
	if ep := mf.Edges.Episode; ep != nil {
		if season := ep.Edges.Season; season != nil {
			if show := season.Edges.TvShow; show != nil {
				return fmt.Sprintf(
					"%s S%02dE%02d", show.Title, season.Number, ep.Number,
				)
			}
		}
	}
	return mf.Path
}
