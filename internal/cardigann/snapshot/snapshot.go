// Package snapshot turns a set of upstream definition files into the
// converted snapshot streamline reads — data/<file>.json, index.json and the
// NOTICE — for both the build-time sync (cmd/cardigann-sync, into the
// embedded copy) and the runtime update (internal/cardigann/update, into the
// data directory). One code path, so the two can never write different
// shapes or drop the provenance the licence rests on.
package snapshot

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/convert"
)

// ValidName is the shape of a file name a snapshot can carry: one
// //go:embed accepts (no leading . or _, no :), and one that is only ever a
// plain path segment under data/.
var ValidName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.yml$`)

// File is one upstream definition: the schema version whose directory it
// was read from, its name under definitions/v<Version>/, and its YAML.
type File struct {
	Version int
	Name    string
	Data    []byte
}

// Build converts every file into the snapshot's catalog and its encoded
// definitions, keyed by file name without .yml. Where a name is read from
// more than one schema version, the newest wins — upstream's per-file
// fallback (cardigann.SchemaVersion). A file the converter
// refuses — or whose name a snapshot cannot carry — is skipped with the
// reason, never fatal: one broken upstream file must not block every other
// tracker's update.
func Build(
	files []File,
	up cardigann.Upstream,
	skipped []cardigann.Skipped,
) (cardigann.Catalog, map[string][]byte) {
	cat := cardigann.Catalog{
		Schema:    cardigann.SchemaVersion,
		Converter: convert.Version,
		Upstream:  up,
		Skipped:   slices.Clone(skipped),
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
		if !ValidName.MatchString(f.Name) {
			cat.Skipped = append(cat.Skipped, cardigann.Skipped{
				File: f.Name, Reason: "file name cannot be embedded",
			})
			continue
		}
		rel := fmt.Sprintf("definitions/v%d/%s", f.Version, f.Name)
		// Keyed by file name, not id: that is what Prowlarr stores an indexer
		// under and fetches an update by, and a handful of files upstream are
		// named apart from the id inside them (bluebird.yml holds
		// bluebirdhd). It also keeps every write inside data/, whatever an
		// upstream id says.
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
	Sort(&cat)
	return cat, defs
}

// Sort orders a catalog's listings by file name, which is what keeps a
// snapshot byte-reproducible.
func Sort(cat *cardigann.Catalog) {
	slices.SortFunc(cat.Definitions, func(a, b cardigann.Summary) int {
		return cmp.Compare(a.File, b.File)
	})
	slices.SortFunc(cat.Skipped, func(a, b cardigann.Skipped) int {
		return cmp.Compare(a.File, b.File)
	})
	slices.Sort(cat.Held)
}

// Write replaces the snapshot under out wholesale, so a definition upstream
// deleted disappears here too instead of lingering as a stale file.
func Write(out string, cat cardigann.Catalog, defs map[string][]byte) error {
	data := filepath.Join(out, "data")
	if err := os.RemoveAll(data); err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0o750); err != nil {
		return err
	}
	for file, b := range defs {
		if err := writeFile(filepath.Join(data, file+".json"), b); err != nil {
			return err
		}
	}
	return WriteIndex(out, cat)
}

// WriteIndex rewrites a snapshot's index.json and the NOTICE it implies.
func WriteIndex(out string, cat cardigann.Catalog) error {
	idx, err := cardigann.EncodeJSON(cat)
	if err != nil {
		return err
	}
	if err := writeFile(filepath.Join(out, "index.json"), idx); err != nil {
		return err
	}
	return writeFile(filepath.Join(out, "NOTICE"), []byte(Notice(cat)))
}

func writeFile(path string, b []byte) error {
	//nolint:gosec // G306: world-readable, like the source tree it mirrors
	return os.WriteFile(path, b, 0o644)
}

// Prefixes are the upstream directories a snapshot reads, newest first.
func Prefixes() []string {
	var out []string
	for v := cardigann.SchemaVersion; v >= cardigann.OldestSchemaVersion; v-- {
		out = append(out, fmt.Sprintf("definitions/v%d/", v))
	}
	return out
}

// Notice is the attribution and licence notice a snapshot ships with.
func Notice(cat cardigann.Catalog) string {
	up := cat.Upstream
	held := ""
	if len(cat.Held) > 0 {
		held = fmt.Sprintf(`
Held at an earlier revision, an update to them awaiting an administrator's
approval: %s.
`, strings.Join(cat.Held, ", "))
	}
	return fmt.Sprintf(`Cardigann indexer definitions
=============================

The files under data/ are converted from the Cardigann YAML indexer
definitions maintained by the Prowlarr project:

    %s (definitions/v%d, falling back to v%d per file)
    revision %s, committed %s
%s
Most of those definitions are synced from Jackett
(https://github.com/Jackett/Jackett). All credit for them belongs to the
Prowlarr and Jackett contributors.

Licence: Jackett is distributed under the GNU General Public License without
specifying a version; section 9 of the GPL version 2 lets a recipient choose
any version published by the Free Software Foundation in that case. The
Prowlarr/Indexers repository carries no licence file of its own. Streamline
distributes these files, as modified, under the GNU General Public License
version 3, like the rest of streamline (LICENSE at the repository root).

Modification notice: %s.
Each file records, under "source", the upstream path and the sha256 of the
file it was converted from. The converter is internal/cardigann/convert; the
sync that wrote this directory is internal/cardigann/cmd/cardigann-sync
(task cardigann:sync). Do not edit these files by hand — re-run the sync.
`, up.Repository, cardigann.SchemaVersion, cardigann.OldestSchemaVersion,
		up.Commit, up.Date, held, convert.Modified)
}

// CheckVersions reads upstream's VERSIONS file. A schema below MIN_VERSION
// is frozen upstream — still readable, never updated again — so syncing it
// would ship a snapshot that looks fresh and is not. warning is set when
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
			"upstream's current schema is v%d; v%d still syncs but will be "+
				"frozen once it drops below MIN_VERSION",
			cur, cardigann.SchemaVersion,
		), nil
	}
	return "", nil
}
