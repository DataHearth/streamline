# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [3.0.0] - 2026-09-08

### Added

- **BREAKING** helm: Require image.tag, drop appVersion fallback
- web: Serve the dashboard at the root route, drop /dashboard
- web: Media probe & import verification UI from design
- config: Add ffmpeg block
- ffmpeg: Ffprobe-backed media probe package
- db: Media info columns on media_file
- db: Thread probe info through media_file writes
- importer: Probe source media before transfer
- hygiene: Media-probe backfill job
- api: Expose probed media_info on media files and episodes
- api: Surface missing ffmpeg on system info
- config: Land probe verification and codec settings
- db: Held status and hold_reasons on download_record
- importer: Probe verification rules
- db: Held download lifecycle helpers
- importer: Hold suspicious imports for review
- api: Resolve held downloads
- api: Series type override, bulk import decisions, path-drift warning
- events: Media events for series and episodes, wire drift and search
- ui: Change-match action and series-aware activity feed
- activity: Add an events view for the media feed
- library: Make the monitored filter tri-state
- quality: Scoring engine core
- quality: Built-in format library
- config: Custom_formats resource and scored profile fields
- config: Custom format CRUD
- **BREAKING** rss: Score-ranked release selection
- rss: Feed-driven score upgrades
- api: Custom-formats endpoints and release score annotations
- restapi: Custom format handlers
- restapi: Score annotations on releases and files
- ui: Custom formats settings page
- ui: Scored profiles and release score display
- quality: Builtin format descriptions
- config: User custom format descriptions
- quality: Curate builtins, group chips, richer presets
- quality: Per-file replacement predicate
- importer: Per-episode replacement inside season packs
- config: Replace_whole_season quality profile key
- rss: Per-episode season pack upgrade selection
- api: Expose replace_whole_season on quality profiles
- ui: Replace_whole_season on the quality profile form
- web: Global in-flight indicator
- quality: Scale size bounds by the episode count a release carries
- config: Add torrent_listen_port to override the builtin client's port
- quality: Name the default profile in API responses
- series: Track episodes through downloading and importing
- torrents: Persist upload totals and surface known peers
- quality: Drop the replace_whole_season profile key
- bittorrent: Persist per-torrent file selection state
- download: File listing and selection on the client interface
- download: Selection intent and resolution on download records
- config: Download.selective_files flag
- download: Resolve a wanted-episode set to torrent file indexes
- download: Selective grab resolves the keep-set before AddTorrent
- rss: Pack grabs carry the episode set they serve
- download: Re-grabbing a held hash widens the selection
- download: Qbittorrent file selection
- download: Transmission file selection
- download: Deluge file selection
- download: Scheduled pass resolves pending file selections
- download: Zero-waste magnet adds for builtin and qbittorrent
- download: Deluge magnets resolve metadata before the add
- api: Expose selection state on queue rows and torrent files
- web: Selective downloads show selected size, not a stuck bar
- db: Per-show upgrade-candidate query
- rss: Missing-search packs replace what they beat
- rss: Missing search skips episodes a grab already serves
- mediaserver: Refresh the section matching the imported media kind
- settings: Edit both plex section keys
- bittorrent: Rebindable packet conn for utp and dht
- bittorrent: Rebindable tcp listener
- bittorrent: Engine owns its peer listening sockets
- bittorrent: Move the listen port without a restart
- api: Endpoint to move the builtin engine's listen port
- bittorrent: Dht announce on port move, plus socket helper cleanup
- download: Let an operator identify an unmatched adopted torrent
- metadata: Translate TVDB season names
- web: Search one season from the episodes tab
- settings: Expose library, download, metadata and lockout config
- settings: Expose the remaining config keys, editable or read-only
- web: Name the movie or series in the manual search modal title
- api: Report grab_failures on an episode
- web: Raise touch targets to 44px below lg
- web: Raise dashboard touch targets below lg
- library: Give episode templates tvdb_id, source, codec and group
- activity: Preview which episodes a pack proposal would import
- activity: Decide a pack proposal per episode, not per anchor
- quality: Answer language and audio-track conditions from the probe
- dashboard: Name what an import added in the arrivals row
- movies: Carry an importing status from the monitor through to counts
- web: Badge movie and series cards as importing
- series: Roll a show's in-flight grab up onto the library list
- web: Name what a series card has landing, not just its gaps
- activity: Sort the tables by column, and open with the queue
- library: Add the monitoring facet, series in-flight filters and release dates
- web: Pull the facet toolbars and release-day captions down from the design
- web: Ship a web-app manifest so the SPA installs to the home screen
- library: Add a bulk Rename files action to both library lists
- Transcoding config
- Transcode job entity
- Transcode job store
- Queue transcode on import
- Resolve ffmpeg and read HDR and audio streams
- Transcode rules, ffmpeg args and progress parser
- Transcode worker
- Transcode library scan
- Transcoding REST endpoints
- web: Transcoding queue and settings pages
- config: Transcoding.verify band and checks
- db: Rejected transcode job status
- transcoding: Reject an encode that fails output verification
- api: Expose transcode verification
- web: Rejected transcode jobs and verification settings
- config: Transcoding.hw_accel and hw_device
- transcoding: VAAPI encoding with a probed software fallback
- api: Expose hardware encoding on /config/transcoding
- web: Hardware encoding settings
- deploy: Vaapi image variant
- transcoding: Keep 10-bit sources at 10 bits on the VAAPI path
- indexer: Scope Prowlarr searches by kind and filter on release ids
- task: Release:app cuts the app release end to end

