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
| Users | Admins create, edit and delete accounts and set what each may do through Subsonic; everyone can see their own account and change their own Subsonic password |

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
- Your app's share list shows how often each link was opened and when it was last opened.
- The page is built from the address your app used to reach Streamline, so make sure the app points at your public address if you want links that work outside your network.

## Transcoding

By default an app gets the original file, so seeking and caching work as with any download. Streamline converts on the fly when the app asks for it:

- a **maximum bitrate** lower than the file's (common for "mobile data" settings), or
- a **format** other than the file's: MP3, Opus or AAC.

The converted stream uses the requested bitrate, between 32 and 320 kbps (MP3 defaults to 192, Opus to 128, AAC to 192 when only a format is asked for). Converted streams cannot be seeked by byte range; apps that support OpenSubsonic's `transcodeOffset` restart the stream at the right time instead. Transcoding needs ffmpeg (see `ffmpeg` in the [Configuration Reference](Configuration-Reference)); without it, or with `ffmpeg.enabled` off, apps always get the original file. At most eight conversions run at once; past that, apps get the original file instead of an error.

## Lyrics

Put a `.lrc` file with the same name next to the song (`01 - Song.flac` and `01 - Song.lrc`) for timed lyrics. Without one, lyrics embedded in the file are used. Streamline never fetches lyrics from the internet: it only reads what is on disk.

## Users

An admin can manage accounts from a Subsonic app's admin screen, and every change goes through the same checks as the Users page of the web UI: the last admin can be neither demoted nor deleted, and nobody can delete themselves.

- **The user name is the email.** An app asks for a user name, an email and a password. The email becomes the login. A user name that differs from it becomes the account's display name.
- **The password is a Subsonic password.** It is what the new account signs in to apps with, and it never becomes the web login password, because Subsonic passwords are stored in a form the server can read back. An account created from an app therefore cannot sign in to the web UI until an admin sets a password on the Users page. Changing a password from an app (yours, or anyone's as an admin) changes the Subsonic password only.
- **Admin.** The app's admin switch makes the account a Streamline admin, with full rights in the web UI too, and turning it off makes it a member. Either change signs the account out of the web UI.
- **Permissions.** Each account has Subsonic-only switches: may stream, may download, may manage playlists, may share, may rate songs, may change their own password. A new account from an app follows the Subsonic defaults (streaming and password changes on, the rest off). Accounts created any other way have every switch on. The switches never limit what the account can do in the web UI. Browsing, searching, starring and scrobbling are always allowed.
- **Maximum bitrate.** An admin can cap an account's streams at 32 to 320 kbps. Every stream above the cap is converted, even when the app asks for the original file. The cap needs ffmpeg: without it, or when the eight conversion slots are busy, the original file is sent.

Only admins can create, edit or delete accounts, list all accounts, or read someone else's. An admin's Subsonic password can do all of that, so keep it as safe as the admin login.

The permissions and the bitrate cap are only editable from a Subsonic app for now; the web UI does not show them.

## Library scan

Streamline does not scan a music folder for new files: music arrives through downloads and imports (see [Importing an Existing Library](Importing-an-Existing-Library)). A rescan from an app runs the **drift check** scheduled job, which notices files that were deleted or moved outside Streamline. Only admins can start it.
