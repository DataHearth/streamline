package subsonic

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	"github.com/datahearth/streamline/ent/artist"
	"github.com/datahearth/streamline/ent/playlist"
	"github.com/datahearth/streamline/ent/share"
	"github.com/datahearth/streamline/ent/track"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/utils/httputil"
)

// shareTokenBytes is the entropy of a share link, the only thing standing
// between the library and whoever holds the URL.
const shareTokenBytes = 16

func newShareToken() (string, error) {
	b := make([]byte, shareTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// publicBase is the origin the client reached this server on, so a share
// link opens on the same host the app is pointed at. X-Forwarded-Host is
// believed only from a configured reverse proxy, as for the OIDC callback.
func publicBase(r *http.Request) string {
	scheme, host := "http", r.Host
	if httputil.ServedOverTLS(r) {
		scheme = "https"
	}
	if httputil.TrustedPeer(r) {
		if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
			host = fwd
		}
	}
	return scheme + "://" + host
}

// sharable is whether the user ownerID may publish p: theirs, or public.
func sharable(p *ent.Playlist, ownerID uint32) bool {
	return p.Public || p.Edges.Owner.ID == ownerID
}

func expired(s *ent.Share, now time.Time) bool {
	return s.ExpiresAt != nil && !now.Before(*s.ExpiresAt)
}

// shareItems reads createShare's ids: songs, albums, artists, or playlists
// the caller can see.
func (h *Handler) shareItems(r *http.Request, u *ent.User) ([]string, error) {
	ctx := r.Context()
	raw := r.Form["id"]
	if len(raw) == 0 {
		return nil, missingParam("id")
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		kind, id, err := parseID(s)
		if err != nil {
			return nil, errNotFoundErr
		}
		var exists bool
		switch kind {
		case kindTrack:
			exists, err = h.ent.Track.Query().Where(track.IDEQ(id)).Exist(ctx)
		case kindAlbum:
			exists, err = h.ent.Album.Query().Where(album.IDEQ(id)).Exist(ctx)
		case kindArtist:
			exists, err = h.ent.Artist.Query().Where(artist.IDEQ(id)).Exist(ctx)
		case kindPlaylist:
			p, err := h.loadPlaylist(ctx, u, id)
			if err != nil {
				return nil, err
			}
			// Even an admin, who can read every playlist, may publish only
			// their own or a public one.
			if !sharable(p, u.ID) {
				return nil, errNotAuthorizedErr
			}
			exists = true
		default:
			return nil, errNotFoundErr
		}
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errNotFoundErr
		}
		out = append(out, formatID(kind, id))
	}
	return out, nil
}

