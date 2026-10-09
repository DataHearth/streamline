# Native indexers (Cardigann definitions)

`internal/cardigann` is streamline's own reader for the Cardigann YAML definitions
Prowlarr and Jackett describe trackers in, so a stack can drop Prowlarr/Jackett. **What
exists today is the converter and the embedded snapshot** — the engine that runs a
definition, the `protocol: cardigann` indexer entry, the runtime definition update and the
catalog UI are not built yet. Nothing outside `internal/cardigann` imports it.

| Path | What it is |
|---|---|
| `internal/cardigann/` | The model (`Definition` and friends, upstream v11 schema key for key), the strict `Decode`, `Catalog`/`Summarize`, `EncodeJSON` |
| `internal/cardigann/convert/` | `Convert(path, yaml)` — the .NET-to-Go rewrites — and `Check(def)`, which validates a converted definition as stored. Pure; no I/O |
| `internal/cardigann/cmd/cardigann-sync/` | The sync: fetch upstream, `Convert` each file, write the snapshot |
| `internal/cardigann/definitions/` | `//go:embed`s the snapshot; `Catalog()` (a fresh copy per call), `Load(file)`, `Notice()`. Only `definitions.go` (and its tests) is hand-written |

## The snapshot is generated

`task cardigann:sync` (optionally `REF=<branch|tag|commit>` or `SRC=<checkout>`) makes a
sparse, shallow, blob-filtered git fetch of `Prowlarr/Indexers` — only
`definitions/v11/` and `VERSIONS` — converts every file, and rewrites
`internal/cardigann/definitions/{data/<file>.json,index.json,NOTICE}` **wholesale**, so a
definition deleted upstream disappears here too.

- **Never hand-edit the snapshot.** Fix the converter and re-sync. The
  `definitions` suite re-encodes every embedded definition and compares it byte for byte
  with the committed file, so a model field that fails to round-trip fails it; it also runs
  `convert.Check` over every file, so an edited regex (in a filter or a template) or
  template that no longer compiles fails it. A *well-formed* hand edit still passes — the
  rule is a rule, not a guard.
- **The output is a pure function of (upstream commit, converter).** No timestamp but the
  upstream commit's own, sorted listings, one file per definition. Re-running a sync
  against the same revision is a no-op diff, so a sync commit shows exactly what upstream
  changed. Commit a sync as its own change (`chore(cardigann): sync definitions to <sha>`).
- **`convert.Version` is bumped whenever the converter's output changes for the same
  input**, and the sync re-run in the same change. The catalog records the version and the
  test fails when it disagrees with `convert.Version` — but nothing can notice a converter
  change made *without* the bump, since the upstream YAML is not in the repo to re-convert.
  The bump is a manual rule; reviewers enforce it.
- Git rather than a tarball: a fetch by ref accepts a commit as well as a branch, and the
  commit SHA comes straight from the checkout for the NOTICE. **The definitions and
  `VERSIONS` are read from `HEAD`'s git objects** (`ls-tree` + `cat-file --batch`), never
  the work tree: what is converted is exactly the commit the NOTICE names, whatever `git
  status` cannot see (assume-unchanged or skip-worktree edits, a narrower sparse checkout),
  and a committed symlink is skipped as "not a plain file" rather than followed out of the
  checkout. A file whose name `//go:embed` could not serve (leading `.`/`_`, a `:`, anything
  outside `[A-Za-z0-9._-]`) is skipped with a reason too. A `-src` directory must be
  the **root of its own repository** (inside any other work tree git answers for that one —
  a copied `definitions/` under streamline's gitignored `tmp/` read as clean and stamped
  streamline's HEAD into the NOTICE), and any local change under `definitions/` or
  `VERSIONS` — tracked, untracked or ignored, flags set explicitly so no git config can hide
  one — is refused: the NOTICE would name a revision the files are not. `-ref` and `-src`
  together are refused too, since `-ref` would be silently ignored.
- `cardigann.SchemaVersion` (11) pins the upstream directory read. The sync fails when
  upstream's `VERSIONS` puts it below `MIN_VERSION` — a frozen version still reads fine and
  is never updated again, so syncing it would ship a snapshot that looks fresh and is not —
  and warns when `CURRENT_VERSION` has moved past it. Moving to a new schema is a port of
  the model, not a flag.
- Definitions are keyed by their **upstream file name** (`Summary.File`, `Load(file)`), not
  their `id`: Prowlarr stores an indexer under its definition file and fetches updates by
  it (`/master/11/bluebird`), and six files upstream are named apart from the id inside
  them (`bluebird.yml` holds `bluebirdhd`). It also means no upstream value decides where
  the sync writes — an id of `../../package` once overwrote `package.json`.
- A definition the converter refuses — or whose JSON encoding fails — is **skipped,
  recorded in `index.json`'s `skipped` with the reason, and printed**, never a failed sync.
  One broken upstream file must not block every other tracker's update.

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
  filter `args`) decode as `Str`/`StrList` — their literal text, which is what yaml.v3 does
  for any scalar decoded into a string. Filter `args` is always a list after decoding,
  whatever shape it was written in.
