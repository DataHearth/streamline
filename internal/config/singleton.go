package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

var (
	mu         sync.RWMutex
	current    *Config
	cfgPath    string
	rawK       *koanf.Koanf
	envOverlay *envLayer
)

// envLayer is the environment's contribution to the loaded config: every key
// STREAMLINE_* supplied, plus the config file's own keys so write-back can put
// the file's values back in their place.
//
// Update rewrites the file from the whole in-memory struct, and that struct is
// the merge of defaults, file and environment. Without this, the first
// write-back — the generated session secret on first boot, a Plex client ID,
// any settings change from the UI — would copy every STREAMLINE_*-injected
// inline secret into the file in plaintext. Injecting a secret by environment
// is precisely how a container keeps it off the volume, so absorbing it there
// defeats the point.
type envLayer struct {
	// file holds only what the config file itself carried: no defaults, no
	// environment. A key it does not have is a key the file never claimed, and
	// write-back leaves it out rather than inventing a value for it.
	file *koanf.Koanf
	keys []string
}

var (
	// ErrNoPath means the config was loaded from a reader (not a file path) so
	// Update has no file to write back to.
	ErrNoPath = errors.New("config has no backing file path")

	// ErrReadOnly means the config is declared read_only, so Update refuses
	// every write-back. Declarative/GitOps deploys mount config read-only and
	// change it through git, not the UI; runtime-generated state
	// (plex_client_id, session secret) is surfaced to the operator instead of
	// persisted here.
	ErrReadOnly = errors.New("config is read-only")

	// ErrEnvOwned means the mutation changed a key the environment supplies.
	// The write-back cannot record such a key — withholding it is the whole
	// point of the env layer — so the change would hold only in memory and be
	// gone at the next restart, with the environment's value authoritative
	// again. Update refuses rather than report a success it cannot keep: an
	// admin rotating a compromised signing secret must not be handed a fresh
	// token and a 200 while the compromised value stays on the shelf.
	//
	// Only a key this update actually changed conflicts. Every other
	// environment-owned key is withheld silently, as it always was — that is
	// an unrelated setting keeping its provenance, not a lost write.
	ErrEnvOwned = errors.New(
		"these settings come from the environment and cannot be changed here",
	)

	// ErrWriteBackUnloadable means this update is what would leave the config
	// file unable to load — pointing auth.session_secret_file at a path that
	// does not exist, say. The file on disk is untouched, and backing that part
	// of the change out is what clears it.
	//
	// It names only the reasons this update adds. A reason the file already had
	// before anything proposed a change is warned about and saved through, and
	// having one does not buy the update a second; see Update.
	ErrWriteBackUnloadable = errors.New(
		"the config file this would write cannot be loaded back",
	)
)

// Get returns a pointer to the current singleton config. Callers must treat
// the returned value as read-only; use Update to mutate.
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// store sets the singleton config (package-internal). k is the fully merged
// load-time koanf (defaults + file + env); it is retained for HiddenString.
// layer describes what the environment contributed to that merge.
//
// This is the one commit point for a load, so a Load that fails anywhere —
// unmarshal, validation, secret files — leaves every one of these describing
// the last load that succeeded.
//
// A nil layer becomes an empty one, so envOverlay and its file koanf are
// non-nil for every caller of stripEnvLayerLocked whatever it was handed. The
// alternative is an invariant tying the layer to p being non-empty, which only
// holds because Update happens to check cfgPath first — one reordering away
// from a nil dereference in the middle of a write-back.
func store(c *Config, p string, k *koanf.Koanf, layer *envLayer) {
	mu.Lock()
	defer mu.Unlock()
	if layer == nil {
		layer = &envLayer{}
	}
	if layer.file == nil {
		layer.file = koanf.New(".")
	}
	current = c
	cfgPath = p
	rawK = k
	envOverlay = layer
}

