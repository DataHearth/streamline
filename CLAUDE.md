# Streamline

Unified media management platform replacing the *arr stack (Radarr, Sonarr, Lidarr, Readarr) and Overseerr. Single self-hosted binary with a slick web UI, REST API for mobile developers, multi-user support with SSO, built-in request system, and automatic media organization. Supports Torznab indexers, torrent download clients (qBittorrent, Transmission, Deluge), and media server notifications (Plex, Jellyfin, Emby).

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
- Auth-middleware `ExcludePaths` is assembled in `internal/server/wire.go` (`/login`, `/register`, `/auth/login`, `/auth/register`, `/auth/oidc/`); matcher in `internal/auth/middleware.go`, paths ending `/` match as prefix.

## Auth & Sessions
- Middleware splits transport by path prefix (`internal/auth/middleware.go`):
  - `/api/v1/*` accepts only `Authorization: Bearer <jwt>` or `X-API-Key`. 401 JSON on failure. Cookies ignored.
  - Everything else authenticates via the `streamline_session` cookie (httpOnly, SameSite=Lax, Secure when TLS/X-Forwarded-Proto=https). 302 to `/login?next=<escaped>` on failure. Bearer ignored.
- Webui auth routes (`internal/server/web/auth.go`, registered via `web.Mount` → `registerWebAuthRoutes`): `GET /auth/config`, `GET /auth/invite/{token}`, `POST /auth/login`, `POST /auth/register`, `POST /auth/logout`, `GET /auth/oidc/{name}/start`, `GET /auth/oidc/{name}/callback`. There is no `GET /login`/`/register` handler — those fall through to the SPA shell (Svelte renders `login.svelte`/`register.svelte`). Login/register take JSON, set the `streamline_session` cookie, and return `204` on success / `4xx` JSON on failure — no server-rendered HTML.
- Login + register rate-limited per IP (5 attempts / 15 min) via `auth.Limiter`.
- An admin is always seeded on a fresh install (`auth.Service.BootstrapSeedAdmin`), so the DB is never empty at request time — there is no first-user-registration special case. `IsFirstUser`/`/auth/config`'s `first_user` flag therefore always report false in practice.
- `auth.registration_mode` (runtime-toggleable via `config.Update`): `disabled | open | invite`.
- Seed admin via `auth.seed_admin.{email, password, password_file}` — file wins if both provided; trimmed of whitespace. No-op if users already exist. When `email` is empty, a default admin (`admin@streamline.local`) is created with a generated password (when none supplied), persisted back into `auth.seed_admin` so the operator can retrieve it.
- Session TTL via `auth.session_ttl` (default `168h`, Go duration string). JWT HMAC signing secret auto-generated on first boot and persisted via atomic YAML write-back (`config.Update`). Ephemeral fallback if config has no backing file (dev/tests).
- Invite lifecycle: admin creates via `POST /api/v1/auth/invites` (raw token shown once). The SPA fetches `GET /auth/invite/{token}` and prefills the email (readonly) when the invite has a bound email. `LookupInviteForPrefill` skips email match; `RegisterWithInvite` enforces it atomically inside a transaction.
- Registration failures are mapped to user-safe messages (`userFacingRegisterError` in `internal/server/web/auth.go`) and returned as JSON; raw service errors are only logged, never sent to the client.

## OIDC
- Multi-provider via `auth.oidc[].{name, issuer, client_id, client_secret}`. `OIDCManager.Init` discovers each at startup; failures silently skip the entry.
- Flow: state + nonce + PKCE (S256) held in short-lived `_oidc_*` cookies scoped to `/auth/oidc/`. Redirect URI = `<STREAMLINE_PUBLIC_URL or http://server.host:port>/auth/oidc/<name>/callback` (see `server.PublicBaseURL`).
- Linking policy (`auth.Service.LoginOIDC`):
  1. Existing `(provider, subject)` → log the linked user in.
  2. `email_verified=false` → `ErrOIDCEmailUnverified` (reject).
  3. Existing user by lowercased email → link identity, promote `auth_method` `local` → `both`.
  4. New user → apply `registration_mode`. `invite` mode consumes the earliest unused+unexpired invite bound to the email; no match → `ErrOIDCNoInvite`. `disabled` → `ErrOIDCRegDisabled`. `open` → user created with `auth.oidc_default_role`.
- OIDC callback failures `302`-redirect to `/login?error=<code>` (`internal/server/web/auth.go`); the SPA's client-side `oidcErrorMessage` (`web/app/routes/login.svelte`) maps codes to human-facing text.

