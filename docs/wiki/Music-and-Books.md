# Music and Books

Streamline manages music and books the same way it manages movies and TV: browse and organise what you already have, then let it search, grab and import what is missing, and let other people request titles. This page covers the parts that are specific to these two libraries. The web pages for them are still being built, so most of what follows is reachable through the [REST API](REST-API) today.

## Adopting an existing collection

Point an import scan at your music or book folders with `kind: music` or `kind: book` (see [Importing an Existing Library](Importing-an-Existing-Library) for the scan, review and commit flow).

**Music** groups every folder that directly holds audio files into one album candidate. Streamline reads the embedded tags, matches the artist and album against MusicBrainz, and lists the candidates for review. Committing adopts the files where they are: no renames, no tag writes. The artist is added unmonitored, so adopting a few albums never turns a whole discography into a search list. Rename mode is refused for music.

**Books** groups ebooks by file name (an `.epub` and a `.mobi` of the same title are one candidate) and every folder of audio files into one audiobook candidate. Identification tries a Calibre `metadata.opf` sidecar first, then the epub's own metadata, then the file name, and resolves through Hardcover by ISBN when one is known. Book scans need a Hardcover key; without one the scan is refused.

### Hardcover limits

Hardcover's free plan allows 60 requests a minute and 5,000 a day. Streamline paces itself at one request a second and keeps its own count of the day (resetting at 00:00 UTC), holding back 500 requests as a reserve so a big scan cannot lock you out of adding authors.

- A book scan checks the budget before every candidate. When only the reserve is left, or Hardcover answers `429`, the scan **fails** with a reason naming the limit and when it clears; re-run it afterwards. Nothing is half-imported.
- Adding or refreshing an author, and approving an author or book request, answer `429` with a `Retry-After` header when the minute limit or the day's budget is gone. Try again after that many seconds; the request stays pending.
- The scheduled author refresh stops at the first `429` and carries on at its next run.
- When a title has an ISBN, a scan resolves it with one request and does not search by title.

## Listening and reading from other apps

| Protocol | Path | Clients | Credentials |
| --- | --- | --- | --- |
| Subsonic 1.16.1 | `/rest` | Symfonium, DSub, play:Sub, Sublime Music | your email plus a generated Subsonic password |
| OPDS 1.2 | `/opds` | KOReader, Moon+ Reader, Foliate, Thorium | your email plus a generated OPDS token |

Both secrets are per user and separate from your login password. Generate, rotate or disable them through `/api/v1/account/subsonic-password` and `/api/v1/account/opds-token`; the account page in the web UI will expose the same actions. Neither secret works for the web UI or the REST API.

Subsonic serves artists, albums, songs, album lists, search and raw streaming with seeking. Transcoding parameters sent by the client are ignored. OPDS serves an author catalogue, a recently-added feed and search, and downloads the best available ebook format per book. Audiobooks never appear in OPDS. Cover thumbnails in OPDS readers are not available yet: the cover URLs require a web session.

## Searching and grabbing

The unit of acquisition is the album for music and the slot (ebook or audiobook) for books. Each has a manual search that returns every release the indexers offer, scored against the quality profile and with rejected releases kept at the end with their reason, and a grab that sends the chosen release to the download client.

A stock install has no music profile, so a music search answers with `no_quality_profile` until you create one in Settings → Quality profiles; the first one you create becomes the default. Books ship with a stock `default` profile.

**Music profiles are tiers, not formats.** A profile ticks the tiers it accepts, best first: *hi-res* (24-bit lossless), *lossless*, *high* (MP3 320, V0, AAC 256), *standard* (MP3 192-256, V2) and *low* (under 192 kbps), and names the *preferred* one, which is where upgrades stop. Leaving hi-res unticked is how a lossless profile keeps 24-bit files off your disk. A release whose name states no quality is listed but set aside, with the reason; if ffprobe is available an imported file is measured, and a file whose real tier your profile does not accept (a "FLAC" that is a 128k transcode) is held for your decision before anything is copied.

**Book profiles cover both slots.** One profile holds the accepted ebook formats (`EPUB`, `AZW3`, `MOBI`, `PDF`, `CBZ`, `CBR`) and audiobook formats (`M4B`, `MP3`, `M4A`, `FLAC`), a preferred one for each, and a minimum audiobook bit rate. A book takes the default profile of its kind (novel, bd, comic or manga). An audiobook whose files are mostly in an unticked format, or whose measured bit rate is under the minimum, is held before import.

