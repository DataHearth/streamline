# Streamline

Unified media management platform replacing the *arr stack (Radarr, Sonarr, Lidarr, Readarr) and Seerr. Single self-hosted binary with a slick web UI, REST API for mobile developers, multi-user support with SSO, built-in request system, and automatic media organization. Supports Torznab and Prowlarr indexers, torrent download clients (qBittorrent, Transmission, Deluge, or the built-in engine), and media server notifications (Plex, Jellyfin, Emby). Shipped v1.0.0; music, books and a player are planned (`docs/wiki/Roadmap.md`).

## Stack
- Go monolith: chi + oapi-codegen (OpenAPI → server) + ent ORM + modernc.org/sqlite (CGO-free)
- Config: koanf (file + env + flags, STREAMLINE_ prefix)
- Logging/Observability: slog + OpenTelemetry (traces, metrics, logs via OTel bridge)
- Frontend: Svelte 5 SPA (TypeScript) — Routify v3 file-routing, TailwindCSS v4, TanStack Query + TanStack Form, valibot schemas; bundled by esbuild, embedded via go:embed

## Frontend

Svelte 5 SPA in `web/app/` (TypeScript everywhere), Routify v3 file-routing over `web/app/routes/`, TanStack Query + Form, valibot, TailwindCSS v4, bundled by esbuild and `//go:embed`-ed. A section's wrapping layout MUST be `_module.svelte` — `_layout.svelte` renders as a sibling route, not a wrapper. Both library lists paginate server-side; settings pages pick save-per-control **or** one TanStack form, never both on a page.

**Conventions, the dropped facets, search folding, the PWA surface and the bundling pipeline: [`docs/agents/frontend.md`](docs/agents/frontend.md) — read it before touching `web/app/**` or `web/embed.go`.**
## Logging
- No `*slog.Logger` plumbing. `cmd/main.go` calls `observability.Setup` then `slog.SetDefault`; every package logs via `slog.XContext(ctx, ...)` (top-level) — never hold a logger field.
- `observability.Setup` returns one `slog.Handler` = `contextEnrichingHandler(multiHandler{stderr, otelslog.Handler})`. stderr is pretty text/json from `log.app.{level,format}`; otelslog bridges to the OTLP logs pipeline (traces/metrics/logs all batch-exported to `otel.endpoint`).
- **`log.app.enabled` gates the stderr sink only**; the OTel pipeline is gated on `otel.endpoint` alone. It used to return early with a `DiscardHandler`, so quieting local logs silently stopped traces and metrics as well — an instance exporting nothing, for a reason nowhere near the OTel config.
- `contextEnrichingHandler` auto-attaches `request_id` (chi), `user.id`/`user.email`/`user.roles` (auth claims, OTel semconv v1.40.0), `http.route` (chi route pattern). Trace/span IDs come from otelslog. Use `slog.XContext` so ctx flows through.
- `observability.LevelCritical` (= `slog.LevelError + 4`, rendered `CRITICAL`) for panics, invariant violations, unrecoverable conditions. Call via `slog.LogAttrs(ctx, observability.LevelCritical, ...)`.
- OTel semconv pinned at v1.40.0 — use `semconv.<Key>Key` constants (e.g. `semconv.HTTPRouteKey`) over string literals; keep versions aligned across files.

