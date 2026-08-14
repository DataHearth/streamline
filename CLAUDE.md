# Streamline

Unified media management platform replacing the *arr stack (Radarr, Sonarr, Lidarr, Readarr) and Seerr. Single self-hosted binary with a slick web UI, REST API for mobile developers, multi-user support with SSO, built-in request system, and automatic media organization. Supports Torznab and Prowlarr indexers, torrent download clients (qBittorrent, Transmission, Deluge), and media server notifications (Plex, Jellyfin, Emby). Shipped v1.0.0; music, books, a built-in torrent client and a player are planned (`docs/ROADMAP.md`).

## Stack
- Go monolith: chi + oapi-codegen (OpenAPI → server) + ent ORM + modernc.org/sqlite (CGO-free)
- Config: koanf (file + env + flags, STREAMLINE_ prefix)
- Logging/Observability: slog + OpenTelemetry (traces, metrics, logs via OTel bridge)
- Frontend: Svelte 5 SPA (TypeScript) — Routify v3 file-routing, TailwindCSS v4, TanStack Query + TanStack Form, valibot schemas; bundled by esbuild, embedded via go:embed

## UI/UX workflow
- **Any** change to `web/app/**` (Svelte components/routes), Tailwind CSS, page layouts, form flows, or interactive patterns MUST invoke the `ui-ux-pro-max:ui-ux-pro-max` skill *before* writing the code. Ensures consistent design tokens/spacing/states across the app.
- Exempt: pure server-side handler logic that doesn't change rendered output.

## Frontend
- Svelte 5 SPA in `web/app/` (TypeScript everywhere — `<script lang="ts">`, `.ts` lib files). Entry `web/app/main.ts` + `App.svelte`; shell `web/app/index.html` (embedded as `web.SPAShell`).
- Routing: Routify v3 (`routify.config.js`) scans `web/app/routes/` → generated `web/app/.routify/` (gitignored, never hand-edit). A section's wrapping layout MUST be `_module.svelte` — a `_layout.svelte` renders as a sibling route, not a wrapper.
- Data/forms: `@tanstack/svelte-query` (`lib/query.ts`, `lib/api.ts`), `@tanstack/svelte-form`, `valibot` schemas (`lib/schemas.ts`). Toasts: `svelte-sonner` (`lib/toast.ts`). Icons: `@lucide/svelte`. Class merge: `lib/cn.ts` (clsx + tailwind-merge) — never duplicate elements in `{#if}/{:else}` just to swap classes.
- Bundling: `pnpm exec routify build` then `node web/app/esbuild.config.mjs` (esbuild + esbuild-svelte) → `web/static/dist/spa.min.{js,css}`. Scalar API-docs bundle is separate: `web/static/js/docs.js` → `docs.min.js` + `docs.min.css`. `task build:js` emits both.
- TailwindCSS v4 (`@tailwindcss/cli`): `web/static/css/input.css` → `style.css`, scanning `web/app/**/*.svelte` (`task build:css`).
- Frontend deps via pnpm (`package.json` is a manifest only, no scripts). JS/CSS lint+format is Biome (`biome.json`) over `web/static/js` + `web/static/css` only — tabs width 2, double quotes, semicolons.
- Distribution: single binary (frontend `//go:embed`-ed), OCI images, Helm charts.

## Logging
- No `*slog.Logger` plumbing. `cmd/main.go` calls `observability.Setup` then `slog.SetDefault`; every package logs via `slog.XContext(ctx, ...)` (top-level) — never hold a logger field.
- `observability.Setup` returns one `slog.Handler` = `contextEnrichingHandler(multiHandler{stderr, otelslog.Handler})`. stderr is pretty text/json from `log.{level,format}`; otelslog bridges to the OTLP logs pipeline (traces/metrics/logs all batch-exported to `otel.endpoint`).
- `contextEnrichingHandler` auto-attaches `request_id` (chi), `user.id`/`user.email`/`user.roles` (auth claims, OTel semconv v1.40.0), `http.route` (chi route pattern). Trace/span IDs come from otelslog. Use `slog.XContext` so ctx flows through.
- `observability.LevelCritical` (= `slog.LevelError + 4`, rendered `CRITICAL`) for panics, invariant violations, unrecoverable conditions. Call via `slog.LogAttrs(ctx, observability.LevelCritical, ...)`.
- OTel semconv pinned at v1.40.0 — use `semconv.<Key>Key` constants (e.g. `semconv.HTTPRouteKey`) over string literals; keep versions aligned across files.

