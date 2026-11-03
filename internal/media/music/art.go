package music

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // a cached cover is as often PNG as JPEG

	"github.com/datahearth/streamline/internal/posters"
)

// embedJPEGQuality is the quality a downscaled cover is encoded at.
const embedJPEGQuality = 90

var errCoverUndecodable = errors.New("cover is not a JPEG or PNG image")

// embeddableCover is the cover as it goes into a file: the cached bytes
// themselves when they are a JPEG or PNG no larger than maxPx on their long
// side, else a JPEG scaled down to it. A WebP or GIF cover, which not every
// player decodes from a tag, is errCoverUndecodable and nothing is embedded.
func embeddableCover(data []byte, maxPx int) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, errCoverUndecodable
	}
	if max(cfg.Width, cfg.Height) <= maxPx {
		return data, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode cover: %w", err)
	}
	w, h := cfg.Width, cfg.Height
	if w >= h {
		w, h = maxPx, max(1, h*maxPx/w)
	} else {
		w, h = max(1, w*maxPx/h), maxPx
	}
	var out bytes.Buffer
	if err := jpeg.Encode(
		&out, posters.Downscale(src, w, h), &jpeg.Options{Quality: embedJPEGQuality},
	); err != nil {
		return nil, fmt.Errorf("encode cover: %w", err)
	}
	return out.Bytes(), nil
}
