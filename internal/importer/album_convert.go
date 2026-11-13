package importer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/otelx"
)

// convertDirPattern names the folder an import converts into: under the music
// root, so placing the result is a rename, and hidden, so no scan reads it.
const convertDirPattern = ".convert-*"

// maxConvertedDriftSec is how far a converted file's duration may sit from its
// source's: both are whole seconds, rounded apart.
const maxConvertedDriftSec = 1

func isWAV(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".wav")
}

// convertWAV converts src to a FLAC of the same base name in dir and returns
// its path, with what it measures, once the result is confirmed to hold the
// source's audio. The source is only read.
func (w *Worker) convertWAV(
	ctx context.Context,
	src, dir string,
) (string, *ffmpeg.AudioInfo, error) {
	ctx, span := tracer.Start(ctx, "importer.convert_wav")
	defer span.End()
	span.SetAttributes(attribute.String("file.path", src))

	base := filepath.Base(src)
	dst := filepath.Join(dir, strings.TrimSuffix(base, filepath.Ext(base))+".flac")
	info, err := w.convertVerified(ctx, src, dst)
	if err != nil {
		if rmErr := os.Remove(
			dst,
		); rmErr != nil &&
			!errors.Is(rmErr, os.ErrNotExist) {
			err = errors.Join(err, rmErr)
		}
		wavConversions.Add(ctx, 1, metric.WithAttributes(
			attribute.String("outcome", "failed"),
		))
		return "", nil, otelx.RecordSpanError(
			span, fmt.Errorf("%w: %s: %w", ErrConversionFailed, base, err),
		)
	}
	wavConversions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", "converted"),
	))
	return dst, info, nil
}

func (w *Worker) convertVerified(
	ctx context.Context,
	src, dst string,
) (*ffmpeg.AudioInfo, error) {
	if err := w.probe.ConvertToFLAC(ctx, src, dst); err != nil {
		return nil, err
	}
	want, err := w.probe.ProbeAudio(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("probe source: %w", err)
	}
	got, err := w.probe.ProbeAudio(ctx, dst)
	if err != nil {
		return nil, fmt.Errorf("probe result: %w", err)
	}
	drift := int64(got.DurationSec) - int64(want.DurationSec)
	switch {
	case got.Codec != "flac":
		return nil, fmt.Errorf("result is %q, not flac", got.Codec)
	case got.SampleRateHz != want.SampleRateHz:
		return nil, fmt.Errorf(
			"result is %d Hz, source %d Hz", got.SampleRateHz, want.SampleRateHz,
		)
	case drift > maxConvertedDriftSec || drift < -maxConvertedDriftSec:
		return nil, fmt.Errorf(
			"result lasts %ds, source %ds", got.DurationSec, want.DurationSec,
		)
	}
	return got, nil
}