## Observability
- `internal/otelx` is the leaf OTel helper package. Holds `HTTPClient` (otelhttp-wrapped — use for every outbound HTTP call; never `http.DefaultClient`) and `RecordSpanError(span, err) error` (inline: `return ..., otelx.RecordSpanError(span, err)` — no named returns). Must stay dependency-free; `internal/observability` imports `internal/auth`, so anything auth transitively uses must live in `otelx` not `observability`.
- Per-package tracer: `var tracer = otel.Tracer("github.com/datahearth/streamline/internal/<pkg>")`. Span names `<pkg>.<op>` (e.g. `download.grab`, `rss.process_movie`, `indexer.query`).
- DB auto-instrumented via `otelsql.Open` + `RegisterDBStatsMetrics` in `internal/db/client.go`. Add business-logic spans in service methods + `span.SetAttributes` for domain context.
- Prefer semconv helper funcs (`semconv.UserEmail`, `semconv.UserRoles`, `semconv.UserID`, `semconv.DBSystemNameSQLite`) over raw `attribute.String("user.email", …)`.

## HTTP Routes
- `/health` — pre-auth bare JSON endpoint (NOT in OpenAPI), for k8s probes + load balancers. Registered in `internal/server/server.go`.
- `/api/docs` — Scalar UI shell (`web.Handler.APIDocs`). `/api/v1/openapi.yaml` — embedded spec. REST API mounted via `restapi.Mount`.
- SPA fallback: `s.router.NotFound` → `web.Handler.SPAShell`, which writes the embedded `web/app/index.html` for every non-API, non-static path; Routify owns client-side routing incl. its own 404. Static assets served at `/static/*` from `fs.Sub(web.Assets, "static")` (wired in `web.Mount`).
- `/posters/{kind}/{id}/poster.jpg` — poster proxy via `s.posters.Serve`.
- Auth-middleware `ExcludePaths` is assembled in `internal/server/wire.go` (`/login`, `/register`, `/auth/login`, `/auth/register`, `/auth/oidc/`); matcher in `internal/server/middleware/auth.go`, paths ending `/` match as prefix.

## Auth & Sessions
- Middleware splits transport by path prefix (`internal/server/middleware/auth.go`):
  - `/api/v1/*` accepts only `Authorization: Bearer <jwt>` or `X-API-Key`. 401 JSON on failure. Cookies ignored.
  - Everything else authenticates via the `streamline_session` cookie (httpOnly, SameSite=Lax, Secure when TLS/X-Forwarded-Proto=https). 302 to `/login?next=<escaped>` on failure. Bearer ignored.
- Webui auth routes (`internal/server/web/auth.go`, registered via `web.Mount` → `registerWebAuthRoutes`): `GET /auth/config`, `GET /auth/invite/{token}`, `POST /auth/login`, `POST /auth/register`, `POST /auth/logout`, `GET /auth/oidc/{name}/start`, `GET /auth/oidc/{name}/callback`. There is no `GET /login`/`/register` handler — those fall through to the SPA shell (Svelte renders `login.svelte`/`register.svelte`). Login/register take JSON, set the `streamline_session` cookie, and return `204` on success / `4xx` JSON on failure — no server-rendered HTML.
- Login + register rate-limited per IP (5 attempts / 15 min) via `auth.Limiter`. `/api/v1` has its own budget (20 / 15 min, `server.apiFailureLimiter`) charged **only when authentication fails** — a valid key or session is never metered, so bulk consumers need no exemption. The hidden `auth.api_failure_limit` key overrides the count; `0` turns metering off, which is what `e2e/apptest` sets so the 131-route anonymous sweep is not itself throttled.
- An admin is seeded on a fresh install (`auth.Service.BootstrapSeedAdmin`), so the DB is normally non-empty at request time and there is no first-user-registration special case. One exception: a configured `password_file` that reads back empty seeds *nobody* (see below), leaving an empty DB with `registration_mode: disabled` — unreachable until the secret materialises and the process restarts. `IsFirstUser` has no caller outside bootstrap, and `/auth/config` exposes no `first_user` field.
- `auth.registration_mode` (runtime-toggleable via `config.Update`): `disabled | open | invite`.
- Seed admin via `auth.seed_admin.{email, password, password_file}` — file wins if both provided; trimmed of whitespace. No-op if users already exist. An empty `email` resolves to `admin@streamline.local`, and that account gets a generated password when no password *and no `password_file`* was configured (a configured file reading back empty is an unmaterialised secret, not a request to generate — generating would flip `IsFirstUser` and the real file could never apply). The generated password is printed once to stdout on the single greppable `default admin credentials — email: … password: …` line and is never written to the config nor passed to slog (stderr + OTLP). `BootstrapSeedAdmin` never mutates `auth.seed_admin`; a stored plaintext left by an older release is the operator's to delete.
- Session TTL via `auth.session_ttl` (default `168h`, Go duration string). JWT HMAC signing secret auto-generated on first boot and persisted via atomic YAML write-back (`config.Update`). Ephemeral fallback if config has no backing file (dev/tests).
- Invite lifecycle: admin creates via `POST /api/v1/auth/invites` (raw token shown once). The SPA fetches `GET /auth/invite/{token}` and prefills the email (readonly) when the invite has a bound email. `LookupInviteForPrefill` skips email match; `RegisterWithInvite` enforces it atomically inside a transaction.
- Registration failures are mapped to user-safe messages (`userFacingRegisterError` in `internal/server/web/auth.go`) and returned as JSON; raw service errors are only logged, never sent to the client.

