# Music and Books

Streamline manages music and books the same way it manages movies and TV: browse and organise what you already have, then let it search, grab and import what is missing, and let other people request titles. This page covers the parts that are specific to these two libraries. The web pages for them are still being built, so most of what follows is reachable through the [REST API](REST-API) today.

## Adopting an existing collection

Point an import scan at your music or book folders with `kind: music` or `kind: book` (see [Importing an Existing Library](Importing-an-Existing-Library) for the scan, review and commit flow).

**Music** groups every folder that directly holds audio files into one album candidate. Streamline reads the embedded tags, matches the artist and album against MusicBrainz, and lists the candidates for review. Committing adopts the files where they are: no renames, no tag writes. The artist is added unmonitored, so adopting a few albums never turns a whole discography into a search list. Rename mode is refused for music.

**Books** groups ebooks by file name (an `.epub` and a `.mobi` of the same title are one candidate) and every folder of audio files into one audiobook candidate. Identification tries a Calibre `metadata.opf` sidecar first, then the epub's own metadata, then the file name, and resolves through Hardcover by ISBN when one is known. Book scans need a Hardcover key; without one the scan is refused.

## Listening and reading from other apps

| Protocol | Path | Clients | Credentials |
| --- | --- | --- | --- |
| Subsonic 1.16.1 | `/rest` | Symfonium, DSub, play:Sub, Sublime Music | your email plus a generated Subsonic password |
| OPDS 1.2 | `/opds` | KOReader, Moon+ Reader, Foliate, Thorium | your email plus a generated OPDS token |

Both secrets are per user and separate from your login password. Generate, rotate or disable them through `/api/v1/account/subsonic-password` and `/api/v1/account/opds-token`; the account page in the web UI will expose the same actions. Neither secret works for the web UI or the REST API.

Subsonic serves artists, albums, songs, album lists, search and raw streaming with seeking. Transcoding parameters sent by the client are ignored. OPDS serves an author catalogue, a recently-added feed and search, and downloads the best available ebook format per book. Audiobooks never appear in OPDS. Cover thumbnails in OPDS readers are not available yet: the cover URLs require a web session.

## Searching and grabbing

The unit of acquisition is the album for music and the slot (ebook or audiobook) for books. Each has a manual search that returns every release the indexers offer, scored against the quality profile and with rejected releases kept at the end with their reason, and a grab that sends the chosen release to the download client.

A stock install has no music or book quality profiles, so searches answer with `no_quality_profile` until you create one under the music, ebook and audiobook profile settings.

When a download completes, music is imported with corrected tags and MusicBrainz ids written into the library copy, renamed per `library.music_naming`. Because tags are written, music imports copy instead of hardlinking (and instead of moving when seeding is kept on), or the torrent you are still seeding would be corrupted. Books are never modified: an ebook is placed as one file named per `library.ebook_naming`, an audiobook as one folder per `library.audiobook_naming` with its original file names.

## Automation

RSS sync, the missing search and the metadata refresh ([Scheduled Jobs](Scheduled-Jobs)) cover albums and book slots alongside movies and episodes. Feed items are routed by indexer category: 3000-range for music, 3030 for audiobooks and 7000-range for ebooks. Artists and authors are refreshed a few at a time per run to respect the upstream rate limits, and new releases inherit the artist's monitoring or the author's monitor policy.

## Requests

Users can request an artist (its whole discography), a single album, an author (per the author monitor policy) or a single book with a kind: ebook, audiobook or both. Approving an artist or author adds them monitored. Approving an album or a book adds the parent unmonitored and monitors just what was asked for. See [Requests and Users](Requests-and-Users).

## Known gaps

- Adding a prolific artist fetches every release group's track list up front and can take minutes.
- Adopting music or books does not trigger a media-server library refresh.
- Multi-disc albums split into `CD1`, `CD2` folders are adopted as separate candidates.
- OPDS covers need a web session, so readers show no thumbnails.
