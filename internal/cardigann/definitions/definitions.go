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
	"slices"
	"strings"
	"sync"

	"github.com/datahearth/streamline/internal/cardigann"
)

// ErrNotFound means the snapshot carries no definition under that name.
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
// upstream revision it was converted from, and what the sync skipped. The
// result is the caller's to filter, sort or edit: every slice is a copy, so
// nothing done to it reaches the next caller.
func Catalog() (cardigann.Catalog, error) {
	c, err := catalog()
	if err != nil {
		return c, err
	}
	c.Definitions = slices.Clone(c.Definitions)
	for i := range c.Definitions {
		s := &c.Definitions[i]
		s.Links = slices.Clone(s.Links)
		s.Replaces = slices.Clone(s.Replaces)
		s.Categories = slices.Clone(s.Categories)
		s.MovieSearch = slices.Clone(s.MovieSearch)
		s.TVSearch = slices.Clone(s.TVSearch)
	}
	c.Skipped = slices.Clone(c.Skipped)
	return c, nil
}

// Load decodes one embedded definition by its file name (Summary.File) —
// not its id, which a few upstream files spell differently.
func Load(file string) (*cardigann.Definition, error) {
	if file == "" || strings.ContainsAny(file, `/\`) || !fs.ValidPath(file) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	b, err := files.ReadFile("data/" + file + ".json")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, file)
	}
	if err != nil {
		return nil, err
	}
	var d cardigann.Definition
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("embedded cardigann definition %q: %w", file, err)
	}
	return &d, nil
}

// Notice is the attribution and licence notice shipped with the snapshot.
// Anything that distributes or displays the definitions carries it.
func Notice() string {
	b, _ := files.ReadFile("NOTICE")
	return string(b)
}
