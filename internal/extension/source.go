package extension

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/otelx"
)

var errWasmTooLarge = fmt.Errorf(
	"extension module is over the %d MiB limit",
	MaxWasmBytes>>20,
)

func resolveSource(
	ctx context.Context,
	e config.ExtensionEntry,
	dir string,
) ([]byte, error) {
	switch {
	case filepath.IsAbs(e.Source):
		f, err := os.Open(e.Source)
		if err != nil {
			return nil, fmt.Errorf("read extension module: %w", err)
		}
		defer f.Close()
		data, err := readCapped(f)
		if err != nil {
			return nil, err
		}
		if e.SHA256 != "" {
			if err := checkSHA(data, e.SHA256); err != nil {
				return nil, err
			}
		}
		return data, nil
	case strings.HasPrefix(e.Source, "https://"):
		return fetchSource(ctx, e, dir)
	case strings.HasPrefix(e.Source, "http://"):
		return nil, errors.New("source must be https, not http")
	default:
		return nil, fmt.Errorf(
			"source must be an https URL or an absolute path, got %q",
			e.Source,
		)
	}
}

func fetchSource(
	ctx context.Context,
	e config.ExtensionEntry,
	dir string,
) ([]byte, error) {
	if e.SHA256 == "" {
		return nil, errors.New("an https source needs sha256")
	}
	cached := filepath.Join(dir, strings.ToLower(e.SHA256)+".wasm")
	data, err := os.ReadFile(cached) //nolint:gosec // sha-named
	if err == nil && checkSHA(data, e.SHA256) == nil {
		return data, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.Source, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch extension module: %w", err)
	}
	resp, err := otelx.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch extension module: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"fetch extension module: the server answered %s",
			resp.Status,
		)
	}
	data, err = readCapped(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := checkSHA(data, e.SHA256); err != nil {
		return nil, err
	}
	if err := keep(dir, cached, data); err != nil {
		return nil, err
	}
	return data, nil
}

func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxWasmBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read extension module: %w", err)
	}
	if len(data) > MaxWasmBytes {
		return nil, errWasmTooLarge
	}
	return data, nil
}

func checkSHA(data []byte, want string) error {
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), want) {
		return errors.New("the module's sha256 does not match the configured one")
	}
	return nil
}

func keep(dir, path string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("keep extension module: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return fmt.Errorf("keep extension module: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // already renamed on success
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("keep extension module: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("keep extension module: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("keep extension module: %w", err)
	}
	return nil
}
