# Native indexers (Cardigann definitions)

`internal/cardigann` is streamline's own reader for the Cardigann YAML definitions
Prowlarr and Jackett describe trackers in, so a stack can drop Prowlarr/Jackett. **What
exists today is the converter, the embedded snapshot and the engine**: a `protocol:
cardigann` indexer entry, created from the catalog in the SPA or in the config file, runs any definition in the
snapshot, logging in to its tracker when the definition says to (`## Logins`) and getting
past a browser challenge through a FlareSolverr-compatible solver (`## Browser
challenges`). A daily job keeps the definitions current with upstream, holding any change
that re-points a configured tracker for an administrator (`## Runtime updates`). Settings →
Indexers browses the catalog and generates the form from a definition's settings (`## The
catalog and the API`).

| Path | What it is |
|---|---|
| `internal/cardigann/` | The model (`Definition` and friends, upstream v12 schema key for key), the strict `Decode`, `Catalog`/`Summarize`, `EncodeJSON` |
| `internal/cardigann/convert/` | `Convert(path, yaml)` — the .NET-to-Go rewrites — and `Check(def)`, which validates a converted definition as stored. Pure; no I/O |
| `internal/cardigann/snapshot/` | `Build` (convert a file set into catalog + encoded definitions), `Write`/`WriteIndex` (data/, index.json, NOTICE), `Notice`, `CheckVersions` — the one code path both the sync and the runtime update write through |
| `internal/cardigann/cmd/cardigann-sync/` | The sync: fetch upstream with git, read `HEAD`'s blobs, hand them to `snapshot` |
| `internal/cardigann/definitions/` | `//go:embed`s the snapshot and resolves what is in force under a data directory: `Load(dir, file)`, `Catalog(dir)` (custom folded in), `Snapshot(dir)`, `Raw(dir, file)`, `Notice()`. `dir` empty = the embedded copy alone. Only `definitions.go` (and its tests) is hand-written |
| `internal/cardigann/update/` | The runtime update: `Run` (GitHub commit → tarball → `snapshot` → `<dir>/current`, holding trust changes in `<dir>/pending`), `List`, `Diff`, `Approve`, `Reject` |
| `internal/cardigann/engine/` | Runs a converted definition: renders its search requests, parses the responses (HTML, XML, JSON) into releases, resolves a release's download block. The port of Prowlarr's `CardigannRequestGenerator`/`CardigannParser`/`CardigannBase` |
| `internal/indexer/cardigann.go` | The `indexer.Client` over an engine, `CardigannSite`, and `ResolveDownload` for the grab path |

## The snapshot is generated

`task cardigann:sync` (optionally `REF=<branch|tag|commit>` or `SRC=<checkout>`) makes a
sparse, shallow, blob-filtered git fetch of `Prowlarr/Indexers` — only
`definitions/v12/`, `definitions/v11/` and `VERSIONS` — converts every file, and rewrites
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
- **Two schema directories, newest per file.** `cardigann.SchemaVersion` (12) is the newest
  schema read and `OldestSchemaVersion` (11) the oldest. v12 is v11 plus the `base64decode`
  and `base64encode` filters (Prowlarr's `WebEncoders.Base64Url*`: URL alphabet, no
  padding; the decoder takes the standard alphabet and padding too), and upstream puts a
  file in `v12/` only when it needs them — at `8df0764` that is `blueroms` alone — while
  Prowlarr falls back to `v11/` file by file. `snapshot.Build` does the same: every
  directory in `snapshot.Prefixes()` is read and the newest copy of a name wins, its
  `source.path` naming the directory it came from. The sync fails when upstream's
  `VERSIONS` puts `SchemaVersion` outside `MIN_VERSION`..`MAX_VERSION` — a frozen version
  still reads fine and is never updated again, so syncing it would ship a snapshot that
  looks fresh and is not — and warns when `CURRENT_VERSION` has moved past it. Moving to a
  new schema is a port of the model (new keys, new filters), then a bump of the constant.
- Definitions are keyed by their **upstream file name** (`Summary.File`, `Load(file)`), not
  their `id`: Prowlarr stores an indexer under its definition file and fetches updates by
  it (`/master/11/bluebird`), and six files upstream are named apart from the id inside
  them (`bluebird.yml` holds `bluebirdhd`). It also means no upstream value decides where
  the sync writes — an id of `../../package` once overwrote `package.json`.
