# Streamline

[![CI](https://github.com/datahearth/streamline/actions/workflows/ci.yaml/badge.svg)](https://github.com/datahearth/streamline/actions/workflows/ci.yaml)
[![Image](https://github.com/datahearth/streamline/actions/workflows/image.yaml/badge.svg)](https://github.com/datahearth/streamline/actions/workflows/image.yaml)
[![Release](https://img.shields.io/github/v/release/datahearth/streamline)](https://github.com/datahearth/streamline/releases/latest)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/datahearth/streamline)](go.mod)

Self-hosted media manager for movies, TV, music and books. Replaces the \*arr stack (Radarr, Sonarr, Lidarr, Readarr) and Seerr with a single binary.


https://github.com/user-attachments/assets/f08f3d32-3172-4de7-afeb-163a484403ce


|  |  |
| --- | --- |
| [![Dashboard](docs/assets/dashboard.png)](docs/assets/dashboard.png) | [![Activity](docs/assets/activity-queue.png)](docs/assets/activity-queue.png) |
| **Dashboard** — the whole library at a glance | **Activity** — live progress, speed and ETA per download |

*[More screenshots](#screenshots) at the bottom.*

## Features

- One library for movies, TV, music and books — see the [roadmap](docs/wiki/Roadmap.md) for what is shipped and what is next
- **Music**: MusicBrainz metadata, album and discography-pack grabs, tags and artwork written into your files, and a built-in Subsonic/OpenSubsonic server, so any Subsonic app streams the library
- **Books**: Hardcover metadata, books and series with an ebook and an audiobook slot each, an OPDS catalog for e-reader apps, and send-to-Kindle by email
- Adopt an existing library: scan untracked files and import them under review — and torrents you added by hand
- Upload albums, discographies and books from the browser, imported like a finished download — no access to the server's disk needed
- Monitoring, RSS auto-grab, manual search, and a calendar of upcoming releases
- Quality profiles with custom-format scoring and upgrades, import verification, activity queue and history
- Multi-user with SSO (OIDC), invites, API keys
- Built-in request system (Seerr replacement) for every media type
- Indexers: Torznab, Prowlarr
- Torrent clients: qBittorrent, Transmission, Deluge, or the built-in BitTorrent engine — no external client needed; optionally download only the files a pack is wanted for
- Media server notifications and deep links: Plex, Jellyfin, Emby
- Optional transcoding to a quality profile's rules, verified before it replaces a file, with VAAPI hardware encoding
- Database backups before every upgrade, daily and from the CLI, with `streamline restore`
- Installable web app (PWA), in English and French
- REST API (OpenAPI 3.0 spec)
- OpenTelemetry traces, metrics, logs
- GitOps-friendly: `read_only` config mode rejects runtime config writes
- CGO-free SQLite — single binary, no database server, no runtime, nothing to install alongside it
- Optional `ffprobe` and `ffmpeg`, each on its own: `ffprobe` unlocks media info and import verification, `ffmpeg` transcoding and Subsonic transcoding; without them Streamline runs exactly as before, just without those ([details](#optional-ffmpeg-and-ffprobe))

## Quick start (Docker Compose)

```yaml
services:
  streamline:
    image: ghcr.io/datahearth/streamline:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes:
      - ./data:/data
      # Writable: config.yaml is written here on first start, and the generated
      # session secret and every change saved from Settings go back into it.
      - ./config:/etc/streamline
      # ONE mount holding both your library and the folder your torrent client
      # finishes downloads in. Imports hardlink by default (library.import_mode,
      # `copy` and `move` also work), and a hardlink cannot cross two mounts,
      # even two of the same disk.
      - /srv/data:/srv/data
    environment:
      # Written into config.yaml on first start.
      STREAMLINE_LIBRARY__DOWNLOAD_PATH: /srv/data/downloads
      STREAMLINE_LIBRARY__MOVIE_PATH: /srv/data/media/movies
      STREAMLINE_LIBRARY__SERIES_PATH: /srv/data/media/series
      STREAMLINE_LIBRARY__MUSIC_PATH: /srv/data/media/music
      STREAMLINE_LIBRARY__EBOOK_PATH: /srv/data/media/books
      STREAMLINE_LIBRARY__AUDIOBOOK_PATH: /srv/data/media/audiobooks
```

```bash
mkdir -p config data   # both must be writable by uid 1000, the image's user
docker compose up -d
```

Open http://localhost:8080.

Your torrent client needs the same folder mounted at the same path, or the paths it reports won't resolve inside Streamline's container. A setting the environment supplies can't be changed from Settings; once the first start has written the paths into `config/config.yaml`, drop the variables to edit them there instead.

### First login

An admin account is seeded on first boot. Set `auth.seed_admin.email` and `auth.seed_admin.password` (or `password_file`) in the config beforehand to choose the credentials.

If you don't, Streamline creates `admin@streamline.local` with a generated password and prints it **once**, to stdout, on a single line mentioning `default admin`. Capture it before anything else scrolls past:

```bash
docker compose logs streamline | grep 'default admin'                            # Compose
docker logs streamline 2>&1 | grep 'default admin'                              # Docker
kubectl -n streamline logs deploy/streamline-streamline | grep 'default admin'  # Helm
```

Copy it right then and change it from Settings. It is never written to your config file and never passed to the log pipeline, so if you lose it the only way back is to wipe the data dir and re-seed — `auth.seed_admin` is applied against an empty database and ignored afterwards. Anything scraping container stdout captured it too, so treat it as compromised until you rotate it.

Streamline does not read or rewrite `auth.seed_admin.password` once the admin exists. If your file still holds a password there, it's your file: delete the leftover plaintext yourself.

## Install

### Docker

```bash
docker run -d --name streamline \
  -p 8080:8080 \
  -v streamline-data:/data \
  -v "$PWD/config:/etc/streamline" \
  -v /srv/data:/srv/data \
  -e STREAMLINE_LIBRARY__DOWNLOAD_PATH=/srv/data/downloads \
  -e STREAMLINE_LIBRARY__MOVIE_PATH=/srv/data/media/movies \
  -e STREAMLINE_LIBRARY__SERIES_PATH=/srv/data/media/series \
  -e STREAMLINE_LIBRARY__MUSIC_PATH=/srv/data/media/music \
  -e STREAMLINE_LIBRARY__EBOOK_PATH=/srv/data/media/books \
  -e STREAMLINE_LIBRARY__AUDIOBOOK_PATH=/srv/data/media/audiobooks \
  ghcr.io/datahearth/streamline:latest
```

When `/etc/streamline/config.yaml` does not exist, the first start writes it with the defaults and the library paths from the environment. To edit the file before anything reads it, write it first with `docker run --rm -v "$PWD/config:/etc/streamline" ghcr.io/datahearth/streamline:latest config init --output /etc/streamline/config.yaml` and set the paths there instead.

The library mount is **required**, not an optional extra: Streamline creates `library.movie_path` and `library.series_path` at startup and exits if it cannot. They default to `/media/movies` and `/media/series`, so without a mount and paths of your own the container stops with `create library path /media/movies: mkdir /media: permission denied`. Music and books default to `/data/music`, `/data/books` and `/data/audiobooks` — inside the data volume, a different filesystem from your downloads, where a hardlink import fails — so set all six. Keep library and downloads under one mount so the default `hardlink` import mode works (`library.import_mode` also accepts `copy` and `move`).

The config directory must stay writable. A read-only mount loses the generated session secret on every restart (everyone is signed out) and fails every save from Settings; for a declarative deploy use `read_only: true` and supply the secret yourself — see [GitOps and Kubernetes](https://github.com/DataHearth/streamline/wiki/GitOps-and-Kubernetes).

Tags: `latest`, `edge` (main branch), `X.Y.Z`, `X.Y`, `X`, `sha-<short>`. Each also comes as a `-vaapi` variant (`latest-vaapi`, `X.Y.Z-vaapi`, …) for hardware transcoding.

The image ships static `ffmpeg` and `ffprobe` alongside the binary in `/usr/local/bin`, so the [optional media features](#optional-ffmpeg-and-ffprobe) work with no extra setup. That static build cannot open a GPU: the `-vaapi` variant is a larger Ubuntu-based image carrying an ffmpeg with VAAPI and libvmaf, and needs the render node passed in (`--device /dev/dri` plus `--group-add` with its group id).

### Docker Compose

The Quick start snippet above is the deployment template. [deploy/compose.yaml](deploy/compose.yaml) is the project's *local test stack* — it builds the image from source and wires up gluetun (VPN), qBittorrent, Prowlarr and Plex against `tmp/`. Useful as a wiring reference, not as a starting point for your own deployment.

For a full observability stack (VictoriaMetrics + VictoriaLogs + VictoriaTraces + Grafana Alloy + Grafana), see [deploy/compose.observability.yaml](deploy/compose.observability.yaml). It is *development-only*: every port binds to `127.0.0.1`, because the Victoria\* backends serve unauthenticated write and admin APIs to anyone who can reach them. To use it from another machine, front it with an authenticated TLS reverse proxy or a VPN rather than widening the bindings.

### Helm

```bash
helm install streamline oci://ghcr.io/datahearth/charts/streamline \
  --namespace streamline --create-namespace \
  --set image.tag=X.Y.Z
```

`image.tag` is required: the chart has no `appVersion` and refuses to render without one, so which Streamline release runs — and when it changes — is always your call. Pin the chart with `--version X.Y.Z`; omit it to pull the latest chart release.

The chart is versioned independently of Streamline itself — a chart fix ships without an app release, and an app release ships without a chart bump. `--version` selects the *chart*, `image.tag` the app (an `X.Y.Z-vaapi` tag for hardware transcoding, with the chart's `hwAccel` block). App releases are tagged `vX.Y.Z`, chart releases `chart-vX.Y.Z`; the image tags drop the `v`.

The chart points all six library paths (movies, series, music, ebooks, audiobooks and downloads) at one shared volume, mounted at `/srv` by default, so every import can hardlink. Chart 2.4.0 and older left the music and book paths on the database volume under `/data`: if you keep albums or books there, pin those paths or move the folders before upgrading (see the note in `values.yaml`).

### NixOS / Nix

The flake ships a package, a NixOS module, a home-manager module and a VM test that boots the service for real.

```nix
inputs.streamline.url = "github:DataHearth/streamline";

# then, in your nixosSystem modules:
imports = [ inputs.streamline.nixosModules.default ];
nixpkgs.overlays = [ inputs.streamline.overlays.default ];

services.streamline = {
  enable = true;
  openFirewall = true;
  settings.library = {
    movie_path = "/srv/media/movies";
    series_path = "/srv/media/series";
    download_path = "/srv/downloads";
  };
};
```

`settings` is freeform YAML, so every key in the configuration reference works under its own name. The sandbox's writable paths are derived from `data_dir` and every `library.*_path` you set, so add `music_path`, `ebook_path` and `audiobook_path` beside the three above if you use music or books (their defaults sit under `/data`, outside the sandbox). By default the declaration seeds `/var/lib/streamline/config.yaml` on first start and the web UI stays editable; set `mutableSettings = false` for a fully declarative, read-only instance. See [the NixOS wiki page](https://github.com/DataHearth/streamline/wiki/NixOS) for secrets, hardware transcoding and the sandbox's writable paths.

### Unraid and Proxmox VE

An Unraid Community Applications template ([deploy/unraid](deploy/unraid/streamline.xml)) and a Proxmox VE LXC installer and updater ([deploy/lxc](deploy/lxc/streamline.sh)) are in the repository; the [Installation](https://github.com/DataHearth/streamline/wiki/Installation#unraid) and [Proxmox](https://github.com/DataHearth/streamline/wiki/Proxmox) wiki pages walk through both.

### Binary (from GitHub releases)

Download from [Releases](https://github.com/datahearth/streamline/releases/latest). Binaries available for:

- Linux: amd64, arm64
- macOS: amd64, arm64
- Windows: amd64, arm64

```bash
# Linux amd64 example
curl -fsSL -o streamline.tar.gz \
  https://github.com/datahearth/streamline/releases/latest/download/streamline_<version>_linux_amd64.tar.gz
tar xzf streamline.tar.gz
mkdir -p ~/.config/streamline
cp config.example.yaml ~/.config/streamline/config.yaml
# edit data_dir and the library.*_path keys, then:
./streamline --config ~/.config/streamline/config.yaml
```

Each archive includes a `config.example.yaml` with default values. There is no default config location: without `--config` Streamline runs on the built-in defaults alone, which file movies under `/media/movies` and keep the database in `./data` relative to wherever you started it.

Verify checksum:

```bash
curl -fsSL -o checksums.txt https://github.com/datahearth/streamline/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

`checksums.txt` is itself signed with cosign (keyless, GitHub OIDC). Verify it before trusting the hashes:

```bash
curl -fsSL -o checksums.txt.bundle https://github.com/datahearth/streamline/releases/latest/download/checksums.txt.bundle
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity-regexp="https://github.com/DataHearth/streamline/.github/workflows/release.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

Each archive also ships an SPDX SBOM (`<archive>.sbom.spdx.json`).

### From source

Requires Go >= 1.27.3, Node >= 24.22.2, pnpm 12, [Task](https://taskfile.dev) — or `nix develop`, whose devshell pins all of them.

```bash
git clone https://github.com/datahearth/streamline.git
cd streamline
pnpm install --frozen-lockfile
task
./streamline --config ./config.yaml   # written with the defaults if missing
```

The defaults put the movie and series libraries under `/media`, which a non-root user cannot create: set the `library.*_path` keys to folders you can write, or pass them as `STREAMLINE_LIBRARY__*_PATH` on the first start, which writes them into the new file.

### Optional: ffmpeg and ffprobe

Streamline needs nothing but itself to run. Two optional binaries from the FFmpeg suite unlock extra
features, and each is optional on its own. Streamline looks for both in `ffmpeg.path`, or on `$PATH`
when that is empty, and uses whichever it finds:

- **`ffprobe`** reads files:
  - **Media info**: resolution, codecs, duration, bitrate and stream languages on files and episodes
  - **Import verification**: holding a finished download whose actual resolution, duration or codec
    contradicts what the release claimed, instead of importing it blind
  - **Measured music and audiobook quality**: an album's real tier (a "FLAC" that is a 128k
    transcode) and an audiobook's real bit rate, checked against the profile
- **`ffmpeg`** writes files:
  - **Transcoding**: re-encoding imported files to a quality profile's rules (`transcoding.enabled`,
    off by default), including VAAPI hardware encoding
  - **Subsonic transcoding**: MP3/Opus/AAC for an app that asks for a format or a lower bit rate,
    and the per-account maximum bit rate

Without `ffprobe`, imports fall back to the release name, exactly as before the feature existed, and
`GET /api/v1/system/info` reports `ffmpeg_warn` so you know why media info is missing. Without
`ffmpeg`, the transcode worker stays idle (logged at boot) and Subsonic apps get the original file.
Neither case errors or blocks anything. `ffmpeg.enabled: false` switches off everything in both
lists.

**The official Docker image ships both binaries.** Static `ffmpeg` and `ffprobe` are copied into
`/usr/local/bin` from a digest-pinned [`mwader/static-ffmpeg`](https://hub.docker.com/r/mwader/static-ffmpeg)
stage, so Docker and Helm users get every extra with zero configuration (the `-vaapi` image carries
its own build with hardware encoding). Binary and source installs use whatever your OS provides.
Most package managers ship both binaries in one package (`apt install ffmpeg`, `brew install ffmpeg`,
`pacman -S ffmpeg`).

## Configuration

Generate a default config:

```bash
streamline config init --output ~/.config/streamline/config.yaml
```

Every config key can also be set via environment variables with the `STREAMLINE_` prefix. A double underscore (`__`) is the path separator; a single underscore is literal, so keys with underscore segments stay reachable: `STREAMLINE_LOG__APP__LEVEL=debug` → `log.app.level`, `STREAMLINE_AUTH__SESSION_SECRET=…` → `auth.session_secret`, `STREAMLINE_OTEL__ENDPOINT=…` → `otel.endpoint`.

Validate a config file:

```bash
streamline config validate --config ~/.config/streamline/config.yaml
```

Login is rate-limited per client address: **5 attempts per 15 minutes**, counted
as a sliding window, so no fifteen minutes anywhere on the clock ever contains
more than five. Each attempt comes back to the budget exactly fifteen minutes
after it was made, and refusals carry a `Retry-After` saying when — always at
least one second. IPv6 clients are counted per `/64`, not per address: the
smallest block an ISP routes to one subscriber is a `/64`, so counting single
addresses would let one client step around its own throttle by picking the next
address out of its allocation. Everything sharing a `/64` therefore shares one
budget. IPv4 is counted per address.

Attempts are recorded per address and nothing is ever dropped to make room, so
no traffic from any number of addresses can hand a throttled client its attempts
back early, and nobody who has made no attempts is ever refused. Up to 262,144
addresses are tracked at once — about 41 MiB when full, which only a flood
approaches; an ordinary install holds one entry per client. Past that, an
address seen for the first time is allowed **without being counted** rather than
turned away.

That last sentence is the trade, so here is what it costs. 262,144 keys is a
routed IPv6 `/46`; a `/48` — what a tunnel broker or cheap VPS hands out — is
65,536 and stays fully metered, as does anything smaller. Measured at 20
attempts from each of a million addresses, the limiter granted 5.00 per address
up to the cap and 16.07 across the million, so an attacker who owns enough
addresses to fill the table can go unmetered from the ones beyond it. Reaching
that point means already holding 262,144 addresses that were each cut off at
five, and every account behind them still locks after
`auth.lockout.threshold` failed attempts. The table clears itself within thirty
minutes of the flood stopping, with no restart.

Clear a locked-out account:

```bash
streamline auth unlock user@example.com
```

### Single sign-on (OIDC)

Providers are configured under `auth.oidc[]`. How far Streamline trusts one is
two independent questions, and each has its own key: `email_linking` says which
existing accounts it may take over, `allow_admin` says whether it may hand out
the `admin` role. Both default closed.

`name` identifies the provider everywhere — in the callback URL, in the trust
lookup, in the discovered-verifier cache — so two entries may not share one.
Streamline refuses to start on a duplicate rather than let the entry that
validates a token differ from the entry whose `allow_admin` bounds it.

```yaml
auth:
  oidc:
    - name: keycloak
      issuer: https://sso.example.com/realms/main
      client_id: streamline
      client_secret_file: /run/secrets/oidc
      email_linking: disabled          # disabled (default) | non_admin | all
      allow_admin: false               # default — no login here yields admin
      role_claim: realm_access.roles   # dotted paths reach nested claims
      role_mapping:
        streamline-admins: admin
        streamline-users: member
```

**`email_linking` — which accounts it may adopt.** "Adopt" means a federated
identity the provider has never presented before signing in as an existing
account **because the email addresses match**. That is only safe where nobody
can choose their own address at the IdP: anywhere they can — an IdP with open
self-registration, or one that marks an unchecked address verified — a matching
email would otherwise mint a login for any local user, the seeded admin
included.

| `email_linking` | May adopt an existing account |
| --- | --- |
| `disabled` (default) | no |
| `non_admin` | non-admin accounts only |
| `all` | any account, `admin` included |

The setting gates the adoption, not what the adoption leaves behind. An
adoption links the federated identity to the account permanently, and every
later login matches on that identity without consulting `email_linking` at all
— so a provider back at `disabled` still signs in as each account it adopted
while it was open, local password and all. That is what makes the migration
procedure below work, and it is the same reason the pass has to be a short one:
Streamline has no unlink, so the only way to undo a binding is to delete the
user, which cascades the identity away with it.

**`allow_admin` — whether it may grant `admin`.** With it `false` (the default)
**no login through this provider ever puts an account on `admin`**, with no
exception: not a claim mapped to `admin` by `role_mapping`, not an
`auth.default_role` of `admin` a signup falls back to, and not the role
carried by an invite consumed through SSO. A user in both an admin group and a
member group lands on member; one in an admin group alone keeps whatever role
they already had. Set `allow_admin: true` for a provider that really is allowed
to decide who administers Streamline.

The two keys are deliberately separate. Were one key to mean both, tightening
the adoption tier could *raise* the role ceiling — an account of federated
origin, adopted while the provider was at `non_admin`, would become promotable
to admin the moment the operator set that provider back to `disabled`. Kept
apart, each axis is monotone: no move on one can add capability on the other.

> [!IMPORTANT]
> **Upgrade impact.** Both keys default closed, which an install already
> signing users in through SSO has to account for.
>
> *Adoption.* `email_linking` defaults to `disabled`, so a user whose SSO login
> reaches their local account only by a matching email — rather than by an
> identity already linked from a previous SSO login — fails at the login screen
> with *"this SSO account is not linked to a Streamline user"*
> (`oidc_link_not_allowed`). To bind those
> identities, open the provider up for one pass and close it again:
>
> 1. set `email_linking: non_admin` on the provider and restart;
> 2. have each affected user sign in through SSO once — that login links the
>    identity permanently;
> 3. set `email_linking: disabled` again and restart.
>
> Admin accounts are not covered by `non_admin`. Either move them during a
> maintenance window with `email_linking: all`, or leave them on password login.
> The pass does not touch roles: an adoption never writes one, `allow_admin`
> alone decides how high a role can go, and none of the three steps changes it.
>
> *Roles.* `allow_admin` defaults to `false`, so a provider whose `role_mapping`
> grants `admin` does not grant it until you set it. An existing admin whose claims
> map only to `admin` keeps the role — the barred mapping is dropped, not
> downgraded, so nothing is written. One whose claims *also* map to a lower role
> is demoted to it on the next login, that being the highest role the provider
> may confer. Set `allow_admin: true` on the providers you want in charge of
> admin.
>
> Users who already signed in through SSO, and password-only installs, need
> nothing else.

### Running behind a reverse proxy

`X-Forwarded-For` is believed only when the connecting peer is listed in
`server.trusted_proxies`. The list is empty by default, so out of the box the
TCP peer is always treated as the client:

```yaml
server:
  trusted_proxies:
    - 10.1.0.7/32     # the ingress
    - 10.1.0.8/32     # its standby
```

The chain is read right to left: entries that are themselves listed in
`server.trusted_proxies` are skipped and the first entry outside the list is the
client. Any number of proxy hops may be skipped, so a deep chain — CDN, WAF,
load balancer, ingress, sidecar — still resolves the real client. An entry that
cannot be parsed as an address aborts the walk, leaving the connecting proxy as
the client, and is warned about at most once a minute.

> [!WARNING]
> List the proxies themselves, as narrowly as you can — ideally one `/32` (or
> `/128`) per proxy. **Never a whole client subnet.** Naming a range that
> clients can also occupy — `10.0.0.0/8`, `192.168.0.0/16`, a Kubernetes pod or
> node CIDR — makes every host in that range a trusted proxy: any of them can
> then send `X-Forwarded-For` naming an address inside `auth.trusted_networks`
> and be handed the `auth.trusted_role` identity without authenticating. One
> forged entry is enough — there is no hop count or padding to get past.

**Upgrade impact.** With `server.trusted_proxies` empty, as it is by default,
an install behind a reverse proxy attributes every request to the proxy's own
address:

- access logs and the IP shown on each session record read as the proxy;
- the login rate limit keys on the proxy, so **all** users behind it share one
  5-attempt / 15-min budget and lock each other out;
- `auth.mode: trusted-network` does not recognise LAN clients, because the
  address compared against `auth.trusted_networks` is the proxy's.

Set `server.trusted_proxies` if a proxy sits in front.
Direct-to-binary installs need no change.

## Supported integrations

| Type             | Supported                                                                 |
| ---------------- | ------------------------------------------------------------------------- |
| Indexers         | Torznab, Prowlarr                                                         |
| Download clients | qBittorrent, Transmission, Deluge, built-in BitTorrent engine             |
| Media servers    | Plex, Jellyfin, Emby (books: Jellyfin and Emby — Plex has no book library) |
| Metadata         | TMDB (movies, TV), TVDB (TV), MusicBrainz (music), Hardcover (books) — TMDB, TVDB and Hardcover take an API key of your own; Deezer, Cover Art Archive and Wikipedia for artwork and artist overviews, keyless |
| Clients          | Subsonic / OpenSubsonic apps (`/rest`), OPDS 1.2 readers (`/opds`), e-readers by email (EPUB, PDF) |

## Verifying images

All images are signed with [cosign](https://github.com/sigstore/cosign) via GitHub OIDC (keyless). Verify:

```bash
cosign verify ghcr.io/datahearth/streamline:latest \
  --certificate-identity-regexp="https://github.com/DataHearth/streamline/.github/workflows/image.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

SBOMs are attached as cosign attestations. Fetch:

```bash
cosign download attestation ghcr.io/datahearth/streamline:latest \
  --predicate-type=https://spdx.dev/Document
```

Every image is scanned by [grype](https://github.com/anchore/grype) before it is tagged: a fixable vulnerability of severity high or above leaves the image untagged and fails the run. The report is attached to the Image workflow run and shown in its summary.

## Contributing

Bug reports and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for setup,
the build/test loop, conventions and the policy on AI-assisted PRs. Participation
is governed by the [Code of Conduct](CODE_OF_CONDUCT.md).

Found a vulnerability? Don't open an issue — see [SECURITY.md](SECURITY.md).

## License

[GPL-3.0-or-later](LICENSE)

## Links

- Issues: https://github.com/datahearth/streamline/issues
- Releases: https://github.com/datahearth/streamline/releases
- Changelog: [CHANGELOG.md](CHANGELOG.md)
- Roadmap: [docs/wiki/Roadmap.md](docs/wiki/Roadmap.md)

## Screenshots

> **The library shown in every screenshot and in the demo video is fabricated.** Every title it holds is
> public domain or openly licensed: Blender Open Movies, silent and mid-century films and TV whose copyright
> has lapsed, Creative Commons albums (Jonathan Coulton's *Thing a Week*, Brad Sucks, Josh Woodward, Broke For
> Free and Chris Zabriskie) and public-domain books. An artist's wider discography appears only as metadata.
> Posters, covers and metadata come from public metadata providers; the files behind them are placeholders the
> screenshot generator makes (a still frame, silence, a book of one title page), not the works. Streamline is a
> library manager — it neither hosts nor distributes media. It does not endorse copyright infringement or any
> other unlawful use, and the authors accept no responsibility for how you use it. Obtaining content you have
> the right to is your responsibility.

|  |  |
| --- | --- |
| [![Movies](docs/assets/movies.png)](docs/assets/movies.png)<br>**Movies** — poster grid with per-title status | [![Movie detail](docs/assets/movie-detail.png)](docs/assets/movie-detail.png)<br>**Movie detail** — cast, artwork, and ffprobe media info |
| [![Series](docs/assets/series.png)](docs/assets/series.png)<br>**Series** — every show, monitored and missing counts | [![Episodes](docs/assets/series-episodes.png)](docs/assets/series-episodes.png)<br>**Episodes** — seasons, air dates, per-file quality |
| [![Import verification](docs/assets/import-verification.png)](docs/assets/import-verification.png)<br>**Import verification** — a mismatched file is held, not imported | [![History](docs/assets/history.png)](docs/assets/history.png)<br>**History** — every grab, where it came from, what it cost |
| [![Manual search](docs/assets/manual-search.png)](docs/assets/manual-search.png)<br>**Manual search** — every release from every indexer, one grab away | [![Library import](docs/assets/library-import.png)](docs/assets/library-import.png)<br>**Library import** — adopt an existing library under review |
| [![Music](docs/assets/music.png)](docs/assets/music.png)<br>**Music** — every artist with its albums, what you hold and what is wanted | [![Artist discography](docs/assets/music-artist.png)](docs/assets/music-artist.png)<br>**Discography** — albums, track lists and tiers from MusicBrainz |
| [![Books](docs/assets/books.png)](docs/assets/books.png)<br>**Books** — one shelf of standalone books and series | [![Book detail](docs/assets/book-detail.png)](docs/assets/book-detail.png)<br>**Book** — an ebook and an audiobook slot, each with its own edition |
| [![Book series](docs/assets/book-series.png)](docs/assets/book-series.png)<br>**Series** — every volume, monitored as a whole | [![On a phone](docs/assets/mobile.png)](docs/assets/mobile.png)<br>**On a phone** — installable web app with bottom navigation |
| [![Requests](docs/assets/requests.png)](docs/assets/requests.png)<br>**Requests** — a built-in Seerr, no second service | [![Calendar](docs/assets/calendar.png)](docs/assets/calendar.png)<br>**Calendar** — films, episodes, albums and books landing this month |
| [![Sign in](docs/assets/login.png)](docs/assets/login.png)<br>**Sign in** — local accounts, or SSO through any OIDC provider | [![Settings](docs/assets/settings.png)](docs/assets/settings.png)<br>**Settings** — the server at a glance; file-only settings stay in the config file |