// HiddenString reads a raw koanf key that is deliberately NOT part of the
// public config surface (no struct field, no defaults() entry, no schema
// entry) — test-only seams such as metadata.tmdb.base_url. Returns "" when
// unset. Hidden keys are read once at boot by their consumers; config.Update
// rewrites the file from the struct, so a hidden key is erased from disk on
// the first write-back (the in-memory snapshot keeps it). Acceptable because
// these keys exist only for e2e.
func HiddenString(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if rawK == nil {
		return ""
	}
	return rawK.String(key)
}

// ResetForTest clears the singleton. Tests only.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	current = nil
	cfgPath = ""
	rawK = nil
	envOverlay = nil
}

// stripEnvLayerLocked takes every key the environment supplied back out of k
// and reports the keys it touched. A key the file itself carries returns to the
// file's value; a key the file never carried is deleted, so the write-back
// stays silent about it instead of asserting a default the process is not
// running on. Keys the environment set but the Config struct has no field for —
// hidden test seams, unrelated STREAMLINE_* vars — never reach k, so they are
// skipped; so are list-nested keys, since koanf keeps a slice as one leaf value
// and reaching inside it here would rewrite the list as a map.
//
// Deleting rather than defaulting is what keeps the file honest: it describes
// the settings it owns and nothing else. It does not make an environment-owned
// setting survive the variable going away — nothing written to this file could,
// short of copying the environment's value in, which is exactly the leak this
// package exists to prevent and would make an env-supplied
// auth.trusted_networks or server.trusted_proxies permanent instead of
// evaporating with the variable. Load says so out loud instead
// (announceEnvLayer, warnDefaultedKeys, warnEnvShadowsFile), and Update refuses
// the write that would count on it (envOwnedWritesLocked).
//
// Held by Update's mu.Lock, which is what makes reading envOverlay safe. store
// sets it together with current, and Update refuses to run without current.
func stripEnvLayerLocked(k *koanf.Koanf) ([]string, error) {
	var stripped []string
	for _, key := range envOverlay.keys {
		if !k.Exists(key) {
			continue
		}
		if envOverlay.file.Exists(key) {
			if err := k.Set(key, envOverlay.file.Get(key)); err != nil {
				return nil, fmt.Errorf("restore %q: %w", key, err)
			}
		} else {
			k.Delete(key)
		}
		stripped = append(stripped, key)
	}
	return stripped, nil
}