- **`.github/workflows/cardigann-sync.yaml` runs the sync every Monday** (and on
  `workflow_dispatch`, with an optional `ref`), runs the cardigann suites over the result,
  and force-pushes it to the one `cardigann/sync` branch, opening or refreshing a single
  pull request — an unmerged sync is superseded by the next, which contains it. A pull
  request opened with the Actions token starts no workflow, so **CI does not run on it
  until someone pushes to the branch or closes and reopens it**; the body says so. The
  repository must allow Actions to create pull requests (Settings → Actions → General).
- A definition the converter refuses — or whose JSON encoding fails — is **skipped,
  recorded in `index.json`'s `skipped` with the reason, and printed**, never a failed sync.
  One broken upstream file must not block every other tracker's update.

At `8df0764` (2026-10-09): 580 definitions (one of them from v12), 579 converted, 1 skipped — `1337x.yml`, an
unbalanced `)` in a search-path template that Jackett's regex-driven template engine
tolerates and Go's parser does not. The fix belongs upstream; do not teach the converter to
guess at unbalanced parentheses.

## Runtime updates

Prowlarr refreshes its definitions at boot and on a schedule; a release-pinned snapshot
alone would leave a tracker that moved domains broken until the next streamline release.
`jobs.CardigannUpdate` (`cardigann-update`, `schedules.cardigann_update`, 24h) runs
`update.Run` against `config.CardigannDir()` (`<data_dir>/cardigann`):

- **What is fetched**: the commit `api.github.com/repos/Prowlarr/Indexers/commits/master`
  names, then `codeload.github.com/…/tar.gz/<sha>` (≈750 KB; the whole repository, every
  schema version) — so the NOTICE names an exact commit, as the sync's does, without git
  on the host. Only regular `definitions/v12/*.yml`, `definitions/v11/*.yml` and `VERSIONS`
  entries are read; inflated
  size is capped. Nothing is fetched past the API call when the commit is the one in force.
- **One code path with the sync**: `snapshot.Build` converts, `snapshot.Write` writes
  `data/`, `index.json` and the NOTICE — the provenance the licence rests on is written the
  same way at runtime. `current/` is replaced **wholesale and atomically** (written beside,
  renamed into place) so a reader sees the old set or the new one.
- **What is in force** (`definitions.Load`/`Catalog`): `custom/<file>.yml` (the operator's
  own YAML, converted on every load, marked `custom` in the catalog) over the runtime
  `current/` snapshot over the embedded one. `current/` counts **only while its upstream
  commit is newer than the embedded one and it was written by this converter version and
  schema** — an upgraded binary falls back to its own newer embedded copy, and a converter
  change forces the next run to rewrite. Its index is cached per directory;
  `definitions.Invalidate` follows every write.