**Upgrades.** With *Upgrade allowed* on, the RSS scan replaces what you have with a better release it sees until the preferred tier or format is reached: an album per track, only where the new tier beats that track's own. Nothing searches in order to upgrade. Ticking *replace existing files* on a manual grab replaces the matching files once the new ones are in place and verified.

When a download completes, music is imported with corrected tags and MusicBrainz ids written into the library copy, renamed per `library.music_naming`. Because tags are written, music imports copy instead of hardlinking (and instead of moving when seeding is kept on), or the torrent you are still seeding would be corrupted. Books are never modified: an ebook is placed as one file named per `library.ebook_naming`, an audiobook as one folder per `library.audiobook_naming` with its original file names.

## Adding an artist

Adding an artist returns at once with one album per MusicBrainz release group. Streamline then fills in the tracks, labels, credits, covers, a Wikipedia overview and a Deezer photo in the background, queued so that no single artist holds the rest up and always behind anything you are waiting on (MusicBrainz allows one request a second for everyone). The artist page shows what has arrived and keeps checking until it is done.

An artist's **monitor** policy decides which albums are searched for: *all* monitors every release group, *future* only the ones with no date or a date still ahead, *manual* leaves the choice to you album by album, *none* monitors nothing. Changing the policy re-applies it to the albums already there, and a refresh applies it to release groups new to the artist. An album dated in the future is *upcoming*, not missing, until its release date.

The overview comes from Wikipedia in English and French, linked through MusicBrainz's Wikidata entry, and is shown in your language when there is an article in it. The photo is Deezer's, found through MusicBrainz's own Deezer link, or by name when exactly one Deezer artist has that name: a namesake never gets the wrong face.

## Discography packs and per-artist actions

An artist's page can search the indexers for a **discography pack** (only packs named for that artist are listed, each judged against the artist's quality profile) and grab one. The pack is linked to every wanted, released album of the artist; when it finishes, each folder is matched to one of those albums by its name (and year, when it names one) and imported like an ordinary album download. Albums no folder names go back to wanted, nothing counted against them. *Search now* on an artist, album or track runs one search-and-grab pass at once, ignoring the cooldown. *Rename* previews or applies the library naming pattern to every file of the artist, from the database, without touching tags. Deleting a track's file puts an available album back to wanted.

## Album covers

Streamline stores one cover per album, taken from the first of these that has an image:

1. A picture embedded in one of the album's audio files.
2. A `cover`, `folder` or `front` image (`.jpg`, `.jpeg` or `.png`, any case) in the folder of the album's first file.
3. Deezer, looked up by the album's barcode as MusicBrainz records it.
4. Deezer, searched by artist and title; the result is used only when Deezer's artist matches yours.
5. The Cover Art Archive front cover for the release group.

Deezer needs no account or key. The first two sources apply once the album's files are on disk, after an adoption or an import, and they replace a cover fetched earlier, since your own artwork wins. The other three only run while an album has no cover, and an artist refresh retries the albums that still have none. Albums added before barcodes were stored skip step 3 and go straight to the search.

## Automation

RSS sync, the missing search and the metadata refresh ([Scheduled Jobs](Scheduled-Jobs)) cover albums and book slots alongside movies and episodes. Feed items are routed by indexer category: 3000-range for music, 3030 for audiobooks and 7000-range for ebooks. Artists and authors are refreshed a few at a time per run to respect the upstream rate limits, and new releases inherit the artist's monitoring or the author's monitor policy.

## Requests

Users can request an artist (its whole discography), a single album, an author (per the author monitor policy) or a single book with a kind: ebook, audiobook or both. Approving an artist adds them with every album monitored, and the request turns available on the artist's first imported album. Approving an author adds them monitored; approving an album or a book adds the parent unmonitored and monitors just what was asked for. See [Requests and Users](Requests-and-Users).

## Known gaps

- A big bulk import hydrates in the background at one album every four seconds, so an artist's tracks can take a while to fill in.
- Adopting music or books does not trigger a media-server library refresh.
- Multi-disc albums split into `CD1`, `CD2` folders are adopted as separate candidates.
- OPDS covers need a web session, so readers show no thumbnails.
- A book Hardcover credits to several authors (anthologies, co-written titles) is listed under whichever of them you added first.
