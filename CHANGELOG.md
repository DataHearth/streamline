# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] - 2026-08-17

### Added

- imports: Search shows in the tv import review
- web: Add a monitored filter to the library toolbars
- dashboard: Count series and break free space down by library path
- series: Store episode overviews and expose the media file path
- web: Add an episode details modal, badge unmonitored episodes missing
- web: Bulk-select and act on movies and series
- web: Split torrents onto their own activity page
- web: Import the dashboard reworks from the design project
- web: Select controls and persisted sort for movies and series
- web: Show file path on movie detail file card
- movies: Enforce single media file per movie
- series: Enforce single media file per episode
- series: Unmonitor TBA episodes until a title or air date lands
- web: Drop the Files tab from series detail
- series: Opt-in specials monitoring with settings toggle and bulk apply
- **BREAKING** rss: Scan indexer feeds for wanted episodes
- web: Floating add button and select menus that stay on screen
- web: Split add/request flow with lookup detail and touch screen
- library: Advanced settings section with library path migration
- web: Responsive phone and tablet UI for library, detail and nav
- media: Serve movie and series detail metadata from the database
- web: Touch search surfaces and add pill for phone and tablet
- web: Activity and torrents on touch
- web: Dashboard on touch and add affordance follow-up
- web: Translate the SPA with paraglide js (en/fr)
- web: Sync calendar, imports and requests touch layouts from design
- web: Sync settings and account touch layouts from design
- server: Add security-headers middleware (CSP, XFO, nosniff, referrer, HSTS)
- auth: Meter /api/v1 credential failures per IP
- web: Pull dashboard and calendar layout work from the design project
- cli: Add user administration to the auth command group (#22)
- auth: Deny identity mutations to API-key requests

### Changed

- web: Drop the seasons tab and badge fileless series as missing
- web: Drop dead QueueItem type, stale Density import and unused EmptyCard
- role: Make db params take a role that names its authority
- web: Drop unreachable states from the live queue state map
- 

### Fixed

- otelx: Bound outbound requests with a client timeout
- metadata: Collapse concurrent tvdb logins with singleflight
- web: Render one canonical version string
- web: Stop counting unmonitored fileless titles as wanted, colour the toolbar counts
- web: Split wanted from missing in series and season episode counts
- web: Drop hero isolate so the play-on menu paints over the tab bar
- web: Hide the dashboard scroller's native scrollbar
- web: Stop the torrents page flickering and the nav highlight sticking
- bittorrent: Bound the lost peer wakeup stall with a short keepalive
- download: Derive infohash locally when qbittorrent reports none
- restapi: Keep indexer name on manual grab results
- web: Reveal poster card overlays on keyboard focus only, not mouse clicks
- web: Show em-dash placeholders for missing file fields on movie detail
- web: Negative-cache missing posters instead of re-requesting on every mount
- series: Scope season rollups to monitored or downloaded episodes
- web: Match the season progress bar to the hero bar
- web: Show relative dates as year/month/day spans
- web: Skip the torrent nav query when the builtin engine is off
- rss: Skip feed and missing-search passes when no download client is enabled
- settings: Correct read-only lock scope and gate invites on registration mode
- web: Rework add-sheet drag for mobile Gecko smoothness
- web: Poster taps select instead of navigating in select mode
- web: Keep free-space value clear of the dashboard disk info button
- media: Key movie metadata staleness off last_refreshed_at
- tvshow: Degrade on cast fetch failure and bound the refresh tick
- web: Hide the empty layout section and correct series bulk search
- web: Render upcoming episodes in the calendar and dashboard lists
- web: Ignore accents and case in library search
- web: Show the import transfer-mode select in rename mode
- media: Drop cached posters and sidecars when deleting a title
- ci: Compile paraglide messages in the docker frontend stage
- restapi: Enforce default-deny role guard on every /api/v1 operation
- download: Restrict grab download URLs to configured indexers
- calendar: Show each upcoming episode's real status
- ci: Pin third-party actions to commit SHAs
- otelx: Keep indexer credentials out of traces and errors
- auth: Keep the generated seed admin password out of config and logs
- web: Block cross-site posts to the auth endpoints
- deploy: Harden the runtime image, chart securityContext and image CI
- download: Drop the duplicated release-source guard
- otelx: Redact fragment-only URLs and bound the unwrap walk
- server: Trust X-Forwarded-* only from configured proxies
- auth: Bound the login limiter without evicting throttled keys
- auth: Believe X-Forwarded-* only from configured proxies
- middleware: Accept the SPA session cookie without Fetch Metadata
- config: Create data_dir instead of requiring it to pre-exist
- web: Compare Origin against a bare serialised origin only
- web: Share one return-to guard and close its backslash hole
- auth: Stop successful logins from spending the rate limit
- web: Keep the app shell off the login page
- config: Keep env-owned values out of the write-back and guard what it writes
- auth: Gate OIDC account linking and cap the roles a provider may grant
- auth: Close the login enumeration oracles and the stored-hash DoS
- posters: Cap the artwork fetch at 20 MB
- deploy: Stop automounting the ServiceAccount token
- middleware: Send no-store and COOP on non-static responses
- middleware: Accept a same-origin GET carrying only a Referer
- download: Contain torrent-name paths and serialize entity imports
- restapi: Clamp pagination limits to their documented maxima
- restapi: Tighten infra read roles, error output and draft tests
- request: Back active-request de-duplication with a unique index
- ent: Mark the password and invite token hashes sensitive
- auth: Bound self-registration, admin demotion and API keys
- deps: Update go modules (#9)
- deploy: Bind the observability stack to loopback and document the risk (#12)
- web: Give calendar kinds their own colours and unify the poster pill
- web: Title-case the live queue's Importing state word
- bittorrent: Disable WebTorrent to drop the WebRTC attack surface (#17)
- deps: Update module github.com/knadh/koanf/providers/env to v2 (#39)
- server: Set HTTP server timeouts to stop Slowloris (#14)
- server: Cap request body size on web auth and API JSON decoders (#19)
- auth: Unify forwarded-proto TLS detection for cookies (#13)
- mediaserver: Restrict Plex PIN flow to the starting admin (#15)
- auth: Keep the last admin during OIDC role sync (#20)
- auth: Revoke API keys on password change and admin reset (#18)
- auth: Revoke sessions when an admin changes a user's role (#16)
- auth: Require a session to create API keys

## [1.3.0] - 2026-07-31

### Added

- imports: Scan tv folders against tvdb
- dashboard: Report library filesystem free space

### Fixed

- web: Read sidebar version from /system/info instead of a hardcoded dev string
- restapi: Default page and limit on the movie list
- web: Add status tokens for stalled and fetching
- web: Drop the redundant add-torrent cta and row density toggle
- web: Give movies and series a matching toolbar layout
- web: Stop series detail actions reflowing between tabs
- auth: Record api key last-used timestamps

## [1.2.0] - 2026-07-30

### Added

- movies: Flag already-added titles in TMDB search
- web: Failed movie tab and already-added search flags

### Changed

- web: Shared kebab menu, formatBytes, session cards, read-only chrome
- server: Reuse grab conversion and scan-failure helpers

### Fixed

- auth: Drop revoked and expired rows from session list - closes #2
- rss: Episode search eligibility and per-item quality profiles
- download: Treat stopped and queued complete qBittorrent torrents as completed
- library: Episode drift revert, movie delete cascade, episode import parity
- tvshow: Profile validation, monitored-aware season views, pilot preset, counters
- auth: Atomic invite consumption, user delete with invites, limiter pruning
- metadata: Synchronize TVDB token and re-login on expiry
- server: Importer shutdown panic, scheduler rootCtx race, DB close on boot failure
- api: Expose failed movie status
- web: Movie quality-profile flow, add-modal reset, on-accent contrast
- web: History pagination, drawer scroll lock, play-on error state
- web: Scan navigation, series param reset, settings breadcrumbs
- web: Request approval invalidation, auth form reset, filter debounce, number field clearing
- web: Debounce user search, clearable priority fields
- server: Plex-only library_section on update, honest is_current for admin sessions
- web: Send explicit clear signal for library_section
- movies: Surface quality_profile in API responses and modal default option
- deps: Bump vulnerable modules and pin Go to 1.26.5
- series: Wire series renamer into REST server
- library: Return 404 for unknown import decision targets
- movies: 404 on refreshing metadata of unknown movie
- media: 404 renaming files of an unknown movie or series
- requests: 404 on approve/deny/reopen of unknown request
- library: 404 listing files/shows of an unknown import scan
- library: Scope import decision writes to their scan
- bittorrent: Deterministic teardown and port reuse in restore integration spec
- bittorrent: Drain piece-completion writes before closing the store

## [1.1.0] - 2026-07-21

### Added

- config: Builtin download client config
- db: Torrent session entity
- bittorrent: Builtin bittorrent engine
- bittorrent: Seed limits and torrent management views
- download: Builtin client in download manager
- server: Wire builtin torrent engine
- api: Torrents REST endpoints
- web: Add built-in torrent client UI
- torrents: Upload speed, eta, badges — close the design-fidelity gap
- web: Apply claude-design UI adjustments

### Fixed

- build: Inject version into internal/buildinfo, not main
- build: Glob web/static so asset changes rebuild the binary
- bittorrent: Address builtin torrent client review findings
- web: Unset builtin knobs read as auto/unlimited
- web: Draw checkbox glyph ourselves, native accent glyph off-center
- bittorrent: Drive downloads via file priorities, not DownloadAll

## [1.0.0] - 2026-07-12

### Changed

- Initial commit
- release: Add release:tag task, drop redundant release:gh

### Fixed

- sysinfo: Make disk-usage probe cross-platform for releases (#1)

[2.0.0]: https://github.com/datahearth/streamline/compare/v1.3.0..v2.0.0
[1.3.0]: https://github.com/datahearth/streamline/compare/v1.2.0..v1.3.0
[1.2.0]: https://github.com/datahearth/streamline/compare/v1.1.0..v1.2.0
[1.1.0]: https://github.com/datahearth/streamline/compare/chart-v1.0.1..v1.1.0
[1.0.0]: https://github.com/datahearth/streamline/tree/v1.0.0

<!-- generated by git-cliff -->