- **The trust hold**: a changed definition that a configured indexer runs (enabled or not)
  is compared on the **hosts it names** — links, legacy links and every absolute
  `http(s)://` URL anywhere in its JSON — and its **login block**. Either changing keeps the
  old version in `current/` (listed in the catalog's `held` and the NOTICE) and puts the new
  one in `pending/` with its reasons. A definition deleted upstream that is in use is held
  the same way (`removed`). Everything else applies at once. `Approve` moves the pending
  file into `current/` and rewrites the index and NOTICE; `Reject` keeps the old version and
  records the refused upstream `sha256` in `pending/index.json`, so the same version is
  never proposed again while a later one is. A pending index written against another
  snapshot than the one in force is ignored (`List` returns nothing).
- **REST**: `GET /cardigann/updates` (the snapshot in force + each held update with its
  unified diff, from `hexops/gotextdiff`, and the indexers running it),
  `POST /cardigann/updates/{file}/approve|reject`, admin-only. They live under
  `/cardigann`, not `/indexers`, so no indexer name can collide with them. Settings →
  Indexers shows the held updates above the list.

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

## The engine

In the YAML (or through the API, `## The catalog and the API`) — the REST API lists a
`cardigann` entry with its host and port as the tracker site's:

```yaml
indexers:
  - name: YTS
    protocol: cardigann
    enabled: true
    cardigann:
      definition: yts            # file name in the snapshot, not the display name
      site: https://yts.gg/      # optional; must be one of the definition's links
      settings: {}               # by setting name; omitted ones take the default
```

- **`config.CheckCardigann` is the gate**, run by `checkInvariants`: the definition loads,
  the site is one of its `links`, and
  `engine.Config` accepts the settings — an undeclared name, a select value outside its
  option keys, or a checkbox that is not a boolean is refused. Two upstream selects default
  to a key their own options lack (`DESC` beside `desc`; `t` beside nt/ts/sz/i), which
  Prowlarr indexes as -1 and throws on; `selectDefault` takes a case-insensitive match, else
  the first option. A Torznab/Prowlarr entry's API-key requirement moved from the struct tag
  into `checkIndexers`, since a tag cannot say "unless protocol is cardigann".
- **Settings resolve as `GetBaseTemplateVariables` does**: a ticked checkbox is the string
  `".True"` and an unticked one `""`, a select its option key, text and password as given;
  `sitelink` is always set. The audit log redacts every key under `.cardigann.settings`
  wholesale — which setting is a secret is the converter's per-definition call, and a map
  of strings used to print whole because `addLeaves` did not expand it.
- **Ids follow the definition's mode, as Prowlarr's caps do.** `queryVars` sets
  `.Query.IMDBID`/`IMDBIDShort`/`TMDBID`/`TVDBID` only when the mode for the search's kind
  lists the id, and **an id search leaves `.Query.Q` empty**, as the *arr clients leave the
  term out of one; season and episode fill `.Query.Episode` (`S01E02`) only when the mode
  lists them. A kind whose mode the definition lacks falls back to its `search` mode with
  the term alone. `indexer.fallbacks` skips the IMDb rung for cardigann (the engine already
  sent every id the definition takes) and keeps the bare-title rung.
- **Requests**: a path is rendered with `urlEncoded` (each substituted value
  `WebUtility.UrlEncode`d, then `+` → `%20`), `$raw` the same then split into pairs, other
  inputs plain and dropped when empty (unless `allowEmptyInputs`); the query string encodes
  values (in the definition's encoding) and never keys, as `GetQueryString` does. A path
  that already carries a `?` gets `&` — Prowlarr would append a second `?`. Duplicate GET
  URLs are skipped. A search path follows no redirect unless it says so, and an unfollowed
  one fails the search (`ErrStatus`) — upstream's "session expired" cue. Requests to one
  indexer are paced at `max(2s, requestDelay)` by a pacer keyed on the entry's name, so a
  config reload does not reset it. A cookie jar lives per engine; the UA is a desktop
  browser's, as Jackett sends.
- **Rows**: HTML through goquery; XML through `encoding/xml` (`RawToken`, so prefixes stay)
  into the same node tree — the HTML parser would empty every RSS `<link>`, a void element
  there. `after` merges the next **selected** rows, not the DOM siblings. A field selector
  tries the row itself first, `:root` restarts from the document, `remove` mutates the row
  as AngleSharp's does. JSON rows support the path subset the definitions use (`a.b`, `[n]`,
  `['k']`, a leading `$`) and the `:has/:not/:contains` filters through regexp2, since
  `_jsonSelectorRegex` needs a lookahead; `attribute` + `multiple` expand a row into its
  children, and a `..field` selector reads the unexpanded parent. A failing required field
  skips the field on an HTML row and fails the whole response on a JSON one, as upstream.
  Links resolve against the URL that was **asked for**, not wherever a redirect landed.
- **Release fields** are normalised as `ParseFields` does (seeders/leechers over 5,000,000
  read as 0, ids take the first run of digits, sizes in binary units, `category`/
  `categorydesc` through the caps). A non-private release with an info hash and no magnet
  gets one built with the public tracker list. `andmatch` drops releases missing a word of
  the term (`and`, `the`, `an` aside); an id search keeps everything.
- **Dates**: `fromUnknown` is `DateTimeUtil.FromUnknown` — Rfc1123Z, a unix timestamp,
  "now", "… ago", today/yesterday/tomorrow, "<weekday> at", a missing year filled — but its
  last resort, the DateTimeRoutines fuzzy parser, is **a layout list here** (`fuzzyLayouts`,
  month-first as its USDate default). A shape neither knows fails the field.
- **Downloads**: a release link on an enabled cardigann entry's site is resolved at grab
  time (`indexer.ResolveDownload`, called from `download.resolveTorrentSource`): the
  `before` request, then the `infohash` block or the first `selectors` entry whose link is
  a magnet or a bencoded body (`testlinktorrent`), with the download (else search) headers.
  `checkReleaseSource` admits the tracker's site host and port for a cardigann entry, and
  every hop is still held to it. No v11 download selector sets `usebeforeresponse`, so the
  model has no such field there and each selector re-reads the release page.
- **Test** runs the feed query (no term) and passes on a readable answer, results or not.
  **Feed** is the same query.

What the engine does **not** do yet: pick a tracker's own category for a search beyond the
newznab root of the kind, or page past the first response.

## The catalog and the API

- `GET /cardigann/catalog` is `definitions.Catalog(dir)` (custom included) as summaries;
  the SPA filters it client-side (language, privacy, top category, login burden).
  `GET /cardigann/catalog/{file}` adds the settings. **An `info*` setting's text is
  upstream HTML and goes out as plain text** (goquery `Text()`), never as markup for the
  SPA to trust.
- `POST /indexers` with `protocol: cardigann` takes `definition`, `site`, `settings`,
  `proxy`; host, port and api_key are required by `checkIndexers` for the other protocols
  only, so `IndexerCreate` requires nothing but `name`. A draft test runs
  `config.CheckCardigann` first (an undeclared setting is a plain 422) and needs no
  `draftTargetRefused` — the target is a definition link, never a caller-named host.
- **Secrets never leave**: `Indexer.settings` carries the non-secret values, `secrets_set`
  names the secret ones holding a value, `proxy` comes back without userinfo. On `PUT`,
  `config.CardigannPatch` keeps a secret sent empty or left out — unless the definition
  changes, which starts the settings (and site) over; `proxy` absent keeps, `""` clears, and
  the SPA sends it only when the field changed, since the redacted form sent back would
  drop the credentials. Switching an entry's protocol away from `cardigann` drops the
  block. Any edit `Forget`s the session.
- The SPA's form is a TanStack form over `{name, site, enabled, priority, private, proxy,
  settings}` with one field per valued setting (`settings.<name>`): text/password
  (`secret` → password input, "stored, leave blank" help), checkbox (`"true"`/`"false"`),
  select (option keys). A definition with a captcha says to save first and log in from the
  card; one with a challenge names `cardigann.solver_url`.

## Logins

A port of `CardigannRequestGenerator.DoLogin`/`CheckIfLoginIsNeeded` and the login half of
`HttpIndexerBase.FetchIndexerResponse`. The engine runs one login; **when to log in, and when
not to, is `internal/indexer`'s** (`Cardigann.run`).

- **Methods** (`engine.Login`): `post` (inputs as a form to `login.path`, redirects
  followed), `form` (fetch the landing page — following redirects only when the definition's
  `followredirect` says so — read the form `login.form` names, keep every named, enabled
  `input` that is not an unticked checkbox or radio, then overlay `inputs`, `selectorinputs`
  and the captcha answer; `getselectorinputs` go on the submit URL; `submitpath` overrides
  the form's action; multipart when the form says so), `get` (`path?inputs`, always a `?`),
  `oneurl` (`path` + the `oneurl` input), `cookie` (the `cookie` setting parsed as a Cookie
  header, no request). `post` and `form` start from the definition's `login.cookies`, `get`
  and `oneurl` from none. A page with `script[src*="simpleCaptcha"]` is answered from
  `simpleCaptcha.php` itself. `login.selectors` makes every input key a CSS selector whose
  `name` is the field.
- **Failure** is a 401 or the first `error` block whose selector matches the response (its
  `message` selector, else the element's text) — `ErrLoginFailed` carrying **the tracker's
  message and never a URL** (a `oneurl` path is a secret), which is why the REST layer may
  echo it while it echoes no other probe error. `login.test.path` is unused, as in
  Prowlarr; only `test.selector` matters.
- **A lapsed session** is `CheckIfLoginIsNeeded`: a search response that redirects, is
  ≥ 400, or is HTML (or untyped) without `login.test.selector` → `ErrLoginRequired`. The
  indexer logs in and repeats the call **once**; still asked → a failed login.
- **Sessions** are the jar's cookies for the site URL plus the user agent
  (`engine.Session`), kept in the `IndexerSession` row under the indexer's config name,
  live for `engine.SessionLifetime` (30 days, Prowlarr's) and extended by every call that
  works — written only when they changed or a day of lifetime was used, so an RSS tick does
  not write a row per indexer. With no live session a login runs **before** the first
  request (Prowlarr spends a request to find out). An edit to, or the removal of, the
  indexer drops the row (`Manager.Forget`).
- **Backoff**: an automatic login that fails stores `login_failures` and a `retry_at` from
  Prowlarr's escalation (1 min, 5, 10, 15, 30, 1 h, 3, 6, 12, 24 h) and drops the cookies;
  until then a call that needs a login fails with `ErrLoginBackoff` and **sends nothing** —
  a wrong password retried every RSS tick is how an account gets banned. A person's login
  (`POST /indexers/{name}/login`, the draft test) ignores it. Logins of one indexer are
  serialised by a per-name mutex.
- **Captchas**: an automatic `form` login whose landing page shows the `captcha` image
  stops with `ErrCaptchaRequired` (a definition whose page shows none this time logs in).
  `POST /indexers/{name}/captcha` fetches the page and image, holds the landing page (body +
  cookies) in memory for ten minutes under a random token, and returns the image as a
  `data:` URL **whose type is sniffed, never the tracker's header**; `POST
  /indexers/{name}/login {token, answer}` submits that page. A draft has nowhere to hold
  one, so a captcha tracker is saved first, then logged in.
- **Downloads** go out with the session: `ResolveDownload` runs the download block inside
  the same login handling and adds the session's `Cookie` and user agent to the target's
  headers, which `download.resolveTorrentSource` sends.

## Browser challenges

A port of Prowlarr's `FlareSolverr` indexer proxy and `CloudFlareDetectionService`, applied
in `Engine.do` — the one choke point every request goes through (search, login, captcha,
download block), so all of them are covered.

- **Detection** (`isChallenge`) is Prowlarr's, exactly: the `Server` header must be
  `cloudflare`, `cloudflare-nginx` or `ddos-guard`, and then either a 503/403 whose body is
  one of the known challenge pages (`Just a moment...`, `Access denied`, `Attention
  Required! | Cloudflare`, `error code: 1020`, `DDOS-GUARD`), or the Dutch trackers' custom
  page (`Vary: Accept-Encoding,User-Agent`, no `Content-Encoding`, "ddos" in the body). Go's
  transport drops `Content-Encoding` after inflating, as .NET's automatic decompression
  does, so the last rule reads the same.
- **Solving**: `POST {cardigann.solver_url}/v1` with `request.get` (or `request.post` and
  `postData` for a urlencoded form — a multipart one is refused, as upstream), `maxTimeout`
  from `cardigann.solver_timeout` (60s). A 200 or 500 answer is read; no cookies is a
  failure carrying the solver's `message`. The cookies go into the jar for the request's
  host (and the site's), the solution's user agent becomes the engine's, and the request is
  sent **once more**; still challenged → `ErrChallenge`. With no solver configured the
  request fails with `ErrChallenge` naming `cardigann.solver_url`. The solver call has its
  own timeout (`solver_timeout` + 10s), not `otelx.HTTPClient`'s 30s.
- **The clearance persists** through the ordinary session: `Engine.Session` carries the
  user agent, `IndexerSession.user_agent` stores it, and a definition that logs in nowhere
  still gets a session row once a solve leaves cookies or a user agent behind. A
  `cf_clearance` is bound to the client address and user agent, which is why both travel
  together and why the download target gets the user agent too.
- **`cardigann.proxy`** (per indexer; http, https, socks5) builds the engine's client with
  `otelx.ProxiedHTTPClient` and is passed to the solver as `proxy` (credentials split into
  `username`/`password`), so the browser earns its clearance from the address the engine's
  requests leave from. The final `.torrent` fetch in `download.resolveTorrentSource` does
  **not** go through it — a tracker that challenges the download URL itself, from a
  different address, still fails there. The audit log redacts `.cardigann.proxy`.
- Errors: `indexer.ErrChallenge` (outcome `challenge`), echoed in connection tests — the
  engine's messages carry no tracker URL, and a transport error reaching the solver is
  redacted.

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
