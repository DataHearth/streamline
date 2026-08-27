# Configuration Reference

Every configuration key, its default, and where it can be changed from.

- [Sources and precedence](#sources-and-precedence)
- [Environment variables](#environment-variables)
- [Secrets](#secrets)
- [What's editable at runtime](#whats-editable-at-runtime)
- [CLI](#cli)
- [Reference](#reference)
  - [Top level](#top-level) · [server](#server) · [auth](#auth) · [library](#library) · [schedules](#schedules) · [metadata](#metadata) · [ffmpeg](#ffmpeg) · [log](#log) · [otel](#otel) · [events](#events)
  - [media_server](#media_server) · [download_clients](#download_clients) · [indexers](#indexers) · [quality_profiles](#quality_profiles) · [custom_formats](#custom_formats)

---

## Sources and precedence

Configuration is assembled by [koanf](https://github.com/knadh/koanf) from three layers, later overriding earlier:

1. **Built-in defaults** — every key has one
2. **The config file** — YAML, at `--config` / `-c`
3. **Environment variables** — `STREAMLINE_`-prefixed

Every key is optional. An unset key falls back to its default, so a minimal config file is legitimate — you only need to state what you're changing.

Generate a file containing every key at its default:

```bash
streamline config init --output ~/.config/streamline/config.yaml
```

Validate one before restarting into it:

```bash
streamline config validate --config ~/.config/streamline/config.yaml
```

`config validate` also reads from stdin, which makes it usable in CI.

---

## Environment variables

Prefix `STREAMLINE_`. **A double underscore (`__`) is the path separator; a single underscore is literal.** That distinction is what keeps keys with underscores in their names reachable.

| Config key | Environment variable |
| --- | --- |
| `log.app.level` | `STREAMLINE_LOG__APP__LEVEL` |
| `auth.session_secret` | `STREAMLINE_AUTH__SESSION_SECRET` |
| `auth.seed_admin.password` | `STREAMLINE_AUTH__SEED_ADMIN__PASSWORD` |
| `metadata.tmdb_api_key` | `STREAMLINE_METADATA__TMDB_API_KEY` |
| `otel.endpoint` | `STREAMLINE_OTEL__ENDPOINT` |
| `library.import_mode` | `STREAMLINE_LIBRARY__IMPORT_MODE` |

Arrays (`indexers`, `download_clients`, `auth.oidc`, `quality_profiles`, `custom_formats`) can't be expressed sensibly as environment variables. Put them in the file and use [`_file` secret references](#secrets) for the sensitive parts.

One non-prefixed variable is also read: **`STREAMLINE_PUBLIC_URL`** sets the canonical external base URL, used for OIDC redirect URIs and invite links. Without it, Streamline derives a base from `http://<server.host>:<server.port>`.

### torrent_listen_port

A top-level key that overrides the built-in download client's own `listen_port`:

```yaml
torrent_listen_port: 61847
```

It is top-level, rather than another field on the `download_clients` entry, so that the environment can reach it — a single underscore is literal and `__` is the path separator, so **`STREAMLINE_TORRENT_LISTEN_PORT`** names it exactly, while nothing *inside* `download_clients[]` is addressable at all.

That matters for one situation: peering through a commercial VPN that assigns a **forwarded port per session**. Such a port rotates on every reconnect or server change, so it cannot be written into a config file that git owns and mounts read-only.

```yaml
# gluetun sidecar, on every port reassignment
VPN_PORT_FORWARDING: "on"
VPN_PORT_FORWARDING_UP_COMMAND: '/bin/sh -c "... {{PORT}} ..."'
```

Behaviour worth knowing:

- **It wins wherever it is set.** The entry's own `listen_port` is ignored — a forwarded port is the only value that can be right, and a file naming a different one is stale by construction. Leave it at `0` (the default) to use the entry's value.
- **It is read once, at startup.** Changing it needs the process restarted; the engine builds its client config during wiring and does not rebind afterwards. A rotation therefore costs one container restart, which is what a gluetun UP command should trigger.
- **It is validated like any port.** A value outside 1–65535 fails config validation at boot rather than being silently ignored.
- **It applies to the built-in engine only.** External clients (qBittorrent, Transmission, Deluge) manage their own listening port; Streamline never sets it for them.

Without a forwarded port, peering is outbound-only: downloads work, uploads stay at zero because nothing can open a connection to you.

---

## Secrets

Every secret-bearing key has a `_file` twin that reads the value from a path instead. The file's contents are trimmed of surrounding whitespace. When both are set, **the file wins**.

| Inline | File |
| --- | --- |
| `auth.session_secret` | `auth.session_secret_file` |
| `auth.seed_admin.password` | `auth.seed_admin.password_file` |
| `auth.oidc[].client_secret` | `auth.oidc[].client_secret_file` |
| `metadata.tmdb_api_key` | `metadata.tmdb_api_key_file` |
| `metadata.tvdb_api_key` | `metadata.tvdb_api_key_file` |
| `indexers[].api_key` | `indexers[].api_key_file` |
| `download_clients[].password` | `download_clients[].password_file` |
| `download_clients[].api_key` | `download_clients[].api_key_file` |
| `media_server.servers[].api_key` | `media_server.servers[].api_key_file` |

This is what makes Streamline work cleanly with Docker secrets, SOPS, sealed-secrets and Vault Agent — the config file stays in git, the values arrive as mounted files.

### Values Streamline generates for itself

Two values are generated on first boot and **written back into your config file** if they're empty:

- **`auth.session_secret`** — the JWT HMAC signing key. Regenerating it invalidates every session.
- **`media_server.plex_client_id`** — the `X-Plex-Client-Identifier` this instance presents.

A third, `auth.seed_admin.password`, is generated and persisted only when you asked for a seeded admin without supplying a password.

With no writable config file (a `:ro` mount, `read_only: true`, or no file at all) the session secret falls back to an **ephemeral** value, regenerated at every start — meaning everyone is logged out on each restart. For any deployment where the config isn't writable, supply `auth.session_secret` explicitly. See [GitOps and Kubernetes](GitOps-and-Kubernetes).

---

## What's editable at runtime

Some config is hot — changed through the UI or API, applied immediately, persisted back to the file. The rest requires an edit and a restart.

| Area | Runtime-editable? |
| --- | --- |
| Indexers, download clients, media servers, quality profiles | ✅ Full CRUD |
| Schedule intervals, pause/resume/run | ✅ |
| `auth.registration_mode`, `auth.session_ttl`, `auth.oidc_default_role` | ✅ |
| `library.monitor_specials` | ✅ |
| `library.probe.*` | ✅ Applies to the next import — see [Import verification](#import-verification) |
| `ffmpeg.enabled` | ✅ |
| `ffmpeg.path` | ⚠️ Accepted immediately, but only picked up by the process's prober on the next restart |
| `download.selective_files` | ✅ |
| OIDC providers | ⚠️ CRUD works, but only loaded at startup — restart required |
| Everything else | ❌ File only, restart required |

Notably **not** runtime-editable: all library paths, `import_mode`, `data_dir`, server host/port, metadata keys, logging, OTel, and lockout thresholds.

Setting `read_only: true` refuses every runtime write, turning the first two rows into ❌ as well.

---

## CLI

```
streamline [global options] [command]

GLOBAL OPTIONS
  --config, -c <path>    path to config file
  --version, -v          print version
```

| Command | Purpose |
| --- | --- |
| `config init [--output <path>]` | Write a default config to stdout or a file |
| `config validate [--config <path>]` | Load a config (or stdin) and report errors |
| `auth unlock <email>` | Clear lockout state on an account |

Running `streamline` with no command starts the server.

---

## Reference

Defaults shown are the built-in ones, as emitted by `streamline config init`.

### Top level

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `data_dir` | string | `./data` | Runtime data (SQLite DB, posters). **Must already exist.** Pin it to an absolute path in containers |
| `read_only` | bool | `false` | Reject all runtime config write-backs. For GitOps deploys |
| `torrent_listen_port` | int | `0` | Overrides the builtin download client's `listen_port`. Top-level so `STREAMLINE_TORRENT_LISTEN_PORT` can reach it — see [torrent_listen_port](#torrent_listen_port) |
| `quality_default_profile` | string | `default` | Profile used when an item names none |

### server

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `server.host` | string | `0.0.0.0` | Bind address |
| `server.port` | int | `8080` | 1–65535 |

### auth

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `auth.mode` | enum | `full` | `full` \| `trusted-network` \| `disabled` — see [Authentication and SSO](Authentication-and-SSO#auth-modes) |
| `auth.trusted_networks` | []cidr | `[]` | CIDRs auto-authenticated when mode is `trusted-network` |
| `auth.trusted_role` | enum | `admin` | Role granted to trusted-network requests |
| `auth.session_secret` | string | *generated* | JWT HMAC key |
| `auth.session_secret_file` | path | — | Mutually exclusive with the above |
| `auth.session_ttl` | duration | `168h` | Session lifetime |
| `auth.registration_mode` | enum | `disabled` | `disabled` \| `open` \| `invite` |
| `auth.oidc_default_role` | enum | `member` | Role for users auto-created via OIDC |
| `auth.seed_admin.email` | string | `""` | Bootstrap admin. No-op once any user exists |
| `auth.seed_admin.password` | string | `""` | Generated and persisted if left empty |
| `auth.seed_admin.password_file` | path | `""` | Wins over `password` |
| `auth.lockout.threshold` | int | `10` | Failed logins before an account locks |
| `auth.lockout.window` | duration | `15m` | Window those failures are counted over |
| `auth.lockout.duration` | duration | `15m` | How long the lock lasts |
| `auth.oidc[]` | array | `[]` | See [Authentication and SSO](Authentication-and-SSO#oidc) |

Independently of `auth.lockout`, login and registration are rate-limited per IP at **5 attempts / 15 minutes**. That limit is not configurable.

### library

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `library.movie_path` | path | `/media/movies` | Movie library root |
| `library.series_path` | path | `/media/series` | TV library root |
| `library.download_path` | path | `/downloads` | Where Streamline reads finished torrents from. Combined with the torrent name: `<download_path>/<torrent.Name>` |
| `library.movie_naming` | template | `{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}` | See [Quality Profiles and Naming](Quality-Profiles-and-Naming#file-naming) |
| `library.series_naming` | template | `{title} ({year})/Season {season}/{title} - S{season:2}E{episode:2} - {episode_title} [{quality}].{ext}` | |
| `library.import_mode` | enum | `hardlink` | `hardlink` \| `copy` \| `move` |
| `library.monitor_specials` | bool | `false` | Monitor season 0 on add/discovery. **Runtime-editable** |
| `library.probe.always_ask` | bool | `false` | Hold every import for manual approval instead of importing straight away. Needs no ffprobe. **Runtime-editable** |
| `library.probe.min_duration_ratio` | float | `0.5` | Hold an import when the probed duration falls below this share of the expected runtime — the check for sample clips and truncated remuxes. A ratio, not a percentage: `0.5` is half. Greater than 0, at most 1. **Runtime-editable** |
| `library.no_match_cooldown` | duration | `6h` | Quiet period after a search finds nothing acceptable |
| `library.max_grab_failures` | int | `3` | Consecutive failures before an item is marked failed |
| `library.keep_torrent_seeding` | bool | `true` | Leave torrents seeding after import |
| `library.import_max_attempts` | int | `3` | Import retries before giving up |
| `library.allowed_download_roots` | []path | `[]` | If non-empty, a torrent's save path must sit under one of these or import is refused. Security fence — empty disables the check |
| `library.drift_grace_ticks` | int | `3` | Consecutive `drift_check` ticks a file may be missing before its record is deleted (1–20). At the default 15m interval, 3 ticks ≈ 45 minutes of tolerance for a flaky mount |

#### Import verification

When ffprobe is available (see [`ffmpeg`](#ffmpeg)), Streamline checks a finished
download against what the release claimed *before* moving anything into the
library. A file that fails is not imported and not discarded: the record moves
to `held` and waits for you in Activity → Queue, with one reason per failed
check.

| Check | Holds when |
| --- | --- |
| `corrupt` | ffprobe cannot read the file (reported alone — nothing else is knowable) |
| `resolution` | The probed resolution is *below* what the release name claimed. Classified by width, so a 1920×800 scope film still counts as 1080p. Higher than claimed never holds |
| `duration` | Probed duration is under `library.probe.min_duration_ratio` × the title's runtime. Skipped when the runtime is unknown |
| `codec` | The profile's `allowed_codecs` is non-empty and the probed video codec is not in it |
| `always_ask` | `library.probe.always_ask` is on and nothing else objected |

A season pack is verified whole: any bad file holds the entire pack before any
file is moved. Resolve a hold from the UI, or with
`POST /api/v1/downloads/{id}/resolve` — see
[REST API](REST-API#resolving-a-held-download).

With ffmpeg disabled or the binary missing, only `always_ask` can hold anything;
every other check is skipped and imports behave as before.

### schedules

Go duration strings. All are runtime-editable, pausable and runnable on demand — see [Scheduled Jobs](Scheduled-Jobs).

| Key | Default | | Key | Default |
| --- | --- | --- | --- | --- |
| `schedules.download_monitor` | `30s` | | `schedules.movie_orphan_scan` | `6h` |
| `schedules.import_scan` | `60s` | | `schedules.tv_orphan_scan` | `6h` |
| `schedules.movie_rss_sync` | `15m` | | `schedules.drift_check` | `15m` |
| `schedules.tv_rss_sync` | `15m` | | `schedules.cleanup` | `24h` |
| `schedules.movie_missing_search` | `12h` | | `schedules.movie_metadata_refresh` | `24h` |
| `schedules.tv_missing_search` | `12h` | | `schedules.tv_metadata_refresh` | `24h` |
| `schedules.media_probe` | `15m` | | `schedules.file_selection` | `5s` |

**Deprecated aliases**, still honoured with a warning at boot: `rss_sync` (→ `movie_rss_sync`), `missing_search`, `metadata_refresh` and `orphan_scan` (each → both the `movie_*` and `tv_*` keys).

### metadata

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `metadata.tmdb_api_key` | string | `""` | **Required for movies.** No key, no movie search |
| `metadata.tvdb_api_key` | string | `""` | **Required for TV.** |
| `metadata.language` | BCP-47 | `en` | Empty lets the provider pick its own default |
| `metadata.tmdb_region` | ISO 3166-1 α-2 | `FR` | Uppercase. Drives which country's digital release dates feed the calendar — set it to yours |

Both keys have `_file` twins.

### ffmpeg

Backs the media probe feature: technical details (resolution, codecs, duration, bitrate) read from your files with `ffprobe` and shown as `media_info` on movies and episodes. See [REST API](REST-API#media-probe).

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `ffmpeg.enabled` | bool | `true` | Turns probing off entirely. **Runtime-editable** |
| `ffmpeg.path` | path | `""` | A **directory** holding the `ffmpeg`/`ffprobe` binaries — not a binary path. Empty resolves via `$PATH`. Read once at boot; changing it needs a restart |

Missing binaries (or `enabled: false`) degrade gracefully — imports and library scans work exactly as they did before this feature existed, just without `media_info`. Nothing errors at boot. `GET /api/v1/system/info` surfaces `ffmpeg_warn: true` when probing is enabled but ffprobe wasn't found; the official Docker image ships the binaries, so this only bites custom builds or `path` misconfiguration.

### log

Two independent loggers: `log.app` (application) and `log.http` (access log).

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `log.app.enabled` | bool | `true` | |
| `log.app.level` | enum | `info` | `debug` \| `info` \| `warn` \| `error` |
| `log.app.format` | enum | `text` | `text` \| `json` |
| `log.app.output` | string | `stderr` | `stderr`, an absolute path, or a path relative to `data_dir` |
| `log.http.enabled` | bool | `true` | |
| `log.http.format` | enum | `json` | `json` \| `combined` (combined uses RFC3339 timestamps, not the Apache format) |
| `log.http.output` | string | `stderr` | As above |

Both take a `rotate` block, applied when output is a file path:

| Key | Default |
| --- | --- |
| `rotate.max_size_mb` | `100` |
| `rotate.max_backups` | `5` |
| `rotate.max_age_days` | `30` |
| `rotate.compress` | `true` |

### otel

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `otel.endpoint` | string | `""` | OTLP endpoint. Empty disables export entirely |

The OTel SDK defaults to HTTPS. For a plaintext collector, set `OTEL_EXPORTER_OTLP_INSECURE=true`. See [Observability and Logging](Observability-and-Logging).

### events

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `events.retention` | duration | `2160h` | 90 days. How long activity events are kept |

### media_server

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `media_server.plex_client_id` | string | *generated* | `X-Plex-Client-Identifier` |
| `media_server.servers[]` | array | `[]` | |

Per entry:

| Field | Required | Notes |
| --- | --- | --- |
| `name` | ✅ | Unique key; the API addresses servers by it |
| `server_type` | ✅ | `plex` \| `jellyfin` \| `emby` |
| `host` | ✅ | Base URL |
| `api_key` / `api_key_file` | | Plex uses the PIN flow instead |
| `enabled` | | |
| `library_section` | | Plex section key holding movies |
| `library_section_tv` | | Plex section key holding TV |

Both section keys are Plex-only. Leave them unset and Streamline looks the section up by matching your library path against the paths Plex reports — which only works when Plex sees the library at the same path Streamline does. In Docker or Kubernetes it usually doesn't (Streamline's `/srv/streamline/movies` is Plex's `/data/movies`), so the lookup misses and Streamline falls back to rescanning **every** section. That works, but it is a bigger scan than you need: set the two keys to scope it. Settings → Media Servers lists the available sections (`POST /api/v1/media-servers/discover`).

### download

Governs [selective file download](First-Run-Setup#selective-file-download) — grabbing an episode-scoped pack downloads only the files that episode needs.

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `download.selective_files` | bool | `false` | Off is bit-for-bit today's whole-torrent grab, and the rollback path. **Runtime-editable** |
| `download.selection_grace` | duration | `10m` | How long a magnet-sourced selection may sit unresolved before giving up and downloading the release whole |

### download_clients

| Field | Required | Notes |
| --- | --- | --- |
| `name` | ✅ | |
| `client_type` | ✅ | `qbittorrent` \| `transmission` \| `deluge` \| `builtin` |
| `host`, `port`, `auth_method` | ✅ unless `builtin` | `auth_method`: `password` \| `api_key` |
| `username`, `password`/`password_file`, `api_key`/`api_key_file` | | Per `auth_method` |
| `use_ssl` | | |
| `priority` | | 0–255, lower is tried first |
| `enabled` | | |

Built-in engine only (ignored for external clients):

| Field | Required | Notes |
| --- | --- | --- |
| `download_dir` | ✅ for `builtin` | Where the engine writes |
| `listen_port` | | Incoming BitTorrent port. Overridden by [`torrent_listen_port`](#torrent_listen_port) when that is set — required if your VPN assigns a forwarded port per session, since such a port can't live in a file |
| `max_upload_kbps`, `max_download_kbps` | | `0` = unlimited |
| `seed_ratio` | | Stop seeding at this ratio. Uploaded bytes are persisted per torrent and accumulate across restarts, so a restart doesn't hand a torrent back its ratio |
| `seed_time` | | Stop seeding after this duration, measured from the persisted completion time |
| `disable_dht` | | |
| `bind_interface` | | Bind to one interface — useful for a VPN tunnel |

### indexers

| Field | Required | Notes |
| --- | --- | --- |
| `name` | ✅ | |
| `host`, `port` | ✅ | |
| `protocol` | ✅ | `torznab` \| `prowlarr` |
| `path` | | Torznab endpoint path |
| `api_key` / `api_key_file` | | |
| `use_ssl` | | |
| `priority` | | 0–255, lower first |
| `enabled` | | |

### quality_profiles

| Field | Required | Notes |
| --- | --- | --- |
| `name` | ✅ | Referenced by `quality_default_profile` and per-title |
| `preferred_resolution` | ✅ | `720p` \| `1080p` \| `2160p` — hard ceiling of the accepted band |
| `min_resolution` | ✅ | Same set — hard floor |
| `upgrade_allowed` | | Whether a file already on disk can be replaced by a higher-scoring release. See [Quality Profiles and Naming](Quality-Profiles-and-Naming) |
| `allowed_codecs` | | ffprobe codec names (`hevc`, `av1`, `h264`, `vp9`, `mpeg2video`). Empty — the default — means any codec. A finished download whose video codec isn't listed is [held](#import-verification) for a decision rather than imported |
| `formats` | | `[{name, score}]` — custom formats (built-in or `custom_formats`) scored for this profile. See [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats) |
| `min_score` | | Minimum total matched-format score a release needs to be grabbed. Default `0` |
| `upgrade_until_score` | | Stop upgrading once the current file's score reaches this value. `0` (default) means no cap |

One profile named `default` (1080p/1080p, upgrades allowed, no formats) ships out of the box.

> **Configuring more than nothing:** with *no* profiles configured at all, every release is rejected. Grabbing at an unknown quality bar is treated as worse than grabbing nothing.

### custom_formats

| Field | Required | Notes |
| --- | --- | --- |
| `name` | ✅ | Must not collide with a built-in format name |
| `description` | | Optional free text, shown on the format's row and as a hint wherever it's scored. No effect on matching |
| `conditions` | ✅ | At least one `{type, ...}` condition. Full type reference and matching semantics: [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats) |

Ten formats (x265, x264, av1, remux, hdr, resolution tiers, multi-audio, dubbed) ship compiled into the binary and need no config entry — `custom_formats` is only for your own. They all *describe* a release and none of them judges one: group blocklists and rip-source opinions are preference, so they're yours to write here rather than ours to ship.
