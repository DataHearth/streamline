// Package update keeps the Cardigann definitions in the data directory
// current with upstream between releases — Prowlarr's
// IndexerDefinitionUpdateService, with one difference that matters: a change
// that would move a configured tracker to other domains, or change how its
// login works, is held until an administrator approves it. A definition is
// code that receives the tracker credentials; an upstream edit should not
// re-point them unseen.
//
// The new set is fetched as a tarball of the upstream commit the GitHub API
// names, converted by the same internal/cardigann/snapshot the build-time
// sync uses, and written wholesale to definitions.Current(dir), with the
// NOTICE and per-file provenance the licence rests on.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/definitions"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
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

var (
	// ErrNoUpdate is an approval or rejection for a definition with no update
	// held.
	ErrNoUpdate = errors.New("no update is held for that definition")
	// mu serialises every write under a directory: the job, approvals and
	// rejections.
	mu sync.Mutex
)

// Pending is an upstream change held for approval.
type Pending struct {
	File    string   `json:"file"`
	Name    string   `json:"name"`
	Reasons []string `json:"reasons"`
	// SHA256 is the held upstream file's, what a rejection remembers.
	SHA256 string `json:"sha256,omitempty"`
	// Removed is a definition upstream deleted.
	Removed bool `json:"removed,omitempty"`
}

type pendingIndex struct {
	Upstream cardigann.Upstream `json:"upstream"`
	Pending  []Pending          `json:"pending"`
	// Rejected maps a file to the upstream sha256 an administrator turned
	// down; that version is not proposed again, a later one is.
	Rejected map[string]string `json:"rejected,omitempty"`
}

// Result says what a run did.
type Result struct {
	Commit  string
	Current bool
	Changed []string
	Added   []string
	Removed []string
	Held    []string
	Warning string
}

func pendingDir(dir string) string { return filepath.Join(dir, "pending") }

// Run brings dir's definitions up to upstream's latest commit. inUse says
// whether a configured indexer runs a definition: only those are held.
func Run(
	ctx context.Context,
	dir string,
	client *http.Client,
	inUse func(file string) bool,
) (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	up, err := latest(ctx, client)
	if err != nil {
		return Result{}, err
	}
	res := Result{Commit: up.Commit}
	base, err := definitions.Snapshot(dir)
	if err != nil {
		return res, err
	}
	if base.Upstream.Commit == up.Commit {
		res.Current = true
		return res, nil
	}
	files, versions, err := archive(ctx, client, up.Commit)
	if err != nil {
		return res, err
	}
	if res.Warning, err = snapshot.CheckVersions(versions); err != nil {
		return res, err
	}
	cat, defs := snapshot.Build(files, up, nil)

	prev, err := readPending(dir)
	if err != nil {
		return res, err
	}
	proposed := pendingIndex{Upstream: up, Rejected: map[string]string{}}
	held := map[string][]byte{}
	summaries := map[string]cardigann.Summary{}
	for _, s := range base.Definitions {
		summaries[s.File] = s
	}
	keepOld := func(file string, old []byte) {
		defs[file] = old
		cat.Definitions = slices.DeleteFunc(cat.Definitions,
			func(s cardigann.Summary) bool { return s.File == file })
		cat.Definitions = append(cat.Definitions, summaries[file])
		cat.Held = append(cat.Held, file)
	}

	for _, s := range slices.Clone(cat.Definitions) {
		old, err := definitions.Raw(dir, s.File)
		if errors.Is(err, definitions.ErrNotFound) {
			res.Added = append(res.Added, s.File)
			continue
		}
		if err != nil {
			return res, err
		}
		if bytes.Equal(old, defs[s.File]) {
			continue
		}
		reasons, err := trustChanges(old, defs[s.File])
		if err != nil {
			return res, err
		}
		if len(reasons) == 0 || !inUse(s.File) {
			res.Changed = append(res.Changed, s.File)
			continue
		}
		next := defs[s.File]
		sha := sourceSHA(next)
		keepOld(s.File, old)
		res.Held = append(res.Held, s.File)
		if prev.Rejected[s.File] == sha {
			proposed.Rejected[s.File] = sha
			continue
		}
		held[s.File] = next
		proposed.Pending = append(proposed.Pending, Pending{
			File: s.File, Name: s.Name, Reasons: reasons, SHA256: sha,
		})
	}
	for _, s := range base.Definitions {
		if _, ok := defs[s.File]; ok {
			continue
		}
		if !inUse(s.File) {
			res.Removed = append(res.Removed, s.File)
			continue
		}
		old, err := definitions.Raw(dir, s.File)
		if err != nil {
			return res, err
		}
		keepOld(s.File, old)
		res.Held = append(res.Held, s.File)
		proposed.Pending = append(proposed.Pending, Pending{
			File: s.File, Name: s.Name, Removed: true,
			Reasons: []string{"removed upstream"},
		})
	}
	snapshot.Sort(&cat)
	if err := swap(definitions.Current(dir), func(out string) error {
		return snapshot.Write(out, cat, defs)
	}); err != nil {
		return res, err
	}
	if err := swap(pendingDir(dir), func(out string) error {
		return writePending(out, proposed, held)
	}); err != nil {
		return res, err
	}
	definitions.Invalidate(dir)
	return res, nil
}

