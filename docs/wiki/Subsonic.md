# Subsonic

Streamline speaks the Subsonic API (version 1.16.1) at `/rest`, plus the OpenSubsonic extensions most modern apps look for. Point any Subsonic app at your Streamline address (`https://your-host`, the app adds `/rest` itself) and sign in with your **email** as the user name and your **Subsonic password** as the password. Generate that password on the Account page; it is separate from your login password and never works for the web UI.

Apps such as Symfonium, DSub, Ultrasonic, Tempo, play:Sub, Feishin and Sublime Music speak this API.

## What works

| Area | What you get |
| --- | --- |
| Browsing | Artists (A–Z index), artist → albums → songs in both the tag view and the folder view, genres, artist biography (the Wikipedia overview) and similar artists |
| Lists | Album lists: newest, random, A–Z by name or artist, by year, by genre, starred, highest rated, most played, recently played. Random songs, songs by genre, now playing, starred items |
| Search | All three versions (`search`, `search2`, `search3`), including the empty query apps use to sync the whole library |
| Stars, ratings and plays | Star and unstar songs, albums and artists; rate them 1–5; scrobbling counts plays and records when you last played something |
| Playlists | Create, rename, comment, reorder by removing and adding, make public, delete |
| Play queue sync | Save the queue on one device and pick it up on another, by song or by position (OpenSubsonic `indexBasedQueue`) |
| Bookmarks | Save a position in a song (useful for long tracks and DJ mixes) |
| Sharing | Public links to songs, albums, artists or playlists, with an optional description and expiry date |
| Streaming | The original file with seeking, or a transcode to MP3, Opus or AAC when the app asks for a lower bitrate or another format |
| Lyrics | From a `.lrc` file next to the song (timed, karaoke-style in apps that support it) or from lyrics embedded in the file's tags |
| Downloads | The original file, for offline listening |
| Internet radio | A shared list of radio streams; any user can listen, only admins can add, edit or remove stations |
| Library scan | "Rescan" in an app checks every file Streamline tracks and marks missing ones (admins only) |
| Users | Read your own account and change your own Subsonic password; admins can list accounts. Accounts and their Subsonic permissions are managed in the web UI |

Not available: podcasts, chat, jukebox mode, video and avatars.

## Stars, ratings and play counts

Stars, ratings and play counts belong to you: other users never see yours. Playing a song through to the end in an app with scrobbling turned on counts one play on the song, its album and its artist, so "most played" and "recently played" album lists fill in as you listen. **Top songs** for an artist rank their songs by plays across every user, and stay empty until someone has listened.

**Now playing** shows what everyone is listening to, by user name (email) and app. An entry disappears when the song finishes, or a few minutes after the song should have ended if the app never says so. It is kept in memory only, so a restart clears it.

## Playlists

A playlist is private to you unless you make it public. Public playlists show up for every user, but only you (or an admin) can change or delete them. A song whose file disappears from the library is skipped when the playlist is shown, and comes back if the file returns.

## Sharing

Sharing creates a link like `https://your-host/share/AbCdEf…` that anyone can open without an account. The page lists the shared songs with a player for each one and the album cover. It shows your display name, never your email. A link to an album or artist always plays what is in the library when it is opened, so an album shared before all its tracks arrived includes them later.

- The link is the only protection, so share it the way you would share a file.
- A link with an expiry date stops working on that date; deleting the share stops it at once.
- A link plays with its owner's permissions: creating one needs both the stream and share permissions, an account whose share permission is switched off stops every link it handed out, and one whose stream permission is switched off leaves its links listing songs without playing them, and a link's songs play within its owner's maximum bitrate.
- Your app's share list shows how often each link was opened and when it was last opened.
- The page is built from the address your app used to reach Streamline, so make sure the app points at your public address if you want links that work outside your network.

## Transcoding

By default an app gets the original file, so seeking and caching work as with any download. Streamline converts on the fly when the app asks for it:

- a **maximum bitrate** lower than the file's (common for "mobile data" settings), or
- a **format** other than the file's: MP3, Opus or AAC.

The converted stream uses the requested bitrate, between 32 and 320 kbps (MP3 defaults to 192, Opus to 128, AAC to 192 when only a format is asked for). Converted streams cannot be seeked by byte range; apps that support OpenSubsonic's `transcodeOffset` restart the stream at the right time instead. Starting at a time always converts the song, even when the app asked for the original, and when no conversion can run (no ffmpeg, or all eight slots still busy after a few seconds) the app gets an error rather than the song from its start. Transcoding needs ffmpeg (see `ffmpeg` in the [Configuration Reference](Configuration-Reference)); without it, or with `ffmpeg.enabled` off, apps always get the original file. At most eight conversions run at once; past that, apps get the original file instead of an error.

## Lyrics

Put a `.lrc` file with the same name next to the song (`01 - Song.flac` and `01 - Song.lrc`) for timed lyrics. Without one, lyrics embedded in the file are used. Section headers such as `[Chorus: Artist]` in untimed lyrics are kept as lines. Streamline never fetches lyrics from the internet: it only reads what is on disk.

## Users

Accounts are managed by an admin in Streamline's web UI (Settings → Users), never from a Subsonic app. An app's admin screen can list accounts (admins only), but creating, editing or deleting one is refused with a message pointing to the web UI. Anyone can read their own account.

- **The user name is the email** you sign in with.
- **Changing your password from an app** changes your own Subsonic password only, never your web login password, and only if your account allows it. Nobody can change another account's password from an app, admins included.
- **Permissions.** On a user's page in the web UI, the **Subsonic apps** card has switches for what the account may do from an app: stream, download, create and edit playlists, create share links, rate, and change their own Subsonic password. Every account starts with all of them on. They never limit the web UI, and browsing, searching, starring and scrobbling are always allowed.
- **Maximum bitrate.** The same card caps the account's streams (64 to 320 kbps, or no limit). Every stream above the cap is converted, even when the app asks for the original file. The cap needs ffmpeg: without it, or when the eight conversion slots are busy, the original file is sent.

## Library scan

Streamline does not scan a music folder for new files: music arrives through downloads and imports (see [Importing an Existing Library](Importing-an-Existing-Library)). A rescan from an app runs the **drift check** scheduled job, which notices files that were deleted or moved outside Streamline. Only admins can start it.
