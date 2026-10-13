# Music and Books

Streamline manages music and books the same way it manages movies and TV: browse and organise what you already have, then let it search, grab and import what is missing, and let other people request titles. This page covers the parts that are specific to these two libraries. The web pages for them are still being built, so most of what follows is reachable through the [REST API](REST-API) today.

## Adopting an existing collection

Point an import scan at your music or book folders with `kind: music` or `kind: book` (see [Importing an Existing Library](Importing-an-Existing-Library) for the scan, review and commit flow).

**Music** groups every folder that directly holds audio files into one album candidate. Streamline reads the embedded tags, matches the artist and album against MusicBrainz, and lists the candidates for review. Committing adopts the files where they are, with no renames. **Write Streamline's tags into these files**, beside the Commit button, decides whether Streamline also writes its own tags into them (off unless you tick it): switched off, the files stay exactly as they are and Streamline keeps off their tags from then on (an album that already held files keeps its own setting); switched on, they are tagged as each album is committed and kept in step like downloaded music (see [Tags and artwork](#tags-and-artwork)). A row whose files a torrent is still seeding says so: those files are tagged once the torrent is removed from its client. Albums adopted before this version are left untagged until you choose otherwise. The artist is added unmonitored, so adopting a few albums never turns a whole discography into a search list. Rename mode is refused for music.

**Books** groups ebooks by file name (an `.epub` and a `.mobi` of the same title are one candidate) and every folder of audio files into one audiobook candidate. Identification tries a Calibre `metadata.opf` sidecar first, then the epub's own metadata, then the file name, and resolves through Hardcover by ISBN when one is known. Book scans need a Hardcover key; without one the scan is refused. Committing adds each matched book unmonitored (so adopting a few books never turns into a search list) and attaches the files where they are; a scan spends one Hardcover request per candidate for the search, and the commit reads its books twenty to a request.

### Hardcover limits

Hardcover's free plan allows 60 requests a minute and 5,000 a day. Streamline paces itself at one request a second and keeps its own count of the day (resetting at 00:00 UTC), holding back 500 requests as a reserve so a big scan cannot lock you out of adding books.

- A book scan checks the budget before every candidate. When only the reserve is left, or Hardcover answers `429`, the scan **fails** with a reason naming the limit and when it clears; re-run it afterwards. Nothing is half-imported.
- Searching, adding or refreshing a book or series, and approving a book or series request, answer `429` with a `Retry-After` header when the minute limit or the day's budget is gone. Try again after that many seconds; the request stays pending.
- Hardcover is called in batches of twenty: a search is up to three requests (one when you search only books; memoised for five minutes), adding a book is one, adding a series is two on the page you wait for and one more per twenty volumes in the background, and the scheduled refresh reads twenty books to a request. Pictures come from Hardcover's image host and cost no request.
- Background work (hydrating a series, the scheduled refresh) stops when only the reserve is left, or at the first `429`, and carries on at its next run.
- When a title has an ISBN, a scan resolves it with one request and does not search by title.

## Listening and reading from other apps

| Protocol | Path | Clients | Credentials |
| --- | --- | --- | --- |
| Subsonic 1.16.1 | `/rest` | Symfonium, DSub, play:Sub, Sublime Music | your email plus a generated Subsonic password |
| OPDS 1.2 | `/opds` | KOReader, Moon+ Reader, Foliate, Thorium | your email plus a generated OPDS token |

