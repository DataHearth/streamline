# Streamline

[![CI](https://github.com/datahearth/streamline/actions/workflows/ci.yaml/badge.svg)](https://github.com/datahearth/streamline/actions/workflows/ci.yaml)
[![Image](https://github.com/datahearth/streamline/actions/workflows/image.yaml/badge.svg)](https://github.com/datahearth/streamline/actions/workflows/image.yaml)
[![Release](https://img.shields.io/github/v/release/datahearth/streamline)](https://github.com/datahearth/streamline/releases/latest)
[![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/datahearth/streamline)](go.mod)

Self-hosted unified media manager. Replaces the \*arr stack (Radarr, Sonarr, Lidarr, Readarr) and Seerr with a single binary.

<!-- ![Screenshot](docs/screenshot.png) -->

## Features

- Unified movie & TV library (music & books planned — see the [roadmap](docs/ROADMAP.md))
- Adopt an existing library: scan untracked files and import them under review
- Monitoring, RSS auto-grab, manual search, and a calendar of upcoming releases
- Quality profiles, activity queue and history
- Multi-user with SSO (OIDC), invites, API keys
- Built-in request system (Seerr replacement)
- Indexers: Torznab, Prowlarr
- Torrent download clients: qBittorrent, Transmission, Deluge
- Media server notifications and deep links: Plex, Jellyfin, Emby
- REST API (OpenAPI 3.0 spec)
- OpenTelemetry traces, metrics, logs
- GitOps-friendly: `read_only` config mode rejects runtime config writes
- CGO-free SQLite — single-binary, zero external deps

## Quick start (Docker Compose)

```yaml
services:
  streamline:
    image: ghcr.io/datahearth/streamline:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes:
      - ./data:/data
      - ./config:/etc/streamline:ro
      # Your media library, plus the finished-downloads dir your torrent client
      # writes to. Keep both on one filesystem: the default import mode is
      # `hardlink` (library.import_mode — `copy` and `move` also work).
      - /path/to/media:/media
      - /path/to/downloads:/downloads
```

```bash
mkdir -p config data
docker run --rm -v "$PWD/config:/etc/streamline" \
  ghcr.io/datahearth/streamline:latest config init --output /etc/streamline/config.yaml
docker compose up -d
```

Open http://localhost:8080.

### First login

An admin account is seeded on first boot. Set `auth.seed_admin.email` and `auth.seed_admin.password` (or `password_file`) in the config beforehand to choose the credentials. If you don't, Streamline creates `admin@streamline.local` with a generated password and writes it back into `auth.seed_admin` in your config file — read it from there, then change it.

## Install

### Docker

```bash
docker run -d --name streamline \
  -p 8080:8080 \
  -v streamline-data:/data \
  -v "$PWD/config:/etc/streamline:ro" \
  ghcr.io/datahearth/streamline:latest
```

Tags: `latest`, `edge` (main branch), `vX.Y.Z`, `X.Y`, `X`, `sha-<short>`.

### Docker Compose

The Quick start snippet above is the deployment template. [deploy/compose.yaml](deploy/compose.yaml) is the project's *local test stack* — it builds the image from source and wires up gluetun (VPN), qBittorrent, Prowlarr and Plex against `tmp/`. Useful as a wiring reference, not as a starting point for your own deployment.

For a full observability stack (VictoriaMetrics + VictoriaLogs + VictoriaTraces + Grafana Alloy + Grafana), see [deploy/compose.observability.yaml](deploy/compose.observability.yaml).

### Helm

```bash
helm install streamline oci://ghcr.io/datahearth/charts/streamline \
  --namespace streamline --create-namespace
```

Pin a version with `--version X.Y.Z`; omit it to pull the latest release.

The chart is versioned independently of Streamline itself — a chart fix ships without an app release, and an app release ships without a chart bump. `--version` selects the *chart*; the app version it deploys is the chart's `appVersion` (override with `--set image.tag=X.Y.Z`). App releases are tagged `vX.Y.Z`, chart releases `chart-vX.Y.Z`.

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
cp config.example.yaml ~/.config/streamline/config.yaml
./streamline
```

Each archive includes a `config.example.yaml` with default values.

Verify checksum:

```bash
curl -fsSL -o checksums.txt https://github.com/datahearth/streamline/releases/latest/download/checksums.txt
sha256sum -c checksums.txt --ignore-missing
```

`checksums.txt` is itself signed with cosign (keyless, GitHub OIDC). Verify it before trusting the hashes:

```bash
curl -fsSL -o checksums.txt.bundle https://github.com/datahearth/streamline/releases/latest/download/checksums.txt.bundle
cosign verify-blob checksums.txt --bundle checksums.txt.bundle \
  --certificate-identity-regexp="https://github.com/datahearth/streamline/.github/workflows/release.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

Each archive also ships an SPDX SBOM (`<archive>.sbom.spdx.json`).

### From source

Requires Go >= 1.26, Node >= 24, pnpm, [Task](https://taskfile.dev).

```bash
git clone https://github.com/datahearth/streamline.git
cd streamline
task
./streamline
```

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

Login is rate-limited per IP (5 attempts / 15 min). Clear a locked-out account:

```bash
streamline auth unlock user@example.com
```

## Supported integrations

| Type             | Supported                            |
| ---------------- | ------------------------------------ |
| Indexers         | Torznab, Prowlarr                    |
| Download clients | qBittorrent, Transmission, Deluge    |
| Media servers    | Plex, Jellyfin, Emby                 |

## Verifying images

All images are signed with [cosign](https://github.com/sigstore/cosign) via GitHub OIDC (keyless). Verify:

```bash
cosign verify ghcr.io/datahearth/streamline:latest \
  --certificate-identity-regexp="https://github.com/datahearth/streamline/.github/workflows/image.yaml@.*" \
  --certificate-oidc-issuer=https://token.actions.githubusercontent.com
```

SBOMs are attached as cosign attestations. Fetch:

```bash
cosign download attestation ghcr.io/datahearth/streamline:latest \
  --predicate-type=https://spdx.dev/Document
```

Every image push is scanned by [grype](https://github.com/anchore/grype) for known vulnerabilities (severity >= high). Results are uploaded to the repository's [Security tab](https://github.com/datahearth/streamline/security/code-scanning).

## License

[GPL-3.0-or-later](LICENSE)

## Links

- Issues: https://github.com/datahearth/streamline/issues
- Releases: https://github.com/datahearth/streamline/releases
- Changelog: [CHANGELOG.md](CHANGELOG.md)
- Roadmap: [docs/ROADMAP.md](docs/ROADMAP.md)