## Observability
- `internal/otelx` is the leaf OTel helper package. Holds `HTTPClient` (otelhttp-wrapped — use for every outbound HTTP call; never `http.DefaultClient`) and `RecordSpanError(span, err) error` (inline: `return ..., otelx.RecordSpanError(span, err)` — no named returns). Must stay dependency-free; `internal/observability` imports `internal/auth`, so anything auth transitively uses must live in `otelx` not `observability`.
- Per-package tracer: `var tracer = otel.Tracer("github.com/datahearth/streamline/internal/<pkg>")`. Span names `<pkg>.<op>` (e.g. `download.grab`, `rss.process_movie`, `indexer.query`).
- DB auto-instrumented via `otelsql.Open` + `RegisterDBStatsMetrics` in `internal/db/client.go`. Add business-logic spans in service methods + `span.SetAttributes` for domain context.
- Prefer semconv helper funcs (`semconv.UserEmail`, `semconv.UserRoles`, `semconv.UserID`, `semconv.DBSystemNameSQLite`) over raw `attribute.String("user.email", …)`.
- **Never put a row id in a metric attribute** — `movie.id`, `episode.id`, a request path with an id in it. That is one time series per row in the library. Ids belong in the log line and on the span; a metric attribute is a low-cardinality dimension (`outcome`, `kind`, `status`, an indexer or client *name*).
- **A span that can fail must record the failure.** Every terminal path funnels its error through `otelx.RecordSpanError` or, where a helper owns the exits, through one choke point (`transcoding.record`, `scheduler.executeJob`). A span left untouched by its own failure paths reports OK, and filtering a backend for error spans then finds nothing wrong.
- **Every goroutine started with `go` defers `observability.RecoverPanic(ctx, what, onPanic)`.** `middleware.Recoverer` covers the request path and nothing else; an unrecovered panic in a background worker takes the whole process down. `onPanic` is the caller's policy — a scheduled job records `errJobPanicked` and retries next tick, a transcode fails terminally, a boot goroutine calls `stop()` for a clean shutdown.
- Sampling is `otel.sample_ratio` (default `0.05`), overridden by `OTEL_TRACES_SAMPLER` — `Setup` skips its own `WithSampler` when that variable is set, because passing the option at all makes the SDK ignore the env. `otel.insecure` is needed for an `http://` collector; `otel.environment` fills `deployment.environment`.
- `otel.SetErrorHandler` routes SDK export failures into the stderr handler **only** — never the full handler, or a failing log exporter is fed its own error records.

## HTTP Routes
- `/health` — pre-auth bare JSON endpoint (NOT in OpenAPI), for k8s probes + load balancers. Registered in `internal/server/server.go`.
- `/api/docs` — Scalar UI shell (`web.Handler.APIDocs`). `/api/v1/openapi.yaml` — embedded spec. REST API mounted via `restapi.Mount`.
- SPA fallback: `s.router.NotFound` → `web.Handler.SPAShell`, which writes the embedded `web/app/index.html` for every non-API, non-static path; Routify owns client-side routing incl. its own 404. Static assets served at `/static/*` from `fs.Sub(web.Assets, "static")` (wired in `web.Mount`).
- `/posters/{kind}/{id}/poster.jpg` — poster proxy via `s.posters.Serve`.
- `DELETE /api/v1/torrents/{hash}` also calls `downloads.PurgeRecordForHash`, dropping the live download record behind that torrent and reverting its media. The monitor's orphan sweep would eventually do it, but only after `monitorOrphanGrace` (2 min) — a window that exists to tolerate a client that hasn't caught up with its own listing, which a removal issued through streamline is not. The Torrents page invalidates `["activity","queue"]` on success for the same reason.
- The auth middleware's `excludePaths` list is assembled in `internal/server/wire.go` and handed to `middleware.NewAuth` (`/health`, `/api/docs`, `/api/v1/openapi.yaml`, `/static/`, `/login`, `/register`, `/auth/login`, `/auth/register`, `/auth/config`, `/auth/invite/`, `/auth/oidc/`); matcher in `internal/server/middleware/auth.go`, paths ending `/` match as prefix.

## Auth, sessions & OIDC

The middleware splits transport by path prefix: `/api/v1/*` takes Bearer, `X-API-Key`, or a same-origin `streamline_session` cookie; everything else is cookie-only and 302s to `/login`. API keys are read-only on the identity band. OIDC is multi-provider, and provider trust is **two independent axes** — `email_linking` (which accounts it may adopt) and `allow_admin` (whether it may grant admin).

`role.Federated` in `internal/role/` is the single admin ceiling, enforced by a package boundary that makes skipping it a compile error, plus type-resolved specs over package `auth`. **Never merge the two trust keys, never export the role field, never move the type back into `auth`** — four rounds of weaker guards were each bypassed.

**Linking policy, the API-key denial list, seed admin, registration modes and what the guards deliberately do not cover: [`docs/agents/auth-and-oidc.md`](docs/agents/auth-and-oidc.md) — read it before touching `internal/auth/`, `internal/role/`, or the auth middleware.**

## Config surface

Seven sections are readable and patchable at runtime (`/config/{auth,library,ffmpeg,download,metadata,system,transcoding}`, admin-only, partial-patch); everything else is file-only and needs a restart. The trust boundary and every secret are deliberately file-only — a key the API can reach is a key the API can be made to grant. Media servers, download clients, indexers and quality profiles live in the YAML config, name-keyed and hot-editable, not in ent/SQLite.