Both secrets are per user and separate from your login password. Generate, rotate or disable them through `/api/v1/account/subsonic-password` and `/api/v1/account/opds-token`; the account page in the web UI exposes the same actions. A secret is shown **once**, when you generate it; a lost one is replaced, not read back, and rotating signs every player or reader out. Each card also shows when the secret was created, when it was last used and by which client (the Subsonic `c` parameter, or the OPDS reader's name). Neither secret works for the web UI or the REST API.

For OPDS the catalogue URL carries no secret: point the reader at `https://your-host/opds` and sign in with your **email** as the user name and the **token** as the password. The server stores only a hash of the token.

Subsonic serves artists (getArtists and getIndexes), albums, songs, album lists, search and raw streaming with seeking. Transcoding parameters sent by the client are ignored. OPDS serves an author catalogue (authors and writers, series volumes listed under theirs), a recently-added feed and search over titles and every credited name, and downloads the best available ebook format per book. Audiobooks never appear in OPDS. Cover thumbnails are served from an OPDS route too, so readers that only hold the OPDS token show them.

## How books are organised

A book is a title with two slots, an **ebook** and an **audiobook**, each monitored, downloaded and replaced on its own. A manga, comic or BD series is a **series** whose volumes are books of their own; the library lists a standalone book or a whole series as one entry, never a volume by itself. People are credits on a book or a series (author, writer, artist, colorist, cover, translator, narrator). There is no author page and nothing is followed per author: you monitor the book or the series.

- **Editions.** For every book Streamline keeps the best Hardcover edition per language, publisher and format, up to forty. Each slot points at one edition, and the title shown is the best edition in the book's *preferred language* (the library language, `library.book_language`, until you change it on the book). Changing the preferred language retitles the book and re-picks the edition of every slot that has no file yet. Choosing another edition for a slot that already holds a file keeps the file until a release of the new edition arrives, then replaces it.
- **Series.** Adding a series is quick for any length: the first twenty volumes are read at once and the others appear as placeholders ("One Piece #25", no cover) that fill in over the next minutes while the page shows the series as hydrating. A placeholder is never searched or imported. A series has one monitor setting (all volumes, only volumes released from now on, or none), one quality profile that applies to every volume, and one edition choice (a language and publisher) applied to the volumes that have no file yet.
- **Searching for a series.** Hardcover only matches a series by its own name, so Streamline also looks at the books that match your search, including their translated and alternative titles, and offers the series they belong to. Searching a French title therefore finds the series, with a line "via « title »" saying which book matched. A result shows a Series badge with its volume count and a fan of the first three volumes' covers, and a link to the series on Hardcover. Series that Hardcover lists with no main volumes are left out.
- **Kind.** A book is a novel, a BD, a comic or a manga, guessed from its Hardcover genres and original language and correctable on the book. The kind picks the default quality profile (Settings → Quality profiles has one default per kind), and a series follows its first volume.
- **Unreleased books** are listed but never searched for until their release date; a series ignores its unreleased volumes when deciding whether it is missing something.

## Searching and grabbing

The unit of acquisition is the album for music and the slot (ebook or audiobook) for books. Each has a manual search that returns every release the indexers offer, scored against the quality profile and with rejected releases kept at the end with their reason, and a grab that sends the chosen release to the download client. A book's search covers both slots unless you pick one; the slot a release fills is read from its container (`EPUB`, `CBZ`, `M4B`, …). The indexer is asked for the author and the slot edition's title, so a French edition is searched under its French title. A release naming a language other than the edition's is shown in a manual search but skipped by every automatic path; a release naming none is accepted.

A stock install ships a `default` music profile (lossless and high, preferring lossless) and a `default` book profile used for every book kind; change or replace them in Settings → Quality profiles.

**Music profiles are tiers, not formats.** A profile ticks the tiers it accepts, best first: *hi-res* (24-bit lossless), *lossless*, *high* (MP3 320, V0, AAC 256), *standard* (MP3 192-256, V2) and *low* (under 192 kbps), and names the *preferred* one, which is where upgrades stop. Leaving hi-res unticked is how a lossless profile keeps 24-bit files off your disk. A release whose name states no quality is listed but set aside, with the reason; if ffprobe is available an imported file is measured, and a file whose real tier your profile does not accept (a "FLAC" that is a 128k transcode) is held for your decision before anything is copied.

**Book profiles cover both slots.** One profile holds the accepted ebook formats (`EPUB`, `AZW3`, `MOBI`, `PDF`, `CBZ`, `CBR`) and audiobook formats (`M4B`, `MP3`, `M4A`, `FLAC`), a preferred one for each, and a minimum audiobook bit rate. A book uses its own profile if you picked one, else its series', else the default of its kind (novel, bd, comic or manga); a comic or manga needs a profile that accepts `CBZ`. An audiobook whose files are mostly in an unticked format, or whose measured bit rate is under the minimum, is held before import.

**Upgrades.** With *Upgrade allowed* on, the RSS scan replaces what you have with a better release it sees until the preferred tier or format is reached: an album per track, only where the new tier beats that track's own. Nothing searches in order to upgrade. Ticking *replace existing files* on a manual grab replaces the matching files once the new ones are in place and verified.

When a download completes, music is imported renamed per `library.music_naming`, and Streamline writes its own tags into the files (see [Tags and artwork](#tags-and-artwork)).

Books are never modified: an ebook is placed as one file named per `library.ebook_naming` (a volume of a series per `library.book_series_naming`), an audiobook as one folder per `library.audiobook_naming` with its original file names. The templates can use `{Author}`, `{Title}` (the slot edition's), `{Year}`, `{Language}`, `{Series}` and `{Volume}` / `{Volume:02}`. A downloaded book whose details Hardcover has not delivered yet (a series placeholder) is held rather than filed under a wrong name; resolve it once the series has finished hydrating. **Rename files** on a book or series page moves the files already in the library to their template paths, never onto a file that exists.

## Tags and artwork

Streamline writes what the library knows into each music file, so media servers and players that read files (Jellyfin, Navidrome, Plex with local metadata, a phone or a car stereo) show exactly that: title, artist and featured artists, album artist, track and disc numbers with their totals, the edition's release date and the original one, up to five genres, label, catalog number, barcode, country, release type, the MusicBrainz ids of the artist, release, release group, recording and release track, and the credits: composers, lyricists, writers and arrangers of the song, the recording's performers with their instruments, and its producers, engineers, mixers, conductor and remixer. MP3, FLAC, M4A, Ogg Vorbis and Opus are all tagged. A tag Streamline has no value for is removed rather than left stale.

Everything else stays in the file. Under Settings → Music, **Tags to keep** lists tags Streamline never writes or removes (ReplayGain, lyrics, AcoustID, BPM and the musical key by default) and **Tags to remove** lists the ones it deletes (comments, encoded-by credits and the web links a release leaves behind).

**Artwork** follows the album's cover (see [Album covers](#album-covers)):

- *Embed the album cover* puts it in each file as the front picture, scaled down to *Embedded cover size* (1000 px on its long side by default; a cover already smaller goes in as it is). A front cover the release already carried is never replaced — including one a rip stored as an untyped ("Other") picture — and other pictures in a file (a back cover, booklet pages) are kept. Switching the setting off removes only the cover Streamline embedded.
- *Write cover.jpg and folder.jpg* leaves the full-size cover beside each album's files as `cover.jpg` (or `cover.png`), and the artist's photo as `folder.jpg` in the artist's folder: the folder above the album's, when it is named after the artist, as the default naming pattern makes it. Where a `cover`, `folder` or `front` image Streamline did not write is already there, it is left alone and nothing is added beside it. Switching the setting off removes only the files Streamline wrote.

Streamline tells its own copies apart from yours, so they are never mistaken for the album's own artwork. The images are written with the tags, by the job below. Under *Managed* a new cover is written into the files and beside them; under *On import*, **Retag library** brings them up to date. An artist photo is refreshed the next time one of the artist's albums is tagged.

**When files are written** is the *Writing tags* setting:

- *Off* never writes into a file.
- *On import* writes each file once, when it is imported, or adopted with tagging switched on. After that the file is left alone.
- *Managed* keeps files in step with the library: when a refresh, MusicBrainz's track and credit data, or a setting changes what a file should carry, the file is rewritten. Edits made to those tags in another tool are undone; tags Streamline does not manage are never touched.

**Each release's Tags panel** (the tag button beside a release's searches, shown once it has files) reads every file and lists what Streamline would change: the tag, what the file holds, and what Streamline writes, with *Show unchanged tags* for the rest, including the ones it leaves as they are. From there:

- *Write tags now* rewrites the release's files at once, whatever the mode.
- *Keep these files in step* is the release's own switch: an album adopted without tagging starts off, and switching it on queues its files.
- *Tags for every file* and *Tags for this track* set tags by hand, written over what the library knows (a genre you prefer, a mood, a corrected title). A tag with no value is removed from the files; *Tags to keep* in Settings → Music still wins. A hand-set `PERFORMER:<INSTRUMENT>` names every performer of that instrument: the library's names for it are replaced, not added to.

The panel, *Write tags now* and the hand-set tags are open to every member who can edit the library (the same people who can monitor or search a release), not to admins only. Account-wide tag settings and **Retag library** stay admin-only.

Under *Managed*, Streamline also notices edits made in another tool: each run of the job checks up to 200 written files, the ones checked longest ago first, and one whose Streamline tags were changed is rewritten in that same run. On a large library a full round therefore takes several runs (every 30 minutes by default), so an edit can stay in place for a while before it is undone; running the job by hand checks every file at once. Tags it does not write, such as ReplayGain added by a player, are left alone.

The writing itself happens in the `music-tag-sync` job ([Scheduled Jobs](Scheduled-Jobs), every 30 minutes by default; run it by hand to start at once). A file is written to a copy beside it and swapped in only once the new tags read back correctly, so a failed write leaves the file as it was; one that keeps failing is given up on after five attempts. The copy keeps the file's owner, group, permissions and extended attributes (ACLs included). When Streamline cannot give it the same owner or group — the file belongs to another user, or to a group Streamline's user is not in — the file is left untouched and marked failed at once, with the reason on the Tags panel, rather than changing who can read it. A copy left behind by a crash is hidden, never taken for a track, and removed by the next write in its folder. **Retag library** queues every file for a full rewrite, which is how files written before a change in what Streamline writes, or under *On import*, are brought up to date.

**Seeding torrents are never written to.** A file a torrent still serves from its own path waits until the torrent is removed from the client. A hardlinked import shares its bytes with the torrent's copy: the *While a torrent seeds* setting decides whether it waits for the torrent too (*Wait*, one copy on disk, the default) or is copied and tagged at once (*Copy now*, two copies while the torrent seeds). *Wait* also covers a library adopted from folders another tool hardlinked out of the client's downloads: Streamline finds the torrent by the file it shares, even with no download of its own behind it. Files waiting on a torrent take turns, so the ones whose torrent is gone are written even while many others still wait. Streamline only knows a torrent is gone when the client stops listing it, so a torrent kept paused in the client keeps its files waiting.

Albums added before this version get their genres, edition dates and per-track credits fetched again in the background, at MusicBrainz's slow background pace, starting with the albums you hold files for. Until they are in, the genre, dates, release track id and credit tags already in the album's files are left as they are. A release too large for MusicBrainz's full credits answer (a box set) keeps the credit tags its files carry.

## Adding an artist

Adding an artist returns at once with one album per MusicBrainz release group. Streamline then fills in the tracks, labels, credits, covers, a Wikipedia overview and a Deezer photo in the background, queued so that no single artist holds the rest up and always behind anything you are waiting on (MusicBrainz allows one request a second for everyone). The artist page shows what has arrived and keeps checking until it is done.

An artist's **monitor** policy decides which albums are searched for: *all* monitors every release group, *future* only the ones with no date or a date still ahead, *manual* leaves the choice to you album by album, *none* monitors nothing. Changing the policy re-applies it to the albums already there, and a refresh applies it to release groups new to the artist.

*All* and *future* only monitor the **release types** ticked under Settings → Music (`library.music_monitored_types`): albums and EPs by default. Singles (which mostly repeat an album's tracks), live releases and compilations are still listed on the artist page, in their own sections, but arrive unmonitored; tick one by hand to have it searched. An unmonitored release you hold nothing of carries no *Wanted* label and is left out of the artist's wanted count. Changing the types applies to artists added and releases found from then on; **Apply to existing artists** on the same page re-applies every *all* or *future* artist's policy under the new types, but leaves alone any album you monitored or unmonitored by hand. Changing an artist's own policy is starting over: it re-applies to every album, including those. *Manual* and *none* artists are never touched. An album dated in the future is *upcoming*, not missing, until its release date.

The overview comes from Wikipedia in English and French, linked through MusicBrainz's Wikidata entry, and is shown in your language when there is an article in it. The photo is Deezer's, found through MusicBrainz's own Deezer link, or by name when exactly one Deezer artist has that name: a namesake never gets the wrong face.

## Discography packs and per-artist actions

An artist's page can search the indexers for a **discography pack** (only packs named for that artist are listed, each judged against the artist's quality profile) and grab one. The pack is linked to every wanted, released album of the artist; when it finishes, each folder is matched to one of those albums by its name (and year, when it names one) and imported like an ordinary album download. Albums no folder names go back to wanted, nothing counted against them. *Search now* on an artist, album or track runs one search-and-grab pass at once, ignoring the cooldown.

When a finished download cannot be imported, the album is marked **Failed** and the reason is shown on its panel, e.g. a release that turned out to be one whole-disc audio file with a CUE sheet, which Streamline does not split into tracks. A failed album is no longer searched for automatically, so the same release is not grabbed again. Pick another release with *Manual search*, or retry the failed download from Activity → History once its cause is fixed. The artist still counts a failed album as wanted. *Rename* previews or applies the library naming pattern to every file of the artist, from the database, without touching tags. Deleting a track's file puts an available album back to wanted.

## Album covers

Streamline stores one cover per album, taken from the first of these that has an image:

1. A picture embedded in one of the album's audio files, other than the copy Streamline embedded itself.
2. A `cover`, `folder` or `front` image (`.jpg`, `.jpeg` or `.png`, any case) in the folder of the album's first file, other than the `cover.jpg` Streamline wrote.
3. Deezer, looked up by the album's barcode as MusicBrainz records it.
4. Deezer, searched by artist and title; the result is used only when Deezer's artist matches yours.
5. The Cover Art Archive front cover for the release group.

Deezer needs no account or key. The first two sources apply once the album's files are on disk, after an adoption or an import, and they replace a cover fetched earlier, since your own artwork wins. They are read at those moments and when the album's tags are written: a `cover.jpg` you drop next to an album that already has a cover is **not** picked up by an artist refresh, which only looks for covers that are still missing. To have it read, use *Write tags now* in the release's Tags panel (with either artwork setting on). The cover Streamline itself writes beside the files and into them is never taken for yours. The other three only run while an album has no cover, and an artist refresh retries the albums that still have none. Albums added before barcodes were stored skip step 3 and go straight to the search.

## Automation

RSS sync, the missing search and the metadata refresh ([Scheduled Jobs](Scheduled-Jobs)) cover albums and book slots alongside movies and episodes. Feed items are routed by indexer category: 3000-range for music, 3030 for audiobooks and 7000-range for ebooks. Artists are refreshed a few at a time per run to respect MusicBrainz's rate limit, and new releases inherit the artist's monitoring. Books are refreshed in batches of twenty, oldest first (series placeholders before anything else), and volumes a series gains follow the series' monitor setting. A series is searched with one query for the whole series: each wanted volume takes the best release whose name carries its volume number, and at most three volumes the series query missed get a query of their own per run.

## Requests

Users can request an artist (its whole discography), a single album, a single book or a whole series. Approving an artist adds it monitored for all releases (so every album of a type ticked under Settings → Music), and the request turns available on the artist's first imported album. Approving an album asks MusicBrainz which artist the release group belongs to (never the requester's typed wording), adds that artist with every other album unmonitored if the library lacks it, and monitors just that album; an artist already in the library keeps its own profile, and a release group missing from the artist's list answers `422` with the request left pending; approving a book adds it with both slots monitored, and a series with every volume monitored. Change that on the title's page afterwards. See [Requests and Users](Requests-and-Users).

## Known gaps

- A big bulk import hydrates in the background at one album every four seconds, so an artist's tracks can take a while to fill in.
- Plex has no book library type, so book imports refresh Jellyfin and Emby only. Music refreshes Plex's music (artist) libraries.
- Multi-disc albums split into `CD1`, `CD2` folders are adopted as separate candidates.
- Volume packs and "intégrale" releases are treated as collections and never grabbed.
- Hardcover has no CBZ or CBR concept, so a comic or manga volume listed there only as paperback has that paperback shown as its ebook edition.
- A series volume cannot be deleted on its own; it would come back at the next refresh. Delete the series.
- Whether a book is a BD or a comic rests on its original language, so check the kind of anything unusual.

Ebooks can also be emailed straight to an e-reader such as a Kindle or PocketBook; see [Send to Kindle](Send-to-Kindle).