- `OrderedMap` encodes through the json/v2 interfaces (`MarshalJSONTo`/`UnmarshalJSONFrom`),
  which `encoding/json` honours since Go's json/v2 became the default implementation. It
  writes on the caller's encoder, so `EncodeJSON`'s no-HTML-escaping and indent apply
  inside it. A `jsontext.Token` is only valid until the next read — copy a key out of it
  before decoding its value.
- **`\/` is read as `/` in double-quoted scalars only** (`protectSlashes` + `sanitize`).
  YAML 1.2 — and the .NET parser upstream is written against — reads `"\/"` as `/` for
  JSON compatibility; yaml.v3 is 1.1 and rejects it, which skipped four definitions.
  Everywhere else (plain, single-quoted, block) the backslash is literal text and must
  survive: `\/` in a CSS identifier or a `replace` argument is not `/`. So each unescaped
  `\/` is swapped for an escape yaml.v3 knows for `/` before parsing, and put back as `\/`
  in every scalar that is not double-quoted, where the escape was never read. A source that
  already contains that escape is refused rather than guessed at.
- **A second YAML document, and any explicit tag, are refused.** `yaml.Unmarshal` reads the
  first document and ignores the rest; and a tag changes decoding behind the strict check's
  back — `!!null` on a mapping still fills the struct while skipping the unknown-key walk,
  `!!binary` base64-decodes a string. Upstream writes neither.
- **Anchors, aliases and duplicate keys are refused.** Upstream uses none (its CI runs
  yamllint), and both are what a hostile file uses: walking an alias tree is quadratic or
  worse, and duplicate keys mean something different on each side — Prowlarr keeps every
  duplicate `fields` entry in order (a `KeyValuePairList`) and reads other duplicates
  last-wins, `OrderedMap.Get` first-wins. This matters for the runtime update, which decodes fetched files.
- Decoding happens once, from the already-checked node tree: `checkKnown` is a superset of
  `KnownFields`, so a second parse of the source bought nothing.
- **Upstream defaults that are not Go zero values stay distinguishable**:
  `testlinktorrent` is `*bool` (absent means true upstream — fetch each non-magnet link and
  fall through to the next download selector when it is not a torrent), and a response's
  `noResultsMessage` is `*string` (set, even to `""`, it is the "no results" body). A
  search path's `followredirect` is a plain bool: both engines default it to false
  regardless of the definition-level flag, which only governs the login landing page.
