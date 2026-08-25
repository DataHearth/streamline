# Roadmap

What Streamline does today and what's planned. No dates — this says *whether* a feature is coming, not when.

Missing something you need? [Open an issue](https://github.com/datahearth/streamline/issues).

## Media types

| Feature | Status |
| --- | --- |
| Movies | Shipped |
| TV shows | Shipped |
| Music | Planned |
| Books | Planned |

Music and books follow the same path as movies and TV: browse and organise your existing library first, then automatic searching and grabbing, then requests. Music will also be reachable from Subsonic clients, and books from OPDS readers.

## Downloading

| Feature | Status |
| --- | --- |
| qBittorrent, Transmission, Deluge | Shipped |
| Torznab indexers | Shipped |
| Prowlarr | Shipped |
| Built-in torrent client (no external download client needed) | Shipped |
| Media info from ffprobe (codec, resolution, duration, bitrate) | Shipped |
| Import verification — hold a download that doesn't match what it claimed | Shipped |
| Custom-format quality scoring (Radarr-style profiles, RSS-driven movie upgrades) | Shipped |
| Usenet (NZB indexers + SABnzbd/NZBGet) | Planned |

The built-in client is the newest of these and still has a rough edge: a download can occasionally stall while a seeder is connected. It usually clears itself within seconds; one that doesn't can be nudged with pause and resume. If you already run qBittorrent, Transmission or Deluge, there's no need to switch.

## Playback

| Feature | Status |
| --- | --- |
| Plex, Jellyfin, Emby notifications + deep links | Shipped |
| Built-in player (stream from Streamline itself, no media server) | Planned |

## Platform

| Feature | Status |
| --- | --- |
| Multi-user, SSO (OIDC), invites | Shipped |
| Request system | Shipped |
| REST API (OpenAPI 3.0) | Shipped |
| OpenTelemetry traces, metrics, logs | Shipped |
| Docker images, Helm chart, single binary | Shipped |
| Library path migration (re-root a moved library) | Shipped |
