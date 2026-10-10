# Music and Books

Streamline manages music and books the same way it manages movies and TV: browse and organise what you already have, then let it search, grab and import what is missing, and let other people request titles. This page covers the parts that are specific to these two libraries. The web pages for them are still being built, so most of what follows is reachable through the [REST API](REST-API) today.

## Adopting an existing collection

Point an import scan at your music or book folders with `kind: music` or `kind: book` (see [Importing an Existing Library](Importing-an-Existing-Library) for the scan, review and commit flow).

**Music** groups every folder that directly holds audio files into one album candidate. Streamline reads the embedded tags, matches the artist and album against MusicBrainz, and lists the candidates for review. Committing adopts the files where they are: no renames, no tag writes. The artist is added unmonitored, so adopting a few albums never turns a whole discography into a search list. Rename mode is refused for music.

**Books** groups ebooks by file name (an `.epub` and a `.mobi` of the same title are one candidate) and every folder of audio files into one audiobook candidate. Identification tries a Calibre `metadata.opf` sidecar first, then the epub's own metadata, then the file name, and resolves through Hardcover by ISBN when one is known. Book scans need a Hardcover key; without one the scan is refused. Committing adds each matched book unmonitored (so adopting a few books never turns into a search list) and attaches the files where they are; a scan spends one Hardcover request per candidate for the search, and the commit reads its books twenty to a request.

### Hardcover limits

Hardcover's free plan allows 60 requests a minute and 5,000 a day. Streamline paces itself at one request a second and keeps its own count of the day (resetting at 00:00 UTC), holding back 500 requests as a reserve so a big scan cannot lock you out of adding books.

- A book scan checks the budget before every candidate. When only the reserve is left, or Hardcover answers `429`, the scan **fails** with a reason naming the limit and when it clears; re-run it afterwards. Nothing is half-imported.
- Searching, adding or refreshing a book or series, and approving a book or series request, answer `429` with a `Retry-After` header when the minute limit or the day's budget is gone. Try again after that many seconds; the request stays pending.
- Hardcover is called in batches of twenty: a search is two requests (memoised for five minutes), adding a book is one, adding a series is two on the page you wait for and one more per twenty volumes in the background, and the scheduled refresh reads twenty books to a request. Pictures come from Hardcover's image host and cost no request.
- Background work (hydrating a series, the scheduled refresh) stops when only the reserve is left, or at the first `429`, and carries on at its next run.
- When a title has an ISBN, a scan resolves it with one request and does not search by title.

## Listening and reading from other apps

| Protocol | Path | Clients | Credentials |
| --- | --- | --- | --- |
| Subsonic 1.16.1 | `/rest` | Symfonium, DSub, play:Sub, Sublime Music | your email plus a generated Subsonic password |
| OPDS 1.2 | `/opds` | KOReader, Moon+ Reader, Foliate, Thorium | your email plus a generated OPDS token |

