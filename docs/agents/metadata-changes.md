# Metadata changes

> **Read before touching** `internal/db/metadata_change.go`, `internal/media/*/changes.go`, the `/metadata-changes` handlers, `web/app/lib/metadata-changes.ts` or `web/app/components/changes/`.
>
> **Covers** — what a refresh applies on its own and what it asks about, the `MetadataChange` row and its contract, following a change, and the SPA surfaces.

## The rule

A metadata refresh, for every type, writes what it can by itself: text, dates, lengths, art, credits, merged recording ids, and anything about an item holding no files. **It stops and asks only when a change would alter what an item's files are matched against, or take the item out of the library.** Nothing in that second set changes until an admin decides.

| Owner | Kinds | Detected in |
|---|---|---|
| album | `edition_removed`, `edition_merged`, `edition_preferred`, `tracks_unlisted`, `album_removed`, `album_merged` | `music.hydrateAlbum` (`reviewAlbum`, `reviewTracks`), only for an album holding files |
| artist | `artist_removed`, `artist_merged` | `music.RefreshOne` (`reviewArtist`): a 404, or an MBID other than the stored one |
| tv_show | `series_removed`, `episodes_unlisted` | `tvshow.RefreshOne`: a TVDB 404, and the episodes holding files `ReconcileEpisodes(…, keepHeld=true)` kept |
| movie | `movie_removed` | `movie.refreshOne`: a TMDB 404 |
| book | `book_removed`, `edition_removed` | `book.RefreshBook` / `refreshBooks`: missing from the batch, and `reviewEditions` (`ExistingEditions`) |

TMDB and TVDB answer a 404 as `metadata.ErrNotFound`; a merge is MusicBrainz answering a lookup with another id (`ResolveRelease` tells a merged release from a deleted one). Hardcover exposes no merges, so a book has no `*_merged` kind.

## The row

`MetadataChange`: `kind`, `state` (`pending|kept`), `fingerprint`, `payload` (what the SPA shows: `files`, `current`/`upstream` edition summaries, `items` with `id`/`label`/`title`, `target`, `slots`, `seasons`), one owner edge, cascading with its owner. **One row per owner and kind** (a unique index per owner edge). `db.ReportChange` inserts a pending row, refreshes a pending one, leaves a kept one alone **for the same fingerprint** and makes it pending again for another — that is "Keep as is, asked again only when the provider changes the same thing differently". `db.ClearChange` deletes the row whatever its state once the condition is gone, so its return is a new change. The fingerprint is what identifies the change: the target MBID of a merge, the canonical release, the sorted track or episode ids, `kind:edition` pairs for a book.

## Following

`POST /metadata-changes/{id}/follow` dispatches by owner to each service's `FollowChange`:

- album: `edition_removed`/`edition_preferred` → `SetAlbumEdition("")` (canonical); `edition_merged` → `SetAlbumEdition(target)`; `tracks_unlisted` → `DeleteAlbumTracks` (files stay on disk, untracked); `album_merged` → `RepointAlbum` (drops the target's stub when nothing was done with it, else `409 change_conflict`) then `hydrateFollow`.
- artist: `artist_merged` → `RepointArtist`: the MBID moves, or every album is handed to the artist already in the library and this one is deleted.
- tv_show: `episodes_unlisted` → `DeleteUnlistedEpisodes`, then the seasons the provider no longer lists that are left empty.
- book: `edition_removed` → `SetBookSlotEdition(…, 0)` for each format, then `RefreshBook` picks a new one.
- every `*_removed` of an item: `409 not_followable` — find it again (re-identify, movies and series), remove it, or keep it.

Picking an edition, re-identifying and removing go through the existing endpoints; `SetAlbumEdition` (`hydrateFollow`) and the re-identify paths clear the owner's changes. Admins only, everywhere.

## SPA

`/metadata-changes` (Activity group, sidebar count, top-bar title, palette page, dashboard "Needs you" chip) lists every pending change, filtered client-side by media and kind group. `pendingChangesQuery` (`["metadata-changes","list"]`, every page through `apiAllPages`) is the one source for the list, the cover marks (`markIndex`: an artist also carries its albums' changes) and the banners (`ChangeBanners` on the movie, series, book and artist pages and the album panel). `["metadata-changes"]` is a nav-count key, so every mutation refreshes it. `ChangeCard` is both the list row and the banner and owns follow, keep, find-again (`ReidentifyDialog`), the album's edition picker and removal (`DeleteTitleDialog`). An album has no delete endpoint, so `album_removed` offers keep only.
