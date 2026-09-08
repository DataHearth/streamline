# Agent docs

Subsystem knowledge that is too large to keep in `CLAUDE.md` but too expensive to
rediscover. Each file is the **detail behind a pointer stub** in `CLAUDE.md` — the stub
carries the shape and the invariants you must not break without reading further; the file
carries the rules and the reasons.

These are not tutorials and not user documentation (that is `docs/wiki/`). They are written
for whoever — human or agent — is about to change the code, and they exist because almost
every rule in them was paid for once already: a real release, a real library, a real
homelab failure. **Read the relevant file before editing its area; do not simplify a rule
without reading why it is shaped that way.**

| File | Read before touching |
|---|---|
| [`auth-and-oidc.md`](auth-and-oidc.md) | `internal/auth/`, `internal/role/`, auth middleware, `internal/server/web/auth.go`, anything writing a user's role |
| [`bulk-import-matching.md`](bulk-import-matching.md) | `internal/library/` parsing & naming templates, `internal/library/bulkimport/`, rename services, TMDB/TVDB classifiers |
| [`config-surface.md`](config-surface.md) | `internal/config/`, `/config/*` handlers, `api/config.schema.json`, config-backed resource CRUD |
| [`downloads-and-imports.md`](downloads-and-imports.md) | `internal/bittorrent/`, `internal/importer/`, `internal/download/adopt.go`, `internal/mediaserver/` |
| [`frontend.md`](frontend.md) | `web/app/**`, `web/static/**`, `routify.config.js`, `web/embed.go` |
| [`media-lifecycle.md`](media-lifecycle.md) | episode/movie status transitions, `internal/events/`, `internal/db` list queries, the `media-probe` job, re-identify |
| [`quality-scoring.md`](quality-scoring.md) | `internal/quality/**`, `qualityctx`, RSS feed/missing-search scanners, profile & custom-format handlers |
| [`selective-file-download.md`](selective-file-download.md) | `internal/download/manager.go`, `selection_pass.go`, `internal/bittorrent/` piece priorities, any client's `SetWantedFiles`/`ListFiles` |
| [`transcoding.md`](transcoding.md) | `internal/transcoding/`, `internal/ffmpeg/`, `quality_profiles[].transcode`, `/transcoding/*` handlers |

## Upkeep

- A change to one of these subsystems updates its file **in the same change**, exactly as
  it would have updated `CLAUDE.md` before the split.
- Keep the `CLAUDE.md` stub true. It states the shape and the load-bearing invariants; if a
  change moves those, the stub moves with it.
- New page = new row above **and** a stub in `CLAUDE.md`. A file nothing points at is a
  file nobody reads.
- Same bar as `CLAUDE.md`: record the ruling and why the obvious alternative is wrong.
  Rediscovery is the cost being avoided, so keep the evidence for anything that reads as
  arbitrary without it.
