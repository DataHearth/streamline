// Command cardigann-sync fetches the Prowlarr/Indexers definitions, converts
// them with internal/cardigann/convert and writes the embedded snapshot under
// internal/cardigann/definitions. Run it through `task cardigann:sync`.
//
// The output is a pure function of the upstream revision and the converter:
// no timestamps but the upstream commit's own, sorted listings, one file per
// definition. Re-running against the same revision is a no-op diff, so a
// sync PR shows exactly what upstream changed and nothing else.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/datahearth/streamline/internal/cardigann"
	"github.com/datahearth/streamline/internal/cardigann/snapshot"
)

const upstreamRepo = "https://github.com/Prowlarr/Indexers"

func main() {
	var (
		src = flag.String("src", "",
			"existing Prowlarr/Indexers checkout (default: fetch -ref)")
		ref = flag.String("ref", "master", "upstream branch, tag or commit")
		out = flag.String("out", "internal/cardigann/definitions",
			"output directory")
	)
	flag.Parse()
	// -ref only chooses what to fetch, so beside -src it would be silently
	// ignored and the NOTICE would name whatever the checkout holds instead.
	refSet := false
	flag.Visit(func(f *flag.Flag) { refSet = refSet || f.Name == "ref" })
	if refSet && *src != "" {
		fmt.Fprintln(os.Stderr, "cardigann-sync: pass -ref or -src, not both")
		os.Exit(2)
	}
	if err := run(*src, *ref, *out); err != nil {
		fmt.Fprintln(os.Stderr, "cardigann-sync:", err)
		os.Exit(1)
	}
}

func run(src, ref, out string) error {
	if src == "" {
		tmp, err := os.MkdirTemp("", "cardigann-sync-")
		if err != nil {
			return err
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		if err := fetch(tmp, ref); err != nil {
			return err
		}
		src = tmp
	}
	up, err := upstream(src)
	if err != nil {
		return err
	}
	versions, err := git(src, "cat-file", "blob", "HEAD:VERSIONS")
	if err != nil {
		return err
	}
	warning, err := snapshot.CheckVersions(versions)
	if err != nil {
		return err
	}
	if warning != "" {
		fmt.Fprintln(os.Stderr, "cardigann-sync: warning:", warning)
	}

	files, skipped, err := definitionBlobs(src)
	if err != nil {
		return err
	}
	cat, defs := snapshot.Build(files, up, skipped)
	if err := snapshot.Write(out, cat, defs); err != nil {
		return err
	}
	fmt.Printf("cardigann-sync: %s@%s — %d converted, %d skipped\n",
		upstreamRepo, up.Commit[:12], len(cat.Definitions), len(cat.Skipped))
	for _, s := range cat.Skipped {
		fmt.Printf("  skipped %s: %s\n", s.File, s.Reason)
	}
	return nil
}

// fetch makes a sparse, shallow, blob-filtered checkout of just the schema
// version we read: a full clone carries every frozen version since v1.
// Fetching by ref rather than cloning by branch is what lets -ref name a
// commit as well as a branch or tag.
func fetch(dir, ref string) error {
	version := fmt.Sprintf("/definitions/v%d/", cardigann.SchemaVersion)
	steps := [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", upstreamRepo + ".git"},
		{"sparse-checkout", "set", "--no-cone", version, "/VERSIONS"},
		{"fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", ref},
		{"checkout", "-q", "FETCH_HEAD"},
	}
	for _, args := range steps {
		if _, err := git(dir, args...); err != nil {
			return err
		}
	}
	return nil
}

// upstream reads the checkout's revision and refuses local edits: the
// snapshot's notice names an upstream commit, which a dirty tree is not.
//
// dir must be the top of its own repository: inside any other work tree, git
// would answer for that one — a copied definitions/ folder under a streamline
// checkout's gitignored tmp/ reads as clean and stamps streamline's HEAD into
// the NOTICE. Untracked and ignored files count as changes, set explicitly so
// no git configuration can hide one the glob would still read.
func upstream(dir string) (cardigann.Upstream, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return cardigann.Upstream{}, err
	}
	same, err := samePath(dir, strings.TrimSpace(top))
	if err != nil {
		return cardigann.Upstream{}, err
	}
	if !same {
		return cardigann.Upstream{}, fmt.Errorf(
			"%s is not the root of a Prowlarr/Indexers checkout", dir,
		)
	}
	status, err := git(dir, "status", "--porcelain", "--untracked-files=all",
		"--ignored=matching", "--", "definitions", "VERSIONS")
	if err != nil {
		return cardigann.Upstream{}, err
	}
	if strings.TrimSpace(status) != "" {
		return cardigann.Upstream{}, fmt.Errorf(
			"%s has local changes; sync from a clean upstream revision", dir,
		)
	}
	head, err := git(dir, "log", "-1", "--format=%H%n%cI")
	if err != nil {
		return cardigann.Upstream{}, err
	}
	commit, date, _ := strings.Cut(strings.TrimSpace(head), "\n")
	return cardigann.Upstream{
		Repository: upstreamRepo,
		Commit:     commit,
		Date:       date,
	}, nil
}