// swap writes a directory beside dst and renames it into place, so a reader
// sees the old set or the new one, never half of each.
func swap(dst string, write func(out string) error) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o750); err != nil {
		return err
	}
	if err := write(tmp); err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	old := dst + ".old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

func latest(ctx context.Context, client *http.Client) (cardigann.Upstream, error) {
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
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(c.SHA) {
		return cardigann.Upstream{}, fmt.Errorf(
			"github commits: unexpected sha %q",
			c.SHA,
		)
	}
	return cardigann.Upstream{
		Repository: repository, Commit: c.SHA, Date: c.Commit.Committer.Date,
	}, nil
}

// archive reads every schema directory's definitions and VERSIONS out of the
// commit's tarball. Anything but a regular file is left out.
func archive(
	ctx context.Context,
	client *http.Client,
	commit string,
) ([]snapshot.File, string, error) {
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
		files    []snapshot.File
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
		files = append(files, snapshot.File{Version: version, Name: name, Data: b})
	}
	if versions == "" || len(files) == 0 {
		return nil, "", fmt.Errorf(
			"upstream archive: no %v or VERSIONS", snapshot.Prefixes(),
		)
	}
	return files, versions, nil
}

var urlHost = regexp.MustCompile(`(?i)\bhttps?://([^/"'\s?#]+)`)

// trustChanges names what a change does to where a definition sends things
// and how it logs in: the hosts it names (links, legacy links, any absolute
// URL anywhere in it) and its login block.
func trustChanges(old, next []byte) ([]string, error) {
	var a, b cardigann.Definition
	if err := json.Unmarshal(old, &a); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(next, &b); err != nil {
		return nil, err
	}
	var reasons []string
	ha, hb := hosts(old), hosts(next)
	var added, gone []string
	for h := range hb {
		if !ha[h] {
			added = append(added, h)
		}
	}
	for h := range ha {
		if !hb[h] {
			gone = append(gone, h)
		}
	}
	slices.Sort(added)
	slices.Sort(gone)
	if len(added) > 0 {
		reasons = append(reasons, "new domains: "+strings.Join(added, ", "))
	}
	if len(gone) > 0 {
		reasons = append(reasons, "dropped domains: "+strings.Join(gone, ", "))
	}
	la, err := json.Marshal(a.Login)
	if err != nil {
		return nil, err
	}
	lb, err := json.Marshal(b.Login)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(la, lb) {
		reasons = append(reasons, "login changed")
	}
	return reasons, nil
}

func hosts(raw []byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range urlHost.FindAllSubmatch(raw, -1) {
		out[strings.ToLower(string(m[1]))] = true
	}
	return out
}

func sourceSHA(raw []byte) string {
	var d struct {
		Source *cardigann.Source `json:"source"`
	}
	if json.Unmarshal(raw, &d) != nil || d.Source == nil {
		return ""
	}
	return d.Source.SHA256
}

