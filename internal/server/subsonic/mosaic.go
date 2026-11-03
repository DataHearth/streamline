package subsonic

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/gif" // a cached cover can be any type a source hands back
	"image/jpeg"
	_ "image/png"
	"net/http"
	"os"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/posters"
)

const (
	mosaicTile        = 300
	mosaicJPEGQuality = 85
)

// mosaicLayouts places n covers on the four tiles: two go on the diagonals,
// three repeat the first, so no layout leaves a tile empty.
var mosaicLayouts = map[int][4]int{
	2: {0, 1, 1, 0},
	3: {0, 1, 2, 0},
	4: {0, 1, 2, 3},
}

// servePlaylistCover draws the covers of a playlist's first four distinct
// albums as a 2×2 grid, as Navidrome does. A playlist of one album, or whose
// other covers are missing or undecodable (WebP: there is no decoder in the
// standard library), gets that album's cover as it is.
//
// ponytail: drawn on every request, clients cache by coverArt id; cache the
// JPEG keyed on the album ids if it ever shows in a profile.
func (h *Handler) servePlaylistCover(
	w http.ResponseWriter,
	r *http.Request,
	ts []*ent.Track,
) error {
	var first uint32
	seen := map[uint32]bool{}
	var covers []image.Image
	for _, t := range ts {
		id := t.Edges.Album.ID
		if seen[id] {
			continue
		}
		seen[id] = true
		if first == 0 {
			first = id
		}
		if img := h.squareCover(id); img != nil {
			covers = append(covers, img)
			if len(covers) == 4 {
				break
			}
		}
	}
	layout, ok := mosaicLayouts[len(covers)]
	if !ok {
		h.posters.Serve(w, r, "albums", first)
		return nil
	}
	grid := image.NewRGBA(image.Rect(0, 0, 2*mosaicTile, 2*mosaicTile))
	for i, c := range layout {
		at := image.Pt(i%2*mosaicTile, i/2*mosaicTile)
		draw.Draw(
			grid,
			image.Rectangle{Min: at, Max: at.Add(image.Pt(mosaicTile, mosaicTile))},
			covers[c],
			image.Point{},
			draw.Src,
		)
	}
	var out bytes.Buffer
	if err := jpeg.Encode(
		&out, grid, &jpeg.Options{Quality: mosaicJPEGQuality},
	); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "image/jpeg")
	_, err := w.Write(out.Bytes())
	return err
}

// squareCover is the album's cached cover cropped to its centre square and
// scaled to one tile, nil when there is none it can read.
func (h *Handler) squareCover(albumID uint32) image.Image {
	data, err := os.ReadFile(h.posters.Path("albums", albumID))
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	side := min(b.Dx(), b.Dy())
	at := b.Min.Add(image.Pt((b.Dx()-side)/2, (b.Dy()-side)/2))
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return nil
	}
	square := sub.SubImage(
		image.Rectangle{Min: at, Max: at.Add(image.Pt(side, side))},
	)
	return posters.Downscale(square, mosaicTile, mosaicTile)
}