### Changed

- series: Push list filtering and episode counts into SQL
- db: Index the series tree foreign keys and cache posters at w500
- hygiene: Batch drift-check last_seen bookkeeping into one update per tick
- ui: Budget grid mounts on the movies and series pages
- db: Replace_mode on download records
- rss: Movie upgrades use the shared replacement predicate
- web: Move the releases table to shared
- Derive GOMEMLIMIT from the cgroup memory limit
- db: Index the hot predicates and the unindexed foreign keys
- db: Size the page cache, bound the WAL and analyze on boot
- movies: Count statuses in one GROUP BY pass
- bittorrent: Persist uploaded bytes only when they moved
- scheduler: Derive last_started_at instead of writing it every run
- download: Pace the file-selection pass inside its run
- auth: Debounce last_seen_at writes and split session lookup failures from 401
- bittorrent: Cap the anacrolix peer pool and unverified-bytes buffer
- db: Drop the cast blob from every list projection
- config: Memoise assembled quality profiles per config generation
- indexer: Bound torznab responses and cap merged results
- download: Skip the adoption scan when no client is enabled
- db: Open the raw driver when OTel has no endpoint
- series: Apply a monitoring preset in four statements, not one per row
- scheduler: Stagger the first run of each job at boot
- observability: Sample traces and size the log batcher for a small host
- hygiene: Page the drift check over a lean projection
- library: Index tracked shows by tvdb id without the episode tree
- movies: Push list filtering into SQL and store parsed file fields
- web: Paginate the library lists and drop the monitor filter
- library: Drop the never-populated {imdb_id} naming token
- **BREAKING** api: Stop carrying imdb_id end to end
- web: Give the app one dropdown surface
- web: Unfold the sidebar's activity group into two rows
- db: Lean-load the movie on the pending queue
- **BREAKING** config: Rename auth.oidc_default_role to auth.default_role
- task: Merge the chart release into the release namespace
- middleware: Body-limit carve-out uses auth.IsAdmin

### Fixed

