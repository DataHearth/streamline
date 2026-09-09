# Streamline

[![Release](https://img.shields.io/github/v/release/datahearth/streamline)](https://github.com/datahearth/streamline/releases/latest)
[![License](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://github.com/datahearth/streamline/blob/main/LICENSE)
[![CI](https://github.com/datahearth/streamline/actions/workflows/ci.yaml/badge.svg)](https://github.com/datahearth/streamline/actions/workflows/ci.yaml)

Self-hosted unified media manager. One binary replaces Radarr, Sonarr and Seerr — library management, indexer searching, download-client handling, file organisation, multi-user requests, and a REST API.

[![Dashboard](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/dashboard.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/dashboard.png)

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

## A quick look

| | |
| --- | --- |
| [![Movies](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/movies.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/movies.png) | [![Series](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/series.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/series.png) |
| **Movies** — poster grid with per-title status | **Series** — monitored and missing counts |
| [![Activity queue](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/activity-queue.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/activity-queue.png) | [![Calendar](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/calendar.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/calendar.png) |
| **Activity queue** — live progress, speed, ETA | **Calendar** — upcoming episodes and releases |
| [![Manual search](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/manual-search.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/manual-search.png) | [![Requests](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/requests.png)](https://raw.githubusercontent.com/DataHearth/streamline/main/docs/assets/requests.png) |
| **Manual search** — releases from every indexer | **Requests** — the built-in Seerr |

---

## What Streamline does today

Movies and TV are shipped, with the built-in torrent client, quality scoring, transcoding, requests and the REST API. Music, books, Usenet and a built-in player are not.

The full picture — every media type, download path, playback option and platform feature, marked Shipped, In progress or Planned — is on the **[Roadmap](Roadmap)**.

---

## About the screenshots, and about what you do with this

> [!NOTE]
> Every library shown in a screenshot, GIF or example on this wiki and in the repository is fabricated. Titles, posters and metadata are pulled from public metadata providers to make the interface legible; there are no files behind them. Nothing depicted is a real download, a real release, or an endorsement of one.

> [!IMPORTANT]
> Streamline is a library manager. It indexes metadata, talks to indexers and download clients you configure yourself, and organises files that already exist on your disk. It hosts nothing and distributes nothing. It does not endorse copyright infringement or any other unlawful use, and the authors accept no responsibility for how you choose to use it — making sure you have the right to the content you acquire is entirely on you.

---

## Getting help

- **Bug or feature request** — [open an issue](https://github.com/datahearth/streamline/issues)
- **Security vulnerability** — do *not* open an issue; see [SECURITY.md](https://github.com/datahearth/streamline/blob/main/SECURITY.md)
- **Contributing** — [CONTRIBUTING.md](https://github.com/datahearth/streamline/blob/main/CONTRIBUTING.md)

Streamline is [GPL-3.0-or-later](https://github.com/datahearth/streamline/blob/main/LICENSE).

---

*These pages live in the main repository under [`docs/wiki/`](https://github.com/datahearth/streamline/tree/main/docs/wiki) and are mirrored here automatically. Edits made through the wiki UI are overwritten on the next sync — send a pull request instead.*
