package posters

import (
	"context"
	"fmt"
	"image"
	_ "image/gif" // registers the GIF decoder for image.Decode
	"image/jpeg"
	_ "image/png" // registers the PNG decoder for image.Decode
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// variantWidths is every width a poster is served at besides its own. A
// browser reducing a poster by four or five times in one pass draws it
// aliased, so the SPA asks for the rung about twice the size it displays and
// leaves the browser a reduction it filters well. The ladder is closed: an
// open width would let a client fill the data volume one pixel at a time.
// web/app/lib/posters.ts holds the same list.
var variantWidths = []int{120, 200, 320, 480, 640}

// maxVariantSourcePixels bounds what one resize decodes into memory, at four
// bytes a pixel.
const maxVariantSourcePixels = 40_000_000

const variantJPEGQuality = 85

// resizing bounds how many posters are decoded at once: a page of cards asks
// for all of its variants together.
var resizing = make(chan struct{}, max(1, runtime.GOMAXPROCS(0)/2))

// variantWidth reads the `w` query parameter: 0 without one, and an error for
// a width outside the ladder.
func variantWidth(q url.Values) (int, error) {
	raw := q.Get("w")
	if raw == "" {
		return 0, nil
	}
	asked, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("posters: unsupported width %q", raw)
	}
	// The rung is returned, never the parsed value: nothing a request sends
	// reaches a file name.
	for _, w := range variantWidths {
		if w == asked {
			return w, nil
		}
	}
	return 0, fmt.Errorf("posters: unsupported width %q", raw)
}

func variantPath(orig string, w int) string {
	return filepath.Join(filepath.Dir(orig), "poster.w"+strconv.Itoa(w)+".jpg")
}

// variant returns the path of orig scaled down to w pixels wide, creating it
// on first use, or "" when the original is to be served as it is: it is
// already that narrow, it is not a format the standard library decodes, or it
// has transparency a JPEG would lose. A variant carries its original's
// modification time, and one that does not is rebuilt, which is how a
// replaced poster drops its old variants.
func variant(ctx context.Context, orig string, w int) (string, error) {
	st, err := os.Stat(orig)
	if err != nil {
		return "", err
	}
	dst := variantPath(orig, w)
	if vst, err := os.Stat(dst); err == nil && vst.Size() > 0 &&
		vst.ModTime().Equal(st.ModTime()) {
		return dst, nil
	}

	select {
	case resizing <- struct{}{}:
		defer func() { <-resizing }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	f, err := os.Open(filepath.Clean(orig))
	if err != nil {
		return "", err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		slog.DebugContext(
			ctx,
			"poster not resizable",
			"cache.path",
			orig,
			"error",
			err,
		)
		return "", nil
	}
	if cfg.Width <= w || cfg.Width*cfg.Height > maxVariantSourcePixels {
		return "", nil
	}
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode poster: %w", err)
	}
	h := max(1, (cfg.Height*w+cfg.Width/2)/cfg.Width)
	scaled := Downscale(src, w, h)
	if !scaled.Opaque() {
		return "", nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), "poster-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if err := jpeg.Encode(
		tmp,
		scaled,
		&jpeg.Options{Quality: variantJPEGQuality},
	); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("encode poster variant: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chtimes(tmpName, st.ModTime(), st.ModTime()); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("stamp poster variant: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("rename: %w", err)
	}
	return dst, nil
}
