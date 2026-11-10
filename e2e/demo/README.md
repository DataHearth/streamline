# Demo library and README screenshots

`task docs:screenshots` retakes the images in `docs/assets/` (all but `calendar.png`, see
[The calendar](#the-calendar)). It builds the binary, then
`go run ./e2e/demo` does the rest:

1. boots a throwaway instance on a scratch directory, with its own port, admin and config;
2. seeds the demo library through the REST API — the same calls the SPA makes;
3. shoots each README page at 1440×900 (and `mobile.png`, three phone pages side by side) into `docs/assets/`.

Nothing outside the scratch directory is touched but `-out`. `-keep` leaves the instance running
afterwards (the URL and the login are printed), `-only dashboard,movies` retakes a subset, `-scale 2`
shoots at 2×.

It needs `ffmpeg` (the media files) and a Chromium (`CHROME_PATH`, one on `PATH`, or Playwright's
under `PLAYWRIGHT_BROWSERS_PATH`, as `.claude/skills/verify/shot` finds it).

## What the library holds

`catalog.go` is the whole recipe: public-domain films and TV, the Blender Studio's open films,
Creative Commons music and public-domain books, looked up by name. What the seeder does with them
(`seed.go`, `downloads.go`):

- **Adopted from disk** by the bulk import: films, series seasons, albums and ebooks, written by
  `e2e/demomedia`. A video file is a real MKV header — resolution, HDR, codecs and a feature-length
  duration the probe reads — padded to its release size with a sparse hole, so a 30 GB film costs
  a few KB. Albums carry the real track titles and release group, read from the instance once the
  artist is followed.
- **Grabbed from `e2e/faketracker`**: two quick films (one imports, one is held on its resolution —
  the import-verification shot), two albums, an ebook and an audiobook (history), then three films and
  two season packs, large and rate-limited (`-rate`), so the queue is mid-download when it is shot.
  The manual-search film is listed at every resolution.
- **Followed with nothing on disk**: whole discographies, two book series, a few wanted films and books.
- **Requests** from a member account, one approved and one denied.
- **The movie root scanned again and left awaiting review** after more films land in it, with one file no provider knows (an in-place scan has to stay inside the library, so the adopted films show as existing beside them).

A step that fails (a title the provider no longer answers for, a cassette entry that is missing) is
logged and skipped, and the run goes on: the summary at the end lists what was skipped.

## The cassette

Every call the instance makes to a metadata provider or image CDN — TMDB, TVDB, MusicBrainz,
Hardcover, Deezer, the Cover Art Archive, Wikimedia — goes through a recording HTTPS proxy
(`proxy.go`). The instance trusts the proxy's throwaway CA (`SSL_CERT_FILE`) and routes through it
(`HTTPS_PROXY`), so the application has no seam of its own for this.

- **A plain run replays** `e2e/demo/cassette/`: no key, no network, the same library every time. A
  request the cassette lacks answers `502` and is listed in the summary.
- **A call that is no provider's is refused in both modes**, neither recorded nor counted: the
  built-in indexer definitions the instance fetches from GitHub (`offCassette` in `proxy.go`), a
  tarball of megabytes the demo has no use for.
- **The cassette lives on the `demo-cassette` branch**, an orphan holding nothing else, not in
  `main`'s history: its images are megabytes no build needs. `task docs:screenshots` fetches it into
  the (gitignored) directory when it is not there (`task docs:cassette:fetch`; `CASSETTE_REMOTE` and
  `CASSETTE_BRANCH` override `origin` and `demo-cassette`). After recording, `task
  docs:cassette:publish` replaces the branch with the directory as one parentless commit, so the
  branch never accumulates superseded images either.
- **`-- -record`** sends what the cassette lacks to the real providers and keeps it. It needs
  `STREAMLINE_METADATA__TMDB_API_KEY`, `__TVDB_API_KEY` and `__HARDCOVER_API_KEY` in the environment.
  To refresh from scratch, delete the directory first.

An exchange is one JSON file under its host, named by a hash of the method, the URL (query sorted)
and the body. The provider keys are replaced by `REDACTED` before hashing and in everything written,
and a `token` field in a response (TVDB's login) is replaced too, so a cassette holds no credential
and replays with none (recording, that login is fetched again rather than replayed: the instance
needs the real token). Error answers (`401`, `403`, `429`, `5xx`) are passed through and never kept.
An image wider than 800 px is scaled down by a whole factor and re-encoded before it is kept (and
handed to the instance the same way, so a recording run draws what its replay will).

Recording waits, before the shots, for every followed artist's discography to finish hydrating: at
MusicBrainz's one request a second that takes minutes, and an album the worker had not reached
would be one the unthrottled replay then asks for and misses.

Replaying moves every date within two years of an exchange's recording forward by the whole weeks
since, so an album that was a week away the day of the recording is a week away today. Older dates,
a film's or a book's own, stay put. Each exchange carries
the day it was kept (`recorded_at`), so one added by a later `-record` is shifted from its own day;
one without the field is from `cassette.json`'s `recorded_at`, the day the cassette was started.

## The calendar

`calendar.png` is the one image the demo does not retake today. The calendar lists wanted films,
monitored episodes, albums and books dated this month or the next thirty days, and a library of
public-domain and open titles has none. The date shift keeps such an entry current, it cannot make
one.

So the shot is taken only when `/calendar/upcoming` answers with something, and is otherwise skipped
with a warning, leaving the image in `-out` as it is rather than replacing it with an empty grid. It
comes back by itself with a cassette recorded while a catalog entry had a date ahead of it (a
followed artist with an album announced, a book series with a volume due).
