package restapi

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/datahearth/streamline/internal/library/audiotags"
	"github.com/datahearth/streamline/internal/media/music"
)

// Bounds on hand-set tags: enough for any real tag, small enough that one
// PATCH cannot grow every file of an album by megabytes.
const (
	maxOverrideKeys   = 64
	maxOverrideValues = 32
	maxOverrideValue  = 1024
)

// overrideKeyRe is a tag name as an override takes it: no wildcard, and a
// colon for the qualified keys TagLib reports (PERFORMER:GUITAR).
var overrideKeyRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_ :]{0,62}$`)

// tagOverrides validates and normalises hand-set tags: keys upper-cased as
// TagLib reports them, values trimmed, empty values dropped. It rejects
// before storing anything, since nothing validates a body against the spec.
func tagOverrides(in TagOverrides) (map[string][]string, error) {
	if len(in) > maxOverrideKeys {
		return nil, fmt.Errorf("at most %d tags can be set by hand", maxOverrideKeys)
	}
	out := make(map[string][]string, len(in))
	for k, vs := range in {
		key := strings.ToUpper(strings.TrimSpace(k))
		if !overrideKeyRe.MatchString(key) {
			return nil, fmt.Errorf("%q is not a tag name", k)
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("tag %s is set twice", key)
		}
		if len(vs) > maxOverrideValues {
			return nil, fmt.Errorf(
				"tag %s: at most %d values",
				key,
				maxOverrideValues,
			)
		}
		values := []string{}
		for _, v := range vs {
			v = strings.TrimSpace(v)
			if len(v) > maxOverrideValue {
				return nil, fmt.Errorf(
					"tag %s: a value is longer than %d characters",
					key,
					maxOverrideValue,
				)
			}
			if v != "" {
				values = append(values, v)
			}
		}
		out[key] = values
	}
	return out, nil
}

func (s *Server) GetMusicAlbumTags(
	ctx context.Context,
	request GetMusicAlbumTagsRequestObject,
) (GetMusicAlbumTagsResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return GetMusicAlbumTags403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	v, err := s.music.AlbumTags(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrAlbumNotFound):
		return GetMusicAlbumTags404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return GetMusicAlbumTags500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetMusicAlbumTags200JSONResponse(toMusicAlbumTags(request.Id, v)), nil
}

func toMusicAlbumTags(albumID uint32, v *music.AlbumTagsView) MusicAlbumTags {
	out := MusicAlbumTags{
		AlbumId:   albumID,
		Mode:      MusicAlbumTagsMode(v.Mode),
		Managed:   v.Managed,
		Overrides: overridesOut(v.Overrides),
		Files:     make([]MusicTagFile, 0, len(v.Files)),
	}
	for _, f := range v.Files {
		tf := MusicTagFile{
			MediaFileId: f.File.ID,
			TrackId:     f.Track.ID,
			Disc:        int(f.Track.Disc),
			Position:    int(f.Track.Position),
			Title:       f.Track.Title,
			FileName:    filepath.Base(f.File.Path),
			Format:      f.File.Format,
			State:       MusicTagFileState(f.State),
			Error:       optString(f.File.TagsError),
			TaggedAt:    f.File.TaggedAt,
			ReadError:   optString(f.ReadError),
			Overrides:   overridesOut(f.Track.TagOverrides),
			Keys:        make([]MusicTagKey, 0, len(f.Keys)),
		}
		for _, k := range f.Keys {
			_, albumSet := v.Overrides[k.Key]
			_, trackSet := f.Track.TagOverrides[k.Key]
			tf.Keys = append(tf.Keys, MusicTagKey{
				Key:        k.Key,
				Current:    nonNil(k.Current),
				Desired:    nonNil(k.Desired),
				Status:     MusicTagKeyStatus(k.Status),
				Overridden: k.Status != audiotags.KeyKept && (albumSet || trackSet),
			})
		}
		out.Files = append(out.Files, tf)
	}
	return out
}

func overridesOut(m map[string][]string) TagOverrides {
	if m == nil {
		return TagOverrides{}
	}
	return TagOverrides(m)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Server) ApplyMusicAlbumTags(
	ctx context.Context,
	request ApplyMusicAlbumTagsRequestObject,
) (ApplyMusicAlbumTagsResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return ApplyMusicAlbumTags403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	n, err := s.music.ApplyAlbumTags(ctx, request.Id)
	switch {
	case errors.Is(err, music.ErrTagsOff):
		return ApplyMusicAlbumTags409JSONResponse{
			ConflictJSONResponse: conflictResp("music_tags_off", err.Error()),
		}, nil
	case errors.Is(err, music.ErrAlbumNotFound):
		return ApplyMusicAlbumTags404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return ApplyMusicAlbumTags500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return ApplyMusicAlbumTags202JSONResponse{Queued: n}, nil
}

func (s *Server) PatchMusicTrack(
	ctx context.Context,
	request PatchMusicTrackRequestObject,
) (PatchMusicTrackResponseObject, error) {
	if err := requireNotRequestOnly(ctx); err != nil {
		return PatchMusicTrack403JSONResponse{
			ForbiddenJSONResponse: requestOnlyResp,
		}, nil
	}
	if request.Body == nil {
		return PatchMusicTrack422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"tag_overrides is required",
			),
		}, nil
	}
	overrides, err := tagOverrides(request.Body.TagOverrides)
	if err != nil {
		return PatchMusicTrack422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	err = s.music.SetTrackTagOverrides(ctx, request.Id, overrides)
	switch {
	case errors.Is(err, music.ErrTrackNotFound):
		return PatchMusicTrack404JSONResponse{
			NotFoundJSONResponse: errNotFound(err.Error()),
		}, nil
	case err != nil:
		return PatchMusicTrack500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return PatchMusicTrack204Response{}, nil
}
