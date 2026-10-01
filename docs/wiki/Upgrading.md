# Upgrading

Most releases need nothing from you: replace the binary or bump the image tag, restart, and the database migrates itself on boot. This page lists the releases that **do** need something — a renamed key, a default that changed, a behaviour you may have been relying on.

**Read every section between the version you run and the one you're moving to, oldest first.** A jump from 1.3.0 to 3.2.0 crosses 2.0.0, 3.0.0 and 3.1.0, and each one's steps still apply.

Before any upgrade, back up `data_dir`. Migrations only run forward: a database a newer release has migrated is not guaranteed to open on an older one, so the backup is your only way back.

| Release | What needs your attention |
| --- | --- |
| [3.1.0](#310) | `log.app.enabled: false` no longer stops traces and metrics |
| [3.0.0](#300) | `auth.oidc_default_role` renamed, imports verified against the release, score-ranked selection with a hard resolution ceiling, `imdb_id` gone from the API |
| [2.0.0](#200) | OIDC account linking and admin grants closed by default, `server.trusted_proxies`, `auth.trusted_role` defaults to `member`, scheduled jobs renamed, seed admin password no longer written back |
| [Helm chart 2.0.0](#helm-chart-200) | `image.tag` is required |

The Helm chart is versioned separately from Streamline; its own changes are [at the end](#helm-chart). The full list of changes for each release is in the [changelog](https://github.com/datahearth/streamline/blob/main/CHANGELOG.md).

---

## 3.1.0

### `log.app.enabled: false` disables the stderr sink only

Before 3.1.0, `log.app.enabled: false` returned early and switched off the **whole** observability pipeline: traces, metrics and OTLP logs stopped too, whatever `otel.endpoint` said. It now only silences the local stderr logger, and the OTel pipeline is controlled by `otel.endpoint` alone.

If you turned app logging off *in order to* stop exporting telemetry, clear `otel.endpoint` instead. Otherwise nothing to do — an instance that had quietly stopped exporting starts again. See [Observability and Logging](Observability-and-Logging).

---

## 3.0.0

### `auth.oidc_default_role` is now `auth.default_role`

The key now covers both self-registration paths, local and SSO, and was renamed to match. **The old name is no longer read**, and there is no fallback: if your config still sets `auth.oidc_default_role`, the value is ignored and new self-registered accounts land on `member`, with nothing in the logs to say so. Rename the key.

The API is the same clean break: `GET /api/v1/config/auth` returns only `default_role`, and a `PATCH` naming `oidc_default_role` is ignored as an unknown field. See [Authentication and SSO](Authentication-and-SSO#which-role-a-new-account-gets).

### Finished downloads are verified before they are imported

3.0.0 added the ffprobe-backed media probe. With `ffmpeg.enabled: true` (the default) and `ffprobe` found — which the official Docker image always ships — every finished download is probed **before** it is moved, and one that is unreadable, whose real resolution is below what the release name claimed, whose runtime falls well short of the title's, or whose codec the profile's `allowed_codecs` excludes is **held** instead of imported. A held download waits for you in Activity → Queue: import it anyway, or delete it and search again.

Expect a few held entries after the upgrade, mostly releases that overstate their resolution. Tune or relax the checks under [`library.probe`](Configuration-Reference#import-verification), or turn probing off with `ffmpeg.enabled: false` to import on the release name alone, as before. Files already in your library are not re-judged; the `media-probe` job only backfills their media info.

### Release selection is score-ranked, and `preferred_resolution` is a hard ceiling

Release selection moved from "first release that passes" to score-then-select. Three things change for an existing install:

1. **The best release is picked by score, then seeders.** Previously the first match won — whichever the indexer listed first. A profile with no `formats` still benefits: it becomes seeders-ranked instead of first-hit.
2. **A profile with `upgrade_allowed: false` and `min_resolution` below `preferred_resolution` can now grab anywhere in that band.** Before, "upgrades off" meant "accept only exactly `preferred_resolution`". Now it means "accept the whole band, just don't replace a file already there".
3. **`preferred_resolution` is a hard ceiling everywhere a profile is evaluated**, including the RSS and missing-search jobs, not just interactive search.

> [!WARNING]
> If your library already has files above a profile's `preferred_resolution`, review your profiles before relying on automatic search: grabbing above the ceiling silently stops until the profile is raised.

See [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats).

### `imdb_id` is gone from the API

`LookupDetail` and `RequestMediaDetails` no longer carry `imdb_id`, and the `{imdb_id}` naming token was removed. The token was never populated, so it always rendered empty and no file name on disk changes. An API client that read `imdb_id` should key on `tmdb_id` (movies) or `tvdb_id` (series).

### The dashboard lives at `/`

`/dashboard` was dropped; the dashboard is the root route. Update any bookmark pointing at the old path.

---

## 2.0.0

### OIDC: account linking and admin grants are closed by default

Two defaults changed, both closing something earlier releases left open. Both are per-provider keys on `auth.oidc[]` and file-only, so they cannot be changed from the UI. See [Account linking](Authentication-and-SSO#account-linking) and [Role mapping](Authentication-and-SSO#role-mapping) for what each one governs.

**Adoption.** Earlier releases adopted a matching account unconditionally, and `email_linking` defaults to `disabled`. Any user whose local account was being reached that way — rather than by an identity already linked from a previous SSO login — starts failing at the login screen with *"An account already uses this email address. Sign in with your password, then ask an administrator to enable account linking for this provider."* (`oidc_link_not_allowed`). To bind those identities, open the provider up for one pass and close it again:

1. set `email_linking: non_admin` on the provider and restart;
2. have each affected user sign in through SSO once — that login links the identity permanently;
3. set `email_linking: disabled` again and restart.

Admin accounts are not covered by `non_admin`. Either move them during a maintenance window with `email_linking: all`, or leave them on password login. The pass does not touch roles: an adoption never writes one, `allow_admin` alone decides how high a role can go, and none of the three steps changes it.

Keep the pass short. Streamline has no unlink: the only way to undo a binding made while the provider was open is to delete the user.

**Roles.** `allow_admin` defaults to `false`, so a provider whose `role_mapping` grants `admin` stops doing so until you set it. An existing admin whose claims map only to `admin` keeps the role — the barred mapping is dropped, not downgraded, so nothing is written. One whose claims *also* map to a lower role is demoted to it on the next login, that being the highest role the provider may now confer. Set `allow_admin: true` on the providers you want back in charge of admin.

Nothing else changes for users who already signed in through SSO, or for password-only installs.

### `X-Forwarded-For` is believed only from `server.trusted_proxies`

`server.trusted_proxies` is new, and it defaults to empty: no peer is trusted. Earlier releases trusted the last `X-Forwarded-For` entry unconditionally. Until you set it, an install behind a reverse proxy attributes every request to the proxy's own address:

- access logs and the IP shown on each session record read as the proxy;
- the login rate limit keys on the proxy, so **all** users behind it share one 5-attempt / 15-min budget and lock each other out;
- `auth.mode: trusted-network` stops recognising LAN clients, because the address compared against `auth.trusted_networks` is now the proxy's;
- `X-Forwarded-Proto` is ignored too, so behind a TLS-terminating proxy logins loop (no `Secure` cookie) and OIDC sends an `http://` `redirect_uri` your IdP rejects.

Set `server.trusted_proxies` as part of the upgrade if a proxy sits in front, listing the proxies themselves as narrowly as you can — ideally one `/32` each, never a client subnet. Direct-to-binary installs need no change. See [Configuration Reference](Configuration-Reference#server).

### `auth.trusted_role` defaults to `member`

It used to default to `admin`, so under `auth.mode: trusted-network` every request from a trusted network was an unauthenticated admin. If you relied on that default, those requests are now `member`. Set `auth.trusted_role: admin` explicitly only if you really mean it — and with `server.trusted_proxies` set correctly, since a forged source address is then all it takes. See [Authentication and SSO](Authentication-and-SSO).

### The generated seed admin password is no longer written to your config

Earlier releases saved the generated admin password into `auth.seed_admin.password`. Streamline no longer reads or rewrites that value once the admin exists. It's your file: delete the leftover plaintext yourself, and rotate the password if anything else could read that file.

### Scheduled jobs are split by media type

Every media-scoped job is now keyed `movie-*` / `tv-*`:

| Old name | New name |
| --- | --- |
| `rss-sync` | `movie-rss-sync` |
| `missing-search` | `movie-missing-search` |
| `metadata-refresh` | `movie-metadata-refresh` |
| `orphan-scan` | `movie-orphan-scan` |
| `series-orphan-scan` | `tv-orphan-scan` |

`tv-rss-sync`, the feed scanner for wanted episodes, is new in this release.

The database is migrated for you. The old `schedules.*` config keys (`schedules.rss_sync`, `schedules.missing_search`, `schedules.metadata_refresh`, `schedules.orphan_scan`) are still honoured with a warning at boot, and one that drove both libraries is applied to both new keys; rename them to silence the warning. **`/api/v1/schedules/{name}` paths change**, so update any script or cron calling them by the old name. See [Scheduled Jobs](Scheduled-Jobs).

---

## Helm chart

### Helm chart 2.0.0

`image.tag` is required: the chart no longer falls back to its `appVersion`, so it never picks a Streamline version for you. An install or upgrade without it fails with `image.tag is required`. Pin the app release you want with `--set image.tag=X.Y.Z` and bump it yourself to upgrade. See [Installation](Installation#kubernetes--helm).

Chart 2.0.0 ships alongside app 2.0.0, and chart 2.1.0 alongside app 3.0.0: the app sections above apply to values you set under the chart's `config:` block too.
