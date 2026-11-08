// Package snapshot fetches upstream's Cardigann definitions and converts
// them: the newest Prowlarr/Indexers commit, read as one tarball, every file
// run through internal/cardigann/convert. What it returns is held in memory
// (internal/cardigann/definitions); nothing is written to disk.
package snapshot

import (
	"archive/tar"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
)

const (
	repository = "https://github.com/Prowlarr/Indexers"
	commitsAPI = "https://api.github.com/repos/Prowlarr/Indexers/commits/master"
	archiveURL = "https://codeload.github.com/Prowlarr/Indexers/tar.gz/"
	// maxArchive bounds the inflated tarball: the whole repository is a few
	// MB, every schema version since v1 included.
	maxArchive = 128 << 20
	maxFile    = 4 << 20
)

var sha1Hex = regexp.MustCompile(`^[0-9a-f]{40}$`)

// File is one upstream definition: the schema version whose directory it
// was read from, its name under definitions/v<Version>/, and its YAML.
type File struct {
	Version int
	Name    string
	Data    []byte
}

// Build converts every file into a catalog and its encoded definitions,
// keyed by file name without .yml. Where a name is read from more than one
// schema version, the newest wins — upstream's per-file fallback
// (cardigann.SchemaVersion). A file the converter refuses is skipped with
// the reason, never fatal: one broken upstream file must not take every
// other tracker with it.
func Build(
	files []File,
	up cardigann.Upstream,
) (cardigann.Catalog, map[string][]byte) {
	cat := cardigann.Catalog{
		Schema:    cardigann.SchemaVersion,
		Converter: convert.Version,
		Upstream:  up,
	}
	files = slices.Clone(files)
	slices.SortStableFunc(files, func(a, b File) int {
		return cmp.Compare(b.Version, a.Version)
	})
	seen := map[string]bool{}
	defs := make(map[string][]byte, len(files))
	for _, f := range files {
		if seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		rel := fmt.Sprintf("definitions/v%d/%s", f.Version, f.Name)
		// Keyed by file name, not id: that is what Prowlarr stores an indexer
		// under and fetches an update by, and a handful of files upstream are
		// named apart from the id inside them (bluebird.yml holds
		// bluebirdhd).
		file := strings.TrimSuffix(f.Name, ".yml")
		d, err := convert.Convert(rel, f.Data)
		var b []byte
		if err == nil {
			b, err = cardigann.EncodeJSON(d)
		}
		if err != nil {
			cat.Skipped = append(cat.Skipped, cardigann.Skipped{
				File: f.Name, Reason: err.Error(),
			})
			continue
		}
		defs[file] = b
		cat.Definitions = append(cat.Definitions, cardigann.Summarize(file, d))
	}
	slices.SortFunc(cat.Definitions, func(a, b cardigann.Summary) int {
		return cmp.Compare(a.File, b.File)
	})
	slices.SortFunc(cat.Skipped, func(a, b cardigann.Skipped) int {
		return cmp.Compare(a.File, b.File)
	})
	return cat, defs
}

// Latest names upstream's newest commit.
func Latest(ctx context.Context, client *http.Client) (cardigann.Upstream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, commitsAPI, nil)
	if err != nil {
		return cardigann.Upstream{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return cardigann.Upstream{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return cardigann.Upstream{}, fmt.Errorf(
			"github commits: status %d",
			resp.StatusCode,
		)
	}
	var c struct {
		SHA    string `json:"sha"`
		Commit struct {
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxFile)).
		Decode(&c); err != nil {
		return cardigann.Upstream{}, fmt.Errorf("github commits: %w", err)
	}
	if !sha1Hex.MatchString(c.SHA) {
		return cardigann.Upstream{}, fmt.Errorf(
			"github commits: unexpected sha %q",
			c.SHA,
		)
	}
	return cardigann.Upstream{
		Repository: repository, Commit: c.SHA, Date: c.Commit.Committer.Date,
	}, nil
}

// Fetch reads up's commit and converts it. warning is set when upstream's
// current schema has moved past the one streamline reads.
func Fetch(
	ctx context.Context,
	client *http.Client,
	up cardigann.Upstream,
) (cardigann.Catalog, map[string][]byte, string, error) {
	files, versions, err := archive(ctx, client, up.Commit)
	if err != nil {
		return cardigann.Catalog{}, nil, "", err
	}
	warning, err := CheckVersions(versions)
	if err != nil {
		return cardigann.Catalog{}, nil, "", err
	}
	cat, defs := Build(files, up)
	return cat, defs, warning, nil
}

// archive reads every schema directory's definitions and VERSIONS out of the
// commit's tarball. Anything but a regular file is left out.
func archive(
	ctx context.Context,
	client *http.Client,
	commit string,
) ([]File, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		archiveURL+url.PathEscape(commit), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream archive: status %d", resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("upstream archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, maxArchive))
	var (
		files    []File
		versions string
	)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("upstream archive: %w", err)
		}
		// GitHub roots every entry under "<repo>-<sha>/".
		_, rel, ok := strings.Cut(h.Name, "/")
		if !ok || h.Typeflag != tar.TypeReg {
			continue
		}
		version, name := 0, ""
		for v := cardigann.SchemaVersion; v >= cardigann.OldestSchemaVersion; v-- {
			if n, ok := strings.CutPrefix(
				rel,
				fmt.Sprintf("definitions/v%d/", v),
			); ok {
				version, name = v, n
			}
		}
		if rel != "VERSIONS" && (version == 0 || strings.Contains(name, "/") ||
			!strings.HasSuffix(name, ".yml")) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxFile))
		if err != nil {
			return nil, "", fmt.Errorf("upstream archive: %w", err)
		}
		if rel == "VERSIONS" {
			versions = string(b)
			continue
		}
		files = append(files, File{Version: version, Name: name, Data: b})
	}
	if versions == "" || len(files) == 0 {
		return nil, "", fmt.Errorf(
			"upstream archive: no definitions/v%d..v%d or VERSIONS",
			cardigann.OldestSchemaVersion, cardigann.SchemaVersion,
		)
	}
	return files, versions, nil
}

// CheckVersions reads upstream's VERSIONS file. A schema below MIN_VERSION
// is frozen upstream — still readable, never updated again — so reading it
// would serve a catalog that looks fresh and is not. warning is set when
// upstream's current schema has moved past the one streamline reads.
func CheckVersions(content string) (string, error) {
	vals := map[string]int{}
	for line := range strings.Lines(content) {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			vals[strings.TrimSpace(k)] = n
		}
	}
	lo, okLo := vals["MIN_VERSION"]
	hi, okHi := vals["MAX_VERSION"]
	if !okLo || !okHi {
		return "", errors.New("VERSIONS: MIN_VERSION or MAX_VERSION missing")
	}
	if v := cardigann.SchemaVersion; v < lo || v > hi {
		return "", fmt.Errorf(
			"upstream supports schema v%d..v%d, streamline reads v%d: "+
				"port internal/cardigann to the new schema", lo, hi, v,
		)
	}
	if cur := vals["CURRENT_VERSION"]; cur > cardigann.SchemaVersion {
		return fmt.Sprintf(
			"upstream's current schema is v%d; v%d still reads but will be "+
				"frozen once it drops below MIN_VERSION",
			cur, cardigann.SchemaVersion,
		), nil
	}
	return "", nil
}