// Update deep-clones the current config, runs fn, validates the result,
// writes it atomically to the backing file, then swaps the singleton.
// Returns ErrNoPath if no file path was captured at Load time.
// Update holds mu.Lock across the full clone/validate/write/swap sequence.
// This blocks concurrent Get readers for the duration of disk I/O; acceptable
// because Update is expected to fire only on admin-triggered settings changes.
//
// Update either persists everything fn changed or changes nothing at all — no
// file written, no singleton swapped, and no directory created on the way to
// deciding. A caller that got nil can act on the change having reached disk.
//
// It refuses with ErrEnvOwned when fn writes a key the environment supplies,
// and with ErrWriteBackUnloadable when this update is what would leave the
// file unloadable.
//
// It does not refuse a file that already needed the environment to load: an
// env-supplied quality_default_profile whose profile list lives in the file, a
// data_dir the file leaves empty, a session_secret_file naming a path only the
// environment corrects. Those files are one dropped variable away from a boot
// that fails whether or not anything saves — the write-back neither caused it
// nor can fix it, since recording the environment's value is the leak this
// layer exists to prevent. Update logs the reason and saves.
//
// The trade is deliberate. Refusing instead makes every settings save fail
// permanently — adding an indexer, registering the Plex client ID, changing a
// schedule, rotating a secret — on an install that boots and runs fine, with
// no way out but hand-editing the file the UI is supposed to manage. So the
// cost lands the other way: a file in that state keeps being saved, and stays
// as unloadable-without-the-environment as it was found. Load warns about it
// on every boot (warnFileNeedsEnv) and Update warns on every save.
//
// That trade is struck one reason at a time, not one file at a time — and a
// reason is one rule broken, a struct tag or an invariant or an unreadable
// secret file, not a phase and not a file. One rule broken among the ones that
// ran: a field whose first tag fails hides its later tags from both sides of
// the comparison (see Config.Validate), so this compares the reasons it can
// see rather than every reason the two files have. The reasons the file already had
// are warned about and saved through; a reason only the proposed file has is
// this update's doing and refuses it, however many others the file was already
// carrying and whichever rule found them. An install propped up in one place
// is not an install that may be broken in another, and the key an update
// breaks is often one the environment does not supply at all — nothing would
// put that value back.
func Update(ctx context.Context, fn func(*Config) error) error {
	mu.Lock()
	defer mu.Unlock()

	if current == nil {
		slog.ErrorContext(ctx, "config update called before load")
		return errors.New("config not loaded")
	}
	if current.ReadOnly {
		return ErrReadOnly
	}
	if cfgPath == "" {
		return ErrNoPath
	}

	cloned, err := cloneLocked()
	if err != nil {
		slog.ErrorContext(ctx, "config clone failed", "error", err)
		return fmt.Errorf("clone: %w", err)
	}
	if err := fn(cloned); err != nil {
		return err
	}
	if err := cloned.Validate(); err != nil {
		slog.ErrorContext(ctx, "config validation failed", "error", err)
		return fmt.Errorf("validate: %w", err)
	}
	next, err := flatten(cloned)
	if err != nil {
		slog.ErrorContext(ctx, "config flatten failed", "error", err)
		return fmt.Errorf("flatten: %w", err)
	}
	if err := envOwnedWritesLocked(next); err != nil {
		slog.WarnContext(
			ctx,
			"refused a config change that the environment would override again at the next restart",
			"error",
			err,
			"path",
			cfgPath,
		)
		return err
	}
	withheld, err := stripEnvLayerLocked(next)
	if err != nil {
		slog.ErrorContext(ctx, "config strip failed", "error", err)
		return fmt.Errorf("strip: %w", err)
	}
	if err := reportLoadableLocked(ctx, next); err != nil {
		return err
	}
	if err := writeYAMLAtomic(cfgPath, next); err != nil {
		slog.ErrorContext(
			ctx,
			"failed to save config",
			"path",
			cfgPath,
			"error",
			err,
		)
		return fmt.Errorf("write: %w", err)
	}
	current = cloned
	if len(withheld) > 0 {
		slog.WarnContext(
			ctx,
			"these settings come from the environment and were left out of the config file — change them there, not here",
			"config.env.keys",
			strings.Join(withheld, ", "),
			"path",
			cfgPath,
		)
	}
	slog.InfoContext(ctx, "config saved to disk", "path", cfgPath)
	return nil
}

