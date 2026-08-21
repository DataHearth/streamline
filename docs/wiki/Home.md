# Streamline

Self-hosted unified media manager. One binary replaces Radarr, Sonarr and Seerr — library management, indexer searching, download-client handling, file organisation, multi-user requests, and a REST API.

This wiki has two halves. Pick the one that matches you.

---

## 🎬 Operating Streamline

For anyone running Streamline for themselves, their family, or a handful of friends. No Go, no Kubernetes, no YAML beyond copy-paste.

| Page | What it covers |
| --- | --- |
| **[Installation](Installation)** | Docker Compose, plain binary, Unraid/Synology/TrueNAS, Helm |
| **[First-Run Setup](First-Run-Setup)** | Logging in, metadata keys, indexers, download client, folders |
| **[Adding Movies and TV](Adding-Movies-and-TV)** | Searching, monitoring, quality, manual grabs |
| **[Importing an Existing Library](Importing-an-Existing-Library)** | Pointing Streamline at files you already have |
| **[Activity and Calendar](Activity-and-Calendar)** | The queue, history, stuck downloads, what's coming |
| **[Requests and Users](Requests-and-Users)** | Inviting people, roles, approving requests |
| **[Troubleshooting](Troubleshooting)** | Nothing downloads, nothing imports, and other common walls |

---

## ⚙️ Advanced

For operators who want the whole surface: every config key, declarative deploys, the API, and the machinery behind the buttons.

| Page | What it covers |
| --- | --- |
| **[Configuration Reference](Configuration-Reference)** | Every key, its default, and its env-var form |
| **[Authentication and SSO](Authentication-and-SSO)** | Auth modes, OIDC, roles, API keys, lockout |
| **[Quality Profiles and Naming](Quality-Profiles-and-Naming)** | How releases are accepted or rejected; filename templates |
| **[Scheduled Jobs](Scheduled-Jobs)** | Every background job, its interval, and how to drive it |
| **[REST API](REST-API)** | Authentication, endpoint map, worked examples |
| **[Observability and Logging](Observability-and-Logging)** | OpenTelemetry, log formats, rotation |
| **[GitOps and Kubernetes](GitOps-and-Kubernetes)** | `read_only` mode, the Helm chart, secrets, path migration |

---

## What Streamline does today

| | Status |
| --- | --- |
| Movies, TV shows | Shipped |
| Music, books | Planned |
| qBittorrent, Transmission, Deluge | Shipped |
| Built-in torrent client (no external client needed) | Shipped |
| Torznab, Prowlarr indexers | Shipped |
| Usenet (NZB + SABnzbd/NZBGet) | Planned |
| Plex / Jellyfin / Emby notifications and deep links | Shipped |
| Built-in player (stream from Streamline itself) | Planned |
| Multi-user, OIDC SSO, invites, request system | Shipped |
| REST API, OpenTelemetry, Helm chart, single binary | Shipped |

The canonical, always-current version of this table is [`docs/ROADMAP.md`](https://github.com/datahearth/streamline/blob/main/docs/ROADMAP.md).

---

## Getting help

- **Bug or feature request** — [open an issue](https://github.com/datahearth/streamline/issues)
- **Security vulnerability** — do *not* open an issue; see [SECURITY.md](https://github.com/datahearth/streamline/blob/main/SECURITY.md)
- **Contributing** — [CONTRIBUTING.md](https://github.com/datahearth/streamline/blob/main/CONTRIBUTING.md)

Streamline is [GPL-3.0-or-later](https://github.com/datahearth/streamline/blob/main/LICENSE).

---

*These pages live in the main repository under [`docs/wiki/`](https://github.com/datahearth/streamline/tree/main/docs/wiki) and are mirrored here automatically. Edits made through the wiki UI are overwritten on the next sync — send a pull request instead.*
