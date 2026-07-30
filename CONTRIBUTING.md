# Contributing

Thanks for taking an interest. Bug reports and small focused PRs are the most
useful things you can send.

## Before you write code

- **Bug fix** — just open a PR. Link the issue if there is one.
- **New feature** — open an issue first so we can agree on the shape before you
  spend an evening on it. Check [docs/ROADMAP.md](docs/ROADMAP.md); music,
  books and the built-in player are already planned, and a half-built version
  of a planned feature is harder to merge than nothing.
- **New integration** (indexer, download client, media server) — welcome. Copy
  the shape of an existing one in `internal/indexer/`, `internal/download/`
  (e.g. `deluge.go`) or `internal/mediaserver/`.

## Setup

Requires Go >= 1.26, Node >= 24, [pnpm](https://pnpm.io) and
[Task](https://taskfile.dev). Nix users get all of it from the flake devshell
(`nix develop`, or direnv).

```bash
git clone https://github.com/datahearth/streamline.git
cd streamline
pnpm install --frozen-lockfile
task                    # builds frontend assets + the binary
```

The frontend is `//go:embed`-ed into the binary, so `task build:go` depends on
`task build:js` and `task build:css`. Always build through Task — raw
`go build` / `go test` / `pnpm exec` skips steps that CI does not.

For development:

```bash
streamline config init --output ./tmp/config.yaml    # or ./streamline config init …
task dev                # live reload via air, serves http://localhost:8080
```

## The loop

```bash
task lint               # golangci-lint + biome
task fmt                # golangci-lint fmt + biome format --write
task test               # all suites
task test:unit -- ./internal/metadata/...   # one package
```

`task lint` and `task test` are what CI runs; if they pass locally the PR
should be green.

## Code generation

Generated files are committed. Regenerate and include the result when you
change an input:

| Changed                        | Run                                          |
| ------------------------------ | -------------------------------------------- |
| `api/openapi.yaml`             | `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml` |
| `ent/schema/`                  | `go generate ./ent`, then `task migrate:diff -- <name>` |
| a mocked interface             | `go tool mockery`                            |
| any of the above               | `task generate` does all three               |

`api/openapi.yaml` is the source of truth for the REST API — write the spec
first, then implement the generated handler interface.

## Conventions

The full house style lives in [CLAUDE.md](CLAUDE.md) — it is written for AI
agents but it is an accurate description of how the codebase is put together
(logging, observability, config-backed resources, auth, frontend). Worth
skimming before a larger change. The short version:

- **Go**: tabs, `slog.XContext(ctx, …)` for logging (no logger plumbing), a
  span per service operation via the package `tracer`, no named returns, no
  ignored errors.
- **Tests**: Ginkgo + Gomega. One `_test.go` per source file, in the same
  package. Every top-level `Describe` needs a
  `Label("unit"|"integration"|"e2e")` or the suite is silently skipped.
- **Frontend**: Svelte 5 + TypeScript (`<script lang="ts">` everywhere),
  TailwindCSS v4, TanStack Query/Form, valibot. A section's wrapping layout is
  `_module.svelte` — `_layout.svelte` renders as a sibling route instead.
- **Comments**: only where the code cannot explain itself. No restatements, no
  section banners, no changelog notes in the source.
- **Config-backed resources**: indexers, download clients, media servers and
  quality profiles live in the YAML config, not in SQLite. New config keys must
  be added to `config.defaults()` and `api/config.schema.json` — two tests
  guard that.

## AI-assisted contributions

Streamline is built with heavy AI assistance — that's what [CLAUDE.md](CLAUDE.md)
is for. So this is not a "no AI" project, and you don't need to hide that you
used a coding agent. There is one rule:

**You are the author. The model is a tool.**

In practice:

- **Understand every line you submit.** If a reviewer asks why a function is
  shaped that way and the honest answer is "the model wrote it", the PR isn't
  ready. Read the diff before you open it.
- **Run `task lint` and `task test` yourself.** An agent claiming the tests pass
  is not evidence that they pass. Paste real output if a change is subtle.
- **Verify the code against reality, not plausibility.** Agents invent config
  keys, ent methods, and API endpoints that look exactly right and don't exist.
  Build it.
- **Mention it in the PR description.** One line — "written with Claude Code" —
  so a reviewer knows which failure modes to look for. No stigma attached.
- **Point your agent at `CLAUDE.md`.** It is the house style and it is kept
  current; a PR that follows it needs far less review.

What gets closed without much discussion:

- Large unreviewed diffs the submitter can't explain, or PRs that clearly
  weren't run
- Agent-generated refactors, "modernisations", or comment/docstring sweeps nobody
  asked for
- Scanner or agent output filed as a bug report or security report with no
  reproduction — see [SECURITY.md](SECURITY.md) for what a real report looks like

Reviewer time is the scarce resource here. A small PR you fully understand is
worth more than a large one you don't.

## Commits and PRs

Conventional Commits, subject line only unless the body earns its place:

```
fix(library): scope import decision writes to their scan
feat(indexer): support Jackett
```

`type(scope): msg`, scope optional. The changelog is generated from these by
git-cliff, so don't hand-edit `CHANGELOG.md`.

Keep the PR to one topic. Rebase on `main` rather than merging it in. Fill in
the PR template checklist.

## License

Contributions are licensed under [GPL-3.0-or-later](LICENSE), same as the
project.
