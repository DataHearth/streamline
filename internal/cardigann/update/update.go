// Package update keeps the catalog of Cardigann definitions current with
// upstream and brings the copy each configured built-in indexer carries
// along — Prowlarr's IndexerDefinitionUpdateService, with one difference
// that matters: a change that would move a configured tracker to other
// domains, or change how its login works, is held until an administrator
// approves it. A definition is code that receives the tracker credentials;
// an upstream edit should not re-point them unseen.
//
// The catalog lives in memory (internal/cardigann/definitions); an indexer's
// entry names the upstream commit its definition is read at, and the
// version itself is kept under the data directory. What is held, or was
// turned down, is forgotten on restart and worked out again by the next
// run.
package update

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
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
	"github.com/datahearth/streamline/internal/config"
)

// ErrNoUpdate is an approval or rejection for a definition with no update
// held.
var ErrNoUpdate = errors.New("no update is held for that definition")

var (
	// mu serialises fetches, runs, approvals and rejections.
	mu   sync.Mutex
	held = map[string]Pending{}
	// rejected maps a file to the upstream sha256 an administrator turned
	// down; that version is not proposed again, a later one is.
	rejected = map[string]string{}
)

// Pending is a new version of a definition configured indexers run, held
// for an administrator: one that re-points its hosts or its login, or any
// change at all when the configuration is read-only.
type Pending struct {
	File     string
	Name     string
	Reasons  []string
	Indexers []string
	// Commit is the upstream revision of the new version.
	Commit string
	// Diff runs from the version the first of Indexers is pinned to.
	Diff string
	spec []byte
}

// Result says what a run did.
type Result struct {
	Commit string
	// Current is a run that found upstream where the catalog already was.
	Current bool
	// Applied names the indexers moved to a new version of their definition.
	Applied []string
	// Held names the definitions held for an administrator.
	Held    []string
	Warning string
}

// Fetch puts upstream's newest commit in force as the catalog, unless it
// already is.
func Fetch(ctx context.Context, client *http.Client) (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	return fetch(ctx, client)
}

// Ensure fetches the catalog when nothing has been fetched since the
// process started.
func Ensure(ctx context.Context, client *http.Client) error {
	mu.Lock()
	defer mu.Unlock()
	if definitions.Current() != nil {
		return nil
	}
	_, err := fetch(ctx, client)
	return err
}

func fetch(ctx context.Context, client *http.Client) (Result, error) {
	up, err := snapshot.Latest(ctx, client)
	if err != nil {
		return Result{}, err
	}
	res := Result{Commit: up.Commit}
	if s := definitions.Current(); s != nil && s.Upstream().Commit == up.Commit {
		res.Current = true
		return res, nil
	}
	cat, defs, warning, err := snapshot.Fetch(ctx, client, up)
	if err != nil {
		return res, err
	}
	res.Warning = warning
	definitions.Install(definitions.NewSet(cat, defs))
	return res, nil
}