## OIDC
- Multi-provider via `auth.oidc[].{name, issuer, client_id, client_secret}`. `OIDCManager.Init` discovers each at startup; failures silently skip the entry.
- Flow: state + nonce + PKCE (S256) held in short-lived `_oidc_*` cookies scoped to `/auth/oidc/`. Redirect URI = `<STREAMLINE_PUBLIC_URL or http://server.host:port>/auth/oidc/<name>/callback` (see `server.PublicBaseURL`).
- Provider trust is **two independent axes**, one key each, both defaulting closed: `auth.oidc[].email_linking` (which accounts it may adopt) and `auth.oidc[].allow_admin` (whether it may grant `admin`). They were one key; tightening the adoption tier could then *raise* the role ceiling. Keeping them apart is what makes each axis monotone — no move on one can add capability on the other. Never re-merge them.
- Linking policy (`auth.Service.LoginOIDC`):
  1. Existing `(provider, subject)` → log the linked user in, re-syncing the claim-mapped role through `capOIDCRole`.
  2. `email_verified=false` → `ErrOIDCEmailUnverified` (reject).
  3. Existing user by email → adoption is **opt-in per provider** via `email_linking` (`disabled` default / `non_admin` / `all`); when permitted, link identity and promote `auth_method` `local` → `both`, otherwise `ErrOIDCLinkNotAllowed`. A matching email is not proof of account ownership — an IdP that lets users self-assert a verified address would otherwise mint a login for any local user, seeded admin included. The adoption itself never writes a role; claim-mapped roles resume syncing on the next login, via step 1.
  4. New user → apply `registration_mode`. `invite` mode consumes the earliest unused+unexpired invite bound to the email; no match → `ErrOIDCNoInvite`. `disabled` → `ErrOIDCRegDisabled`. `open` → falls back to `auth.oidc_default_role`.
- `email_linking` governs **adoption only** (`emailLinkingAllowed`): `disabled` adopts nothing, `non_admin` adopts non-admin accounts (the migration setting — turn on, have users sign in once, turn off), `all` adopts any account including admin. It never affects a role.
  - It gates the adoption, not its result. Step 1 matches on the identity an adoption linked and never reads the key, so a provider set back to `disabled` still signs in as every account it adopted while it was open — including local-password accounts it did not create. That is what makes the README's migration procedure work; there is no unlink, so deleting the user is the only way to drop the identity.
