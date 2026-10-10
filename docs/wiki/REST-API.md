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
> API keys are **read-only on the identity surface**: any non-GET request under `/auth/me`, `/auth/password`, `/auth/invites`, `/auth/jwt`, `/account`, or `/users` returns `403` with a key — those actions need a session (Bearer JWT or the SPA cookie). The two credentials are otherwise equal on media and settings endpoints. That's why the key-creation example below authenticates with a JWT, not a key.

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

**Series counts leave the specials out.** `total_seasons`, `total_episodes`, `have_episodes` and `wanted_episodes` cover the numbered seasons only — season 0 is reported as its own entry in the detail response's `seasons` array, with its own counts, but never folded into the show's totals or into `status=missing`. `total_seasons` counts the seasons themselves, so a season the library follows no episode of still counts as one. A show whose only files are specials therefore reports `have_episodes: 0`: to ask whether it has files on disk, read the `seasons` array, not the rollup.

**`last_added` is the show's most recent import, not its add date.** `added_at` is when the show record was created; `last_added` (absent until a file has landed) summarises the newest batch — every file imported within an hour of the newest one — as `at`, `seasons`, `episodes` (within `seasons[0]`, omitted for a multi-season pack), `episode_title` for a single episode, `whole_season` when the batch filled its season, and `count`. Sort on `last_added.at` to order shows by arrival.

**The counts endpoints are faceted.** `GET /movies/counts` and `GET /series/counts` accept the same filter params as their list endpoints (`status`, `monitored`, `type`, `query`), and every tally comes back counted with the request's *other* filters applied and its own facet's filter left out. That is what lets a filter UI show how many titles each value would leave: counted against its own selection a facet zeroes every row but the chosen one, and nothing else can be picked. `query` is not a facet — it has no "all" row to keep selectable — so it narrows every tally.

Each facet carries its own `*_total` "all" row (`status_total`, `type_total`, `monitored_total`); the three diverge as soon as two facets are filtered, so read each from its own. `total` is separate again: the library, filtered by nothing. So are `wanted_episodes`, `downloading_episodes` and the movie `trend`. Called with no parameters the whole response is the unfiltered library, which is the shape older clients already expected.

