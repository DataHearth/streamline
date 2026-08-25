# REST API

Streamline's API is the same one its own web UI uses — there's no privileged internal surface. Anything the SPA can do, you can do.

- [Interactive docs](#interactive-docs)
- [Authentication](#authentication)
- [Conventions](#conventions)
- [Endpoint map](#endpoint-map)
- [Media probe](#media-probe)
- [Worked examples](#worked-examples)
- [Generating a client](#generating-a-client)

---

## Interactive docs

| URL | What |
| --- | --- |
| `/api/docs` | [Scalar](https://scalar.com) UI — browse and try endpoints |
| `/api/v1/openapi.yaml` | The raw OpenAPI 3.0.4 spec |

The spec is the source of truth: the Go server types are generated from it with `oapi-codegen`, so it can't drift from the implementation.

Base URL for everything below: `/api/v1`.

---

## Authentication

Two credentials work on `/api/v1/*`:

```bash
# API key — for scripts and long-lived integrations
curl -H "X-API-Key: $KEY" https://streamline.example.com/api/v1/movies

# Bearer JWT — for a session obtained by logging in
curl -H "Authorization: Bearer $JWT" https://streamline.example.com/api/v1/movies
```

Cookies are ignored on `/api/v1` **except** for same-origin browser requests carrying `Sec-Fetch-Site: same-origin` — that's how the SPA authenticates without holding a second credential. Anything outside a browser needs a key or a token.

Failures return `401` with a JSON body. No redirects on the API surface.

The two credentials are equal on media and settings endpoints, but API keys are **read-only on the identity surface**: any non-GET request under `/auth/me`, `/auth/password`, `/auth/invites`, `/auth/jwt`, or `/users` returns `403` with a key — those actions need a session (Bearer JWT or the SPA cookie). That's why the key-creation example below authenticates with a JWT.

### Getting an API key

**Account settings → API keys**, or:

```bash
curl -X POST -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"name":"my-script"}' \
  https://streamline.example.com/api/v1/auth/me/api-keys
```

The raw key is returned **once**. A key inherits its owner's permissions — an admin's key is an admin key, so create read-only integrations under a `member` account.

### Getting a JWT

```bash
curl -c cookies.txt -X POST -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"..."}' \
  https://streamline.example.com/auth/login
```

Note the path: `/auth/login`, **not** `/api/v1/auth/login`. It returns `204` and sets `streamline_session`; the cookie's value is the JWT, so you can lift it out and use it as a Bearer token.

For anything non-interactive, use an API key instead.

---

## Conventions

**Pagination.** Collection endpoints take `?page=` (default 1) and `?limit=` (default 20) and return an envelope:

```json
{ "items": [ ... ], "total": 137, "page": 1, "limit": 20 }
```

`limit` is **capped at 100** on every collection endpoint (200 for activity). A larger value is clamped silently — you get 100 items and a `200`, with nothing in the response saying the limit was reduced. Paginate against `total`, not against "fewer items came back than I asked for", or you will read the first page and stop.

```bash
# wrong: reads 100 of 621 and thinks it is done
curl ".../movies?page=1&limit=500"

# right: walk pages until you have `total`
curl ".../movies?page=1&limit=100"   # -> total: 621
curl ".../movies?page=2&limit=100"
# ...
```

Activity feeds use cursor pagination instead (`?cursor=`, `?limit=`), since they're append-only and time-ordered.

**Name-keyed resources.** Config-backed resources are addressed by name, not numeric ID:

```
/indexers/{name}          /download-clients/{name}
/media-servers/{name}     /quality-profiles/{name}
/schedules/{name}
```

Everything database-backed (movies, series, requests, users, imports) uses numeric IDs.

**Update verbs are not uniform.** Media servers use `PATCH`; indexers, download clients and quality profiles use `PUT`. This is a genuine inconsistency in the API, not a documentation error — check the spec if in doubt.

**Secrets are never returned.** Read views expose booleans — `api_key_set`, `password_set`, `client_secret_set` — instead of values. On update, sending a blank secret **preserves** the existing one rather than clearing it, so you can round-trip a config object without leaking or destroying credentials.

**Sort direction follows the key.** `GET /series?sort=` defaults to the direction the key implies — `title` ascending, `year`, `rating`, `episodes` and `recent` descending, so "most episodes" means most. Pass `?order=asc|desc` to override. `sort=episodes` ranks by the same episode count the list response reports in `total_episodes` (monitored, or already on disk), not by every row the provider lists.

**Errors** are `{"message": "..."}` with a conventional status: `400` bad request, `401` unauthenticated, `403` forbidden (usually not an admin), `404`, `409` conflict (already exists), `422` unprocessable, `500`.

---

## Endpoint map

111 paths. Grouped, with admin-only marked 🔒.

### Movies

| Method | Path |
| --- | --- |
| `GET` `POST` | `/movies` |
| `GET` | `/movies/counts` |
| `GET` `PATCH` `DELETE` | `/movies/{id}` |
| `POST` | `/movies/{id}/search` · `/search-now` · `/grab` · `/refresh-metadata` · `/rename` · `/play-on` |
| `POST` | `/movies/{id}/reidentify` 🔒 — point the entry at a different TMDB title |
| `GET` | `/movies/{id}/recommendations` |
| `DELETE` | `/movies/{id}/files/{fileId}` |
| `GET` | `/search/movie` · `/search/movie/{tmdb_id}` — TMDB lookup |

### Series

| Method | Path |
| --- | --- |
| `GET` `POST` | `/series` — list takes `?status=`, `?type=`, `?query=`, `?sort=`, `?order=` |
| `GET` | `/series/counts` · `/series/lookup` · `/series/lookup/{tvdb_id}` |
| `POST` | `/series/specials/apply` |
| `GET` `PATCH` `DELETE` | `/series/{id}` |
| `GET` | `/series/{id}/browse` |
| `POST` | `/series/{id}/search` · `/grab` · `/refresh-metadata` · `/rename` · `/play-on` |
| `POST` | `/series/{id}/reidentify` 🔒 — point the entry at a different TVDB show |
| `PATCH` | `/series/{id}/seasons/{number}` |
| `POST` | `/series/{id}/seasons/{number}/search` · `/grab` |
| `GET` `PATCH` | `/series/{id}/episodes/{episodeId}` |
| `POST` | `/series/{id}/episodes/{episodeId}/search` · `/grab` |
| `DELETE` | `/series/{id}/episodes/{episodeId}/file` |

### Activity

| Method | Path |
| --- | --- |
| `GET` | `/activity` — event feed (movies, episodes and series; filter with `?movie_id=` or `?series_id=`) |
| `GET` | `/activity/queue` · `/activity/history` |
| `DELETE` | `/activity/queue/{id}` · `/activity/history/{id}` |
| `POST` | `/activity/queue/{id}/pause` · `/resume` · `/activity/history/clear-completed` |
| `GET` | `/activity/pending` 🔒 |
| `POST` | `/activity/pending/{id}/import` · `/replace` · `/ignore` 🔒 |
| `POST` | `/downloads/{id}/resolve` 🔒 — release a held download |

### Requests

| Method | Path | Who |
| --- | --- | --- |
| `GET` `POST` | `/requests` | Any (scoped for `request_only`) |
| `GET` | `/requests/counts` · `/requests/{id}/metadata` | Any |
| `POST` | `/requests/{id}/approve` | admin, member |
| `POST` | `/requests/{id}/deny` · `/reopen` | admin |

### Config-backed resources 🔒

| Method | Path |
| --- | --- |
| `GET` `POST` | `/indexers` · `/download-clients` · `/media-servers` · `/quality-profiles` |
| `GET` `DELETE` | `/{resource}/{name}` |
| `PUT` | `/indexers/{name}` · `/download-clients/{name}` · `/quality-profiles/{name}` |
| `PATCH` | `/media-servers/{name}` |
| `POST` | `/{resource}/test` — test an unsaved config |
| `POST` | `/{resource}/{name}/test` — test a saved one |
| `GET` | `/media-servers/discover` — list libraries/sections |

### Torrents 🔒 (built-in client)

| Method | Path |
| --- | --- |
| `GET` | `/torrents` · `/torrents/{hash}` |
| `POST` | `/torrents/{hash}/pause` · `/resume` |
| `PATCH` | `/torrents/{hash}/files/{index}` — toggle a file |

### Library 🔒

| Method | Path |
| --- | --- |
| `GET` `POST` | `/library/imports` |
| `GET` `DELETE` | `/library/imports/{id}` |
| `POST` | `/library/imports/{id}/cancel` · `/commit` |
| `GET` | `/library/imports/{id}/files` · `/shows` |
| `PATCH` | `/library/imports/{id}/files/{fileId}` · `/shows/{showId}` |
| `POST` | `/library/imports/{id}/decisions` — bulk decision |
| `GET` `POST` | `/library/path-migration` |
| `GET` | `/library/path-migration/roots` |
| `POST` | `/library/path-migration/preview` |

### Auth and users

| Method | Path | Who |
| --- | --- | --- |
| `GET` `PATCH` | `/auth/me` | Any |
| `PUT` | `/auth/password` | Any |
| `GET` `POST` | `/auth/me/api-keys` · `/auth/me/sessions` | Any |
| `DELETE` | `/auth/me/api-keys/{id}` · `/auth/me/sessions/{id}` | Any |
| `POST` | `/auth/jwt/rotate` | 🔒 |
| `GET` `POST` | `/auth/invites` | 🔒 |
| `DELETE` | `/auth/invites/{id}` | 🔒 |
| `GET` `POST` | `/users` | 🔒 |
| `GET` `PATCH` `DELETE` | `/users/{uid}` | 🔒 |
| `POST` | `/users/{uid}/password-reset` · `/unlock` | 🔒 |
| `DELETE` | `/users/{uid}/api-keys/{kid}` · `/sessions/{sid}` | 🔒 |

### Config, schedules, system 🔒

| Method | Path |
| --- | --- |
| `GET` `PATCH` | `/config/auth` · `/config/library` · `/config/ffmpeg` |
| `GET` `POST` | `/config/oidc` |
| `GET` `PATCH` `DELETE` | `/config/oidc/{name}` |
| `GET` | `/schedules` · `/schedules/{name}` |
| `PATCH` | `/schedules/{name}` |
| `POST` | `/schedules/{name}/pause` · `/resume` · `/run` |
| `GET` | `/system/info` |

### Calendar

| Method | Path |
| --- | --- |
| `GET` | `/calendar/upcoming?from=&to=` — movie digital releases and episode air dates |

### Outside `/api/v1`

| Path | Notes |
| --- | --- |
| `GET /health` | Unauthenticated probe. Bare JSON, deliberately not in the spec |
| `POST /auth/login` · `/auth/register` · `/auth/logout` | Cookie-based, `204` on success |
| `GET /auth/config` · `/auth/invite/{token}` | Pre-auth SPA bootstrap |
| `GET /auth/oidc/{name}/start` · `/callback` | The OIDC flow |
| `GET /posters/{kind}/{id}/poster.jpg` | Poster proxy |

---

## Media probe

Technical details read from your files with `ffprobe` — resolution, codecs, duration, bitrate. See [Configuration Reference](Configuration-Reference#ffmpeg) for the config side.

**`media_info`** is a nullable object on `MediaFile` (movies) and `Episode` responses:

```json
{
  "container": "matroska",
  "video_codec": "hevc",
  "width": 3840,
  "height": 1608,
  "duration_seconds": 8130,
  "audio_codec": "eac3",
  "audio_channels": 6,
  "bitrate": 24500000,
  "probed_at": "2026-08-18T12:00:00Z"
}
```

It's absent until the file has been probed, and absent again if the probe failed — check for the key, don't assume it's always there. There's no per-stream breakdown (no `audio_tracks`/`subtitles`) in this release.

It describes the file's **main** video track and its first audio track. Embedded cover art and poster thumbnails are video streams as far as ffprobe is concerned, so the largest one wins — a 4K film carrying a 300×300 poster reports `3840`, not `300`.

**`ffmpeg_warn`** on `GET /system/info` is `true` when `ffmpeg.enabled` is true but ffprobe wasn't found on this process — a misconfigured `ffmpeg.path` or a custom build missing the binaries. The key is absent when `ffmpeg.enabled` is false; the operator opted out, so it's not a warning.

**`GET`/`PATCH /config/ffmpeg`** (admin) reads and edits the runtime config:

```bash
api "$SL/api/v1/config/ffmpeg"
# {"enabled":true,"path":"","found":true,"resolved_path":"/usr/local/bin/ffprobe","restart_required":false}

api -X PATCH -d '{"enabled":false}' "$SL/api/v1/config/ffmpeg"
```

`found` and `resolved_path` are derived from the current process's live prober, not the config file — read-only, sending them in the `PATCH` body has no effect. `path` only takes effect on the next restart, since the prober is built once at boot: a `PATCH` that changes it comes back with `restart_required: true`, and `found` in that same response still describes the old path. Re-sending the path it already has changes nothing and does not raise the flag.

**Import verification** reads the probe result before an import happens: `library.probe.always_ask` and `library.probe.min_duration_ratio` (via `GET`/`PATCH /config/library`) and `allowed_codecs` on a quality profile decide whether a finished download is imported or [held](#resolving-a-held-download) for a decision. See [Configuration Reference](Configuration-Reference#import-verification) for the checks.

---

## Worked examples

```bash
export SL=https://streamline.example.com
export KEY=your-api-key
api() { curl -sS -H "X-API-Key: $KEY" -H 'Content-Type: application/json' "$@"; }
```

**Add a movie by TMDB ID:**

```bash
api -X POST -d '{"tmdb_id":603,"quality_profile":"default"}' "$SL/api/v1/movies"
```

**Find everything still wanted:**

```bash
api "$SL/api/v1/movies?limit=100" | jq '.items[] | select(.status=="wanted") | .title'
```

**Trigger a search for every wanted movie:**

```bash
api -X POST "$SL/api/v1/schedules/movie-missing-search/run"
```

**Add a show, monitoring only missing episodes:**

```bash
api -X POST -d '{"tvdb_id":81189,"preset":"missing"}' "$SL/api/v1/series"
```

`preset` is one of `all`, `future`, `missing`, `existing`, `pilot`, `none`, and is applied once at add time to the season/episode tree.

**Correcting a show's type:**

A series' `type` (`standard`, `anime`, `daily`) is inferred from its TVDB genres
and origin, and it decides how episode files are matched — `anime` matches on
absolute number, everything else on season + episode. A wrong inference
mis-matches every file in the show, so it can be overridden:

```bash
api -X PATCH -d '{"type":"anime"}' "$SL/api/v1/series/86"
```

The override is durable: a metadata refresh no longer re-derives `type`, so it
will not be silently undone. `422` for a value outside the three.

**Does an episode have a file?** `Episode` carries a `has_file` boolean
alongside `path`, so presence does not have to be inferred from an empty string
or from `status`.

**Approve every pending request:**

```bash
api "$SL/api/v1/requests?status=pending" \
  | jq -r '.items[].id' \
  | xargs -I{} curl -sS -X POST -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
      -d '{}' "$SL/api/v1/requests/{}/approve"
```

**Nagios/Prometheus-style health check:**

```bash
curl -fsS "$SL/health" >/dev/null && echo OK
```

**Watch the download queue:**

```bash
watch -n5 "curl -sS -H 'X-API-Key: $KEY' $SL/api/v1/activity/queue \
  | jq -r '.items[] | \"\(.status)\t\(.progress)\t\(.title)\"'"
```

### Resolving a held download

A download that fails import verification sits in the queue with
`"status": "held"` and a `hold_reasons` array — one entry per failed check
(`corrupt`, `resolution`, `duration`, `codec`, `always_ask`), each naming the
file, what was expected and what was found. See
[Configuration Reference](Configuration-Reference#import-verification) for the
rules and the knobs.

```bash
api "$SL/api/v1/activity/queue" \
  | jq '.items[] | select(.status=="held") | {id, title, hold_reasons}'
```

Every held record needs a decision:

```bash
# Import it anyway — verification is skipped on the re-run
api -X POST -d '{"action":"import"}' "$SL/api/v1/downloads/42/resolve"

# Bin it and search again for a replacement
api -X POST -d '{"action":"regrab"}' "$SL/api/v1/downloads/42/resolve"

# Bin it and stop
api -X POST -d '{"action":"delete"}' "$SL/api/v1/downloads/42/resolve"
```

`regrab` and `delete` both remove the torrent **and its files** from the download
client; they differ only in whether the title goes back to wanted for another
search. Admin only. `204` on success, `400` for an action other than the three
above, `409` when the record exists but is not held. A held record also answers
`409` to the queue verbs (pause, resume, `DELETE /activity/queue/{id}`) — its
download is finished, so resolving is the only action it accepts. There is no
release blocklist yet, so a `regrab` may find the same release again.

### Driving a bulk import from the API

Start a scan, wait for `awaiting_review`, then decide in bulk rather than one
row at a time:

```bash
scan=$(api -X POST -d '{"source_path":"/srv/Films","mode":"rename","import_mode":"hardlink"}' \
  "$SL/api/v1/library/imports" | jq -r .id)

# poll until awaiting_review
until [ "$(api "$SL/api/v1/library/imports/$scan" | jq -r .status)" = awaiting_review ]; do sleep 5; done

# see the shape of the review
for c in confirmed ambiguous existing unmatched; do
  printf '%s=%s\n' "$c" "$(api "$SL/api/v1/library/imports/$scan/files?limit=1&classification=$c" | jq -r .total)"
done

# accept every confident match in one call
api -X POST -d '{"decision":"accept","classification":"confirmed"}' \
  "$SL/api/v1/library/imports/$scan/decisions"

# park the ones with no match so they do not block the commit
api -X POST -d '{"decision":"skip","classification":"unmatched"}' \
  "$SL/api/v1/library/imports/$scan/decisions"

api -X POST "$SL/api/v1/library/imports/$scan/commit"
```

`POST .../decisions` returns `{"updated": N}` and dispatches on the scan's kind,
so the same call covers movie files and series shows. Omit `classification` to
hit the whole scan, or pass `ids` to name specific rows; both together are an
AND. The ambiguous rows are the ones that still need a human — resolve those
with the per-row `PATCH`, supplying `tmdb_id`/`tvdb_id`.

**Finding files that sit outside your library roots:**

`POST /movies/{id}/rename` is a safe probe — it returns
`{"movie_id":N,"operations":[]}` when the file is already where the naming
template says it belongs, and a non-empty `operations` array (having moved it)
when it was not. It is currently the only way to discover a file that was
attached in place under some other root.

```bash
api -X POST "$SL/api/v1/movies/105/rename" | jq '.operations | length'
```

**Add an indexer:**

```bash
api -X POST -d '{
  "name":"prowlarr",
  "protocol":"prowlarr",
  "host":"prowlarr",
  "port":9696,
  "api_key":"...",
  "enabled":true
}' "$SL/api/v1/indexers"

api -X POST "$SL/api/v1/indexers/prowlarr/test"
```

---

## Generating a client

The spec is standard OpenAPI 3.0.4, so any generator works:

```bash
curl -fsSL -o openapi.yaml https://streamline.example.com/api/v1/openapi.yaml

# TypeScript
npx openapi-typescript openapi.yaml -o streamline.d.ts

# Python / Kotlin / Swift / …
npx @openapitools/openapi-generator-cli generate \
  -i openapi.yaml -g python -o ./client
```

Building a mobile client is an explicitly supported use case — the API was designed for it, which is why every UI capability has an endpoint behind it.