func cloneLocked() (*Config, error) {
	k, err := flatten(current)
	if err != nil {
		return nil, err
	}
	var out Config
	if err := k.Unmarshal("", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// flatten renders c as a flat koanf: one entry per dotted config key, with a
// list held whole as a single leaf. This is the shape the write-back marshals
// and the shape envLayer's keys address, so it is also the shape to compare two
// configs in — a key that does not exist here is not a config key at all.
func flatten(c *Config) (*koanf.Koanf, error) {
	k := koanf.New(".")
	if err := k.Load(structs.Provider(*c, "koanf"), nil); err != nil {
		return nil, err
	}
	return k, nil
}

// envOwnedWritesLocked reports ErrEnvOwned naming every environment-owned key
// whose value next changes, and nil when the update leaves them all alone.
//
// The comparison is against the running config rather than against the
// environment. What it catches is the write that would vanish —
// stripEnvLayerLocked is about to drop this key from the bytes, so the new
// value would live in the singleton until the process restarted onto the
// environment's. An update that only touches keys the file owns has nothing to
// catch.
//
// Setting an environment-owned key to the value the environment already
// supplies is not a conflict either, but it is a silent no-op: the strip drops
// it like any other, so an operator typing that value into the UI to pin it in
// the file is told the save succeeded and gets no line in the file, and the
// variable is still what supplies it after a restart. Nothing can be done
// about that here — the update is indistinguishable from one that never
// touched the key, and writing the value is the leak the env layer exists to
// prevent. The withheld-keys warning at the end of Update names every key it
// dropped, which is where an operator can see it happen.
//
// Held by Update's mu.Lock, which is what makes reading current and envOverlay
// safe.
func envOwnedWritesLocked(next *koanf.Koanf) error {
	prev, err := flatten(current)
	if err != nil {
		return fmt.Errorf("flatten current: %w", err)
	}
	var conflicts []string
	for _, key := range envOverlay.keys {
		if !next.Exists(key) {
			continue
		}
		if reflect.DeepEqual(next.Get(key), prev.Get(key)) {
			continue
		}
		conflicts = append(conflicts, key)
	}
	if len(conflicts) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrEnvOwned, strings.Join(conflicts, ", "))
}

// loadIssue is one reason a boot would refuse a config: an id, and the error
// an operator reads. Two configs carrying the same defect produce the same id,
// which is what lets a caller tell a reason it inherited from one it is
// introducing — the question Update actually has to answer, and one that
// "loadable" and "unloadable" cannot express between them.
//
// A struct tag's id is the field and the rule, so the same field failing the
// same rule is one issue whatever value it holds. Every other reason — the
// invariants, the secret-file reads, a value that will not decode — is keyed
// on its message, which names the offending value: two rejected values of one
// key are two ids, so an update moving between them reads as introducing a
// reason and is refused. That is the direction with a way out.
//
// The struct-tag half is value-blind, and that is the asymmetry: re-pointing
// one field at a second value the same tag rejects keeps the id, so it reads as
// inherited and saves where the message-keyed half would refuse. Nothing
// reaches it — a tag failure the write-back has and the loaded file does not
// needs an env-owned key, and changing one of those hits ErrEnvOwned first —
// so it is recorded, not fixed.
type loadIssue struct {
	id  string
	err error
}

// loadIssues reports every reason a boot with no environment layer would
// refuse these keys, and nothing when it would start on them. Merging them
// over the defaults reproduces exactly the layer stack Load builds once the
// STREAMLINE_* variables are gone. Its argument is a file's own keys — the
// post-strip write-back, or fileK at load time.
//
// It runs the schedules-alias expansion, Validate and loadSecretFiles: what
// Load runs before it has a config, less ensureDataDir. Creating data_dir is a
// boot step and this only ever asks questions — about a config that may never
// be written, on a host that may not be the one that starts on it, where a
// directory this process cannot make may be mounted by the time the restart
// wants it. So it creates nothing and stores nothing, and a data_dir the next
// boot cannot create is not among the reasons it can report.
//
// Every reason is reported, not the first: each phase runs whatever the ones
// before it found, and each reports every rule it finds broken rather than
// returning on one. That is what lets Update tell a reason it inherited from
// one it is introducing, and the difference is not cosmetic — a phase that
// stopped early put the same inherited reason at the head of both lists, and
// Update saved a write it would otherwise have refused.
//
// Within a phase that holds field by field, not rule by rule: validator stops
// at a field's first failing tag, so a rule behind it does not run and is not
// among the reasons until the one in front of it is fixed (see Config.Validate).
// So this reports every reason it can see, which is not always every reason
// the file has.
//
// loadableConfig can still speak alone, in principle, on keys that will not
// assemble into a Config at all. Nothing reaches that today: both of its early
// returns are koanf merges, which only fail under StrictMerge or a custom merge
// func, and this package configures neither.
func loadIssues(fileKeys *koanf.Koanf) []loadIssue {
	c, issues := loadableConfig(fileKeys)
	if c == nil {
		return issues
	}
	issues = append(issues, splitIssues(c.Validate())...)
	if _, err := loadSecretFiles(c); err != nil {
		issues = append(issues, splitIssues(err)...)
	}
	return issues
}

// loadableConfig rebuilds the Config a boot with no environment layer would
// unmarshal from these keys, and reports what the unmarshal itself refused.
//
// It returns that half-built config rather than nothing, because decoding
// fills in every key it can and reports the rest: a value of the wrong type is
// one reason among the reasons, not a licence to stop counting. A file whose
// port the environment corrects used to report that alone, on both sides of
// Update's comparison, so an update that broke something else entirely saved.
// The key that failed to decode is left at its zero value and the struct tags
// report it a second time, which is a duplicate on both sides and inherited
// like the first.
//
// The config is nil only when the keys will not assemble at all, which is the
// one reason there is to report — a branch nothing currently reaches, since
// both merges below funnel into koanf's merge and it only errors under
// StrictMerge or a custom merge func. Kept because that is a koanf option away,
// not because it fires.
func loadableConfig(fileKeys *koanf.Koanf) (*Config, []loadIssue) {
	k := newDefaultsKoanf()
	if err := k.Merge(fileKeys); err != nil {
		return nil, []loadIssue{{id: err.Error(), err: err}}
	}
	if _, err := applyRenamedScheduleKeys(k); err != nil {
		return nil, []loadIssue{{id: err.Error(), err: err}}
	}
	var c Config
	return &c, splitIssues(k.Unmarshal("", &c))
}

// splitIssues takes an error apart into one issue per reason it carries, so a
// reason the caller inherited does not stand for one it just introduced. A
// joined error — Validate's two halves, loadSecretFiles' unreadable paths —
// becomes its causes, a validator failure becomes one issue per field and rule
// broken, and anything else is a single issue keyed on its message.
//
// The joined case is tried first because errors.AsType walks the whole tree:
// asking for the field errors first finds them inside Validate's join and
// answers with them alone, dropping every invariant joined beside them.
func splitIssues(err error) []loadIssue {
	if err == nil {
		return nil
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		var out []loadIssue
		for _, cause := range joined.Unwrap() {
			out = append(out, splitIssues(cause)...)
		}
		return out
	}
	if fields, ok := errors.AsType[validator.ValidationErrors](err); ok {
		out := make([]loadIssue, 0, len(fields))
		for _, f := range fields {
			out = append(out, loadIssue{id: f.Namespace() + " " + f.Tag(), err: f})
		}
		return out
	}
	return []loadIssue{{id: err.Error(), err: err}}
}

// issueError renders issues as the one error a caller reads, and nil for none.
func issueError(issues []loadIssue) error {
	errs := make([]error, 0, len(issues))
	for _, issue := range issues {
		errs = append(errs, issue.err)
	}
	return errors.Join(errs...)
}

// checkLoadable answers whether a boot with no environment layer would accept
// these keys at all: loadIssues rendered as one error, nil when there is
// nothing to report. A caller that has to tell an inherited reason from one it
// caused wants loadIssues instead — this answer cannot express the difference.
func checkLoadable(fileKeys *koanf.Koanf) error {
	return issueError(loadIssues(fileKeys))
}

// currentWriteBackLoadableLocked reports the same reasons about the file the
// running config would produce if it were saved untouched — the state the
// operator is already in, before this update proposed anything.
//
// Failing to produce that file at all is itself reported as a reason, under an
// id nothing the proposed file can report will match: a baseline that cannot
// be computed must not read as a clean one, or every reason in the proposed
// file silently becomes pre-existing. Reading it as no baseline at all leaves
// the update refused, which is the direction with a way out.
//
// Held by Update's mu.Lock: it reads current and reaches through
// stripEnvLayerLocked into envOverlay.
func currentWriteBackLoadableLocked() []loadIssue {
	prev, err := flatten(current)
	if err != nil {
		return []loadIssue{{id: "baseline: " + err.Error(), err: err}}
	}
	if _, err := stripEnvLayerLocked(prev); err != nil {
		return []loadIssue{{id: "baseline: " + err.Error(), err: err}}
	}
	return loadIssues(prev)
}

// reportLoadableLocked applies the policy Update states: it returns
// ErrWriteBackUnloadable naming the reasons this update introduces, and warns
// — letting the save through — for the ones the file was already carrying.
//
// The comparison is reason against reason. Comparing loadable against
// unloadable made one inherited reason answer for every other: an install
// whose quality_default_profile came from the environment took a
// session_secret_file pointing at nothing — a key the file owns outright and
// the environment cannot put back — and the next boot stopped on it with every
// variable still set.
//
// It is worth no more than the lists it compares, which is why loadIssues
// reports every reason rather than the first: while a rule that failed ended
// its phase, the same inherited reason stood at the head of both lists and hid
// everything behind it, and deleting the quality profile the default names
// saved through on an install whose only other flaw was a proxy list the
// environment corrected.
//
// Held by Update's mu.Lock, which is what makes the baseline comparison read
// current and envOverlay safely.
func reportLoadableLocked(ctx context.Context, next *koanf.Koanf) error {
	issues := loadIssues(next)
	if len(issues) == 0 {
		return nil
	}
	inherited := map[string]bool{}
	for _, issue := range currentWriteBackLoadableLocked() {
		inherited[issue.id] = true
	}
	var caused, kept []loadIssue
	for _, issue := range issues {
		if inherited[issue.id] {
			kept = append(kept, issue)
			continue
		}
		caused = append(caused, issue)
	}
	if len(caused) > 0 {
		err := issueError(caused)
		slog.ErrorContext(
			ctx,
			"refused to write a config file the next boot could not load",
			"path",
			cfgPath,
			"error",
			err,
		)
		return fmt.Errorf("%w: %w", ErrWriteBackUnloadable, err)
	}
	slog.WarnContext(
		ctx,
		"saving a config file that does not load without the environment — a save of the running config with nothing changed carries every one of these reasons too, so this update neither introduced nor repaired them",
		"path",
		cfgPath,
		"error",
		issueError(kept),
	)
	return nil
}

// writeYAMLAtomic marshals k to YAML and persists it to dest.
//
// The file it writes is generated from k, not edited in place: values survive,
// bytes do not. Comments and key order are lost and every default the struct
// carries is materialised, so the file after the first write-back is far longer
// than the one the operator wrote.
//
// It stages the bytes in a temp file under the OS temp dir, then
// renames it into place — atomic when dest shares the temp dir's filesystem.
// When it does not (EXDEV), or dest is a single-file bind mount whose parent
// dir is read-only so rename cannot replace it (EBUSY), it overwrites dest in
// place instead. Staging in the OS temp dir rather than beside dest is what
// lets a read-only config directory (e.g. a Docker single-file bind mount)
// still be updated.
// Does not fsync — config writes are admin-driven and infrequent; a torn write
// on power loss is acceptable.
func writeYAMLAtomic(dest string, k *koanf.Koanf) error {
	data, err := k.Marshal(yaml.Parser())
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "streamline-config-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close(), os.Remove(tmpName))
	}
	if err := tmp.Close(); err != nil {
		return errors.Join(err, os.Remove(tmpName))
	}

	if err := os.Rename(tmpName, dest); err != nil {
		// Different filesystem (EXDEV) or single-file bind mount (EBUSY):
		// the rename cannot land, so overwrite dest in place.
		return errors.Join(
			os.WriteFile(dest, data, 0o600),
			os.Remove(tmpName),
		)
	}
	return nil
}
