package restapi

import (
	"context"
	"errors"
	"time"

	"github.com/datahearth/streamline/internal/bittorrent"
	"github.com/datahearth/streamline/internal/download"
)

// errNoBuiltin is the 404 payload every /torrents endpoint returns when no
// builtin download client (and therefore no engine) is configured.
var errNoBuiltin = errNotFound("no builtin download client configured")

func (s *Server) ListTorrents(
	ctx context.Context,
	_ ListTorrentsRequestObject,
) (ListTorrentsResponseObject, error) {
	if s.torrents == nil {
		return ListTorrents404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	views := s.torrents.ListViews(ctx)
	items := make([]TorrentInfo, 0, len(views))
	for _, v := range views {
		items = append(items, torrentInfoToAPI(v))
	}
	return ListTorrents200JSONResponse{
		TorrentListJSONResponse: TorrentListJSONResponse(TorrentList{
			Items:       items,
			RefreshedAt: time.Now(),
		}),
	}, nil
}

func (s *Server) AddTorrent(
	ctx context.Context,
	request AddTorrentRequestObject,
) (AddTorrentResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return AddTorrent403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if s.torrents == nil {
		return AddTorrent404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	var src download.TorrentSource
	if request.Body.Magnet != nil {
		src.Magnet = *request.Body.Magnet
	}
	if request.Body.Torrent != nil {
		src.Bytes = *request.Body.Torrent
	}
	if (src.Magnet == "") == (len(src.Bytes) == 0) {
		return AddTorrent422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(
				"exactly one of magnet or torrent must be provided",
			),
		}, nil
	}
	hash, err := s.torrents.AddTorrent(ctx, src)
	if err != nil {
		return AddTorrent422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return AddTorrent201JSONResponse{
		TorrentAddResultJSONResponse: TorrentAddResultJSONResponse(
			TorrentAddResult{Hash: hash},
		),
	}, nil
}

func (s *Server) GetTorrent(
	ctx context.Context,
	request GetTorrentRequestObject,
) (GetTorrentResponseObject, error) {
	if s.torrents == nil {
		return GetTorrent404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	d, err := s.torrents.Details(ctx, request.Hash)
	switch {
	case errors.Is(err, download.ErrTorrentNotFound):
		return GetTorrent404JSONResponse{
			NotFoundJSONResponse: errNotFound("torrent not found"),
		}, nil
	case err != nil:
		return nil, err
	}
	return GetTorrent200JSONResponse{
		TorrentDetailsJSONResponse: TorrentDetailsJSONResponse(
			torrentDetailsToAPI(d),
		),
	}, nil
}

func (s *Server) PauseTorrent(
	ctx context.Context,
	request PauseTorrentRequestObject,
) (PauseTorrentResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return PauseTorrent403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if s.torrents == nil {
		return PauseTorrent404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	switch err := s.torrents.PauseTorrent(ctx, request.Hash); {
	case errors.Is(err, download.ErrTorrentNotFound):
		return PauseTorrent404JSONResponse{
			NotFoundJSONResponse: errNotFound("torrent not found"),
		}, nil
	case err != nil:
		return PauseTorrent422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return PauseTorrent204Response{}, nil
}

func (s *Server) ResumeTorrent(
	ctx context.Context,
	request ResumeTorrentRequestObject,
) (ResumeTorrentResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ResumeTorrent403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if s.torrents == nil {
		return ResumeTorrent404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	switch err := s.torrents.ResumeTorrent(ctx, request.Hash); {
	case errors.Is(err, download.ErrTorrentNotFound):
		return ResumeTorrent404JSONResponse{
			NotFoundJSONResponse: errNotFound("torrent not found"),
		}, nil
	case err != nil:
		return ResumeTorrent422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return ResumeTorrent204Response{}, nil
}

func (s *Server) SetTorrentFilePriority(
	ctx context.Context,
	request SetTorrentFilePriorityRequestObject,
) (SetTorrentFilePriorityResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return SetTorrentFilePriority403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	if s.torrents == nil {
		return SetTorrentFilePriority404JSONResponse{
			NotFoundJSONResponse: errNoBuiltin,
		}, nil
	}
	prios := []bittorrent.FilePriority{{
		Index:    request.Index,
		Priority: string(request.Body.Priority),
	}}
	switch err := s.torrents.SetFilePriorities(ctx, request.Hash, prios); {
	case errors.Is(err, download.ErrTorrentNotFound):
		return SetTorrentFilePriority404JSONResponse{
			NotFoundJSONResponse: errNotFound("torrent not found"),
		}, nil
	case err != nil:
		return SetTorrentFilePriority422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	return SetTorrentFilePriority204Response{}, nil
}

func (s *Server) DeleteTorrent(
	ctx context.Context,
	request DeleteTorrentRequestObject,
) (DeleteTorrentResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteTorrent403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	if s.torrents == nil {
		return DeleteTorrent404JSONResponse{NotFoundJSONResponse: errNoBuiltin}, nil
	}
	deleteFiles := false
	if request.Params.DeleteFiles != nil {
		deleteFiles = *request.Params.DeleteFiles
	}
	switch err := s.torrents.RemoveTorrent(ctx, request.Hash, deleteFiles); {
	case errors.Is(err, download.ErrTorrentNotFound):
		return DeleteTorrent404JSONResponse{
			NotFoundJSONResponse: errNotFound("torrent not found"),
		}, nil
	case err != nil:
		return nil, err
	}
	return DeleteTorrent204Response{}, nil
}

func torrentInfoToAPI(v bittorrent.TorrentView) TorrentInfo {
	return TorrentInfo{
		Hash:           v.Hash,
		Name:           v.Name,
		Status:         TorrentInfoStatus(v.Status),
		Progress:       v.Progress,
		Size:           v.Size,
		DownloadSpeed:  v.DownloadSpeed,
		UploadSpeed:    v.UploadSpeed,
		Uploaded:       v.Uploaded,
		Ratio:          v.Ratio,
		Eta:            v.ETA,
		Seeds:          v.Seeds,
		PeerCount:      v.PeerCount,
		SavePath:       v.SavePath,
		AddedAt:        v.AddedAt,
		SeedingStopped: v.SeedingStopped,
		Tracked:        v.Tracked,
	}
}

func torrentDetailsToAPI(d bittorrent.TorrentDetails) TorrentDetails {
	files := make([]TorrentFile, 0, len(d.Files))
	for _, f := range d.Files {
		files = append(files, TorrentFile{
			Index:      f.Index,
			Path:       f.Path,
			Size:       f.Size,
			Downloaded: f.Downloaded,
			Priority:   TorrentFilePriority(f.Priority),
		})
	}
	peers := make([]TorrentPeer, 0, len(d.Peers))
	for _, p := range d.Peers {
		peer := TorrentPeer{Addr: p.Addr}
		if p.Client != "" {
			c := p.Client
			peer.Client = &c
		}
		if p.DownloadRate != 0 {
			r := p.DownloadRate
			peer.DownloadRate = &r
		}
		if p.UploadRate != 0 {
			r := p.UploadRate
			peer.UploadRate = &r
		}
		peers = append(peers, peer)
	}
	return TorrentDetails{
		Hash:           d.Hash,
		Name:           d.Name,
		Status:         TorrentDetailsStatus(d.Status),
		Progress:       d.Progress,
		Size:           d.Size,
		DownloadSpeed:  d.DownloadSpeed,
		UploadSpeed:    d.UploadSpeed,
		Uploaded:       d.Uploaded,
		Ratio:          d.Ratio,
		Eta:            d.ETA,
		Seeds:          d.Seeds,
		PeerCount:      d.PeerCount,
		SavePath:       d.SavePath,
		AddedAt:        d.AddedAt,
		SeedingStopped: d.SeedingStopped,
		Tracked:        d.Tracked,
		Files:          files,
		Trackers:       d.Trackers,
		Peers:          peers,
	}
}