- `allow_admin` is the **admin ceiling**, enforced by the single choke point `oidcrole.Cap` in `internal/auth/oidcrole/`. False (default) means no login through the provider yields admin, with **no exception**: not a claim mapped to `admin`, not an `oidc_default_role` of `admin`, not an invite-carried `admin`. Among claim candidates admin is *dropped* (so an admin-only claim set leaves the existing role untouched, and an admin+member set lands on member); a configured fallback of admin is *clamped* to member, because provisioning has to land somewhere.
  - `oidcrole.Cap` reads no auth_method, no `email_linking`, and no claim of the request being served. Each of those moved with an attacker or an operator, and each of rounds 1-3 died to a different ordering that exploited exactly that.
  - **The ceiling is a compile error to skip, not a lint.** `oidcrole.Role` wraps an unexported field and the package exports no setter and no constructor other than `Cap`, so no code in package `auth` — or anywhere else — can produce a non-zero one. Composite literal, field assignment, a params type invented next year: all fail to build. The zero value is the safe one ("no role decided", every write path skips it), and `EntRole()`/`EntRolePtr()` are the only exits into `db.CreateUserParams`/`UpdateUserParams`. `Cap` also takes the provider's **name**, not its config — a `config.OIDCConfig{AllowAdmin: true}` written at a call site would otherwise be an escalation spelled entirely in blessed calls; named, the ceiling can only be what the operator configured, and an unknown name (or an unloaded config) confers nothing.
  - Four rounds of AST guard preceded that and four rounds were bypassed, each by a shape the previous round had not enumerated — most recently `var esc oidcRole; esc.role = string(entuser.RoleAdmin)`, which needed no new API at all because the field was in the same package. **Do not move the type back into `auth` and do not export the field.**
  - What is left for `Describe("OIDC role writes")` in `oidc_test.go` is a backstop over the surface the compiler does not reach: a raw `entuser.Role` written by hand. It **type-checks** every non-test file of package `auth` — `go/packages` resolves the imports, `go/types` checks the package — and reports each place that names a role field: a `Role:` key in *any* composite literal (no type filter: round 5's suffix-match on `*UserParams` is exactly how `UserPatch{Role: &esc}` slipped past), an assignment to a `.Role` field, an ent `SetRole`/`SetNillableRole`. It fails on any whose value the compiler does not vouch for and whose function is not allow-listed. Vouched means exactly two things: the receiver of `EntRole()`/`EntRolePtr()` **is the type** `oidcrole.Role`, or the value is `string(<x>.Role)` off an `*ent.User` row. **A name is not a type, and round 6 is the proof**: its walker trusted anything *spelled* `<x>.EntRolePtr()`, so a five-line `shadowRole` declared in package `auth` and dropped into `syncOIDCProfile` — not allow-listed, on an OIDC login's path — landed an admin row, an admin JWT and an admin session with all 13 specs green.
  - A second spec builds package `auth`'s call graph, also type-resolved, and fails if `LoginOIDC` can reach any `roleWriters` entry — which is what makes the allow-list's "no OIDC login reaches it" a checked fact rather than a sentence. Every *reference* to a package function is an edge, not only a call in call position: round 6's name-keyed graph saw `up := s.UpdateUser; up(…)` as a call to `up` and reported `UpdateUser` unreached. Types also drop `s.db.UpdateUser` and `tx.CreateUser` on their own, where the name-keyed version needed a hand-maintained list of receiver names to tell them from the service's own methods.
  - Three fixed-point specs fail if the walker stops seeing the known writes, the known calls, or the one write it vouches for by shape, so none can pass by going blind. Injections are **spliced into the real source before it is type-checked**, so an escalation is judged in the function it would really have to live in, against the package's real types, by exactly the code that judges the tree.
  - `roleWriters` names the seven functions that decide a role over an authenticated non-OIDC path (bootstrap seeding, local/invite registration, invite creation, admin CRUD) and is held to its claim by the reachability spec; `roleCarriers` names one (`ListUsers`' filter) and is the one allow-list nothing checks. Adding a name to `roleCarriers` is how this gets reopened — reach for `roleWriters` first and let the reachability spec argue. `issueToken` used to be the second entry: exempting the whole function left `Claims{Role: "admin"}` — a JWT forgery an OIDC login reaches — invisible, so it is held to the row-mirror shape instead and that literal is now caught. The walker skips subdirectories, so `oidcrole` itself is unscanned by design: it is the one package allowed to decide a role, and its own specs cover the ceiling.
  - What none of it covers, stated so the next round does not have to rediscover it:
    - reflection or `unsafe` — `unsafe.Pointer` rewrites the very field the package boundary protects, and the forged value then leaves through the real exit on the real type;
    - a role decided in another package and returned into `auth`;
    - **a write inside a `roleCarriers` function** — `ListUsers` is exempt by name, and nothing checks that a login cannot reach it;
    - an `oidcrole.Cap` call naming a *different* provider than the one being authenticated, which is capped by that provider's ceiling rather than by none;
    - the residue of the JWT mirror: `string(<x>.Role)` proves the value came off *a* user row, not off the row this login authenticated, so `claims.Role = string(other.Role)` inside `issueToken` still passes. Closing it means the session's role stops being a free-standing string — `Claims.Role` would have to become a type only a constructor taking the authenticated `*ent.User` can fill, which is `internal/server`'s middleware and RBAC as much as `auth`'s — and it would still not cover `s.issueToken(ctx, someOtherRow, meta)`, which mints a whole session for the wrong user without naming a role at all.

    The `leave uncovered, on purpose` table injects each of these and watches nothing happen; it fails if one starts being caught, which is a docs fix. Do not restate this list without injecting the shape and watching it fail.
- Provider names are unique per config — `Config.Validate` rejects a duplicate `auth.oidc[].name` (the other name-keyed lists get this from a `unique=Name` tag). Two entries sharing a name split trust from authentication: `oidcManager.Init` keys its map by name so the *last* entry's verifier and client_id authenticate the callback, while `findOIDCProvider` returns the *first* entry's `allow_admin`/`email_linking` — a token from the weaker issuer capped by the stronger entry's ceiling.
- `role_claim` accepts a dotted path into nested claims (`realm_access.roles` for Keycloak); a claim literally named with a dot wins over the path reading. `oidcClaimRoles` returns the mapped candidates *uncapped* — ranking and the ceiling are `oidcrole.Cap`'s, so there is one place to audit. The privilege ordering lives in `oidcrole` too (`AtLeast`, which `auth.RoleAtLeast` delegates to) so RBAC and the ceiling rank roles off one table.
- Both trust keys are config-file only: the REST provider CRUD (`/api/v1/config/oidc`) neither reads nor writes them, so API-created providers get `disabled` + no admin, and `UpdateOIDCProvider`'s in-place patch preserves whatever the file set. `Config.Validate` spells the `email_linking` zero value out as `disabled` so the write-back never emits `email_linking: ""`, which `api/config.schema.json` rejects; `allow_admin` is a bool, so the write-back always emits it and the schema has to know the key.
- OIDC callback failures `302`-redirect to `/login?error=<code>` (`internal/server/web/auth.go`); the SPA's client-side `oidcErrorMessage` (`web/app/routes/login.svelte`) maps codes to human-facing text.

## Config-backed resources
- Media servers, download clients, indexers, and quality profiles live in the YAML config (NOT ent/SQLite), name-keyed and hot-editable — mirroring `auth.oidc[]`. Top-level lists: `media_server.servers[]`, `download_clients[]`, `indexers[]`, `quality_profiles[]` + `quality_default_profile`. Global grab knobs moved to `library.no_match_cooldown` / `library.max_grab_failures` (the old `library.default_quality` block is gone).
- Helpers in `internal/config/resources.go` (`Find*`/`Enabled*`/`PickDownloadClient`/`ResolveQualityProfile`) + per-family CRUD in `internal/config/mutate_*.go` (`Add*`/`Update*`/`Delete*`, secret-preserve on blank). REST handlers (`internal/server/restapi/handler_{download_clients,indexers,media_servers,quality_profiles}.go`) call config directly — no service CRUD; the indexer/download/mediaserver services keep only behavioral methods (`Test`/`TestByName`/`Grab`/`Feed`/`DiscoverSections`/Plex PIN). Read views hide secrets behind `api_key_set`/`password_set` booleans.
- Endpoints are name-keyed (`/api/v1/<resource>/{name}`); media-server update is `PATCH`, the others `PUT`. Movies reference a profile by `quality_profile` string (empty resolves to the default). DownloadRecord/Movie carry `download_client_name`/`indexer_name`/`quality_profile` string columns instead of FK edges.

## Testing
- Framework: Ginkgo (Describe/Context/It/By) + Gomega assertions
- Mocks: Mockery (`go tool mockery`) — config in `.mockery.yaml`, generated to `internal/<pkg>/mocks/`
- Run tests: `task test:unit` / `task test:integration` / `task test:e2e` / `task test` / `task test:coverage` — all `go tool ginkgo run -r` with label filters (e2e capped at `--timeout=1m15s`); forward extra args via `CLI_ARGS`
- `task test:e2e:containers` — container-backed e2e (Docker + `STREAMLINE_E2E_CONTAINERS=1`, 5m timeout); hermetic `test:e2e` excludes `containers`-labeled specs.
- Run single suite: `task test:unit -- ./internal/metadata/...`
- Each Ginkgo suite has a dedicated `<pkg>_suite_test.go` with `TestX` + `RunSpecs` + `BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })` — `testutil.InstallSlog()` routes `slog.Default` to GinkgoWriter for the suite's lifetime.
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

## Code Generation
- API: `go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml` → `internal/server/restapi/gen.go` (package `restapi`) — regenerate after spec changes
- ORM: `go generate ./ent` — regenerate after schema changes
- All codegen: `task generate` (runs `go tool oapi-codegen`, `go generate ./ent`, `go tool mockery`). Versioned migrations: `task migrate:diff -- NAME` diffs the ent schema into a new migration.
- Prefer the narrowest integer type on ent schema fields (`field.Uint8` for bounded counters like grab_failures, `field.Uint16` for ports) — sqlite storage is identical but Go structs stay memory-efficient.
- After ent regen the LSP may report "undefined" for new fields/methods briefly; `task build:go` is the source of truth.

## Build
- Task runner: [Taskfile.yaml](Taskfile.yaml) — sole build orchestrator (no npm scripts).
- **Always** invoke operations via `task <target>`. Raw `go build`/`go test`/`ginkgo`/`golangci-lint`/`pnpm exec` bypass the Taskfile and drift from CI.
- Go tooling runs via the Go `tool` directive: `go tool {ginkgo,golangci-lint,oapi-codegen,mockery,air}` — not system-installed; the flake devshell only ships `go`, `pnpm`, `go-task`, `biome`, etc.
- Full build: `task` (= `build:app` → `build:go`, which depends on `build:js` + `build:css` because assets are `//go:embed`-ed) → `go build -o streamline ./cmd`.
- Frontend: `task build:js` (Scalar docs bundle + `routify build` + esbuild SPA) and `task build:css` (TailwindCSS v4).
- Dev server: `task dev` — live reload via `go tool air` (`.air.toml`; rebuilds with `task build:app`, runs `streamline --config ./tmp/config.yaml`).
- Lint: `task lint` (`lint:go` golangci-lint + `lint:frontend` biome). Format: `task fmt` (`golangci-lint fmt` + `biome format --write`) — Biome, not prettier.
- Clean: `task clean`.

## Release
- App and chart version independently. App release: `task release:changelog VERSION=vX.Y.Z` → commit the CHANGELOG → `task release:tag VERSION=vX.Y.Z` (tags `main`, push fans out to `.github/workflows/{release,image}.yaml`). Chart release: bump `version` in `deploy/helm/streamline/Chart.yaml` → commit → `task chart:tag` (tags `chart-vX.Y.Z`, triggers `.github/workflows/chart.yaml`).
- Artifacts come from goreleaser (`.goreleaser.yaml`): CGO-free binaries for linux/darwin/windows × amd64/arm64, archives carry `config.example.yaml`, `checksums.txt` is cosign-signed (bundle format), one SPDX SBOM per archive. Images are cosign-signed keyless + SBOM-attested + grype-scanned in `image.yaml`.
- `release:tag` / `chart:tag` / `release:image` / `release:helm` publish for real and have no dry-run. Never run one to "test" it — only exercise a precondition's failure path.

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
- `docs/ROADMAP.md` — public, user-facing feature status (Shipped / In progress / Planned). Not an internal phase list; update it when a feature ships or starts.
- `CHANGELOG.md` — generated from conventional commits by git-cliff (`task release:changelog VERSION=vX.Y.Z`); don't hand-write entries.
- `deploy/` — Dockerfile + Helm charts + `compose.yaml` (local test stack: gluetun VPN + qBittorrent + Prowlarr + Plex, builds from source — *not* a user deployment template)
- `deploy/helm/streamline/` — streamline chart (installs to `streamline` ns). Optional subchart `charts/observability/` installs upstream alloy/VM/VL/VT/grafana into `observability` ns via `namespaceOverride`.
- `deploy/helm/streamline/kubeconfig.yaml` — kind cluster kubeconfig (auto-exported by `task helm:kind:up`; flake devshell sets `KUBECONFIG` to this path).

## Helm Gotchas
- VM/VL/VT charts (v0.35/0.12/0.0.7): selector uses `app: server` but template labels drop it. Workaround: set `server.podLabels.app: server` in each subchart values.
- Cross-namespace k8s DNS requires FQDN: `<svc>.<ns>.svc.cluster.local`. Streamline→alloy uses `alloy.observability.svc.cluster.local:4318`.
- OTel SDK defaults to HTTPS. Set `OTEL_EXPORTER_OTLP_INSECURE=true` env when endpoint is HTTP (alloy is HTTP).
