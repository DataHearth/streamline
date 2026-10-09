# Native indexers (Cardigann definitions)

`internal/cardigann` is streamline's own reader for the Cardigann YAML definitions
Prowlarr and Jackett describe trackers in, so a stack can drop Prowlarr/Jackett. **What
exists today is the converter and the embedded snapshot** — the engine that runs a
definition, the `protocol: cardigann` indexer entry, the runtime definition update and the
catalog UI are not built yet. Nothing outside `internal/cardigann` imports it.

| Path | What it is |
|---|---|
| `internal/cardigann/` | The model (`Definition` and friends, upstream v11 schema key for key), the strict `Decode`, `Catalog`/`Summarize`, `EncodeJSON` |
| `internal/cardigann/convert/` | `Convert(path, yaml)` — every .NET-to-Go rewrite. Pure; no I/O |
| `internal/cardigann/cmd/cardigann-sync/` | The sync: fetch upstream, `Convert` each file, write the snapshot |
| `internal/cardigann/definitions/` | `//go:embed`s the snapshot; `Catalog()`, `Load(id)`, `Notice()`. Only `definitions.go` (and its tests) is hand-written |

## The snapshot is generated

`task cardigann:sync` (optionally `REF=<branch|tag|commit>` or `SRC=<checkout>`) makes a
sparse, shallow, blob-filtered git fetch of `Prowlarr/Indexers` — only
`definitions/v11/` and `VERSIONS` — converts every file, and rewrites
`internal/cardigann/definitions/{data/<id>.json,index.json,NOTICE}` **wholesale**, so a
definition deleted upstream disappears here too.

- **Never hand-edit the snapshot.** Fix the converter and re-sync. The
  `definitions` suite re-encodes every embedded definition and compares it byte for byte
  with the committed file; an edit, or a model field that fails to round-trip, fails it.
- **The output is a pure function of (upstream commit, converter).** No timestamp but the
  upstream commit's own, sorted listings, one file per definition. Re-running a sync
  against the same revision is a no-op diff, so a sync commit shows exactly what upstream
  changed. Commit a sync as its own change (`chore(cardigann): sync definitions to <sha>`).
- **`convert.Version` is bumped whenever the converter's output changes for the same
  input**, and the sync re-run in the same change. The catalog records the version; the
  test fails when the two disagree, which is what stops a converter fix landing without the
  data it was for.
- Git rather than a tarball: a fetch by ref accepts a commit as well as a branch, and the
  commit SHA comes straight from the checkout for the NOTICE. A `-src` checkout with local
  changes is refused — the NOTICE would name a revision the files are not.
- `cardigann.SchemaVersion` (11) pins the upstream directory read. The sync fails when
  upstream's `VERSIONS` puts it below `MIN_VERSION` — a frozen version still reads fine and
  is never updated again, so syncing it would ship a snapshot that looks fresh and is not —
  and warns when `CURRENT_VERSION` has moved past it. Moving to a new schema is a port of
  the model, not a flag.
- Definitions are keyed by their `id`, not their file name: a few files upstream are named
  apart from the id inside them (`bluebird.yml` is `bluebirdhd`), and the id is what
  Prowlarr and an indexer entry refer to. A duplicate id is skipped with a reason.
- A definition the converter refuses is **skipped, recorded in `index.json`'s `skipped` with
  the reason, and printed** — never a failed sync. One broken upstream file must not block
  every other tracker's update.

At `8df0764` (2026-10-09): 580 v11 files, 579 converted, 1 skipped — `1337x.yml`, an
unbalanced `)` in a search-path template that Jackett's regex-driven template engine
tolerates and Go's parser does not. The fix belongs upstream; do not teach the converter to
guess at unbalanced parentheses.

## Strict decoding

`cardigann.Decode` rejects any key the model does not know, naming its path. Upstream's
schema sets `additionalProperties: false`, so an unknown key means the schema grew and the
model did not — and a field dropped silently is a tracker that half-works later. yaml.v3's
`KnownFields` is not enough on its own: `Node.Decode` (what every custom unmarshaler,
including every `OrderedMap`, calls) starts a fresh lenient decoder, so `checkKnown` walks
the node tree against the Go types itself.