## Config-backed resources
- Media servers, download clients, indexers, and quality profiles live in the YAML config (NOT ent/SQLite), name-keyed and hot-editable — mirroring `auth.oidc[]`. Top-level lists: `media_server.servers[]`, `download_clients[]`, `indexers[]`, `quality_profiles[]` + `quality_default_profile`. Global grab knobs moved to `library.no_match_cooldown` / `library.max_grab_failures` (the old `library.default_quality` block is gone).
- Helpers in `internal/config/resources.go` (`Find*`/`Enabled*`/`PickDownloadClient`/`ResolveQualityProfile`) + per-family CRUD in `internal/config/mutate_*.go` (`Add*`/`Update*`/`Delete*`, secret-preserve on blank). REST handlers (`internal/server/restapi/handler_{download_clients,indexers,media_servers,quality_profiles}.go`) call config directly — no service CRUD; the indexer/download/mediaserver services keep only behavioral methods (`Test`/`TestByName`/`Grab`/`Feed`/`DiscoverSections`/Plex PIN). Read views hide secrets behind `api_key_set`/`password_set` booleans.
- Endpoints are name-keyed (`/api/v1/<resource>/{name}`); media-server update is `PATCH`, the others `PUT`. Movies reference a profile by `quality_profile` string (empty resolves to the default). DownloadRecord/Movie carry `download_client_name`/`indexer_name`/`quality_profile` string columns instead of FK edges.

## Testing
- Framework: Ginkgo (Describe/Context/It/By) + Gomega assertions
- Mocks: Mockery (`go tool mockery`) — config in `.mockery.yaml`, generated to `internal/<pkg>/mocks/`
- Run tests: `task test:unit` / `task test:integration` / `task test:e2e` / `task test` / `task test:coverage` — all `go tool ginkgo run -r` with label filters (e2e capped at `--timeout=1m15s`); forward extra args via `CLI_ARGS`
- Run single suite: `task test:unit -- ./internal/metadata/...`
- Each Ginkgo suite has a dedicated `<pkg>_suite_test.go` with `TestX` + `RunSpecs` + `BeforeSuite(func() { DeferCleanup(testutil.InstallSlog()) })` — `testutil.InstallSlog()` routes `slog.Default` to GinkgoWriter for the suite's lifetime.
- Mocks emit to `internal/<pkg>/mocks/mock_<Name>.go` — type `Mock<Name>`, constructor `NewMock<Name>(GinkgoT())`
- Regenerate mocks after interface changes: `go tool mockery`
- Span-instrumented funcs wrap ctx via `tracer.Start(...)`, breaking exact-ctx mock matchers. Use `mock.Anything` for the ctx param in `.EXPECT().Fn(mock.Anything, ...)` calls.

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

## Version Control
- Repository uses jj (Jujutsu) with a git backend. Commit a task with `jj new -m "msg"` (creates empty child, auto-snapshots subsequent work). Seal a task by starting the next with `jj new -m "..."`.
- Don't re-run `jj describe` on a commit you're already working in — message is set once per task.
- If working copy holds unrelated leftover edits (e.g. settings.json), describe them into their own commit *before* `jj new` for feature work.
- Abandon empty working-copy commits after no-artifact steps (smoke tests, manual verification): `jj abandon @`.
- Commit messages: Conventional Commits TYPE ONLY — no `(scope)`. Bundle related changes; avoid single-file revisions.

## Project Structure
- `api/openapi.yaml` — OpenAPI spec (source of truth for REST API)
- `internal/` — all application code
- `ent/schema/` — ent ORM schemas
- `web/app/` — Svelte 5 SPA (`routes/`, `components/`, `lib/`); `web/static/` — CSS/JS/fonts/images; `web/embed.go` `//go:embed`s the built assets + SPA shell
- `docs/plans/` — design docs and plans (gitignored, local only)
- `deploy/` — Dockerfile + Helm charts
- `deploy/helm/streamline/` — streamline chart (installs to `streamline` ns). Optional subchart `charts/observability/` installs upstream alloy/VM/VL/VT/grafana into `observability` ns via `namespaceOverride`.
- `deploy/helm/streamline/kubeconfig.yaml` — kind cluster kubeconfig (auto-exported by `task helm:kind:up`; flake devshell sets `KUBECONFIG` to this path).

## Helm Gotchas
- VM/VL/VT charts (v0.35/0.12/0.0.7): selector uses `app: server` but template labels drop it. Workaround: set `server.podLabels.app: server` in each subchart values.
- Cross-namespace k8s DNS requires FQDN: `<svc>.<ns>.svc.cluster.local`. Streamline→alloy uses `alloy.observability.svc.cluster.local:4318`.
- OTel SDK defaults to HTTPS. Set `OTEL_EXPORTER_OTLP_INSECURE=true` env when endpoint is HTTP (alloy is HTTP).