// definitionBlobs reads the schema directory's .yml files out of HEAD's tree
// rather than the work tree. What is converted is then exactly the commit the
// NOTICE names — nothing git status cannot see (an assume-unchanged or
// skip-worktree edit, a sparse checkout leaving files out) can change it, and
// a committed symlink is a symlink, never the file outside the checkout it
// points at. Anything but a plain file with an embeddable name is skipped
// with a reason, not fatal.
func definitionBlobs(dir string) ([]snapshot.File, []cardigann.Skipped, error) {
	prefix := fmt.Sprintf("definitions/v%d/", cardigann.SchemaVersion)
	list, err := git(dir, "ls-tree", "-z", "HEAD", "--", prefix)
	if err != nil {
		return nil, nil, err
	}
	var (
		names, shas []string
		skipped     []cardigann.Skipped
	)
	for entry := range strings.SplitSeq(strings.TrimSuffix(list, "\x00"), "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			continue
		}
		name := strings.TrimPrefix(path, prefix)
		if !strings.HasSuffix(name, ".yml") {
			continue
		}
		switch {
		case fields[0] != "100644" || fields[1] != "blob":
			skipped = append(skipped, cardigann.Skipped{
				File:   name,
				Reason: fmt.Sprintf("not a plain file (mode %s)", fields[0]),
			})
		case !snapshot.ValidName.MatchString(name):
			skipped = append(skipped, cardigann.Skipped{
				File: name, Reason: "file name cannot be embedded",
			})
		default:
			names = append(names, name)
			shas = append(shas, fields[2])
		}
	}
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("no definitions under %s at HEAD", prefix)
	}
	blobs, err := catBlobs(dir, shas)
	if err != nil {
		return nil, nil, err
	}
	out := make([]snapshot.File, len(names))
	for i := range names {
		out[i] = snapshot.File{Name: names[i], Data: blobs[i]}
	}
	return out, skipped, nil
}

// catBlobs reads many objects through one git cat-file --batch.
func catBlobs(dir string, shas []string) ([][]byte, error) {
	//nolint:gosec // G204: fixed git subcommand over object names git itself listed
	cmd := exec.Command("git", "-C", dir, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf(
			"git cat-file: %w: %s",
			err,
			strings.TrimSpace(stderr.String()),
		)
	}
	out := make([][]byte, 0, len(shas))
	for range shas {
		header, rest, ok := bytes.Cut(raw, []byte("\n"))
		f := strings.Fields(string(header))
		if !ok || len(f) != 3 || f[1] != "blob" {
			return nil, fmt.Errorf("git cat-file: unexpected %q", header)
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size+1 > len(rest) {
			return nil, fmt.Errorf("git cat-file: bad size in %q", header)
		}
		out = append(out, rest[:size])
		raw = rest[size+1:]
	}
	return out, nil
}

func samePath(a, b string) (bool, error) {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false, err
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false, err
	}
	ra, err = filepath.Abs(ra)
	if err != nil {
		return false, err
	}
	rb, err = filepath.Abs(rb)
	return ra == rb, err
}

func git(dir string, args ...string) (string, error) {
	//nolint:gosec // G204: fixed git subcommands; ref is the developer's own flag
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(b), nil
}