func readPending(dir string) (pendingIndex, error) {
	var p pendingIndex
	b, err := os.ReadFile(filepath.Join(pendingDir(dir), "index.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(b, &p)
}

func writePending(out string, p pendingIndex, held map[string][]byte) error {
	data := filepath.Join(out, "data")
	if err := os.MkdirAll(data, 0o750); err != nil {
		return err
	}
	for file, b := range held {
		if err := os.WriteFile(
			filepath.Join(data, file+".json"),
			b,
			0o600,
		); err != nil {
			return err
		}
	}
	b, err := cardigann.EncodeJSON(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "index.json"), b, 0o600)
}

// current reads dir's pending index when it was written against the
// snapshot in force: a binary upgraded past the runtime snapshot makes what
// it held moot until the next run.
func current(dir string) (pendingIndex, error) {
	p, err := readPending(dir)
	if err != nil {
		return p, err
	}
	snap, err := definitions.Snapshot(dir)
	if err != nil {
		return p, err
	}
	if p.Upstream.Commit != snap.Upstream.Commit {
		return pendingIndex{}, nil
	}
	return p, nil
}

// List returns the updates held for approval.
func List(dir string) ([]Pending, error) {
	mu.Lock()
	defer mu.Unlock()
	p, err := current(dir)
	return p.Pending, err
}

// Diff is a unified diff from the definition in force to the held update.
func Diff(dir, file string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	p, err := current(dir)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(p.Pending, func(x Pending) bool { return x.File == file })
	if i < 0 {
		return "", ErrNoUpdate
	}
	old, err := definitions.Raw(dir, file)
	if err != nil {
		return "", err
	}
	var next []byte
	if !p.Pending[i].Removed {
		//nolint:gosec // G304: file matched a pending entry this package wrote
		if next, err = os.ReadFile(
			filepath.Join(pendingDir(dir), "data", file+".json"),
		); err != nil {
			return "", err
		}
	}
	a, b := string(old), string(next)
	edits := myers.ComputeEdits(span.URIFromPath(file), a, b)
	return fmt.Sprint(gotextdiff.ToUnified(file, file, a, edits)), nil
}

// Approve puts a held update in force.
func Approve(dir, file string) error {
	mu.Lock()
	defer mu.Unlock()
	p, err := current(dir)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(p.Pending, func(x Pending) bool { return x.File == file })
	if i < 0 {
		return ErrNoUpdate
	}
	entry := p.Pending[i]
	cur := definitions.Current(dir)
	cat, err := definitions.Snapshot(dir)
	if err != nil {
		return err
	}
	cat.Definitions = slices.DeleteFunc(cat.Definitions,
		func(s cardigann.Summary) bool { return s.File == file })
	cat.Held = slices.DeleteFunc(cat.Held, func(f string) bool { return f == file })
	target := filepath.Join(cur, "data", file+".json")
	if entry.Removed {
		if err := os.Remove(target); err != nil {
			return err
		}
	} else {
		held := filepath.Join(pendingDir(dir), "data", file+".json")
		//nolint:gosec // G304: file matched a pending entry this package wrote
		b, err := os.ReadFile(held)
		if err != nil {
			return err
		}
		var d cardigann.Definition
		if err := json.Unmarshal(b, &d); err != nil {
			return err
		}
		//nolint:gosec // G306: world-readable, like the snapshot it joins
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		cat.Definitions = append(cat.Definitions, cardigann.Summarize(file, &d))
	}
	snapshot.Sort(&cat)
	if err := snapshot.WriteIndex(cur, cat); err != nil {
		return err
	}
	definitions.Invalidate(dir)
	return dropPending(dir, p, i, "")
}

// Reject keeps the definition in force and remembers the refused version,
// so the next run does not propose it again; a later upstream change is.
func Reject(dir, file string) error {
	mu.Lock()
	defer mu.Unlock()
	p, err := current(dir)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(p.Pending, func(x Pending) bool { return x.File == file })
	if i < 0 {
		return ErrNoUpdate
	}
	return dropPending(dir, p, i, p.Pending[i].SHA256)
}

func dropPending(dir string, p pendingIndex, i int, rejected string) error {
	file := p.Pending[i].File
	p.Pending = slices.Delete(p.Pending, i, i+1)
	if rejected != "" {
		p.Rejected = maps.Clone(p.Rejected)
		if p.Rejected == nil {
			p.Rejected = map[string]string{}
		}
		p.Rejected[file] = rejected
	}
	held := filepath.Join(pendingDir(dir), "data", file+".json")
	if err := os.Remove(held); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	b, err := cardigann.EncodeJSON(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pendingDir(dir), "index.json"), b, 0o600)
}