- **Order is preserved wherever upstream's semantics depend on it**: `fields` (a later field
  reads an earlier one through `.Result`), a selector's `case` (first match wins), select
  `options` (display order). Those are `OrderedMap`, which also encodes as an ordered JSON
  object. Never turn one into a Go map.
- Scalars upstream writes as string, int or bool interchangeably (`default`, category `id`,
  filter `args`) decode as `Str`/`StrList` — their literal text. Filter `args` is always a
  list after decoding, whatever shape it was written in.
- **`\/` is unescaped before YAML parsing** (`unescapeSlashes`). YAML 1.2 — and the .NET
  parser upstream is written against — reads `"\/"` as `/` for JSON compatibility; yaml.v3
  is 1.1 and rejects it, which skipped four definitions. Outside double quotes the backslash
  was literal, but every such use upstream is a regex or a CSS selector, where `\/` and `/`
  match the same thing. An escaped backslash before a slash (`\\/`) is left alone.

## Regexes: two engines, chosen at conversion

Upstream patterns are .NET regexes; Go's `regexp` is RE2. At `8df0764`, 87 of 1,890
patterns fail to compile in Go, across 67 definitions.

- **Spelling differences are rewritten** (`rewriteRegex`), and the result runs on RE2:
  .NET Unicode block names onto Go scripts (`\p{IsCyrillic}` → `\p{Cyrillic}`,
  `\p{IsCJKUnifiedIdeographs}` → `\p{Han}`, table in `dotnetBlocks`), `\uXXXX` →
  `\x{XXXX}`, and a backslash before a non-ASCII rune dropped (.NET reads `\<NBSP>` as the
  rune; Go rejects it). A block is a code-point range and a script is a set of assigned
  characters, so the Go side is a slight superset — every use upstream is "strip or keep
  this alphabet", where the superset is what was meant.
- **Semantic differences are not rewritten.** Lookarounds and backreferences have no RE2
  form, and hand-rewriting each one means changing the code that consumes its result. Such
  a pattern keeps its original text and its filter gets `"engine": "regexp2"`
  (`cardigann.RegexEngineNET`); a templated `re_replace` is renamed `re_replace_net`. The
  definition gets `regex_net: true`, and so does its catalog row. 38 definitions at
  `8df0764`.
- **The engine is decided here, never by trial at runtime.** Which definitions run on the
  backtracking engine is then visible in the data itself, and a pattern that compiles on
  neither engine fails the definition at sync time rather than a search.
- **The engine must run regexp2 with a match timeout, and treat a timeout as that filter
  failing** — not the search hanging. regexp2 backtracks and has no linear-time
  guarantee; here it runs patterns we do not author over HTML from trackers we do not
  control.
- .NET semantics that survive the rewrite unchanged are accepted as-is: `\d` and `\w` are
  Unicode-aware in .NET and ASCII in Go. Whether any upstream pattern relies on the
  difference has not been audited.
- **Replacement strings follow their pattern's engine.** regexp2 speaks .NET substitution
  natively, so a regexp2 replacement is untouched. An RE2 one is rewritten for
  `Regexp.Expand`: every numbered group is braced (`$1x` in Go is the group named `1x`, not
  group 1 then `x`), `$&` becomes `${0}`, a lone `$` becomes `$$`; `` $` ``, `$'`, `$+` and
  `$_` have no Go form and fail the definition.

## Date layouts

`dateparse`/`timeparse` carry .NET custom formats (`yyyy-MM-dd HH:mm:ss zzz`); every one
upstream is .NET, none Go. `translateDateLayout` maps each letter run onto Go's reference
time (`dotnetDateTokens`). Go parses `15`, `3`, `4`, `5`, `2`, `1` as one *or* two digits,
so .NET's unpadded `H`/`h`/`m`/`s`/`d`/`M` share the padded spelling — definitions only
ever parse. Refused, failing the definition: a lone `y`, `t` or `z`, fractional seconds,
eras, and any literal that Go would read as a layout element (a digit, `Jan`, `Mon`, `PM`,
`MST`, …) — a Go layout has no escape for those. A `dateparse` without a layout is left for
the engine's own format guessing.