// Run fetches upstream and brings every configured built-in indexer to the
// catalog's version of its definition: a change that leaves its hosts and
// its login alone is pinned in its entry, any other is held. A definition
// upstream no longer carries, or that this converter refuses, leaves the
// indexers running it on the version they are pinned to. Kept versions no
// entry is pinned to any more are removed.
func Run(ctx context.Context, client *http.Client) (Result, error) {
	mu.Lock()
	defer mu.Unlock()
	res, err := fetch(ctx, client)
	if err != nil {
		return res, err
	}
	set := definitions.Current()
	commit := set.Upstream().Commit
	cfg := config.Get()
	apply := map[string][]string{}
	next := map[string]Pending{}
	for _, ix := range cfg.Indexers {
		if ix.Protocol != "cardigann" || ix.Cardigann == nil {
			continue
		}
		file, pinned := ix.Cardigann.Definition, ix.Cardigann.Commit
		latest, err := set.Raw(file)
		if errors.Is(err, definitions.ErrNotFound) {
			continue
		}
		if err != nil {
			return res, err
		}
		if pinned == commit {
			if _, err := definitions.Resolve(
				ctx, client, cfg.DataDir, file, commit,
			); err != nil {
				return res, err
			}
			continue
		}
		var have []byte
		var reasons []string
		if pinned != "" {
			have, err = definitions.Resolve(ctx, client, cfg.DataDir, file, pinned)
			if err == nil {
				reasons, err = trustChanges(have, latest)
			}
			if err != nil {
				reasons = []string{
					"the pinned version does not read: " + err.Error(),
				}
			}
		}
		if bytes.Equal(bytes.TrimSpace(have), bytes.TrimSpace(latest)) {
			continue
		}
		if len(reasons) == 0 && !cfg.ReadOnly {
			apply[file] = append(apply[file], ix.Name)
			continue
		}
		if rejected[file] == sourceSHA(latest) {
			continue
		}
		p, ok := next[file]
		if !ok {
			def, err := definitions.Decode(latest)
			if err != nil {
				return res, err
			}
			p = Pending{
				File: file, Name: def.Name, Commit: commit, spec: latest,
				Diff: diff(file, string(have), string(latest)),
			}
		}
		p.Indexers = append(p.Indexers, ix.Name)
		for _, r := range reasons {
			if !slices.Contains(p.Reasons, r) {
				p.Reasons = append(p.Reasons, r)
			}
		}
		next[file] = p
	}
	for _, file := range slices.Sorted(maps.Keys(apply)) {
		latest, err := set.Raw(file)
		if err != nil {
			return res, err
		}
		if err := pin(
			ctx,
			cfg.DataDir,
			file,
			commit,
			latest,
			apply[file],
		); err != nil {
			return res, err
		}
		res.Applied = append(res.Applied, apply[file]...)
	}
	keep := map[string]bool{}
	for _, ix := range config.Get().Indexers {
		if ix.Cardigann != nil {
			keep[ix.Cardigann.Commit] = true
		}
	}
	if err := definitions.Prune(cfg.DataDir, keep); err != nil {
		slog.WarnContext(ctx, "could not remove unused built-in indexer definitions",
			"error", err)
	}
	held = next
	res.Held = slices.Sorted(maps.Keys(held))
	return res, nil
}

func pin(
	ctx context.Context,
	dataDir, file, commit string,
	spec []byte,
	indexers []string,
) error {
	if err := definitions.Keep(dataDir, file, commit, spec); err != nil {
		return err
	}
	return config.PinCardigann(ctx, indexers, commit)
}

func diff(file, a, b string) string {
	edits := myers.ComputeEdits(span.URIFromPath(file), a, b)
	return fmt.Sprint(gotextdiff.ToUnified(file, file, a, edits))
}

// List returns the updates held for approval, by file name.
func List() []Pending {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Pending, 0, len(held))
	for _, p := range held {
		out = append(out, p)
	}
	slices.SortFunc(
		out,
		func(a, b Pending) int { return cmp.Compare(a.File, b.File) },
	)
	return out
}

// Approve writes a held update into the entries of the indexers it was held
// for. A read-only configuration refuses it (config.ErrReadOnly): there the
// file is edited by hand, and the next run finds the update in force.
func Approve(ctx context.Context, file string) error {
	mu.Lock()
	defer mu.Unlock()
	p, ok := held[file]
	if !ok {
		return ErrNoUpdate
	}
	if err := pin(
		ctx, config.Get().DataDir, file, p.Commit, p.spec, p.Indexers,
	); err != nil {
		return err
	}
	delete(held, file)
	return nil
}

// Reject keeps the version the indexers carry and remembers the refused
// one, so the next run does not propose it again; a later upstream change
// is.
func Reject(file string) error {
	mu.Lock()
	defer mu.Unlock()
	p, ok := held[file]
	if !ok {
		return ErrNoUpdate
	}
	rejected[file] = sourceSHA(p.spec)
	delete(held, file)
	return nil
}

// urlHost reads a host off an absolute URL or a scheme-relative one: a path
// written `//other.example/x` resolves against the site to another host, so
// it re-points a query or header secret as surely as `https://` does.
var urlHost = regexp.MustCompile(`(?i)(?:\bhttps?:)?//([^/"'\s?#\\]+)`)

// trustChanges names what a change does to where a definition sends things
// and how it logs in: the hosts it names (links, legacy links, any absolute
// or scheme-relative URL anywhere in it) and its login block.
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
