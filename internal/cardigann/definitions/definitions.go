// Package definitions embeds the converted Cardigann snapshot written by
// `task cardigann:sync`. Nothing under this directory but this file is
// hand-written: data/, index.json and NOTICE are regenerated wholesale.
package definitions

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/datahearth/streamline/internal/cardigann"
)

// ErrNotFound means the snapshot carries no definition under that id.
var ErrNotFound = errors.New("cardigann definition not found")

//go:embed index.json NOTICE data
var files embed.FS

var catalog = sync.OnceValues(func() (cardigann.Catalog, error) {
	var c cardigann.Catalog
	b, err := files.ReadFile("index.json")
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("embedded cardigann index: %w", err)
	}
	return c, nil
})

// Catalog returns the snapshot's index: every definition's summary, the
// upstream revision it was converted from, and what the sync skipped.
func Catalog() (cardigann.Catalog, error) { return catalog() }

// Load decodes one embedded definition by id.
func Load(id string) (*cardigann.Definition, error) {
	if id == "" || strings.ContainsAny(id, `/\`) || !fs.ValidPath(id) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	b, err := files.ReadFile("data/" + id + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	var d cardigann.Definition
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("embedded cardigann definition %q: %w", id, err)
	}
	return &d, nil
}

// Notice is the attribution and licence notice shipped with the snapshot.
// Anything that distributes or displays the definitions carries it.
func Notice() string {
	b, _ := files.ReadFile("NOTICE")
	return string(b)
}