## Templates

Upstream templates are Go `text/template` syntax — Cardigann began as a Go project — but
Jackett evaluates them with its own regex-driven engine, and two differences break Go's
parser:

- **String literals.** Jackett takes `re_replace`'s pattern and replacement verbatim
  between the quotes; Go unquotes them like Go source, so `"\s+"` is a parse error. The
  converter matches each `re_replace` call the way Jackett does, translates the pattern and
  replacement, and re-quotes both with `strconv.Quote`.
- **Lookup names.** Settings and fields are named freely (`2facode`, `cat-id`); Go lexes
  `.Config.2facode` as a number and `.Config.cat-id` as a subtraction. Any `.Config.x` /
  `.Result.x` whose name is not a Go identifier becomes `(index .Config "x")`. **So the
  engine must hand `.Config` and `.Result` to templates as maps**, never structs.

Every string containing `{{` — other than a regex pattern, which upstream never templates —
is then parsed with Go's `text/template` and the stub `FuncMap` (`re_replace`,
`re_replace_net`, `join`); a parse failure fails the definition. Those three names, plus
Go's builtins, are the whole function vocabulary the engine must provide
(`cardigann.TemplateFunc*`). Parsing checks syntax only; what `.Config`/`.Query`/`.Result`
evaluate to (including Jackett's `.True`/`.False` truthiness) is the engine's to define.

## Secrets

`Setting.Secret` is the converter's call, not upstream's. A `password` setting is one, but
cookies, API keys, passkeys, PINs and 2FA codes are all `type: text` upstream, so a
type-only rule would leave every one in clear in the config audit. A `text` setting is
secret when its name contains `pass`, `key`, `cookie`, `token`, `2fa` or `secret`, or is
`pin` (`secretHints`). Over-flagging costs a masked input; under-flagging leaks a
credential. When the config entry lands, its redaction reads this flag — not key names.

## Licensing — why the NOTICE is load-bearing

The decision (option C of the design discussion) is to **embed** the converted snapshot in
the binary, keeping streamline a single file. The basis:

- Jackett, where most definitions originate, ships the GPL-2.0 text and specifies no
  version anywhere (no source headers, no csproj licence, nothing in the README). GPL-2.0
  §9: when a program specifies no version, a recipient may choose any version the FSF has
  published — so the definitions can be taken under GPL-3.0, streamline's licence.
- `Prowlarr/Indexers` carries no licence file of its own.
- Converting does not change any of that: a mechanical translation is still the original
  authors' work, and the GPL's obligations attach to what is distributed — the converted
  files — not to the script that made them.

Hence, and **never to be dropped**:

- `internal/cardigann/definitions/NOTICE` ships inside the embedded set: the attribution,
  the upstream revision, the licence basis and the modification notice. `definitions.Notice()`
  exposes it, so anything that displays or redistributes the definitions can carry it.
- Every converted file has a `source` block — upstream path, sha256 of the original,
  `convert.Modified` — the "this file was changed" notice GPL-2 §2(a) / GPL-3 §5(a) ask for.
  The per-file date lives in the NOTICE (the upstream commit date), not in each file, so a
  sync does not rewrite 579 files just to move a date.
- The runtime update, when it lands, runs the same `Convert` and must write the same
  provenance for what it fetched.

## Size

The snapshot is ~6.3 MB of indented JSON (579 files) plus a ~300 KB index, embedded
uncompressed. That is deliberate: indented per-file JSON is what makes a sync reviewable as
a diff. Compressing it at embed time is the lever if binary size ever matters; the
committed form should stay diffable either way.

## Dependencies

`github.com/dlclark/regexp2/v2` and `go.yaml.in/yaml/v3` were already in the module graph
(golangci-lint and koanf's YAML parser); this package only promoted them to direct
requirements. `go.sum` did not change, and `go mod vendor` produces a byte-identical tree,
so `nix/package.nix`'s `vendorHash` is unaffected.
