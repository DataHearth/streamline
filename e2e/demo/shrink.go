package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
)

// maxImageWidth is the widest image the cassette keeps. Posters and covers
// are drawn at 200-400 CSS px in the screenshots (800 at 2x); a 1,600 px
// original only weighs on the repository.
const maxImageWidth = 800

// shrinkImage scales a JPEG or PNG wider than maxImageWidth down by the
// smallest whole factor that brings it under, averaging each block of pixels
// (stdlib only, no resampling package for a dev tool), and re-encodes it in
// its own format. Anything else, or anything that does not decode, is
// returned as it came.
func shrinkImage(contentType string, data []byte) (string, []byte) {
	if !strings.HasPrefix(contentType, "image/") {
		return contentType, data
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png") {
		return contentType, data
	}
	b := src.Bounds()
	if b.Dx() <= maxImageWidth {
		return contentType, data
	}
	f := (b.Dx() + maxImageWidth - 1) / maxImageWidth
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()/f, b.Dy()/f))
	for y := range dst.Bounds().Dy() {
		for x := range dst.Bounds().Dx() {
			var r, g, bl, a uint32
			for dy := range f {
				for dx := range f {
					cr, cg, cb, ca := src.At(b.Min.X+x*f+dx, b.Min.Y+y*f+dy).RGBA()
					r, g, bl, a = r+cr, g+cg, bl+cb, a+ca
				}
			}
			// f is width/800 rounded up, a handful at most; each channel is
			// a 16-bit average, so >>8 fits a byte.
			n := uint32(f * f) //nolint:gosec // G115: see above
			dst.SetRGBA(x, y, color.RGBA{
				uint8(r / n >> 8),  //nolint:gosec // G115: see above
				uint8(g / n >> 8),  //nolint:gosec // G115: see above
				uint8(bl / n >> 8), //nolint:gosec // G115: see above
				uint8(a / n >> 8),  //nolint:gosec // G115: see above
			})
		}
	}
	var out bytes.Buffer
	if format == "png" {
		if err := png.Encode(&out, dst); err != nil {
			return contentType, data
		}
		return "image/png", out.Bytes()
	}
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80}); err != nil {
		return contentType, data
	}
	return "image/jpeg", out.Bytes()
}
