# REST API

Streamline's API is the same one its own web UI uses — there's no privileged internal surface. Anything the SPA can do, you can do.

- [Interactive docs](#interactive-docs)
- [Authentication](#authentication)
- [Conventions](#conventions)
- [Endpoint map](#endpoint-map)
  - [Movies](#movies) · [Series](#series) · [Activity](#activity) · [Requests](#requests)
  - [Config-backed resources](#config-backed-resources) · [Torrents](#torrents-built-in-client) · [Transcoding endpoints](#transcoding-endpoints) · [Library](#library)
  - [Auth and users](#auth-and-users) · [Config, schedules, system](#config-schedules-system) · [Calendar](#calendar) · [Outside `/api/v1`](#outside-apiv1)
- [Media probe](#media-probe)
- [Transcoding](#transcoding)
- [Quality scoring](#quality-scoring)
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

> [!WARNING]
> API keys are **read-only on the identity surface**: any non-GET request under `/auth/me`, `/auth/password`, `/auth/invites`, `/auth/jwt`, or `/users` returns `403` with a key — those actions need a session (Bearer JWT or the SPA cookie). The two credentials are otherwise equal on media and settings endpoints. That's why the key-creation example below authenticates with a JWT, not a key.

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

> [!IMPORTANT]
> Note the path: `/auth/login`, **not** `/api/v1/auth/login`. It returns `204` and sets `streamline_session`; the cookie's value is the JWT, so you can lift it out and use it as a Bearer token.

For anything non-interactive, use an API key instead.

---

## Conventions

**Pagination.** Collection endpoints take `?page=` (default 1) and `?limit=` (default 20) and return an envelope:

```json
{ "items": [ ... ], "total": 137, "page": 1, "limit": 20 }
```

`limit` must be **between 1 and 100** on every collection endpoint (200 for activity). A value outside that range is a `400` with a message naming the bound. Paginate against `total`, not against "fewer items came back than I asked for", or you will read the first page and stop.

```bash
# wrong: 400 "limit must be between 1 and 100"
curl ".../movies?page=1&limit=500"

# right: walk pages until you have `total`
curl ".../movies?page=1&limit=100"   # -> total: 621
curl ".../movies?page=2&limit=100"
# ...
```

Activity feeds use cursor pagination instead (`?cursor=`, `?limit=`), since they're append-only and time-ordered.

**Filtering and sorting.** `GET /movies` and `GET /series` filter, search and sort server-side — `?status=`, `?query=` (substring over the titles, ignoring case and accents — `detective` finds `Détective Conan`), `?sort=` and `?order=`. `/series` additionally takes `?type=`. Doing this client-side means pulling the whole library first; these params exist so you don't have to.

**Movie list responses carry `file_summary`, not `media_files`.** The rollup gives you `file_count`, `size_bytes` and the largest file's parsed `resolution`/`codec` — enough for a list view, without a page of movies dragging in every file row it owns. Use `GET /movies/{id}` when you need the files themselves.

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

**Series counts leave the specials out.** `total_seasons`, `total_episodes`, `have_episodes` and `wanted_episodes` cover the numbered seasons only — season 0 is reported as its own entry in the detail response's `seasons` array, with its own counts, but never folded into the show's totals or into `status=missing`. `total_seasons` counts the seasons themselves, so a season the library follows no episode of still counts as one.

**Errors** are `{"message": "..."}` with a conventional status: `400` bad request, `401` unauthenticated, `403` forbidden (usually not an admin), `404`, `409` conflict (already exists), `422` unprocessable, `500`. Some carry a stable `code` alongside the message when the caller needs to branch on the specific reason — `last_admin`, `email_exists`, `connection_failed` (a connection test's upstream diagnostic), `invalid_condition` (a custom-format condition that would not compile), `grab_rejected` (a grab the server refused — an untrusted download host, or a release whose files match no wanted episode). Those last three carry a message composed for display, which the web UI shows verbatim; a coded error's `message` is otherwise still advisory.

---

## Endpoint map

117 paths, grouped below. **Auth** is `Authenticated` (any logged-in user or valid credential) or `🔒 Admin`; the Requests group is more granular and spells out the exact roles.

### Movies

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/movies` | List / add movies | Authenticated |
| `GET` | `/movies/counts` | Counts by status | Authenticated |
| `GET` `PATCH` `DELETE` | `/movies/{id}` | Fetch / update / delete a movie | Authenticated |
| `POST` | `/movies/{id}/search` · `/search-now` · `/grab` · `/refresh-metadata` · `/rename` · `/play-on` | Search, force a search, grab, refresh metadata, rename to the naming template, or play on a media server | Authenticated |
| `POST` | `/movies/{id}/reidentify` | Point the entry at a different TMDB title | 🔒 Admin |
| `GET` | `/movies/{id}/recommendations` | TMDB recommendations | Authenticated |
| `DELETE` | `/movies/{id}/files/{fileId}` | Delete a file | Authenticated |
| `GET` | `/search/movie` · `/search/movie/{tmdb_id}` | TMDB title lookup | Authenticated |

### Series

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/series` | List (`?status=`, `?type=`, `?query=`, `?sort=`, `?order=`) / add a series | Authenticated |
| `GET` | `/series/counts` · `/series/lookup` · `/series/lookup/{tvdb_id}` | Counts, TVDB lookup | Authenticated |
| `POST` | `/series/specials/apply` | Apply the specials handling | Authenticated |
| `GET` `PATCH` `DELETE` | `/series/{id}` | Fetch / update / delete a series | Authenticated |
| `GET` | `/series/{id}/browse` | Browse the season/episode tree | Authenticated |
| `POST` | `/series/{id}/search` · `/grab` · `/refresh-metadata` · `/rename` · `/play-on` | Search, grab, refresh metadata, rename, or play on a media server | Authenticated |
| `POST` | `/series/{id}/reidentify` | Point the entry at a different TVDB show | 🔒 Admin |
| `PATCH` | `/series/{id}/seasons/{number}` | Update a season | Authenticated |
| `POST` | `/series/{id}/seasons/{number}/search` · `/grab` | Search or grab a season | Authenticated |
| `GET` `PATCH` | `/series/{id}/episodes/{episodeId}` | Fetch / update an episode | Authenticated |
| `POST` | `/series/{id}/episodes/{episodeId}/search` · `/grab` | Search or grab an episode | Authenticated |
| `DELETE` | `/series/{id}/episodes/{episodeId}/file` | Delete the episode's file | Authenticated |

Each of the three search scopes filters the indexer's answer to its own scope — an episode search returns that episode, a season search returns season packs of that season, a series search returns complete/multi-season packs. The episode search additionally carries `hidden_packs` (present only when non-zero): how many packs covering that episode it excluded, so an empty `items` can be told apart from "it only exists inside a pack".

### People (cast)

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/people` | List cast members credited anywhere in the library (`?query=`, `?limit=`, `?offset=`) | Authenticated |
| `GET` | `/people/{id}` | One person's movie and series credits | Authenticated |

People are rows of their own, related to titles through a `credits` table — one row per person-on-a-title, carrying the character and the billing order. **`id` is that person row, and the only key `/people/{id}` accepts.** It is not a provider id: series cast comes from TVDB and carries no TMDB id at all, so keying the endpoint on `tmdb_id` merged every such actor into a single fictitious person. `tmdb_id` and `tvdb_id` both ride along on the person as data — either is `0` when that provider never named them.

A person is listed only while something credits them. `credits` counts library items — movies plus series — not credit rows, so a person listed twice on one title counts once. The list is ordered by `credits` descending, then by name; `query` folds accents, case and punctuation on both sides, so `?query=beatrice` finds "Béatrice Dalle".

`/people` pages with `limit` (1..100, default 20) and a zero-based `offset` rather than `page`, and answers `{ "items": [...], "total": N, "limit": N, "offset": N }`. Each item:

```jsonc
{
  "id": 42,                                                // persons row — link with this
  "tmdb_id": 1234,                                          // 0 when TMDB never named them
  "tvdb_id": 0,                                             // 0 when TVDB never named them
  "name": "Béatrice Dalle",
  "profile_url": "https://image.tmdb.org/t/p/w185/…jpg",    // omitted when empty
  "credits": 3,

  // Biographical fields — every one optional, every one omitted when empty
  "biography": "French actress born in Brest…",
  "known_for": "Acting",                                    // TMDB only
  "birthday": "1964-12-19",
  "deathday": "1852-11-27",                                 // absent while alive
  "place_of_birth": "Brest, Finistère, France",
  "imdb_id": "nm0001102",
  "instagram_id": "beatricedalle",                          // handle, not a URL
  "twitter_id": "beatricedalle"                             // handle, not a URL
}
```

The biographical block is filled in from the person's own provider record when a title crediting them is ingested, and every field is optional. An empty value is **omitted**, never sent as `""`, so a client should treat absent and empty the same.

Two sources, two shapes of gap:

- **TMDB** (movie cast) supplies all of them, subject to what the provider itself holds.
- **TVDB** (series cast) has no known-for department at all — a series-sourced person never carries `known_for` — and usually no socials either. That is the provider, not a failed fetch; there is no fallback and nothing is synthesised.

`birthday` and `deathday` are the provider's own strings, normally `YYYY-MM-DD`. They are not `date`-typed: both providers return partial and malformed values ("1984", ""), and they are passed through as given rather than dropped.

The fetch happens **once per person, ever**. An actor credited on thirty titles costs one provider call, not thirty, and the pass runs after the title's write has committed, so it can never fail an add or a refresh. A lookup that errors leaves the person unenriched and the next metadata refresh of a title crediting them retries — which is why a person can be listed with none of these fields for a while.

```jsonc
// GET /api/v1/people/42
{
  "id": 42,
  "tmdb_id": 1234,
  "tvdb_id": 0,
  "name": "Béatrice Dalle",
  "profile_url": "https://image.tmdb.org/t/p/w185/…jpg",  // omitted when empty
  // …the same optional biographical fields as the list item
  "biography": "French actress born in Brest…",
  "movies": [{ "movie": { /* Movie */ }, "character": "Betty" }],
  "series": [{ "series": { /* TVShow */ }, "character": "Herself" }]
}
```

`character` belongs to the pairing, not to the person: the same actor carries a different one per title. An `id` no person row occupies is a `404`; the series objects carry no season/episode tree.

The `cast` array on a stored movie or series (`GET /movies/{id}`, `GET /series/{id}`) is served from the same credits, in billing order, and each entry carries `person_id` — the key to `/people/{id}`. A cast entry from a **provider lookup** of a title the library does not hold (`/movies/lookup/{tmdbId}`, `/series/lookup/{tvdbId}`, an expanded request row) has no person row behind it and so omits `person_id`; it still carries `person_url` to the provider's own page.

### Activity

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/activity` | Event feed (movies, episodes and series; filter with `?movie_id=` or `?series_id=`) | Authenticated |
| `GET` | `/activity/queue` · `/activity/history` | Queue and history views | Authenticated |
| `DELETE` | `/activity/queue/{id}` · `/activity/history/{id}` | Remove a queue or history entry | Authenticated |
| `POST` | `/activity/queue/{id}/pause` · `/resume` · `/activity/history/clear-completed` | Pause/resume a download, or clear completed history | Authenticated |
| `GET` | `/activity/pending` | List adoption proposals | 🔒 Admin |
| `GET` | `/activity/pending/{id}/preview` | Preview a proposal | 🔒 Admin |
| `POST` | `/activity/pending/{id}/import` · `/replace` · `/ignore` | Decide a proposal | 🔒 Admin |
| `POST` | `/activity/pending/{id}/identify` | Identify a proposal against metadata | 🔒 Admin |
| `DELETE` | `/activity/pending/{id}` | Forget a proposal so its torrent can be adopted again | 🔒 Admin |
| `POST` | `/downloads/{id}/resolve` | Release a held download | 🔒 Admin |

### Requests

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/requests` | List / create requests | Any (scoped for `request_only`) |
| `GET` | `/requests/counts` · `/requests/{id}/metadata` | Counts, request metadata | Any |
| `POST` | `/requests/{id}/approve` | Approve a request | admin, member |
| `POST` | `/requests/{id}/deny` · `/reopen` | Deny or reopen a request | admin |

### Config-backed resources

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/indexers` · `/download-clients` · `/media-servers` · `/quality-profiles` · `/custom-formats` | List / create | 🔒 Admin |
| `GET` `DELETE` | `/{resource}/{name}` | Fetch / delete by name | 🔒 Admin |
| `PUT` | `/indexers/{name}` · `/download-clients/{name}` · `/quality-profiles/{name}` · `/custom-formats/{name}` | Replace | 🔒 Admin |
| `PATCH` | `/media-servers/{name}` | Update | 🔒 Admin |
| `POST` | `/{resource}/test` | Test an unsaved config | 🔒 Admin |
| `POST` | `/{resource}/{name}/test` | Test a saved one | 🔒 Admin |
| `POST` | `/custom-formats/test` | Evaluate a draft condition set against a sample release | 🔒 Admin |
| `POST` | `/media-servers/discover` | List libraries/sections for a draft (body carries the token) | 🔒 Admin |
| `POST` | `/media-servers/{name}/discover` | Same, for a saved server, using its stored token | 🔒 Admin |
| `POST` | `/quality-profiles/{name}/default` | Point `quality_default_profile` at this profile | 🔒 Admin |

Built-in custom formats are listed alongside user-defined ones (`builtin: true`); `PUT`/`DELETE` against a built-in, or a delete of a format still scored by a quality profile, is `409`. See [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats).

`is_default` on a quality profile marks the one a movie or series with an empty `quality_profile` resolves to. Deleting it is a `409` while it holds the role, so `POST /quality-profiles/{name}/default` is both how you change the default and how you free the old one for deletion.

### Torrents (built-in client)

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/torrents` · `/torrents/{hash}` | List / fetch a torrent | 🔒 Admin |
| `POST` | `/torrents/{hash}/pause` · `/resume` | Pause / resume | 🔒 Admin |
| `PATCH` | `/torrents/{hash}/files/{index}` | Toggle a file | 🔒 Admin |
| `PUT` | `/torrents/listen-port` | Move the running engine's peer sockets; not persisted | 🔒 Admin |

### Transcoding endpoints

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/transcoding/queue` | Newest 200 jobs plus every rejected row, no filter | 🔒 Admin |
| `POST` | `/transcoding/jobs/{id}/cancel` · `/retry` | Cancel or retry a job | 🔒 Admin |
| `POST` | `/transcoding/scan` | Queue the existing library | 🔒 Admin |

All four answer `409` while `transcoding.enabled` is false.

### Library

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/library/imports` | List / start an import scan | 🔒 Admin |
| `GET` `DELETE` | `/library/imports/{id}` | Fetch / delete a scan | 🔒 Admin |
| `POST` | `/library/imports/{id}/cancel` · `/commit` | Cancel or commit a scan | 🔒 Admin |
| `GET` | `/library/imports/{id}/files` · `/shows` | List scanned rows | 🔒 Admin |
| `PATCH` | `/library/imports/{id}/files/{fileId}` · `/shows/{showId}` | Update a row's match | 🔒 Admin |
| `POST` | `/library/imports/{id}/decisions` | Bulk decision | 🔒 Admin |
| `GET` `POST` | `/library/path-migration` | List / start a path migration | 🔒 Admin |
| `GET` | `/library/path-migration/roots` | List roots | 🔒 Admin |
| `POST` | `/library/path-migration/preview` | Preview a migration | 🔒 Admin |

### Auth and users

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `PATCH` | `/auth/me` | Fetch / update your profile | Any |
| `PUT` | `/auth/password` | Change your password | Any |
| `GET` `POST` | `/auth/me/api-keys` · `/auth/me/sessions` | List / create your API keys or sessions | Any |
| `DELETE` | `/auth/me/api-keys/{id}` · `/auth/me/sessions/{id}` | Revoke your own key or session | Any |
| `POST` | `/auth/jwt/rotate` | Rotate the JWT signing secret (logs everyone out) | 🔒 Admin |
| `GET` `POST` | `/auth/invites` | List / create invites | 🔒 Admin |
| `DELETE` | `/auth/invites/{id}` | Revoke an invite | 🔒 Admin |
| `GET` `POST` | `/users` | List / create users | 🔒 Admin |
| `GET` `PATCH` `DELETE` | `/users/{uid}` | Fetch / update / delete a user | 🔒 Admin |
| `POST` | `/users/{uid}/password-reset` · `/unlock` | Reset a password or clear a lockout | 🔒 Admin |
| `DELETE` | `/users/{uid}/api-keys/{kid}` · `/sessions/{sid}` | Revoke another user's key or session | 🔒 Admin |

### Config, schedules, system

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `PATCH` | `/config/auth` · `/config/library` · `/config/ffmpeg` · `/config/download` · `/config/metadata` · `/config/system` · `/config/transcoding` | Read / patch a config section | 🔒 Admin |
| `GET` `POST` | `/config/oidc` | List / add an OIDC provider | 🔒 Admin |
| `GET` `PATCH` `DELETE` | `/config/oidc/{name}` | Fetch / update / remove a provider | 🔒 Admin |
| `GET` | `/schedules` · `/schedules/{name}` | List / fetch a schedule | 🔒 Admin |
| `PATCH` | `/schedules/{name}` | Update a schedule | 🔒 Admin |
| `POST` | `/schedules/{name}/pause` · `/resume` · `/run` | Pause, resume, or run a schedule now | 🔒 Admin |
| `GET` | `/system/info` | Server info | 🔒 Admin |

### Calendar

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/calendar/upcoming?from=&to=` | Movie releases (digital, or theatrical when TMDB has no digital date — see `release_type`) and episode air dates | Authenticated |

### Outside `/api/v1`

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/health` | Unauthenticated probe. Bare JSON, deliberately not in the spec | None |
| `POST` | `/auth/login` · `/auth/register` · `/auth/logout` | Cookie-based, `204` on success | None |
| `GET` | `/auth/config` · `/auth/invite/{token}` | Pre-auth SPA bootstrap | None |
| `GET` | `/auth/oidc/{name}/start` · `/callback` | The OIDC flow | None |
| `GET` | `/posters/{kind}/{id}/poster.jpg` | Poster proxy | — |

---

## Media probe

Technical details read from your files with `ffprobe` — resolution, codecs, duration, bitrate, stream languages. See [Configuration Reference](Configuration-Reference#ffmpeg) for the config side.

**`media_info`** is a nullable object on `MediaFile` (movies) and `Episode` responses:

<details>
<summary><b>Example <code>media_info</code> object</b></summary>

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
  "audio_track_count": 3,
  "audio_languages": ["eng", "fra", "jpn"],
  "subtitle_languages": ["eng", "fra"],
  "probed_at": "2026-08-18T12:00:00Z"
}
```

</details>

It's absent until the file has been probed, and absent again if the probe failed — check for the key, don't assume it's always there. An upgrade of Streamline that adds a new probed field also clears the stamp on files probed before that field existed, so `media_info` goes absent for them until the backfill job gets to them again: a partial probe result read as a complete one would be wrong everywhere, including in [upgrade decisions](Quality-Profiles-and-Custom-Formats#what-a-file-can-be-scored-on).

Stream data is **aggregate, not per-track**: a count and two language sets. There is no per-stream breakdown, so which track holds which language, its codec, and whether it is default or forced are not exposed — do not reconstruct a track list from these, the mapping isn't in the data. Three audio tracks can be two languages. Languages are ISO 639-2/T, deduped and sorted, canonicalised server-side (a file tagged `fre` reports `fra`); `subtitle_languages` excludes forced tracks. Both arrays are omitted when empty, and `audio_track_count` when zero.

`audio_track_count`, `audio_languages` and `subtitle_languages` are also the only probed values a [custom format condition](Quality-Profiles-and-Custom-Formats#what-the-probe-knows-that-you-cannot-match-on) can read, alongside width and video codec.

It describes the file's **main** video track and its first audio track. Embedded cover art and poster thumbnails are video streams as far as ffprobe is concerned, so the largest one wins — a 4K film carrying a 300×300 poster reports `3840`, not `300`.

**`ffmpeg_warn`** on `GET /system/info` is `true` when `ffmpeg.enabled` is true but ffprobe wasn't found on this process — a misconfigured `ffmpeg.path` or a custom build missing the binaries. The key is absent when `ffmpeg.enabled` is false; the operator opted out, so it's not a warning.

**`GET`/`PATCH /config/ffmpeg`** (admin) reads and edits the runtime config:

```bash
api "$SL/api/v1/config/ffmpeg"
# {"enabled":true,"path":"","found":true,"resolved_path":"/usr/local/bin/ffprobe",
#  "version":"6.1.1","restart_required":false}

api -X PATCH -d '{"enabled":false}' "$SL/api/v1/config/ffmpeg"
```

`found` and `resolved_path` are derived from the current process's live prober, not the config file — read-only, sending them in the `PATCH` body has no effect. `version` comes from `ffmpeg -version` and is present only when the **ffmpeg** binary resolves and answers; `found` is ffprobe's and does not imply it, which matters because [transcoding](#transcoding) needs the encoder rather than the prober. `path` only takes effect on the next restart, since the prober is built once at boot: a `PATCH` that changes it comes back with `restart_required: true`, and `found` in that same response still describes the old path. Re-sending the path it already has changes nothing and does not raise the flag.

**Import verification** reads the probe result before an import happens: `library.probe.always_ask` and `library.probe.min_duration_ratio` (via `GET`/`PATCH /config/library`) and `allowed_codecs` on a quality profile decide whether a finished download is imported or [held](#resolving-a-held-download) for a decision. See [Configuration Reference](Configuration-Reference#import-verification) for the checks.

---

## Transcoding

Background re-encoding of imported files, driven by the `transcode` block on a [quality profile](Quality-Profiles-and-Custom-Formats#transcoding-a-profiles-files). The queue is admin-only, and **every endpoint here answers `409` while `transcoding.enabled` is false** — a feature that is off returns a conflict, not an empty list, so a client can tell the two apart.

**`GET /transcoding/queue`** returns the newest 200 jobs, plus every `rejected` row regardless of age, newest first. There is no `status` filter; filter client-side.

<details>
<summary><b>Example queue response</b></summary>

```json
[
  {
    "id": 41,
    "status": "running",
    "attempts": 1,
    "file_path": "/srv/streamline/movies/Heat (1995)/Heat (1995).mkv",
    "media_title": "Heat (1995)",
    "movie_id": 12,
    "percent": 42.5,
    "eta_seconds": 1980,
    "speed": 1.8,
    "created_at": "2026-09-07T09:00:00Z",
    "started_at": "2026-09-07T09:01:12Z"
  },
  {
    "id": 40,
    "status": "succeeded",
    "attempts": 1,
    "file_path": "/srv/streamline/movies/Alien (1979)/Alien (1979).mkv",
    "media_title": "Alien (1979)",
    "movie_id": 9,
    "size_before": 37580963840,
    "size_after": 12884901888,
    "created_at": "2026-09-07T08:00:00Z",
    "started_at": "2026-09-07T08:00:05Z",
    "finished_at": "2026-09-07T08:44:31Z"
  }
]
```

</details>

`status` is `queued` · `running` · `succeeded` · `failed` · `canceled` · `rejected`. `movie_id`, or `series_id` + `episode_id`, links the row back to the item — exactly one pair is set. `error` carries the tail of ffmpeg's stderr from the last failed attempt. `rejected` means the output failed verification — a verdict on the encode, terminal, with `error` naming the check and its numbers.

**`attempts` counts claims, not failures**, so a `running` row is always at 1 or more — the claim that started it is what incremented it. **`size_before` and `size_after` are written together, only when a job succeeds or is rejected**, so neither is present on a queued, running, failed or canceled row; the size of a file mid-encode is not in this payload. `finished_at` likewise appears only once the job reaches a terminal status.

**`percent`, `eta_seconds` and `speed` are live and in-memory only.** They come from the encoder running in *this* process, so they are absent for every status but `running` — and absent for a `running` row whose encode belonged to a process that has since restarted (that row is reset to `queued` on the next boot anyway). Don't treat their absence as zero progress.

**`POST /transcoding/jobs/{id}/cancel`** (204) stops a running encode or drops a queued one. A cancel that arrives after the file has already been swapped in is too late — the job completes. **`POST /transcoding/jobs/{id}/retry`** (204) puts a `failed` or `rejected` job back in the queue with `attempts`, `error` and `finished_at` cleared.

Both answer `409` for a job in the wrong state, **and there is no `404`**: each is a single conditional update, so an id naming no job at all is indistinguishable from one that has already finished.

**`POST /transcoding/scan`** (202) walks the library, re-probes every file whose profile carries a `transcode` block, and queues the non-compliant ones. It returns as soon as the scan is dispatched — there is no scan-status endpoint; watch the queue. `409` while a scan is already running, and `409` with `code: worker_unavailable` when the worker could not run the jobs anyway (`ffmpeg.enabled: false`, or the ffmpeg binary not found in this process) rather than queueing work nothing would drain — the code is how a client tells the two refusals apart.

**`transcoded_at` and `size_before`** appear on `MediaFile` and flat on `Episode` once a file has been re-encoded — `size` names the file as it is now, so the pair is what renders "35 GB → 12 GB". Both are absent for a file that has never been transcoded. The file's [`media_info`](#media-probe) is rewritten in the same update, from the probe the worker took to verify the encode — so it describes the new bytes right away rather than going missing until the backfill catches up.

**`GET`/`PATCH /config/transcoding`** (admin) reads and edits the switch and the budget:

```bash
api "$SL/api/v1/config/transcoding"
# {"enabled":false,"max_concurrent":1,"max_failures":3,"defer_seeding":false,"verify":{"max_size_percent":100,"min_size_percent":5,"health_check":false,"min_vmaf":0}}

api -X PATCH -d '{"enabled":true,"max_concurrent":2}' "$SL/api/v1/config/transcoding"

api -X PATCH -d '{"verify":{"max_size_percent":110,"health_check":true}}' "$SL/api/v1/config/transcoding"
```

Every key here, the `verify` block included, takes effect on the next worker tick — no restart. The binaries come from [`/config/ffmpeg`](#media-probe); this section carries no path of its own.

---

## Editing config

Seven sections are readable and patchable over the API; [Configuration Reference](Configuration-Reference#whats-editable-at-runtime) has the full table of what is hot and what needs a restart. Every one is admin-only, takes a partial body (an omitted field keeps its stored value), and answers with the section's new state.

```bash
api -X PATCH -d '{"import_mode":"copy","max_grab_failures":5}' "$SL/api/v1/config/library"
api -X PATCH -d '{"selective_files":true}'                     "$SL/api/v1/config/download"
api -X PATCH -d '{"lockout":{"threshold":5}}'                  "$SL/api/v1/config/auth"
```

**`/config/library`** also returns `movie_path`, `series_path` and `download_path` — read-only, because a root only moves through [path migration](Importing-an-Existing-Library), which rewrites every stored path before it repoints the config. Sending them in the body has no effect. The bounded counters (`max_grab_failures`, `import_max_attempts` 1–255; `drift_grace_ticks` 1–20) answer `422` outside their range rather than wrapping.

**`/config/metadata`** never echoes an api key. The read view carries `tmdb_api_key_set`/`tvdb_api_key_set` and, when the key comes from a `*_file` path, `tmdb_api_key_file_managed`/`tvdb_api_key_file_managed`. On a `PATCH`, a blank or omitted key **keeps the stored one** — there's no way to clear a key over the API — and setting one inline while the matching `*_file` is configured is a `422`: the file is the source of truth. Every field here is read once at boot, so a real change comes back with `restart_required: true`.

```bash
api "$SL/api/v1/config/metadata"
# {"language":"en","tmdb_region":"US","tmdb_api_key_set":true,"tvdb_api_key_set":true,
#  "tmdb_api_key_file_managed":false,"tvdb_api_key_file_managed":false,"restart_required":false}

api -X PATCH -d '{"language":"fr","tmdb_region":"FR"}' "$SL/api/v1/config/metadata"
```

**`/config/system`** carries the server's own bookkeeping — application and HTTP logging, the OTLP endpoint, and how long media events are kept. `log` nests partially at every level, so patching one field leaves its siblings alone:

```bash
api -X PATCH -d '{"log":{"app":{"level":"debug"}}}' "$SL/api/v1/config/system"
# app.format, app.output and the whole http section keep their stored values
```

`events_retention` is the only field here that reaches this process on its own — the cleanup job reads it per tick. Everything else is read once at boot, so changing it comes back with `restart_required: true`; a patch that only moves retention does not raise the flag.

`log.app.output` and `log.http.output` are each `stderr`, `stdout`, or a file path. `log.*.rotate` applies only to a file path, and `0` on any of its bounds disables that bound — the settings page shows the rotation fields only once the output is a file, for the same reason.

---

## Quality scoring

Full mental model, condition types and the built-in format library: [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats). This section is the API shape only.

**`QualityProfile`** gained four fields alongside the pre-existing `preferred_resolution`/`min_resolution`/`upgrade_allowed`/`allowed_codecs`:

<details>
<summary><b>Example <code>QualityProfile</code></b></summary>

```json
{
  "name": "default",
  "preferred_resolution": "2160p",
  "min_resolution": "1080p",
  "upgrade_allowed": true,
  "formats": [
    { "name": "x265", "score": 100 },
    { "name": "hdr", "score": 50 }
  ],
  "min_score": 0,
  "upgrade_until_score": 500
}
```

</details>

`formats[].name` accepts either a built-in name or a `custom_formats` entry name; an unresolvable name is `422`. `min_score` is **omitted from the response when it's `0`** — the handler only sets the field when the value is non-zero (`*int`), not a signal it's unset; absent reads as `0`.

**Browse-releases responses** (`POST /movies/{id}/search`, the series browse endpoints) annotate each `SearchResult` with the item's own profile:

<details>
<summary><b>Example <code>SearchResult</code></b></summary>

```json
{
  "title": "Movie.Title.2024.2160p.UHD.BluRay.REMUX-GROUP",
  "seeders": 40,
  "score": 300,
  "rejected": false,
  "matched_formats": ["remux", "hdr"]
}
```

</details>

Results are sorted `score` descending, ties broken by seeders — not by seeders alone. `rejected: true` releases (resolution outside the profile band, or score below `min_score`) are still returned with a `reject_reason`, so an operator can grab one deliberately; `score`/`rejected`/`reject_reason`/`matched_formats` are all ignored if sent back on a grab request body.

**`MediaFile.file_score`** is the file's computed score against its movie's current profile — **movie detail responses only**, omitted from list responses same as the rest of that eager-loaded shape. A file outside the profile's band, or below `min_score`, reports `file_score: 0` — the same number the automatic-upgrade decision reads, and there's no separate `rejected` flag on a file. The key is absent entirely when no quality profile is configured at all. `Episode` carries the same field flat (`Episode.file_score`), against the series' profile, on `GET /series/{id}` — the series list response has no `seasons`/`episodes` at all.

**`/custom-formats/test`** evaluates an unsaved condition set against a synthetic sample — `POST /api/v1/custom-formats/test` with `{conditions, sample: {title, size, seeders, episodes}}` (`episodes` defaults to 1 and scales the size bounds). Worked example: [Quality Profiles and Custom Formats § The tester](Quality-Profiles-and-Custom-Formats#the-tester).

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
api "$SL/api/v1/movies?status=wanted&limit=100" | jq '.items[].title'
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
