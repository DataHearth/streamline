# Demo library and README screenshots

`task docs:screenshots` retakes every image in `docs/assets/`. It builds the binary, then
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
- **`-- -record`** sends what the cassette lacks to the real providers and keeps it. It needs
  `STREAMLINE_METADATA__TMDB_API_KEY`, `__TVDB_API_KEY` and `__HARDCOVER_API_KEY` in the environment.
  To refresh from scratch, delete the directory first.

An exchange is one JSON file under its host, named by a hash of the method, the URL (query sorted)
and the body. The provider keys are replaced by `REDACTED` before hashing and in everything written,
and a `token` field in a response (TVDB's login) is replaced too, so a cassette holds no credential
and replays with none. Error answers (`401`, `403`, `429`, `5xx`) are passed through and never kept.

Replaying moves every date within two years of the recording (`cassette.json`'s `recorded_at`)
forward by the whole weeks since, so an episode that aired the week of the recording airs this week,
and the calendar shot always has a month to show. Older dates, a film's or a book's own, stay put.
