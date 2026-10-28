package posters

import (
	"image"
	"image/draw"
)

// Downscale averages each destination pixel over the source pixels it
// covers: a box filter, which for a reduction is as good as anything a
// player shows a cover at, and keeps x/image out of the binary.
func Downscale(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for dy := range h {
		y0, y1 := dy*sh/h, max((dy+1)*sh/h, dy*sh/h+1)
		for dx := range w {
			x0, x1 := dx*sw/w, max((dx+1)*sw/w, dx*sw/w+1)
			var r, g, bl, a, n uint64
			for y := y0; y < y1; y++ {
				row := rgba.Pix[y*rgba.Stride:]
				for x := x0; x < x1; x++ {
					p := row[x*4 : x*4+4]
					r += uint64(p[0])
					g += uint64(p[1])
					bl += uint64(p[2])
					a += uint64(p[3])
					n++
				}
			}
			o := dst.Pix[dy*dst.Stride+dx*4:]
			// An average of bytes is a byte.
			o[0] = byteOf(r / n)
			o[1] = byteOf(g / n)
			o[2] = byteOf(bl / n)
			o[3] = byteOf(a / n)
		}
	}
	return dst
}

func byteOf(v uint64) uint8 {
	return uint8(min(v, 255))
}
