# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [3.3.0] - 2026-10-28

### Added

- Music entities
- Musicbrainz metadata provider
- config: Music quality profiles family
- metadata: Musicbrainz client
- Album cover art via posters proxy
- Music service
- Music REST endpoints
- Music SPA section
- Book entities
- Hardcover metadata provider
- Books config (hardcover key, paths, quality profiles)
- Hardcover client
- Book cover art via posters proxy
- Book service
- Books REST endpoints
- Books SPA section
- web: Section headings move into the page
- Plain-language manual search with tracker privacy and grab history
- library: Audio tag reading
- Ebook metadata reading
- Musicbrainz release-group search
- metadata: Hardcover book search and isbn lookup
- Music release parser and scoring
- Book release parser and scoring
- indexer: Album search
- indexer: Book search
- music: Import scan entities
- books: Import scan entities
- Download record album and book edges
- Music and book request entities
- Per-user subsonic password
- Per-user opds token
- Album release calendar
- Book release calendar
- audiotags: Tag writing
- Subsonic server package
- Opds feed types and auth
- Opds catalog feeds
- Opds downloads
- Music request flow
- Book request flow
- Album grabs
- book: Release search and grab per slot
- Music orphan scanner
- Adopt albums on music import commit
- Book orphan scanner
- Adopt books on import commit
- subsonic: Mount subsonic server
- opds: Mount opds server
- Import scan REST for music and books
- music: Album manual search and grab
- books: Book slot manual search and grab
- Music and book request REST
- importer: Import completed album downloads with tag writing
- importer: Import books per slot
- music: Backlog search and artist refresh schedulers
- book: Backlog search and author refresh schedulers
- rss: Auto-grab wanted albums from indexer feeds
- rss: Auto-grab wanted book slots from indexer feeds
- metadata: Deezer cover client, poster Put, album barcode and embedded-picture reader
- music: Resolve album covers from embedded art, folder, Deezer and the archive
- metadata: Honour Hardcover rate limit and daily budget
- e2e: Album, ebook and audiobook packs in the fake tracker
- web: Pull music, books, settings logs and bar titles from the design project
- Separate default quality profiles for movies and series
- api: Music and books contract for the design
- Music and book quality profiles, requests and lookup art for the design
- web: Wire the music and books design to the real API
- music: The backend the music design needs
- books: The book-first backend the books design needs
- Subsonic and opds access cards, hardcover key and codes, music and book calendar entries
- Music and book import review fields, release-group search and album requests
- web: Pull the second music and books design batch
- config: Seed a default music quality profile
- ci: Build the nix checks on the homelab builder
- activity: Name the album, pack artist or book on queue and history rows (#84)
- web: Lead the login page with SSO, fold the password form (#87)
- scheduler: Give music and books their own scheduled jobs
- metadata: Support preferred book language in Hardcover editions (#88)
- web: Redesign the dashboard around new arrivals across all four media
- library: List recent imports across movies, series, music and books
- music: Sort the artist list by name by default
- activity: Identify unmatched proposals as albums and books
- config: Add smtp section
- mailer: Add stdlib SMTP sender
- account: Add device email to the profile
- books: Send ebooks to a device by email
- api: Add book and series send endpoints
- web: Send books and series to an e-reader
- metadata: Read series membership and slugs from Hardcover
- books: Find series through translated titles and keep their slugs
- api: Expose series covers, slugs and translated matches
- web: Fan series covers and find series by translated title
- web: Bulk pause, resume and remove on torrents and activity
- subsonic: Serve the rest of the Subsonic API and OpenSubsonic (#99)
- music: Write the full MusicBrainz tag set without destroying the file's own (#105)
- web: Swipe actions on the activity and torrent touch rows
- History tab for music and books, swipeable detail tabs
- Record cancellations, monitoring and failed bulk imports for music and books; run the fake tracker anywhere
- music: Preview a release's files and grab only the missing tracks
- series: Grab only the missing episodes from a season or series pack
- series: Preview a season or series pack's files before grabbing it
- music: Preview a discography pack's files before grabbing it
- movies: Show the digital release date on the movie page
- activity: Open a download's torrent from the queue
- torrents: Show and search a builtin torrent by its release title
- search: List albums and episodes as their own search results
- web: Say "Searching…" while the search waits on its answers
- mediaserver: Targeted folder rescans, path mappings, coalesced refreshes (#144)
- config: Write the defaults when the config file is missing
- deploy: Unraid template and Proxmox LXC install script
- db: Back up the database before migrations, daily, and from the CLI
- download: Refuse a grab that would not fit on the download disk
- auth: API key read-only scope and expiry
- music: Co-artist edge and per-track artist credits
- metadata: Read each track's credited artists
- music: Shared albums on every artist's page, kept when an artist is deleted
- music: Tag each track with its credited artists
- music: Show shared albums and per-track artists
- subsonic: Serve shared albums under every artist
- music: Refuse single-file CD images before they download
- subsonic: Draw playlist covers as a mosaic
- ui: Add the add button to the music and books toolbars
- metadata: Make the MusicBrainz server URL configurable
- metadata: Make the MusicBrainz request interval configurable
- settings: Edit the MusicBrainz server and request interval

### Changed

- web: Move torrents, transcoding and imports to top-level routes
- db: Link download records to every episode they cover
- nix: Fetch Go modules without building the frontend
- migrate: Check every generated down step against the replayed schema
- migrate: Check the whole migration history and regenerate broken downs

### Fixed

- hygiene: Adopt library orphans in place instead of hardlinking a twin
- dashboard: Order recently added movies by file import time
- bittorrent: Report seeding only once every wanted piece is hash-verified (#74)
- web: Keep the topbar title still and stop doubling page headings
- web: Translate the remaining UI strings and fix search field chrome (#75)
- deps: Update go modules (#67)
- media: Rescan media servers after deletes, renames and bulk imports
- deps: Update go modules (#76)
- db: Keep a rejected upgrade's episode available
- download: Keep a completed record while its builtin torrent still seeds
- opds: Sanitize download filename and fix gosec directive
- subsonic: Log non-API errors before the generic envelope
- audiotags: Falsifiable replace spec, drop debug print
- media: Book and album grab review fixes
- bulkimport: Record span failures, dedupe adopted tracks, guard book commit and decisions
- Hardcover author role, shared book titles and subsonic POST params
- shared: Review findings for the design backend
- web: Review findings for the design backend
- music: Review findings for the design backend
- books: Review findings for the design backend
- db: Count held records as in flight for album searches
- Review findings for the round-2 backendA change
- Review findings for the round-2 backendB change
- Review findings for the round-2 spa change
- metadata: One member per person from musicbrainz relations
- metadata: Pass over fragment editions when naming a book's original
- indexer: Keep audiobooks out of music searches
- Never auto-grab or import a release that names another album or book
- download: Refuse a grab whose hash an in-flight record already holds
- bittorrent: Fail a magnet the path guard refuses after metadata
- mediaserver: Refresh media servers after music and book imports, renames and deletes
- opds,subsonic: Serve OPDS covers under Basic auth, stamp empty feeds, implement getIndexes
- posters: Serve the cached image's real type and leave no empty dir on a failed fetch
- request: Load the requester edge on the create response
- books: Date a book by its original edition and rank author matches first in lookup
- bulkimport: Adopt a multi-disc music album as one candidate
- subsonic: GetIndexes tracks album and track changes and never answers stale
- bulkimport: Clean the scan root before folding disc folders
- download: Revert stranded albums and book slots, read base32 magnet hashes as hex
- web: Read an import row by its own shape across scan-to-scan navigation
- web: Page title claims name their path so a left page's title never sticks
- web: Drop the in-page Imports heading the top bar already shows
- deps: Build with Go 1.27.2 and take toolchain patches from upstream (#82)
- web: Consistent monitoring, refresh and error feedback across movies, series, music and books (#83)
- music: Set aside album search results named for another album (#85)
- Prevent stale search results in releases dialog (#86)
- web: Artist rows show owned then wanted releases, never unmonitored ones
- music: An album monitored or dropped by hand survives the monitored-types catch-up
- db: The replace_mode migration rolls back
- Make #main positioned to contain sr-only labels (#89)
- series: A season or episode monitored by hand survives the specials catch-up
- deps: Update go modules (#93)
- web: Link Hardcover books through the id route
- mailer: Drop the connection when a message fails mid-write
- config: Accept a dotless smtp sender like streamline@localhost
- books: Leave unowned volumes out of a series send's skipped list
- importer: Refuse single-file CUE album images
- music: Park albums at failed after a refused import
- music: Settle a partial album on a failed or stranded grab
- books: Keep series status, the add pick and the book hits in translated series search
- books: Drop an empty series the batch did not return
- web: Stop retrying lookup covers that do not exist
- cli: Reject unknown commands instead of booting the server (#98)
- quality: Classify 4:3 1080p files as 1080p, not 720p (#100)
- web: Show Ctrl K, not ⌘ K, off Apple platforms (#104)
- docker: Re-run the vaapi apt upgrade on every CI build (#126)
- web: Make the activity page readable on phone, tablet and desktop
- web: The phone selection takes over the top bar
- download: Revert episodes, albums and books when a download is cancelled
- e2e: XML-escape the faketracker's .torrent link
- web: Let the release list scroll with the modal on phones
- music: Rank a partial MusicBrainz date after the fuller ones in its period
- music: Read tracker-tagged release names and interleaved editions
- download: Order the queue by status, then progress
- subsonic: List only albums and artists holding a playable file
- music: Skip video discs and take the standard edition on a same-day tie
- deps: Bump Go to 1.27.3 for GO-2026-6809 (#135)
- Run manual series and movie searches off the request and map add errors by cause
- movies: Return the digital release date from the add response
- importer: Import only the files the record's own torrent lists
- bittorrent: Delete only a removed torrent's own files
- web: Make search say what it finds and reach every settings section
- search: Rank the closest album and episode title first
- tvshow: Title aired episodes the metadata language has no translation for
- config: Ask for the secret again when a resource moves to another address
- config: Ask for the OIDC client secret again on an issuer change
- web: Hide Torrents and Transcoding from the nav while they are off
- downloads: Blocklist a release rejected from the held-download dialog
- db: Walk every episode of a failed record back, not just its anchor
- ci: Stop trying to set Issue Types — this repo has none (#142)
- web: Close phone search and bottom sheets on Back (#143)
- web: Show a distant API key expiry as a localized date
- web: Localize relative dates past one month
- artwork: Tell namesake artists apart for search tile photos
- search: Show covers and identifying text for every lookup hit
- music: Guard library artist photos against shared Deezer pages
- metadata: Wait a minute on a zero MusicBrainz Retry-After
- music: List a shared release group under every artist instead of failing the add
- music: Backfill track artists at any release size instead of bumping the credits layout
- music: Write track artists with MusicBrainz's join phrase and know a merged artist id by name
- music: Report a shared album's owner on every page and space the track credit lines
- subsonic: Name a split release's songs by their own artist and order co-albums by date
- music: Poll and show history for an artist's shared albums too
- music: Hand shared albums over inside the artist delete and re-tag them
- music: Log a shared album's link only once it is committed
- web: Tell a metadata provider's rate limit apart from a login lockout
- music: Hold MusicBrainz prefetch and background calls after a 503
- web: Count an artist's owed releases, not the whole discography
- download: Keep the records of torrents that still seed
- subsonic: Match search on artist and album names
- metadata: Drop Deezer's stock artist picture
- music: Run a refresh's artist photo lookup at interactive priority
- music: Refuse CUE images split one file per disc side
- db: Sort titles and artist names accent-folded
- activity: Show stalled downloads in the queue
- music: Scroll the release list on its own and open on a monitored album
- hygiene: Probe only movie and episode files
- importer: Fall back to file names for untagged album tracks
- music: Sort artists by their displayed name
- music: Treat remix, DJ-mix, mixtape and demo release groups as other
- music: Leave bootleg-only release groups out of the discography
- music: Flag search results that name neither the artist nor the album
- library: Read a year between the artist and the title of a release name
- deps: Update go modules (#149)
- music: Ignore search and bookkeeping events when pruning unlisted albums

## [3.2.0] - 2026-09-25

### Added

- nix: Package, NixOS and home-manager modules, VM test
- metadata: Store TMDB translated and alternative titles
- indexer: Match releases against the provider alias set
- indexer: Record the alias count on search spans
- web: Fade images in once fully decoded
- web: Fade backdrops, cast and lookup images in too
- auth: Let an OIDC provider auto-provision accounts under invite mode
- api: Page the pending adoption list

### Fixed

- ci: Drive the Claude CLI directly for release notes
- ci: Give the release-notes prompt the house style
- web: Translate the nav count lines instead of hard-coding English (#61)
- download: Match adopted movie torrents against aliases too
- media: Report files a delete did not remove and prune orphaned metadata
- download: Spare the seasons a multi-season pack claims
- indexer: Refuse a longer show built on this one's name
- download: Scope a record's episode-state writes to the episodes it claims
- download: Record what an adopted torrent's episodes are
- bittorrent: Survive an unusable announce URL and clean up a failed add
- bittorrent: Report fetching until file priorities are applied
- ci: Give each derived hash its own sentinel so the pins stop flip-flopping
- ci: Derive each package hash from its own attribute
- deps: Update go modules (#66)
- importer: Coalesce an enqueue that lands while its import is in flight
- ci: Enforce the release-notes no-tools rule in the CLI call
- requests: Count only the caller's own requests for request_only
- web: Gate admin-only pages, links and queries by role
- web: Hide the free-space tile from non-admins
- restapi: Answer the spec 403 on member-only movie and series verbs
- auth: Log the account an admin acts on as target.user.*
- web: Hide library controls from request_only users
- library: Keep deletes and renames inside the library root
- library: Widen a dash-chained release group in linear time
- importer: Fail a replacement with no usable media before removing the old file
- tvshow: Leave a pruned episode's file on disk on metadata refresh
- config: Redact secrets held inside list entries in the audit line
- config: Keep the config file owner-only on every write
- auth: Revoke a user's other sessions when OIDC claims change their role
- auth: Keep API keys and re-pointed issuers away from account-granting config
- auth: Withhold the trusted-network grant from cross-site writes
- api: Keep the download client's address out of the pending preview error
- otelx: Cap every outbound response body after gzip inflation
- download: Hold every release redirect hop to the indexer allowlist
- download: Bound adoption work per tick and prune stale proposals past the bind limit
- bittorrent: Refuse torrent names that land on other data and make removal survive a restart
- bittorrent: Cap how many torrents the builtin engine holds
- bittorrent: Keep release-named trackers and webseeds off internal addresses
- ci: Build release notes in a job that holds no publishing credentials
- api: Hand members a sealed release handle instead of the indexer's keyed link
- auth: Withhold the trusted-network grant from hosts DNS rebinding can forge
- api: Tell a grab with an expired release handle to search again
- media: Leave a file in place when the naming template would rename it out of the library
- media: Refuse a single-file delete outside the library root with 409
- download: Reject malformed listing hashes and prune proposals of disabled clients
- bittorrent: Refuse a torrent whose name holds another release's data on disk
- importer: Place a replacement before removing the file it replaces
- otelx: Bound each small JSON answer to its own ceiling

## [3.1.0] - 2026-09-16

### Added

- ci: Publish a chart release with Claude-written notes
- observability: Recover job panics and close the transcode/scheduler metric gaps
- auth: Observe registrations, denials and the rate-limiter lapse
- download: Count adoption, selection and reachability outcomes
- indexer: Classify query failures and instrument the metadata providers
- library: Log destructive file operations and count import runs
- db: Log lifecycle transitions, migrations and the request workflow
- events: Add library, file, transcode and request event types
- events: Record adds, removals and monitoring changes from mutation hooks
- media: Record renames, re-identifies and metadata refreshes
- transcoding: Record terminal job outcomes in the activity feed
- download: Record cancellations and widened grabs
- request: Record approvals against the title they added
- series: Record one monitoring event with an episode count, not one per episode
- web: Show loading skeletons instead of bare text while lists and panels load
- web: Narrate the wait for release search and scans, and lift the grab modifier into the modal footer
- series: Count seasons on the card and keep specials out of show totals
- cast: Normalize cast into persons and credits with real provider ids
- web: Add a cast member page and search people from the palette
- cast: Fetch person details from TMDB and TVDB at ingest
- web: Show biography, life facts and brand links on the cast page
- activity: Forget a proposal so its torrent can be adopted again
- events: Record bulk episode events once per series
- download: Adopt a torrent whose file is already in the library as completed
- tvshow: Keep TVDB aliases on the show and adopt torrents named by them
- transcoding: Defer an encode while its torrent is still seeding
- web: Surface defer_seeding and the deferred re-check on queued jobs
- web: Make every series detail kebab action work
- web: Edit a quality profile's transcode policy
- auth: Choose an invite's lifetime when creating it
- web: Icon the type and monitoring dropdown items
- api: Count list facets against the other filters
- scheduler: Report run progress and broadcast state changes
- jobs: Report progress from every per-item loop
- api: Stream schedules as server-sent events with run progress
- web: Live schedules page with run progress
- scheduler: Let a manual run waive job throttles
- transcoding: Leave lean sources alone with if.min_video_bitrate
- transcoding: Make hw_accel vaapi hardware-only
- download: Remove a builtin torrent with its files once seeding ends
- series: Surface per-series grab and import history
- series: Name what each history event touched
- movies: Give the recommendation add its full detail panel

### Changed

- release: One _changelog task for both changelogs
- release: Tag and push with jj instead of git
- web: Share one event mark table across both activity feeds
- web: Apply bulk import decisions in one request
- requests: Filter by media type server-side
- web: Share detail components and alias @components/@lib imports
- settings: Drop the App name card

### Fixed

- release: Ignore chart-v* tags in git-cliff and goreleaser
- ci: Stop the oldest changelog section swallowing the link block
- ci: Pin the release-notes model instead of drifting with the CLI
- release: Keep helm-scoped commits out of the app changelog
- observability: Stop log.app.enabled from disabling the whole pipeline
- events: Stop a failed event write from failing the mutation it records
- web: Portal the global activity bar to body so it paints above modals
- web: Give each activity event mark its own icon
- calendar: Fall back to the theatrical date when TMDB has no digital release
- web: Scroll long activity lists inside the list, not the page
- web: Give the transcoding page the app's own dropdown and a right-aligned chevron
- web: Tint rejected slate rather than red
- search: Fold punctuation so a title's commas need not be typed
- db: Keep foreign keys off across a migration run
- web: Open the rename modal from the series page kebab
- series: Make a show's monitored flag gate its episodes
- web: Size the IMDb wordmark so it resolves at chip size
- bulkimport: Match an episode file on its parent folder when the basename is bare
- bulkimport: Walk every folder above a bare episode file, not just one
- download: Let a folder torrent's sidecar files pad the size match
- download: Stop vetoing a same-size adoption on a template-parsed group
- importer: Drop a moved torrent from its client once its record imports
- library: Read the library config live in the import service
- activity: Badge History with the total, not the page
- settings: Lock every control on a read-only instance
- api: Stop logging a closed schedule stream as an error
- transcoding: Refund the attempt when boot recovery requeues a job
- docker: Drop pebble from the vaapi image so grype scans only our binary
- download: Adopt, locate and recover imports without stranding episodes
- bittorrent: Fence test fixtures on piece marks after verify
- series: Report a show's last import so arrivals surface new episodes
- people: Carry the episode rollup on series credits
- web: Hold the person backdrop still when the biography expands
- web: Stop read-only fields inviting an edit
- imports: Scroll the scan list inside its card

## [3.0.0] - 2026-09-08

### Added

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

[3.3.0]: https://github.com/datahearth/streamline/compare/v3.2.0..v3.3.0
[3.2.0]: https://github.com/datahearth/streamline/compare/v3.1.0..v3.2.0
[3.1.0]: https://github.com/datahearth/streamline/compare/v3.0.0..v3.1.0
[3.0.0]: https://github.com/datahearth/streamline/compare/v2.0.0..v3.0.0
[2.0.0]: https://github.com/datahearth/streamline/compare/v1.3.0..v2.0.0
[1.3.0]: https://github.com/datahearth/streamline/compare/v1.2.0..v1.3.0
[1.2.0]: https://github.com/datahearth/streamline/compare/v1.1.0..v1.2.0
[1.1.0]: https://github.com/datahearth/streamline/compare/v1.0.0..v1.1.0
[1.0.0]: https://github.com/datahearth/streamline/tree/v1.0.0

<!-- generated by git-cliff -->
