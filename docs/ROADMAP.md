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
| Transcoding — re-encode or remux imported files to a quality profile's rules | Shipped |
| Custom-format quality scoring (Radarr-style profiles, RSS-driven movie upgrades) | Shipped |
| Selective file download (grab a pack, download only the episode you need) | In progress |
| Usenet (NZB indexers + SABnzbd/NZBGet) | Planned |

The built-in client is the newest of these and still has a rough edge: a download can occasionally stall while a seeder is connected. It usually clears itself within seconds; one that doesn't can be nudged with pause and resume. If you already run qBittorrent, Transmission or Deluge, there's no need to switch.

Selective file download is implemented for all four clients but ships behind `download.selective_files`, off by default until it's run in anger; flip it on to try it today.

Transcoding rewrites files on disk after import, to shrink a library or normalise its containers — it is not playback transcoding, which belongs to the built-in player below. It is off by default (`transcoding.enabled`) and never re-encodes HDR or Dolby Vision. Every encode is verified against its source before it replaces the file — duration, resolution, audio and subtitle tracks, a size band, and optionally a full decode pass and a VMAF score — and one that fails is rejected rather than swapped in. Hardware encoding through VAAPI is shipped on Linux (`transcoding.hw_accel`, with the `-vaapi` image variant or any ffmpeg built with VAAPI); it falls back to the CPU per job when the GPU cannot take the codec. VideoToolbox on macOS remains planned.

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
| Installable web app (home-screen manifest) | Shipped |