**Adding or removing a config field means `defaults()` AND `api/config.schema.json`.** Bounded ints go through `narrowUint8` and durations through `checkDuration` — both must reject *before* conversion, since nothing validates a request body against the spec.

**The full section-by-section rules and the resource CRUD contract: [`docs/agents/config-surface.md`](docs/agents/config-surface.md) — read it before touching `internal/config/` or the `/config/*` handlers.**

## Downloads, imports & adoption

The builtin engine owns its own peer sockets and caps its peer pool with constants (not config keys). The importer verifies a probed source against the release claim before any transfer and parks a failing record `held` rather than counting an attempt. Untracked torrents the clients report are matched against the library each monitor tick and either auto-imported or filed as a `pending` proposal. Every successful import dispatches a media-server refresh.

**Engine internals, port rebinding, hold/resolve semantics, the adoption proposal lifecycle and Plex section discovery: [`docs/agents/downloads-and-imports.md`](docs/agents/downloads-and-imports.md) — read it before touching `internal/bittorrent/`, `internal/importer/`, `adopt.go`, or `internal/mediaserver/`.**
## Selective file download

`download.selective_files` (default **false**, runtime-toggleable) gates grabbing only the files a release is wanted *for*; off is bit-for-bit the whole-torrent grab. Two flows — a `.torrent` decodes the metainfo before add, a magnet parks the record `selection_state: pending` until `RunSelectionPass` resolves it. `replace_mode` decides the wanted set; `selection_mode` on `TorrentSession` is what survives a restart.

**Flow rules, per-client quirks and the widen semantics: [`docs/agents/selective-file-download.md`](docs/agents/selective-file-download.md) — read it before touching `internal/download/manager.go`, `selection_pass.go`, or any client's `SetWantedFiles`.**

## Quality scoring

`internal/quality` is the pure scoring engine (no config/db imports); context builders live in `internal/quality/qualityctx`. A release is gated by resolution **band** first (`preferred_resolution` is a hard ceiling), then by summed custom-format score against `min_score`. `quality.ReplacesFile` is the single predicate deciding whether one release replaces one file. Builtins describe a release and never judge one — opinionated rules belong in `custom_formats`.

**Condition semantics, the release-vs-file evidence asymmetry, indexer result filtering, profile assembly and the upgrade paths: [`docs/agents/quality-scoring.md`](docs/agents/quality-scoring.md) — read it before touching `internal/quality/**`, the RSS scanners, or the profile/custom-format handlers.**

## Media lifecycle & library queries

Twelve optional probe columns hang off `MediaFile`; `probed_at` stamped means all twelve were recorded, so **a migration adding a probe column nulls `probed_at` in the same file**. `MediaEvent` has three optional owner edges and exactly one is set. `episode.status` is `wanted | downloading | importing | paused | available | skipped`, and every path that grabs must mark its episodes. The series list filters, sorts and pages in SQL with a per-show counts rollup — never the episode tree.

An episode with no air date is **unaired, never missing**, and five places split aired from unaired — they must agree or a show matches a filter with nothing missing on its page. Specials (season 0) are out of every show-level number — seasons, episodes, the missing filter, the episode sort — and must be out of all of them at once; the specials season still renders with its own counts.

Cast is written inside the title's transaction; the **person biography fetch is not** — it runs after the commit, from the service layer that holds the providers, and `internal/db` stays free of any provider dependency. A person is enriched **once, ever**, keyed on `Person.details_fetched_at`, and no failure there may fail the import.

**Probe semantics, the event hooks, cast ingest and person enrichment, re-identify ordering, the in-flight marking paths and the list push-down: [`docs/agents/media-lifecycle.md`](docs/agents/media-lifecycle.md) — read it before touching status transitions, `internal/events/`, or `internal/db` list queries.**
## Transcoding

`transcoding: {enabled, max_concurrent, max_failures, hw_accel, hw_device, verify}` is the global surface, all runtime-editable with no restart; the **policy** lives on the quality profile (`quality_profiles[].transcode`), not globally. A job is created in the same call as the `MediaFile` row. Encode → verify → atomic swap beside the original; a verified-worse output is `rejected` (terminal, retryable by hand), a failure retries to `max_failures`.

**Job lifecycle, verification rules, boot recovery, VAAPI/hardware notes and the deliberate non-features: [`docs/agents/transcoding.md`](docs/agents/transcoding.md) — read it before touching `internal/transcoding/`, `internal/ffmpeg/`, or the `/transcoding/*` handlers.**