Both secrets are per user and separate from your login password. Generate, rotate or disable them through `/api/v1/account/subsonic-password` and `/api/v1/account/opds-token`; the account page in the web UI exposes the same actions. A secret is shown **once**, when you generate it; a lost one is replaced, not read back, and rotating signs every player or reader out. Each card also shows when the secret was created, when it was last used and by which client (the Subsonic `c` parameter, or the OPDS reader's name). Neither secret works for the web UI or the REST API.

For OPDS the catalogue URL carries no secret: point the reader at `https://your-host/opds` and sign in with your **email** as the user name and the **token** as the password. The server stores only a hash of the token.

Subsonic serves artists, albums, songs, album lists, search and raw streaming with seeking. Transcoding parameters sent by the client are ignored. OPDS serves an author catalogue (authors and writers, series volumes listed under theirs), a recently-added feed and search over titles and every credited name, and downloads the best available ebook format per book. Audiobooks never appear in OPDS. Cover thumbnails in OPDS readers are not available yet: the cover URLs require a web session.

## How books are organised

A book is a title with two slots, an **ebook** and an **audiobook**, each monitored, downloaded and replaced on its own. A manga, comic or BD series is a **series** whose volumes are books of their own; the library lists a standalone book or a whole series as one entry, never a volume by itself. People are credits on a book or a series (author, writer, artist, colorist, cover, translator, narrator). There is no author page and nothing is followed per author: you monitor the book or the series.

- **Editions.** For every book Streamline keeps the best Hardcover edition per language, publisher and format, up to forty. Each slot points at one edition, and the title shown is the best edition in the book's *preferred language* (the library language, `library.book_language`, until you change it on the book). Changing the preferred language retitles the book and re-picks the edition of every slot that has no file yet. Choosing another edition for a slot that already holds a file keeps the file until a release of the new edition arrives, then replaces it.
- **Series.** Adding a series is quick for any length: the first twenty volumes are read at once and the others appear as placeholders ("One Piece #25", no cover) that fill in over the next minutes while the page shows the series as hydrating. A placeholder is never searched or imported. A series has one monitor setting (all volumes, only volumes released from now on, or none), one quality profile that applies to every volume, and one edition choice (a language and publisher) applied to the volumes that have no file yet.
- **Kind.** A book is a novel, a BD, a comic or a manga, guessed from its Hardcover genres and original language and correctable on the book. The kind picks the default quality profile (Settings → Quality profiles has one default per kind), and a series follows its first volume.
- **Unreleased books** are listed but never searched for until their release date; a series ignores its unreleased volumes when deciding whether it is missing something.

## Searching and grabbing

The unit of acquisition is the album for music and the slot (ebook or audiobook) for books. Each has a manual search that returns every release the indexers offer, scored against the quality profile and with rejected releases kept at the end with their reason, and a grab that sends the chosen release to the download client. A book's search covers both slots unless you pick one; the slot a release fills is read from its container (`EPUB`, `CBZ`, `M4B`, …). The indexer is asked for the author and the slot edition's title, so a French edition is searched under its French title. A release naming a language other than the edition's is shown in a manual search but skipped by every automatic path; a release naming none is accepted.

A stock install has no music profile, so a music search answers with `no_quality_profile` until you create one in Settings → Quality profiles; the first one you create becomes the default. Books ship with a stock `default` profile.

**Music profiles are tiers, not formats.** A profile ticks the tiers it accepts, best first: *hi-res* (24-bit lossless), *lossless*, *high* (MP3 320, V0, AAC 256), *standard* (MP3 192-256, V2) and *low* (under 192 kbps), and names the *preferred* one, which is where upgrades stop. Leaving hi-res unticked is how a lossless profile keeps 24-bit files off your disk. A release whose name states no quality is listed but set aside, with the reason; if ffprobe is available an imported file is measured, and a file whose real tier your profile does not accept (a "FLAC" that is a 128k transcode) is held for your decision before anything is copied.

**Book profiles cover both slots.** One profile holds the accepted ebook formats (`EPUB`, `AZW3`, `MOBI`, `PDF`, `CBZ`, `CBR`) and audiobook formats (`M4B`, `MP3`, `M4A`, `FLAC`), a preferred one for each, and a minimum audiobook bit rate. A book uses its own profile if you picked one, else its series', else the default of its kind (novel, bd, comic or manga); a comic or manga needs a profile that accepts `CBZ`. An audiobook whose files are mostly in an unticked format, or whose measured bit rate is under the minimum, is held before import.

**Upgrades.** With *Upgrade allowed* on, the RSS scan replaces what you have with a better release it sees until the preferred tier or format is reached: an album per track, only where the new tier beats that track's own. Nothing searches in order to upgrade. Ticking *replace existing files* on a manual grab replaces the matching files once the new ones are in place and verified.

When a download completes, music is imported with corrected tags and MusicBrainz ids written into the library copy, renamed per `library.music_naming`. Because tags are written, music imports copy instead of hardlinking (and instead of moving when seeding is kept on), or the torrent you are still seeding would be corrupted. Books are never modified: an ebook is placed as one file named per `library.ebook_naming` (a volume of a series per `library.book_series_naming`), an audiobook as one folder per `library.audiobook_naming` with its original file names. The templates can use `{Author}`, `{Title}` (the slot edition's), `{Year}`, `{Language}`, `{Series}` and `{Volume}` / `{Volume:02}`. A downloaded book whose details Hardcover has not delivered yet (a series placeholder) is held rather than filed under a wrong name; resolve it once the series has finished hydrating. **Rename files** on a book or series page moves the files already in the library to their template paths, never onto a file that exists.

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

RSS sync, the missing search and the metadata refresh ([Scheduled Jobs](Scheduled-Jobs)) cover albums and book slots alongside movies and episodes. Feed items are routed by indexer category: 3000-range for music, 3030 for audiobooks and 7000-range for ebooks. Artists are refreshed a few at a time per run to respect MusicBrainz's rate limit, and new releases inherit the artist's monitoring. Books are refreshed in batches of twenty, oldest first (series placeholders before anything else), and volumes a series gains follow the series' monitor setting. A series is searched with one query for the whole series: each wanted volume takes the best release whose name carries its volume number, and at most three volumes the series query missed get a query of their own per run.

## Requests

Users can request an artist (its whole discography), a single album, a single book or a whole series. Approving an artist adds it with every album monitored, and the request turns available on the artist's first imported album. Approving an album asks MusicBrainz which artist the release group belongs to (never the requester's typed wording), adds that artist with every other album unmonitored if the library lacks it, and monitors just that album; an artist already in the library keeps its own profile, and a release group missing from the artist's list answers `422` with the request left pending; approving a book adds it with both slots monitored, and a series with every volume monitored. Change that on the title's page afterwards. See [Requests and Users](Requests-and-Users).

## Known gaps

- A big bulk import hydrates in the background at one album every four seconds, so an artist's tracks can take a while to fill in.
- Adopting music or books does not trigger a media-server library refresh.
- Multi-disc albums split into `CD1`, `CD2` folders are adopted as separate candidates.
- OPDS covers need a web session, so readers show no thumbnails.
- Volume packs and "intégrale" releases are treated as collections and never grabbed.
- Hardcover has no CBZ or CBR concept, so a comic or manga volume listed there only as paperback has that paperback shown as its ebook edition.
- A series volume cannot be deleted on its own; it would come back at the next refresh. Delete the series.
- Whether a book is a BD or a comic rests on its original language, so check the kind of anything unusual.