**Errors** are `{"message": "..."}` with a conventional status: `400` bad request, `401` unauthenticated, `403` forbidden (usually not an admin), `404`, `409` conflict (already exists), `422` unprocessable, `500`. Some carry a stable `code` alongside the message when the caller needs to branch on the specific reason — `last_admin`, `email_exists`, `connection_failed` (a connection test's upstream diagnostic), `invalid_condition` (a custom-format condition that would not compile), `grab_rejected` (a grab the server refused — an untrusted download host, a release whose files match no wanted episode, a built-in client already holding its maximum of 500 torrents, or a search result whose handle expired when the session secret rotated). Those last three carry a message composed for display, which the web UI shows verbatim; a coded error's `message` is otherwise still advisory.

---

## Endpoint map

117 paths, grouped below. **Auth** is `Authenticated` (any logged-in user or valid credential, `request_only` included), `Member` (`admin` or `member` — a `request_only` caller gets 403) or `🔒 Admin`; the Requests group spells out the exact roles.

### Movies

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/movies` | List movies | Authenticated |
| `POST` | `/movies` | Add a movie | Member |
| `GET` | `/movies/counts` | Faceted counts (same filter params as the list) | Authenticated |
| `GET` | `/movies/{id}` | Fetch a movie | Authenticated |
| `PATCH` `DELETE` | `/movies/{id}` | Update / delete a movie | Member |
| `POST` | `/movies/{id}/search` · `/search-now` · `/grab` · `/refresh-metadata` · `/rename` | Search, force a search, grab, refresh metadata, or rename to the naming template | Member |
| `GET` | `/movies/{id}/play-on` | Links to play the movie on a media server | Member |
| `POST` | `/movies/{id}/reidentify` | Point the entry at a different TMDB title | 🔒 Admin |
| `GET` | `/movies/{id}/recommendations` | TMDB recommendations | Authenticated |
| `DELETE` | `/movies/{id}/files/{fileId}` | Delete a file (`409` `outside_library` when its stored path is outside the library root: nothing is deleted) | Member |
| `GET` | `/search/movie` · `/search/movie/{tmdb_id}` | TMDB title lookup | Authenticated |

### Series

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/series` | List (`?status=`, `?type=`, `?query=`, `?sort=`, `?order=`) | Authenticated |
| `POST` | `/series` | Add a series | Member |
| `GET` | `/series/counts` · `/series/lookup` · `/series/lookup/{tvdb_id}` | Faceted counts (same filter params as the list), TVDB lookup | Authenticated |
| `POST` | `/series/specials/apply` | Apply the specials handling | 🔒 Admin |
| `GET` | `/series/{id}` | Fetch a series | Authenticated |
| `PATCH` `DELETE` | `/series/{id}` | Update (`monitored`, `quality_profile`, `preset`, `type`) / delete a series | Member |
| `POST` | `/series/{id}/browse` · `/grab` | Search for complete/multi-season packs, or grab one | Member |
| `POST` | `/series/{id}/search` · `/refresh-metadata` · `/rename` | Search, refresh metadata, or rename | Member |
| `GET` | `/series/{id}/play-on` | Links to play the series on a media server | Member |
| `POST` | `/series/{id}/reidentify` | Point the entry at a different TVDB show | 🔒 Admin |
| `PATCH` | `/series/{id}/seasons/{number}` | Update a season | Member |
| `POST` | `/series/{id}/seasons/{number}/search` · `/grab` | Search or grab a season | Member |
| `PATCH` | `/series/{id}/episodes/{episodeId}` | Update an episode | Member |
| `POST` | `/series/{id}/episodes/{episodeId}/search` · `/grab` | Search or grab an episode | Member |
| `DELETE` | `/series/{id}/episodes/{episodeId}/file` | Delete the episode's file (`409` `outside_library` as for movies) | Member |

Each of the three search scopes filters the indexer's answer to its own scope — an episode search returns that episode, a season search returns season packs of that season, a series search returns complete/multi-season packs. The episode search additionally carries `hidden_packs` (present only when non-zero): how many packs covering that episode it excluded, so an empty `items` can be told apart from "it only exists inside a pack".

### Music

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| `GET` | `/music/search?query=` | Search MusicBrainz for artists; each hit carries `already_added`, `library_id`, `genre`, `area`, `since`. `429` (`code: rate_limited`, `Retry-After`) when MusicBrainz is limiting | Authenticated |
| `GET` | `/music/search/{mbid}` | The hit fields and the detail of one MusicBrainz artist: `overview` (`?lang=en\|fr`, English when that language has no article), `genres`, current `members`, `releases` | Authenticated |
| `GET` | `/music/artists` | Paginated list (`?page=`, `?limit=` 1-100, `?status=wanted\|downloading\|available`, `?monitored=monitored\|unmonitored`, `?query=` accent-folded over name, sort name, genre and album titles, `?sort=recent\|name`, `?order=`); items carry their albums as tiles with no tracks | Authenticated |
| `GET` | `/music/artists/counts` | Faceted counts for the list's toolbar: each facet is counted with the other facet's filter applied, each with its own `*_total` row; with no parameters, the whole library | Authenticated |
| `POST` | `/music/artists` | Add an artist by `mbid` (`monitor` defaults to `all`, optional `quality_profile`); `409` if already added, `422` for an unknown profile or policy, `429` when MusicBrainz is limiting | Member |
| `GET` | `/music/artists/{id}` | Fetch an artist with its full tree: members, and every album with tracks, credits and personnel (`?lang=en\|fr` picks the overview) | Authenticated |
| `PATCH` | `/music/artists/{id}` | Update `monitor` (`all`, `future`, `manual`, `none`; re-applied to the existing albums, never touching their status) and `quality_profile` (`422` for an unknown name; empty clears to the default) | Member |
| `DELETE` | `/music/artists/{id}` | Remove an artist; `?delete_files=true` also deletes files from disk | Member |
| `POST` | `/music/artists/{id}/refresh-metadata` | Re-fetch the artist and its discography from MusicBrainz; new release groups follow the monitor policy and are hydrated in the background | Member |
| `POST` | `/music/artists/{id}/rename` | Rename every track file of the artist to `library.music_naming` from database metadata (`?preview=true` returns the plan without applying it); `{artist_id, operations}` | Member |
| `POST` | `/music/artists/{id}/search-now` | Search and grab the artist's wanted, released albums in the background. `202` | Member |
| `POST` | `/music/artists/{id}/browse` | Search the indexers for the artist's discography packs; items are plain search results | Member |
| `POST` | `/music/artists/{id}/grab` | Grab a discography pack: the item of a browse, unchanged. `202`; `422 grab_rejected` when no album of the artist can take it | Member |
| `GET` | `/music/albums/{id}` | Fetch an album with its tracks, credits and personnel (the object an artist detail embeds) | Authenticated |
| `PATCH` | `/music/albums/{id}` | Update `monitored`; never changes the artist's policy | Member |
| `POST` | `/music/albums/{id}/search` | Search the indexers for one album; plain search results, ranked, with the releases the profile rejects kept at the end flagged `rejected` and `reject_reason` | Member |
| `POST` | `/music/albums/{id}/grab` | Grab a chosen release: an item of a search, unchanged. `202` with no body | Member |
| `POST` | `/music/albums/{id}/search-now` · `/music/tracks/{id}/search-now` | Run one search-and-grab pass for the album (a track searches its album). `202`; a no-op for an album that is available, upcoming or already downloading | Member |
| `DELETE` | `/music/tracks/{id}/file` | Delete the track's files from disk and the library, and put an available album back to wanted; `409` when a path is outside the music root | Member |
| `GET` `POST` | `/music/quality-profiles` | List / create music quality profiles | Authenticated / 🔒 Admin |
| `PUT` `DELETE` | `/music/quality-profiles/{name}` | Update / delete a music quality profile | 🔒 Admin |
| `POST` | `/music/quality-profiles/{name}/default` | Make this the default music profile; the way out of the `409` a delete of the default answers | 🔒 Admin |

Adding an artist is two-phase: the request fetches only the artist and its release-group list (`1 + ceil(n/100)` MusicBrainz requests) and answers `201` with one stub album per release group and `hydrating: true`. Tracks, credits, covers, the Wikipedia overview and the photo arrive in the background; poll the detail (`tracks_pending` marks an album still waiting) until `hydrating` is false. MusicBrainz allows one request per second process-wide, shared with every lookup, so the background work yields to interactive requests and spends at most half the budget, about four seconds per album. Poster URLs are not in the payloads: clients build `/posters/artists/{id}/poster.jpg` and `/posters/albums/{id}/poster.jpg` themselves, and `/posters/lookup/...` for a hit not yet added.

An album search or an artist browse answers `422` with `code: no_quality_profile` when there is no usable profile; a refused grab answers `422` with `code: grab_rejected`. A grab flips the album (or, for a pack, every album it links) to `downloading`. The `status` of an album is `upcoming` while it is wanted and dated after today; that of an artist is the rollup over its monitored, non-upcoming albums.

A music quality profile is `{name, tiers, preferred, upgrade_allowed, is_default}`: `tiers` are the quality tiers it accepts, drawn from `hires`, `lossless`, `high`, `standard`, `low` and returned best first, and `preferred` (one of `tiers`, else `422`) is the tier an upgrade stops at. `is_default` marks the profile an artist with an empty `quality_profile` resolves to; deleting it is a `409`, and the first profile created while none resolves becomes the default. `PUT` takes the same body as `POST` and ignores the name in the body.

A music release in a search result carries `source` as a display label (`FLAC 24/96`, `MP3 V0`, `AAC 256`) and `audio_tier` when the release name states its quality; a release that states none stays in the list with `rejected: true` and `reject_reason`. A grab body also accepts `replace_existing: true`: the old files of the tracks the release matches go only after the new ones are placed and verified (for an artist pack, only on the albums that already had files).

### Books

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| `GET` | `/books/search?query=&type=` | Search Hardcover for books and series to add (`type` is `book`, `series` or `all`, the default; at least 2 characters). Two Hardcover requests, one with `type`, memoised for 5 minutes. Series named like the query come first, then books, capped at 20. Each hit carries `already_added`, its `library_id`, and for an added series the `cover_id` of its first volume | Authenticated |
| `GET` | `/books/search/{hardcover_id}?type=book\|series` | One hit plus its detail (overview, genres, pages, editions, `volume_book_ids`), memoised for 10 minutes; also the book arm of `GET /requests/{id}/metadata` | Authenticated |
| `GET` | `/books` | The shelf: standalone books and series merged into one paged list (`?page=`, `?limit=` 1-100, `?status=`, `?author=`, `?format=`, `?kind=` comma list, `?query=`, `?sort=added\|title\|year`, `?order=`). `type` says which an item is; ids are only unique per `type`. A series volume is never an item of its own | Authenticated |
| `GET` | `/books/counts` | Faceted counts (status, author, format), each with its own `*_total`; with no parameters the whole library | Authenticated |
| `POST` | `/books` | Add a book by `hardcover_id` (one Hardcover request; optional `monitor` `both\|ebook\|audiobook\|none` and `quality_profile`). `404` for an unknown Hardcover id, `409` if already in the library, `422` for an unknown monitor or profile, `429` with `Retry-After` and `code: rate_limited`, `503` without a Hardcover key | Member |
| `GET` | `/books/{id}` | A book with its contributors, editions and the two format slots (state, edition, file, live progress, a replacement under way) | Authenticated |
| `PATCH` | `/books/{id}` | `monitor`, `preferred_language`, `quality_profile` (empty clears), `kind`, and the pair `format` + `edition_id`; `422` for anything it cannot apply. Changing the edition of a slot that holds a file queues a replacement | Member |
| `DELETE` | `/books/{id}` | Remove a book; `?delete_files=true` also deletes files. A series volume answers `409` with `code: series_volume`: it goes with its series | Member |
| `POST` | `/books/{id}/refresh-metadata` | Re-read the book from Hardcover (one request); `404`, `429`, `503` | Member |
| `POST` | `/books/{id}/rename?preview=` | The rename plan of a book's files, applied unless `preview=true` | Member |
| `POST` | `/books/{id}/search?kind=ebook\|audiobook` | Interactive search; `kind` omitted searches both slots and merges them. Flat releases (`slot`, the upper-case container in `source`, `bitrate_kbps` for an audiobook), rejected ones kept at the end with `rejected` and `reject_reason`. The language tag of a release is shown, never filtered on | Member |
| `POST` | `/books/{id}/grab` | Grab a release: a search item, unchanged (`slot` included). The slot is read from the release's container; a `slot` that disagrees answers `422` `code: grab_rejected`, and `?kind=` disagreeing with the body's `slot` answers `400`. `replace_existing: true` removes the slot's old files once the new ones are placed. `202` with no body | Member |
| `POST` | `/books/{id}/search-now?kind=` | Search the monitored wanted slots in the background and grab the best release; `202 {queued}` is how many slots, `400` for an unknown `kind` | Member |
| `POST` | `/books/series` | Add a series by `hardcover_id` (optional `monitor` `all\|future\|none`, `quality_profile`). Two Hardcover requests on the HTTP request, the first 20 volumes hydrated and a stub for every other position; the answer has `hydrating: true` and the rest is filled in the background | Member |
| `GET` | `/books/series/{id}` | A series with its volumes, contributors, `edition` and the `editions` it can be switched to; `hydrating` is true while a volume is still a stub (clients poll) | Authenticated |
| `PATCH` | `/books/series/{id}` | `monitor` (applied to every volume's ebook slot), `quality_profile` (written through to every volume), `edition` (must be one of `editions`; re-picks only volumes without an ebook file) | Member |
| `DELETE` | `/books/series/{id}` | Remove a series with its volumes; `?delete_files=true` also deletes files | Member |
| `POST` | `/books/series/{id}/refresh-metadata` | Re-read the series and its volumes (`1 + ceil(N/20)` requests), adding volumes the skeleton gained | Member |
| `POST` | `/books/series/{id}/rename?preview=` | The rename plan over every volume | Member |
| `POST` | `/books/series/{id}/search-now` | Search the wanted volumes with one series query; `202 {queued}` | Member |
| `GET` `POST` | `/books/quality-profiles` | List / create book quality profiles | Authenticated / 🔒 Admin |
| `PUT` `DELETE` | `/books/quality-profiles/{name}` | Update / delete a book quality profile | 🔒 Admin |
| `POST` | `/books/quality-profiles/{name}/default?kind=novel\|bd\|comic\|manga` | Make this the default profile for one book kind (`kind` is required) | 🔒 Admin |

A book quality profile is `{name, upgrade_allowed, ebook: {formats, preferred}, audiobook: {formats, preferred, min_bitrate}, default_for}`: one profile covers both slots, `upgrade_allowed` is one switch for both, ebook `formats` come from `EPUB`, `AZW3`, `MOBI`, `PDF`, `CBZ`, `CBR` and audiobook `formats` from `M4B`, `MP3`, `M4A`, `FLAC` (upper case, best first), `preferred` must be one of the slot's `formats` and `min_bitrate` is kbps from `0` (no floor) to `1024`, else `422`. A book with no profile of its own takes the default of its kind, so `default_for` lists the kinds a profile is the default of; deleting a profile that is any kind's default is a `409`. A grab body also accepts `replace_existing: true`: every file of the grabbed slot is replaced after the new one is placed and verified, and the other slot is never touched.

A slot with no usable quality profile answers `422` with `code: no_quality_profile`, and a refused grab `422` with `code: grab_rejected`. The grab flips the slot to `downloading`. A book's profile is its own, else its series', else the default of its kind.

Books come from Hardcover, which needs `metadata.hardcover_api_key`. Without it the search, add and refresh endpoints answer `503` while browsing the library keeps working. There are no authors to follow: people are credits on a book or a series. Poster URLs are not in the payloads: clients build `/posters/books/{id}/poster.jpg` (a series uses `cover_id`, a book's own id), `/posters/authors/{author_id}/poster.jpg` for the author, writer and artist credits, and `/posters/lookup/books/{hardcover_id}/poster.jpg` for a lookup hit not in the library yet.

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

`character` belongs to the pairing, not to the person: the same actor carries a different one per title. An `id` no person row occupies is a `404`.

The series objects carry no `seasons` array — the credit query leaves the episode tree unloaded — but they do carry the same pre-aggregated rollup the series list sends (`total_seasons`, `have_episodes`, `total_episodes`, `wanted_episodes`, `downloading_episodes`, `importing_episodes`). Those counts are what a client badges a credit with, and sending the show without them is not the same as sending zeros: a client reading an absent count as zero labels a complete show as missing, which is exactly what the credits page did before they were added.

The `cast` array on a stored movie or series (`GET /movies/{id}`, `GET /series/{id}`) is served from the same credits, in billing order, and each entry carries `person_id` — the key to `/people/{id}`. A cast entry from a **provider lookup** of a title the library does not hold (`/movies/lookup/{tmdbId}`, `/series/lookup/{tvdbId}`, an expanded request row) has no person row behind it and so omits `person_id`; it still carries `person_url` to the provider's own page.

### Activity

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/activity` | Event feed (movies, episodes and series; filter with `?movie_id=` or `?series_id=`) | Authenticated |
| `GET` | `/activity/queue` · `/activity/history` | Queue and history views (history is cursor-paged and carries `total`, every terminal record) | Authenticated |
| `DELETE` | `/activity/queue/{id}` · `/activity/history/{id}` | Remove a queue or history entry | 🔒 Admin |
| `POST` | `/activity/queue/{id}/pause` · `/resume` · `/activity/history/clear-completed` | Pause/resume a download, or clear completed history | 🔒 Admin |
| `GET` | `/activity/pending` | List adoption proposals (`page`, `limit` 1–100, default 50; `total` counts them all) | Authenticated |
| `GET` | `/activity/pending/{id}/preview` | Preview a proposal | Authenticated |
| `POST` | `/activity/pending/{id}/import` · `/replace` · `/ignore` | Decide a proposal | 🔒 Admin |
| `POST` | `/activity/pending/{id}/identify` | Identify a proposal against metadata | 🔒 Admin |
| `DELETE` | `/activity/pending/{id}` | Forget a proposal so its torrent can be adopted again | 🔒 Admin |
| `POST` | `/downloads/{id}/resolve` | Release a held download | 🔒 Admin |
| `POST` | `/activity/history/{id}/retry` | Re-run a failed import — clears the attempt counter and re-queues the record (409 unless it failed) | 🔒 Admin |

### Requests

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `POST` | `/requests` | List / create requests. `?media_type=` takes a comma list (`book,book_series`; an unknown member is a `400`). A request is a `movie`, `tvshow`, `artist` (by `media_mbid`), `book` or `book_series` (by Hardcover `media_id`) | Any (scoped for `request_only`) |
| `GET` | `/requests/counts` · `/requests/{id}/metadata` | Counts, request metadata (an artist, book or series request answers the same detail as its lookup; `429` / `503` mean the provider is out of budget or unconfigured) | Any (scoped for `request_only`) |
| `POST` | `/requests/{id}/approve` | Approve a request with the reviewer's `quality_profile` (empty means the medium's default; a name outside the medium's profiles is a `422`; `429` with `Retry-After` when a book or series request hits Hardcover's limit, `503` without a Hardcover key; the request stays pending). An artist is added monitored `all`, a book `both`, a series `all`; the request turns `available` on the first imported album, or the first imported slot or volume | admin, member |
| `POST` | `/requests/{id}/deny` · `/reopen` | Deny or reopen a request | admin, member |

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
| `POST` | `/quality-profiles/{name}/default` | Make this the default profile. `?media=movie\|series` picks which; absent sets both | 🔒 Admin |

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
| `GET` | `/library/imports/{id}/files` · `/shows` · `/albums` · `/books` | List scanned rows | 🔒 Admin |
| `PATCH` | `/library/imports/{id}/files/{fileId}` · `/shows/{showId}` · `/albums/{albumId}` · `/books/{bookId}` | Update a row's match | 🔒 Admin |
| `POST` | `/library/imports/{id}/decisions` | Bulk decision | 🔒 Admin |
| `GET` `POST` | `/library/path-migration` | List / start a path migration | 🔒 Admin |
| `GET` | `/library/path-migration/roots` | List roots | 🔒 Admin |
| `POST` | `/library/path-migration/preview` | Preview a migration | 🔒 Admin |

An import scan's `kind` takes `movie`, `series`, `music` or `book`. `music` scans album folders against MusicBrainz and rejects `mode=rename` with `422`. `book` scans ebook and audiobook items against Hardcover; starting or committing one without a Hardcover API key answers `503`.

### Auth and users

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` `PATCH` | `/auth/me` | Fetch / update your profile | Any |
| `PUT` | `/auth/password` | Change your password | Any |
| `GET` `POST` | `/auth/me/api-keys` · `/auth/me/sessions` | List / create your API keys or sessions | Any |
| `DELETE` | `/auth/me/api-keys/{id}` · `/auth/me/sessions/{id}` | Revoke your own key or session | Any |
| `GET` `POST` `DELETE` | `/account/subsonic-password` | Read / generate-or-rotate / disable your Subsonic password | Any, session only |
| `GET` `POST` `DELETE` | `/account/opds-token` | Read / generate-or-rotate / disable your OPDS token | Any, session only |
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
| `GET` | `/schedules/events` | Server-sent events: the full list on connect and on every change (see [Scheduled Jobs](Scheduled-Jobs#live-updates)) | 🔒 Admin |
| `PATCH` | `/schedules/{name}` | Update a schedule | 🔒 Admin |
| `POST` | `/schedules/{name}/pause` · `/resume` · `/run` | Pause, resume, or run a schedule now | 🔒 Admin |
| `GET` | `/system/info` | Server info | 🔒 Admin |

### Calendar

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/calendar/upcoming?from=&to=` | Movie releases (digital, or theatrical when TMDB has no digital date — see `release_type`), episode air dates, monitored album releases (`albums`: `id`, `title`, `artist_id`, `artist_name`, `release_date`) and monitored book releases (`books`: `id`, `title`, `author_id`, `author_name`, `release_date`; either slot monitored) | Authenticated |

### Outside `/api/v1`

| Method | Path | What it does | Auth |
| --- | --- | --- | --- |
| `GET` | `/health` | Unauthenticated probe. Bare JSON, deliberately not in the spec | None |
| `POST` | `/auth/login` · `/auth/register` · `/auth/logout` | Cookie-based, `204` on success | None |
| `GET` | `/auth/config` · `/auth/invite/{token}` | Pre-auth SPA bootstrap | None |
| `GET` | `/auth/oidc/{name}/start` · `/callback` | The OIDC flow | None |
| `GET` | `/posters/{kind}/{id}/poster.jpg` | Poster proxy; `kind` is `movies`, `tvshows`, `artists`, `albums`, `books` or `authors` | — |
| `GET` | `/posters/lookup/{kind}/{key}/poster.jpg` | Art for a title that is not in the library yet: `kind` is `artists` or `albums` (a MusicBrainz id, a release-group id for albums) or `books` (a Hardcover book id). The server fetches it from a source it remembered while serving the lookup, so a tile listed before a restart answers `404` until its search runs again | Session |

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

**`hardcover_auth_warn`** on `GET /system/info` is `true` when Hardcover's most recent answer was HTTP 401, which means the configured token was rejected or has expired. The header pill turns amber and Settings → General shows a notice; the flag clears on the next successful Hardcover call. Absent when no 401 is outstanding.

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
  "matched_formats": ["remux", "hdr"],
  "indexer": "my-tracker",
  "indexer_private": true,
  "previously_grabbed_at": "2026-09-20T18:04:11Z"
}
```

</details>

Results are sorted `score` descending, ties broken by seeders — not by seeders alone. `rejected: true` releases (resolution outside the profile band, or score below `min_score`) are still returned with a `reject_reason`, so an operator can grab one deliberately; `score`/`rejected`/`reject_reason`/`matched_formats` are all ignored if sent back on a grab request body.

Every browse result also carries two history-and-source facts, both ignored on a grab body:

- **`indexer_private`** — the `private` setting of the configured indexer the release came through. For a tracker behind Prowlarr, `indexer` names that tracker but the flag is the Prowlarr entry's, since Prowlarr does not report privacy on a release. Absent only when that indexer has been removed from the config since.
- **`previously_grabbed_at`** — when this exact release was last grabbed for the queried item: the movie, or, for an episode, season or series search, any episode of the show. A release matches a download record on info hash when both carry one, and otherwise on title, case-insensitively — so a record whose hash differs is a different upload even under the same name. Failed grabs count; adoption proposals (pending or dismissed) do not. Absent when it was never grabbed.

A result's `download_url` is **not** the indexer's link: that link carries the indexer's API key, so search responses return an opaque handle (`slr1.…`) instead, and `info_url` comes back with its query string stripped. Send the result back unchanged to grab it. Handles stop working when the session secret rotates, so search again after a rotation. A grab body may still carry a plain magnet or a link to one of your configured indexers.

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