- **Prowlarr's load-time clean-up is applied by the converter** (`convert.clean`, mirroring
  `IndexerDefinitionUpdateService.CleanIndexerDefinition` and the request generator's
  fallbacks), so a converted definition carries every default explicitly and the engine
  re-derives none: no `settings` → username + password; no `encoding` → UTF-8; a login
  without `method` → form; a single `search.path` → one more `paths` entry inheriting the
  inputs (the request generator only walks `paths`); login and download without `headers`
  → the search headers (`Login?.Headers ?? Search?.Headers` — an API tracker's login is
  often a GET that needs the search's Authorization header); and the fields Prowlarr
  treats as optional by name (`imdb`, `imdbid`, `tmdbid`, `rageid`, `tvdbid`, `tvmazeid`,
  `traktid`, `doubanid`, `poster`, `banner`, `description`, `genre`, whole key only) get
  `optional: true` — on a JSON response a failing non-optional field aborts every row.

## Regexes: two engines, chosen at conversion

Upstream patterns are .NET regexes, compiled with `RegexOptions.None`; Go's `regexp` is
RE2. At `8df0764`, 87 of 1,890 patterns fail to compile in Go at all — but compiling is
not the bar. **A pattern that compiles on both can still mean different things**, and that
was the larger problem: .NET's `\w \d \s \b` are Unicode-aware and Go's are ASCII, so
before the rewrite below, 125 RE2-assigned patterns in 75 definitions matched differently
on a 40-string multilingual corpus — Cyrillic titles stripped to their year, `Amélie`
split at the `é`.

- **Spelled out for RE2** (`rewriteRE2`), so the result means what .NET means:
  - the shorthand classes as the Unicode sets .NET uses: `\d` → `\p{Nd}`; `\w` → letters,
    non-spacing marks, decimal digits, connector punctuation; `\s` → `char.IsWhiteSpace`
    (`\t-\r`, U+0085, `\p{Z}`); negations likewise, outside a class;
  - .NET block names as **the block's own code-point range** (`dotnetBlocks`), not a Go
    script — a script is not a block (Go's Cyrillic script misses U+0485–0486), and regexp2
    has no block table either, so its path gets the same range;
  - `\uXXXX` → `\x{XXXX}`; a backslash before a non-ASCII rune dropped (.NET reads
    `\<NBSP>` as the rune; Go rejects it); a `[` inside a class escaped, which .NET reads
    literally and Go would take for a POSIX class.
- **What RE2 cannot say goes to regexp2, unchanged but for block names**: lookarounds,
  backreferences, `\b`/`\B` (Go's boundary is ASCII-only — beside a Cyrillic letter it never
  matches), `$` (.NET's also matches just before a final newline, which RE2 could only say
  with a lookahead), named groups (.NET numbers unnamed groups first, Go left to right, so
  `$1` differs), an octal escape above `\377` (.NET keeps its low byte), a negated
  shorthand or negated block inside a class, class subtraction, any `re_replace` whose
  pattern can match the empty string (Go's `ReplaceAll` skips an empty match right after a
  non-empty one, .NET replaces it), and any pattern whose replacement uses a substitution
  only .NET has.
- **regexp2 is not .NET either, and gets the same spelling where they differ**: its bare
  `\w` also takes ZWNJ/ZWJ (U+200C/D), which .NET's does not (only .NET's `\b` counts
  them), so `\w`/`\W` are written out on the regexp2 path too. That matters for Persian
  text, where ZWNJ is everywhere. Its filter gets
  `"engine": "regexp2"` (`cardigann.RegexEngineNET`); a templated `re_replace` is renamed
  `re_replace_net`; the definition and its catalog row get `regex_net: true`. Most
  regexp2 assignments are for `\b` and `$`; the current count is in `index.json`.
- **Checked by differential, not by eye.** Every pattern left on RE2 at `8df0764` was
  matched against its upstream original on regexp2 (with `rewriteNET`'s spelling, so the
  reference is .NET's meaning) over a multilingual corpus plus ZWNJ/ZWJ, trailing-newline
  and empty inputs, comparing match positions and `re_replace` output: zero
  disagreements, where the same run with an ASCII `\w` flags 22 definitions. A
  randomised run (4,000 inputs per pattern) found only the ZWNJ/ZWJ reference issue above. Repeat that check when the rewrite
  changes. The one RE2 behaviour no rewrite can change: a repeated group's capture
  (`(a*)*` on `a` captures `a` in Go, `""` in .NET); nothing upstream relies on it.
- **The engine is decided here, never by trial at runtime.** Which definitions run on the
  backtracking engine is then visible in the data itself, and a pattern that compiles on
  neither engine fails the definition at sync time rather than a search.
- **The engine must run regexp2 with a match timeout, and treat a timeout as that filter
  failing** — not the search hanging. regexp2 backtracks and has no linear-time
  guarantee; here it runs patterns we do not author over HTML from trackers we do not
  control.
- The one known residual difference: `(?i)` is Unicode simple case folding in Go and
  per-character lowering in .NET, which disagree only on a handful of letters such as the
  long s (`ſ` folds to `s` in Go, not in .NET). No upstream pattern is affected that
  matters; it is not worth sending every case-insensitive pattern to regexp2.
- **Replacement strings follow their pattern's engine** (`translatePair`). regexp2 speaks
  .NET substitution natively, so a regexp2 replacement is untouched. An RE2 one is
  rewritten for `Regexp.Expand` — the engine must use `ReplaceAllString`, not the literal
  variant: every numbered group is braced (`$1x` in Go is the group named `1x`, not group 1
  then `x`), a numbered reference is written by value (Go reads `$01` as the group named
  `01`), `$&` becomes `${0}`, a lone `$` becomes `$$`, and a reference to a group the
  pattern does not have is literal text, as .NET leaves it (Go would substitute nothing) —
  for an unreadable `${…}` only the `$` is literal, so references inside the braces still
  expand, as in .NET. `` $` ``, `$'`, `$+` and `$_` send the pattern to regexp2.

## Date layouts

`dateparse`/`timeparse` carry .NET custom formats (`yyyy-MM-dd HH:mm:ss zzz`); every one
upstream is .NET, none Go. `translateDateLayout` maps each letter run onto Go's reference
time (`dotnetDateTokens`), and **a converted filter's `args` is a list of Go layouts, tried
in order** — one per combination where .NET accepts two shapes Go spells apart: `zzz`/`K`
take the offset with or without its colon (`+08:00`, `+0800` — 19 definitions feed
`pubDate` straight in with the colon-less form), and `tt` matches AM/PM in either case
(`PM`, `pm`).

- Padding is meaningful when parsing, on both sides: `dd MM hh mm ss` demand two digits in
  .NET's ParseExact and in Go (`02 01 03 04 05`), `d M h m s` take one or two (`2 1 3 4 5`).
  Go has no padded 24-hour form, so `HH` shares `H`'s `15` and is the one token laxer than
  .NET. Do not "simplify" an unpadded token onto the padded spelling — `h:mm tt` would
  then reject `9:05 PM`.
- Refused, failing the definition: a lone `y`, `t` or `z`, fractional seconds, eras, and any
  literal that Go would read as a layout element (a digit, `Jan`, `Mon`, `PM`, `MST`, …) — a
  Go layout has no escape for those.
- **Parse with `cardigann.ParseDate(layouts, value, now)`**, never bare `time.Parse`. It
  tries the layouts in order, reads a zone-less value in `now`'s location, and fills what a
  layout lacks the way .NET's ParseExact does: no year takes now's year, no date at all
  takes now's date (3 layouts at `8df0764`: btetree, torrentsome, comicat's
  `date_today`); Go alone would give year 0.
- Left to the engine: a `dateparse` without a layout (format guessing). Mixed case like
  `Pm` parses in .NET and in neither Go spelling.

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
is then parsed with Go's `text/template` (and `convert.Check` parses them again, compiling
each `re_replace` pattern on the engine its function name selects and refusing undeclared
`.Config`/`.Result` reads) and the stub `FuncMap` (`re_replace`,
`re_replace_net`, `join`); a parse failure fails the definition. Those three names, plus
Go's builtins, are the whole function vocabulary the engine must provide
(`cardigann.TemplateFunc*`). Parsing checks syntax only; what `.Config`/`.Query`/`.Result`
evaluate to (including Jackett's `.True`/`.False` truthiness) is the engine's to define.

## Engine contract

What the converter cannot bake into the data, and the engine must therefore do exactly as
Prowlarr does. Each rule names the definitions that depend on it.

- **Template data is all strings, and null is `""`.** Prowlarr's nulls — `.False`, an
  unchecked checkbox, every `.Query.*` key (all pre-set), an empty optional field — render
  as `""`; Go renders a nil or missing value as `<no value>` and compares it unequal to
  everything. So: every value a string (`.Categories` a `[]string`), every Prowlarr
  `.Query` key present, every declared field pre-seeded with `""`, `.True` = `"True"`,
  `.False` = `""`. 150 `case` values are `{{ .False }}`; 68 definitions test
  `eq .Query.IMDBID .False`. `convert.Check` refuses a template that reads an undeclared
  setting (`sitelink` aside, which the engine always provides) or field, so a missing key
  can only ever be a `.Query` one.
- **`.Result.<field>` holds the normalised value** Prowlarr's `ParseFields` leaves: numbers
  coerced (`eq .Result.files "1"`, 75 uses), `genre` split and re-joined with `", "`.
- **JSON values are stringified the Newtonsoft way**: an ISO timestamp becomes
  `MM/dd/yyyy HH:mm:ss` (86 JSON definitions append an offset and parse that), booleans
  `True`/`False` (221 `case` keys in 78 definitions), floats in shortest form (`1.0` → `1`),
  arrays joined with `,`. Selectors: leading `.` trimmed, `..` is the parent row,
  `:has`/`:not`/`:contains` suffixes, `:contains` a substring test over the value's string
  form.
- **In a `paths[].path` or `inputs.$raw`, URL-encode substituted values only**, never the
  literal text — every variable, `re_replace`/`join` result and `range` item — always as
  UTF-8, then replace `+` with `%20` in the path. Ordinary inputs are encoded in the
  definition's `encoding`. Go's templates have no hook for it: append `urlencode` to every
  action node of those parse trees.
- **HTML selectors may match the element itself**: a field selector is tried on the row
  first (`dom.Matches(sel) ? dom : QuerySelector`), a `case` key on the selection first. A
  leading `:root` is stripped. goquery's `Find` searches descendants only.
- **Dates**: `cardigann.ParseDate` (trims, tries the layouts, fills a missing year or date
  from now). A failed `dateparse` is not fatal — the value passes through — and the `date`
  field always goes through format guessing afterwards.

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

The snapshot is ~6.4 MB of indented JSON (579 files) plus a ~330 KB index, embedded
uncompressed. That is deliberate: indented per-file JSON is what makes a sync reviewable as
a diff. Compressing it at embed time is the lever if binary size ever matters; the
committed form should stay diffable either way.

## Dependencies

`github.com/dlclark/regexp2/v2` and `go.yaml.in/yaml/v3` were already in the module graph
(golangci-lint and koanf's YAML parser); this package only promoted them to direct
requirements. `go.sum` did not change, and `go mod vendor` produces a byte-identical tree,
so `nix/package.nix`'s `vendorHash` is unaffected.