## Release-name parsing & bulk import matching

`library.Parse` and friends turn a release name into facts; `internal/library/bulkimport/` classifies scanned folders against TMDB/TVDB and commits them. Nearly every rule in this area exists because a specific real-world release broke the obvious implementation — do not simplify one without reading why it is shaped that way.

**Parser rules, title normalization, classifier ranking, commit-time re-resolution and the naming-template/path-sanitizing contract: [`docs/agents/bulk-import-matching.md`](docs/agents/bulk-import-matching.md) — read it before touching `internal/library/` parsing, the rename services, or the metadata classifiers.**

## API conventions
- Every list endpoint bounds `limit` to `[1, 100]` (200 for activity) and **400s** a value outside it (`pagination.go` `limitOr` — there is no request-validation middleware, so the spec's bounds are enforced there). The SPA's `apiAllPages` (`web/app/lib/api.ts`) walks pages at `PAGE_LIMIT = 100` against `total`; the handlers used to clamp silently, and asking for `limit=500` — which nine call sites did — showed 100 of 621 movies with no indication anything was missing.
- `otelx.RecordSpanError(span, nil)` returns nil and leaves the span alone. It used to deref the error for `SetStatus`, so a missing `if err != nil` was a panic the handler reported as a 500 nowhere near the mistake — which is exactly how `BulkDecide` shipped and how the dev server caught it.
- Connection-test 422s carry `code: connection_failed` and the SPA renders their message verbatim — it is composed by our own client code and is the only place the upstream status appears. A plain 422 reads "some of those values weren't accepted", which blames credentials that are fine.
- **A refused grab answers `422` with `code: grab_rejected`**, and the SPA renders that message verbatim (`errorText`, same arm as `connection_failed`). All four grab handlers share it — `GrabMovieRelease`, `GrabEpisodeRelease`, `GrabSeasonRelease`, `GrabSeriesRelease` — over `download.ErrUntrustedSource` and `ErrNoWantedFiles`. Without a code the SPA falls through `BY_CODE` to `byStatus(422)` and shows its generic "some of those values weren't accepted", which blames the request body: on the homelab, two anime episodes refused for `ErrNoWantedFiles` (the `SxxE<absolute>` keep-set gap) were indistinguishable from a malformed release payload, and the message naming the actual reason — the sentinel plus the caller's own release title — was discarded before it reached the screen. `errUnprocessable` still has no code by design; a 422 whose message is a *diagnostic* rather than field validation needs one, or the message never renders.

## Testing
- Framework: Ginkgo (Describe/Context/It/By) + Gomega assertions
- Mocks: Mockery (`go tool mockery`) — config in `.mockery.yaml`, generated to `internal/<pkg>/mocks/`
- Run tests: `task test:unit` / `task test:integration` / `task test:e2e` / `task test` / `task test:coverage` — all `go tool ginkgo run -r --keep-going`; the first three and `test:coverage` (`!e2e`) carry a label filter, `task test` runs everything unfiltered (e2e capped at `--timeout=1m15s`); forward extra args via `CLI_ARGS`, narrow with `LABEL_FILTER=`
- `task test:e2e:containers` — container-backed e2e (Docker + `STREAMLINE_E2E_CONTAINERS=1`, 5m timeout); hermetic `test:e2e` excludes `containers`-labeled specs.
- Run single suite: `task test:unit -- ./internal/metadata/...`
- Each Ginkgo suite has a dedicated `<pkg>_suite_test.go` (the three `internal/utils/*` packages inline it in their single test file) with `TestX` + `RunSpecs` + `BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })` — `testutil.InstallSlog()` routes `slog.Default` to GinkgoWriter for the suite's lifetime.
- Mocks emit to `internal/<pkg>/mocks/mock_<Name>.go` — type `Mock<Name>`, constructor `NewMock<Name>(GinkgoT())`
- Regenerate mocks after interface changes: `go tool mockery`
- Span-instrumented funcs wrap ctx via `tracer.Start(...)`, breaking exact-ctx mock matchers. Use `mock.Anything` for the ctx param in `.EXPECT().Fn(mock.Anything, ...)` calls.

## Multi-agent fix workflows
Convention for landing a batch of independent fixes (audit findings, review comments, a bug list). Iterate on this as we learn.

- **Group findings by file ownership first.** Two agents editing one file is the failure mode; disjoint file domains are what make the rest safe. One group per domain (e.g. `restapi/`, `download+importer`, `middleware+web`, `auth+db+ent`).
- **One workflow per group, one agent per finding.** Each fix agent runs with `isolation: 'worktree'` so it can never see or clobber another's tree. Agents fix *only* their assigned finding — no drive-by refactors, no fixing things they notice.
- **Seed every worktree before the agents build.** A fresh worktree cannot compile *anything* importing `web`: all four `go:embed` inputs are gitignored — `web/static/css/style.css`, `web/static/css/docs.min.css`, `web/static/js/docs.min.js`, `web/static/dist/`. The failure reads `pattern static/css/docs.min.css: no matching files found`, which looks like broken code and is not. Copy them in from the main checkout right after launch (they stay gitignored, so they never pollute a patch); re-run the copy as assemble-phase worktrees appear. Building them per-worktree instead costs a full pnpm+esbuild+tailwind cycle per agent.
- **Agents commit; they do not emit patches.** Every fix agent ends by committing its work on its worktree's own branch, one commit per finding, with a real Conventional Commits subject. Integration is then a merge/cherry-pick against a shared ancestor — a proper 3-way merge that understands moved and adjacent hunks. Patch files were the earlier convention and they mangle exactly this: a flat diff has no base, so two agents editing neighbouring lines of one file produce hunks that apply cleanly in isolation and silently mis-stack when combined. Commits carry the base; diffs don't.
  - Inside a harness worktree the agent uses `git commit` (the worktree is a git worktree, not a jj workspace — this is the "raw git plumbing" exception, not a licence to use git elsewhere). The integrating session brings the work across with jj, which sees those commits after its automatic import.
  - If a patch file is ever unavoidable, generate it with `git diff --no-ext-diff`: this machine sets `diff.external = difft`, so a bare `git diff > x.patch` writes difftastic's *rendered* output — diff-shaped, no `---`/`+++`/`@@`, and `git apply` rejects it with "No valid patches in input". Applying is unaffected; only generation.
- **Agents never write to the main working copy.** A per-workflow assemble agent merges the group's commits in its own worktree, resolves intra-group overlap, verifies `task build:go`, and leaves a single reviewable branch.
- **The main session integrates.** Workflows finish in any order; the session reviews each group's commits, brings them into the working copy, and verifies — serialized, so nothing races on the tree. Never let a workflow write to main itself.
- **Codegen runs once, at the end, in the main tree.** `task generate` / `task migrate:diff` rewrite `ent/*` and `internal/*/mocks/*`; per-workflow codegen produces conflicting generated files against a partial tree. Agents may run it inside their worktree to compile-check, but must keep generated output out of the commits they hand over. Migrations from `task migrate:diff` *are* handed over, unedited.
- **Verification is the session's, after integration** — `task build:go`, `task lint:go`, `task test:unit` over the merged result. A group that passed alone can still break the merge.
- **Clean up after integration.** Remove each group's worktrees and their `worktree-*` branches once its work is in and verified. They live under `.claude/worktrees/` *inside* the repo, so until they go the editor/LSP indexes every half-finished agent tree and reports its errors as if they were yours.
- Agents get no history rewriting and no publishing: no `jj`, no rebase/amend of anything they did not create, no push, no `task dev`, no `task release:*`.

## Docs & Spec Upkeep
- Any change to `/api/v1` behavior — routes, auth/authz, status codes, security schemes, request/response shapes — updates `api/openapi.yaml` in the same change, then `task generate` to regenerate `gen.go` (the spec itself is served from `api/embed.go`'s `//go:embed openapi.yaml`). A handler and its spec entry must never disagree.
- When a feature or behavior changes, update the affected CLAUDE.md section — **or the `docs/agents/` file its stub points at, when the change lands in one of those subsystems** — **and the wiki** (`docs/wiki/` — user-facing pages like `REST-API.md`, `Authentication-and-SSO.md`) in the same change — stale docs describing the old behavior are worse than none.
- `docs/agents/` holds the subsystem detail this file used to carry verbatim — nine files, one per area, each behind a `##` stub above. `docs/agents/README.md` is the index and states the upkeep rule. A stub and its file must never disagree: the stub carries the shape and the load-bearing invariants, the file carries the rules and the reasons.
- `docs/wiki/` is the source of truth for the GitHub wiki; `.github/workflows/wiki.yaml` mirrors it to `DataHearth/streamline.wiki.git` on every push to `main` touching it. Filename = page title (dashes become spaces), `_Sidebar.md` is the nav. Never edit through the wiki UI — the next sync overwrites it.

## Code Generation
- API: `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml` → `internal/server/restapi/gen.go` (package `restapi`) — regenerate after spec changes
- ORM: `go generate ./ent` — regenerate after schema changes
- All codegen: `task generate` (runs `go tool oapi-codegen`, `go generate ./ent`, `go tool mockery`). Versioned migrations: `task migrate:diff -- NAME` diffs the ent schema into a new migration.
- Prefer the narrowest integer type on ent schema fields (`field.Uint8` for bounded counters like `grab_failures`, `field.Uint16` for small ranges like `number`/`year`) — sqlite storage is identical but Go structs stay memory-efficient.
- After ent regen the LSP may report "undefined" for new fields/methods briefly; `task build:go` is the source of truth.

## Build
- Task runner: [Taskfile.yaml](Taskfile.yaml) — sole build orchestrator (no npm scripts).
- **Always** invoke operations via `task <target>`. Raw `go build`/`go test`/`ginkgo`/`golangci-lint`/`pnpm exec` bypass the Taskfile and drift from CI.
- Go tooling runs via the Go `tool` directive: `go tool {ginkgo,golangci-lint,oapi-codegen,mockery,air,govulncheck}` (`task vulncheck` runs the last, as CI does) — not system-installed; the flake devshell only ships `go`, `pnpm`, `go-task`, `biome`, etc.
- Full build: `task` (= `build:app` → `build:go`, which depends on `build:js` + `build:css` because assets are `//go:embed`-ed) → `go build -o streamline ./cmd`.
- Frontend: `task build:js` (Scalar docs bundle + Paraglide message compile + `routify build` + esbuild SPA) and `task build:css` (TailwindCSS v4).
- Dev server: `task dev` — live reload via `go tool air` (`.air.toml`; rebuilds with `task build:app`, runs `streamline --config ./tmp/config.yaml`).
- Lint: `task lint` (`lint:go` golangci-lint + `lint:frontend` biome + `lint:ts` svelte-check). Format: `task fmt` (`golangci-lint fmt` + `biome format --write`) — Biome, not prettier.
- Clean: `task clean`.

## Release
- App and chart version independently, but they release the same way, so both live in one `release` namespace (`.taskfiles/release.yaml`). App: `task release:app VERSION=X.Y.Z`, which is `release:app:changelog` (regenerates the CHANGELOG, commits it on its own revision and advances `main`) then `release:app:tag` (tags `main`, push fans out to `.github/workflows/{release,image}.yaml`). Chart: `task release:chart VERSION=X.Y.Z`, which is `release:chart:bump` (rewrites `version` in `deploy/helm/streamline/Chart.yaml`, regenerates `deploy/helm/streamline/CHANGELOG.md`, commits both, advances `main`) then `release:chart:tag` (tags `chart-vX.Y.Z`, triggers `.github/workflows/chart.yaml`); run the two apart to review the bump commit before the tag goes out. Bump the chart against the diff, not against the version already there — a renovate deps PR bumps the patch itself, so a feature landing after it inherits a patch number that claims nothing shipped. Both take `VERSION` as bare semver; a leading `v` is accepted and stripped, so the same string works for either. The tag prefix is the task's business — `v` for the app, `chart-v` for the chart. The shared halves are `release:_changelog` and `release:_tag`, internal and the only place the jj-vs-git commit dance and the tag push are written. `_tag` is all-jj since jj 0.45 (`jj tag set` + `jj git push --remote origin --tag`) — the tags it writes are lightweight, not annotated, which nothing here reads.
- Artifacts come from goreleaser (`.goreleaser.yaml`): CGO-free binaries for linux/darwin/windows × amd64/arm64, archives carry `config.example.yaml`, `checksums.txt` is cosign-signed (bundle format), one SPDX SBOM per archive. Images are cosign-signed keyless + SBOM-attested + grype-scanned in `image.yaml`.
- Both tags publish a GitHub release, and both bodies come from `.github/actions/release-notes` (composite): it extracts the version's CHANGELOG section, hands it to `anthropics/claude-code-action` to rewrite as operator-facing notes (`--json-schema`, so the result arrives as `structured_output` — no file writes, no repo access), then appends the caller's install/verify footer. **The model is never load-bearing**: the step is `continue-on-error` and skipped entirely without `CLAUDE_CODE_OAUTH_TOKEN`, and empty or unparseable output falls back to the raw changelog section — which is exactly what shipped before. A *missing* changelog section still fails the release, since that is our mistake and not the model's. The chart release is created with `--latest=false` so it does not displace the app release on the releases page.
- `release:app` / `release:app:tag` / `release:chart` / `release:chart:tag` / `release:image` / `release:helm` publish for real and have no dry-run. Both end-to-end targets commit *before* they reach the half that pushes, so a dry check must be `task --dry`, never a real run with a deliberately bad VERSION. Never run one to "test" it — only exercise a precondition's failure path.

## Version Control
- Repository uses jj (Jujutsu) with a git backend. Commit a task with `jj new -m "msg"` (creates empty child, auto-snapshots subsequent work). Seal a task by starting the next with `jj new -m "..."`.
- Don't re-run `jj describe` on a commit you're already working in — message is set once per task.
- If working copy holds unrelated leftover edits (e.g. settings.json), describe them into their own commit *before* `jj new` for feature work.
- Abandon empty working-copy commits after no-artifact steps (smoke tests, manual verification): `jj abandon @`.
- Commit messages: Conventional Commits `type(scope): msg` — add a `(scope)` where it clarifies, omit it when it doesn't. Bundle related changes; avoid single-file revisions.

## Project Structure
- `api/openapi.yaml` — OpenAPI spec (source of truth for REST API)
- `internal/` — all application code
- `ent/schema/` — ent ORM schemas
- `web/app/` — Svelte 5 SPA (`routes/`, `components/`, `lib/`); `web/static/` — CSS/JS/fonts/images; `web/embed.go` `//go:embed`s the built assets + SPA shell
- `docs/plans/` — design docs and plans (gitignored, local only)
- `docs/wiki/Roadmap.md` — public, user-facing feature status (Shipped / In progress / Planned). Not an internal phase list; update it when a feature ships or starts.
- `CHANGELOG.md` — generated from conventional commits by git-cliff (`task release:app:changelog VERSION=X.Y.Z`); don't hand-write entries. Commits touching `deploy/helm/**` are excluded — those belong to `deploy/helm/streamline/CHANGELOG.md`, generated the same way but off `chart-v*` tags by `release:chart:bump` and packaged into the chart tarball. Two configs: `cliff.toml` (app) and `cliff.chart.toml` (chart). They exist separately only because `commit_parsers` differs and git-cliff exposes that neither as a flag nor as an env override — the chart promotes `chore(helm)` and `chore(deps)` into Changed, since a chart release is often nothing but those and cliff.toml's blanket chore skip produced a heading with nothing under it. The `chore(helm): chart vX.Y.Z` bump commit is skipped there, or it lands as an entry inside its own section. The templates are duplicated between the two files — keep them in sync. Path scope stays at each call site (`--exclude-path` for the app, `--include-path` for the chart), so a commit touching app *and* chart appears in both.
- `deploy/` — Dockerfile + Helm charts + `compose.yaml` (local test stack: gluetun VPN + qBittorrent + Prowlarr + Plex, builds from source — *not* a user deployment template)
- `deploy/helm/streamline/` — streamline chart (installs to `streamline` ns). Optional subchart `charts/observability/` installs upstream alloy/VM/VL/VT/grafana into `observability` ns via `namespaceOverride`.
- `deploy/helm/streamline/kubeconfig.yaml` — kind cluster kubeconfig (auto-exported by `task helm:kind:up`; flake devshell sets `KUBECONFIG` to this path).

## Helm Gotchas
- VM/VL/VT charts (pinned 0.45.0/0.13.9/0.1.11): at v0.35/0.12/0.0.7 the selector used `app: server` but the template labels dropped it, so `server.podLabels.app: server` is set in each subchart's values. The pinned charts derive pod labels from the selector labels, so the workaround is likely removable — verify against a rendered template before dropping it.
- Cross-namespace k8s DNS requires FQDN: `<svc>.<ns>.svc.cluster.local`. Streamline→alloy uses `alloy.observability.svc.cluster.local:4318`.
- OTel SDK defaults to HTTPS. Set `OTEL_EXPORTER_OTLP_INSECURE=true` env when endpoint is HTTP (alloy is HTTP).