- ffmpeg: Probe stream duration and skip cover art
- hygiene: Don't let unreadable rows consume the probe batch
- web: Surface the ffmpeg warning and restart notice
- config: Drop ffmpeg.path existence validation
- ffmpeg: Pick the largest video stream, not the first
- web: Label always_ask hold reasons
- web: Match the backend's resolution buckets
- web: Normalize formatDuration hour rollover
- web: Hide queue controls on held rows
- importer: Verify before replacing existing files
- download: Forward the deleteFiles flag to the client
- db: Held records keep their episodes from reverting
- importer: Fall back to the release title for the resolution claim
- importer: Don't hold records whose source file is missing
- api: Reject unknown resolve actions
- api: 409 queue verbs on held downloads
- web: Refresh system info after an ffmpeg config change
- bulkimport: Parse numeric titles and rank candidates before truncating
- bulkimport: Relocate existing files in rename mode and attach on re-add
- library: Paginate list fetches and stop mis-adopting same-titled shows
- bulkimport: Match on original title, fix nil-span panic, document the batch
- library: Collapse whitespace in sanitised paths and prune emptied dirs
- ui: Pin series toolbar rows and show the synopsis on mobile
- library: Size floor for episodes and TVDB search fallback
- posters: Size cache for the hero render and refill it on metadata refresh
- series: Refetch the poster on metadata refresh
- pathmigrate: Scope the boot drift warning to live download rows
- hygiene: Start the drift grace clock without clearing missing_since
- movies: Carry media files in list responses
- sysinfo: Stop rendering a branch build as a release version
- calendar: Stop labelling untitled episodes as digital releases
- ui: Stop layout shifts and reclaim space across the shell
- docker: Let the frontend stage see pnpm-workspace.yaml
- rss,importer: Close per-episode replacement seams
- rss,db: Stop stranding upgrade targets in downloading
- ui: Render searched activity events in movie history
- web: Clear the SPA typecheck and wire it into lint
- deps: Bump moby/go-archive past CVE-2026-17106
- series: Treat undated episodes as unaired, not wanted
- library: Stop parsing hyphenated source tags as release groups
- torrents: Give the torrent list a stable order
- library: Keep hyphenated release groups and fold bare WEB onto WEB-DL
- web: Refresh nav counts after every mutation
- torrents: Drop the download record when its torrent is removed
- bittorrent: File selections survive restart and metadata resolve
- bittorrent: Progress, completion and ratio over wanted files only
- download: Close pending-selection exits and final-review findings
- db: Replace_mode only ever raises
- mediaserver: Plex refresh falls back to every section
- web: Repaint library views when the download queue changes
- web: Load lookup posters eagerly so they render in modals
- mediaserver: Discover sections for a saved server using its stored token
- web: Latch the config form layout so a breakpoint flip cannot remount it
- web: Let a read-only operator pick and copy plex section keys
- web: Show only the picked plex sections in the config snippet
- bittorrent: Restore tcp dialing and single utp socket under owned peer sockets
- bittorrent: State the ipv6 narrowing honestly and stop upnp in tests
- i18n: Translate the three missing french keys
- dashboard: Mix series into the recently-added and wanted rows
- library: Cut a season pack's title at the season token
- web: Quieter, single-cadence nav and dashboard polling
- web: Give importing its own status kind
- indexer: Scope every TV search to what it asked for
- library: Read French season packs, chained ranges and mixed-case resolutions
- dashboard: Count downloading episodes in the downloading tile
- indexer: Report prowlarr as feed-incapable instead of feed-empty
- download: Bound the qbittorrent add response
- ffmpeg: Bound ffprobe stdout
- indexer: Dedupe merged results by release identity
- web: Count the downloading tile off the live queue
- api: Honour series_id on the activity feed
- indexer: Prefer TV releases that name the show being searched
- rss: Don't spend a grab failure on an unreachable indexer
- library: Match an anime file numbering SxxExx by absolute number
- api: Name a rejected grab's 422 so the UI can say why
- importer: Walk a pack's subdirectories so a whole-series torrent imports
- download: Anchor a pack on the first episode it can fill
- library: Read a three-digit episode number
- events: Stop the owner hook failing an owner-less record insert
- deps: Update go modules (#46)
- library: Read parsed_source off the release name, not the renamed file
- quality: Stop a negate condition matching input nobody recorded
- quality: Score a library file from its stored columns, not its renamed path
- web: Narrow the pack season index rather than assuming it
- db: Unstamp existing probes when the stream columns arrive
- calendar: Hold three items per month cell, floored by the lg row height
- settings: Pin the sub-nav below the toolbar, not under it
- indexer: Filter movie search results to the film that was searched
- restapi: Answer grab_rejected for a pack grab with no wanted files
- web: Claim the bottom-sheet drag on touch, not just the header
- db: Unstamp last_refreshed_at so the new release dates backfill
- web: Let the library filter field take the row's slack
- web: Keep the top bar clear of the status bar when installed
- rename: Build names from the media_file row, not the last rename
- deps: Update go modules (#54)
- Address the transcoding branch review
- web: Tell a refused scan apart from a running one
- docker: Upgrade base packages in the vaapi image
- library: Fold accents when normalizing titles for matching
- rss: Never auto-grab a release naming another show
- library: Search titles accent-folded and keep the list on screen while filtering
- library: Keep list filters when returning from a detail page
- spa: Name the reason when an upload is too large

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

[3.0.0]: https://github.com/datahearth/streamline/compare/v2.0.0..v3.0.0
[2.0.0]: https://github.com/datahearth/streamline/compare/v1.3.0..v2.0.0
[1.3.0]: https://github.com/datahearth/streamline/compare/v1.2.0..v1.3.0
[1.2.0]: https://github.com/datahearth/streamline/compare/v1.1.0..v1.2.0
[1.1.0]: https://github.com/datahearth/streamline/compare/v1.0.0..v1.1.0
[1.0.0]: https://github.com/datahearth/streamline/tree/v1.0.0

<!-- generated by git-cliff -->