// expandShare resolves a share's items to the tracks it plays now, in item
// order: an album by disc and position, an artist album by album.
func (h *Handler) expandShare(
	ctx context.Context,
	s *ent.Share,
) ([]*ent.Track, error) {
	var ids []uint32
	for _, item := range s.Items {
		kind, id, err := parseID(item)
		if err != nil {
			continue
		}
		switch kind {
		case kindTrack:
			ids = append(ids, id)
		case kindAlbum, kindArtist:
			q := h.ent.Track.Query().Order(
				track.ByAlbumField(album.FieldReleaseDate),
				track.ByAlbumField(album.FieldID),
				track.ByDisc(),
				track.ByPosition(),
			)
			if kind == kindAlbum {
				q.Where(track.HasAlbumWith(album.IDEQ(id)))
			} else {
				q.Where(track.HasAlbumWith(album.HasArtistWith(artist.IDEQ(id))))
			}
			got, err := q.IDs(ctx)
			if err != nil {
				return nil, err
			}
			ids = append(ids, got...)
		case kindPlaylist:
			p, err := h.ent.Playlist.Query().
				Where(playlist.IDEQ(id)).
				WithOwner().
				Only(ctx)
			if ent.IsNotFound(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			// A playlist made private after it was shared leaves the share.
			if s.Edges.Owner == nil || !sharable(p, s.Edges.Owner.ID) {
				continue
			}
			ids = append(ids, p.TrackIds...)
		}
	}
	return h.playableTracks(ctx, ids)
}

func (h *Handler) toShare(r *http.Request, s *ent.Share) (shareInfo, error) {
	ts, err := h.expandShare(r.Context(), s)
	if err != nil {
		return shareInfo{}, err
	}
	return shareInfo{
		ID:          shareID(s.ID),
		URL:         publicBase(r) + "/share/" + s.Token,
		Description: s.Description,
		Username:    s.Edges.Owner.Email,
		Created:     timestamp(s.CreateTime),
		Expires:     optTimestamp(s.ExpiresAt),
		LastVisited: optTimestamp(s.LastVisitedAt),
		VisitCount:  s.VisitCount,
		Entry:       songList(ts),
	}, nil
}

func (h *Handler) getShares(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ss, err := h.ent.Share.Query().
		Where(share.HasOwnerWith(user.IDEQ(u.ID))).
		WithOwner().
		Order(share.ByCreateTime(), share.ByID()).
		All(r.Context())
	if err != nil {
		return err
	}
	out := &shares{Share: make([]shareInfo, 0, len(ss))}
	for _, s := range ss {
		sh, err := h.toShare(r, s)
		if err != nil {
			return err
		}
		out.Share = append(out.Share, sh)
	}
	return h.respond(w, r, u, &body{Shares: out})
}

func (h *Handler) createShare(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	ctx := r.Context()
	items, err := h.shareItems(r, u)
	if err != nil {
		return err
	}
	token, err := newShareToken()
	if err != nil {
		return err
	}
	create := h.ent.Share.Create().
		SetToken(token).
		SetOwnerID(u.ID).
		SetItems(items).
		SetDescription(strings.TrimSpace(r.FormValue("description")))
	if at, ok := millisParam(r, "expires"); ok && at.UnixMilli() > 0 {
		create.SetExpiresAt(at)
	}
	s, err := create.Save(ctx)
	if err != nil {
		return err
	}
	s.Edges.Owner = u
	sh, err := h.toShare(r, s)
	if err != nil {
		return err
	}
	return h.respond(w, r, u, &body{Shares: &shares{Share: []shareInfo{sh}}})
}

func (h *Handler) ownShare(
	ctx context.Context,
	u *ent.User,
	id uint32,
) (*ent.Share, error) {
	s, err := h.ent.Share.Query().
		Where(share.IDEQ(id), share.HasOwnerWith(user.IDEQ(u.ID))).
		Only(ctx)
	return s, lookupErr(err)
}

// updateShare changes the description and expiry; expires 0 removes it.
func (h *Handler) updateShare(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindShare)
	if err != nil {
		return err
	}
	s, err := h.ownShare(r.Context(), u, id)
	if err != nil {
		return err
	}
	up := s.Update()
	if _, ok := r.Form["description"]; ok {
		up.SetDescription(strings.TrimSpace(r.FormValue("description")))
	}
	if at, ok := millisParam(r, "expires"); ok {
		if at.UnixMilli() <= 0 {
			up.ClearExpiresAt()
		} else {
			up.SetExpiresAt(at)
		}
	}
	if err := up.Exec(r.Context()); err != nil {
		return err
	}
	writeOK(w, r, nil)
	return nil
}

func (h *Handler) deleteShare(
	w http.ResponseWriter,
	r *http.Request,
	u *ent.User,
) error {
	id, err := idParam(r, kindShare)
	if err != nil {
		return err
	}
	if _, err := h.ownShare(r.Context(), u, id); err != nil {
		return err
	}
	if err := h.ent.Share.DeleteOneID(id).Exec(r.Context()); err != nil {
		return lookupErr(err)
	}
	writeOK(w, r, nil)
	return nil
}
