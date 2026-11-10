# Writing an extension

For what an extension is and how to add one, see [Extensions](Extensions). This page is the contract: version **1** of the guest API. The reference implementation is the [OPDS extension](https://github.com/Streamline-HQ/streamline-opds); the SDK it imports is `extensions/sdk` in the Streamline repository.

## Building

A guest is a Go program compiled to WebAssembly with the standard toolchain, using the SDK module `github.com/datahearth/streamline/extensions/sdk`:

```bash
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o myext.wasm .
```

`task build:extensions` does this for the ones in the repository. Keep `main` empty; the host calls two exports.

```go
package main

import "github.com/datahearth/streamline/extensions/sdk"

func main() {}

//go:wasmexport manifest
func manifest() int32 {
	return sdk.WriteManifest(sdk.Manifest{
		Name:          "myext",
		Version:       "1.0.0",
		StreamlineAPI: 1,
		Mounts:        []sdk.Mount{{Prefix: "/myext", Auth: "session"}},
		Views:         []string{"books"},
	})
}

//go:wasmexport handle
func handle() int32 {
	return sdk.Dispatch(func(r sdk.Request) sdk.Response {
		return sdk.Response{Status: 200, Body: []byte("hello")}
	})
}
```

A fresh instance of the module handles **every** request, so nothing in memory survives between two: keep state in your database, or files in your cache folder.

## Manifest

| Field | Notes |
| --- | --- |
| `name` | `^[a-z0-9][a-z0-9-]{0,31}$`; must equal the entry's `name` |
| `version` | Free text, shown in the API |
| `streamline_api` | Must be `1`. A module for another version is refused |
| `mounts[]` | `{prefix, auth}`. `prefix` is one path segment (`/opds`); `auth` is `session` (Streamline signs the user in first and tells you who) or `self` (you check credentials, e.g. HTTP Basic). A prefix that is one of Streamline's own routes is refused; `/rest` and `/share` are free |
| `views[]` | Names from [the table below](#views) |
| `folders[]` | `music`, `artwork`, `cache`, see [Folders](#folders) |
| `services[]` | `serve_file`, `serve_poster`, `ffmpeg`, `library_scan` |
| `account_cards[]` | `{id, kind, title, mount, username, fields}`. `kind: credential` is a credential the account page manages (`mount`, `username`); `kind: settings` is a set of per-user controls an admin edits (`fields`, see [Settings cards](#settings-cards)). Your module answers both at `/api/v1/extensions/<name>/cards/<id>` |

Whatever you list must be granted by the admin, exactly. A settings card that declares no field, two fields with one id, a field of another kind or a number whose `min` is above its `max` makes the manifest unusable, and the extension fails to start.

## Requests and responses

`sdk.Request` carries `Method`, `Path` (relative to your mount: `/opds/authors` arrives as `/authors`), the raw `Query` string, `Headers`, the `Body` (at most 16 MiB; more is a 413), `Caller`, `Folders` and `TargetUser`. `Caller` is `{ID, Role}` and is set only on a `session` mount for a signed-in user; on a `self` mount it is nil and `Authorization` is passed through. The `Cookie` header never reaches you, and `Authorization` and `X-Api-Key` are removed on a `session` mount. `Folders` lists the granted folders this call has mounted, and `TargetUser` is set only on a settings card call from the admin endpoints.

`sdk.Response` is `Status` (0 means 200; outside 100-599 is a 500), `Headers`, `Body` (at most 16 MiB) and, instead of a body, a directive. `Set-Cookie`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Content-Security-Policy` and `X-Content-Type-Options` are dropped from what you send: Streamline sets the last two itself (`sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'` and `nosniff`), so an HTML page you serve can carry inline styles, images and `<audio>` or `<video>` from Streamline's own address, but no script.

Two routes are always Streamline's, whatever your mount: `/api/v1/extensions/<name>/cards...` and `/api/v1/extensions/<name>/api...`. Both need a signed-in user, arrive with a `Caller`, and the path you see starts at `/cards` or `/api`.

### Directives

A response may name one thing for Streamline to send instead of a body. Each runs after your module has returned, so it holds none of your database connections while the bytes go out.

| Directive | Fields | Needs |
| --- | --- | --- |
| `ServeFile` | `FileID`, `Filename`, `ContentType` | the `serve_file` service **and** the `book_files` view. The file must be a present row of `streamline_book_files`; otherwise 404. Streamline streams it with range support |
| `ServePoster` | `Kind` (`books` or `authors`), `ID` | the `serve_poster` service and the view of that kind (`books` or `authors`) |
| `SendFile` | `Path`, `Filename`, `ContentType` | a granted folder holding `Path`, see [Sending a file](#sending-a-file) |
| `StreamFFmpeg` | `Args`, `ContentType`, `Headers`, `Fallback` | the `ffmpeg` service, see [ffmpeg](#ffmpeg) |

A `ServeFile`, `SendFile` or `StreamFFmpeg` answer lives as long as the client keeps reading; a client that stops for 2 minutes is cut.

## Talking to the database

```go
res, err := sdk.Query("SELECT id, title FROM streamline_books WHERE author_name LIKE ?", "%Le Guin%")
// res.Columns, res.Rows ([][]any: numbers are float64, text string, NULL nil)
// res.Error is a statement SQLite refused; err is a failed host call

_, err = sdk.Exec("INSERT INTO tokens(user_id, token_hash) VALUES(?, ?)", id, hash)

sdk.Log("warn", "something odd", "user", id)
```

`Query` and `Exec` run on one connection to **your** database, so a transaction (`BEGIN` ... `COMMIT`) within a request holds. Create your own tables in it freely. Core is attached read-only, and only through the views you were granted: any other read of core, any write to it, and any other `ATTACH` is refused. A statement that runs more than 5 seconds is interrupted. `Log` levels are `debug`, `info`, `warn`, `error`; the line carries your name.

## Folders

Each granted folder is mounted into your module for the length of one call:

| Folder | What it is | Mounted at | Access |
| --- | --- | --- | --- |
| `music` | The music library, `library.music_path` | `/folders/music` | read-only |
| `artwork` | Streamline's poster cache, laid out `<kind>/<id>/poster.jpg` | `/folders/artwork` | read-only |
| `cache` | Your own folder, `<data_dir>/extensions/<name>/cache` | `/folders/cache` | read and write |

Read them with the standard library (`os.ReadFile`, `os.Open`, `os.WriteFile` into the cache). `r.Folders` lists what this call has, each as `sdk.Folder{Name, HostPath, GuestPath}`: `HostPath` is the directory on the server, `GuestPath` where you see it. A folder whose directory is missing when the request arrives is left out, so check `r.Folder(name)` before using it. The views give paths on the server (`music_files.path`); turn one into a path you can open with `r.GuestPath(hostPath)`, which answers false for a path outside every folder you have.

```go
f, ok := r.Folder("music")
p, ok := r.GuestPath(row.Path) // "/srv/music/A/B/01.flac" -> "/folders/music/A/B/01.flac"
b, err := os.ReadFile(p)
```

No path leaves its folder: `..`, and a symbolic link pointing outside, are refused, and you cannot create a link in the cache. Streamline deletes the oldest files of the cache once it holds more than 1 GiB, checked every hour, so treat it as a cache: anything in it can be gone on the next request.

## Sending a file

`SendFile` streams a file from a granted folder with range support. `Path` is the file's path **on the server**, as a view or `Folder.HostPath` gives it, not the guest path; Streamline finds the granted folder holding it. A relative path, a path outside every granted folder, a link leading out or a directory is a 404. `ContentType` defaults to `application/octet-stream`, and `Filename`, when set, makes it an attachment download.

```go
return sdk.Response{SendFile: &sdk.SendFile{Path: row.Path, ContentType: "audio/flac"}}
```

## ffmpeg

The `ffmpeg` service runs Streamline's own `ffmpeg` and `ffprobe`, with the arguments you pass as a list (no shell), in your cache folder, as the server's own process: it is not confined, and the admin is told so when granting it. Paths in the arguments are paths **on the server** (`Folder.HostPath`, a view's `path`), not guest paths, and a relative output path lands in your cache folder. Every run is logged with its full argument list. Nothing runs when ffmpeg is disabled on the server or missing, or when this extension already has 8 runs going; your extension keeps answering either way.

**A call** runs to completion inside your request, within its 30 seconds:

```go
res, err := sdk.FFmpeg("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", row.Path)
// res.ExitCode, res.Stdout (at most 16 MiB), res.Stderr (its last 64 KiB)
// res.Error: not granted, not available, every slot busy, a tool other than
//   ffmpeg or ffprobe, more than 16 MiB of output, or the run cut short
// err: a failed host call
```

A non-zero exit is not an error: it comes back in `ExitCode` with both outputs.

**A stream** is a directive: your module returns, then Streamline runs `ffmpeg` and sends its stdout to the client as it comes, as long as the client keeps reading (a client that stops for 2 minutes is cut, and ffmpeg stopped).

```go
return sdk.Response{StreamFFmpeg: &sdk.StreamFFmpeg{
	Args:        []string{"-i", row.Path, "-map", "0:a:0", "-vn", "-c:a", "libopus", "-f", "ogg", "pipe:1"},
	ContentType: "audio/ogg",
	Fallback:    &sdk.SendFile{Path: row.Path, ContentType: "audio/flac"},
}}
```

Nothing is sent until ffmpeg's first byte, so a failure before it becomes something else:

| What happened | With `Fallback` | Without |
| --- | --- | --- |
| `ffmpeg` not granted | 404 | 404 |
| ffmpeg disabled or missing, every slot busy, or it did not start | the fallback file | 503 with `Retry-After: 5` |
| No output within 30 seconds, or it exited having written nothing | the fallback file | 502 |

Once the first byte is out the answer is a 200 with your `ContentType` (default `application/octet-stream`) and `Headers` (minus the dropped ones), without range support; if ffmpeg fails after that, the body simply ends early. The fallback is sent like any `SendFile`, so it needs a folder holding its path.

## Library scan

The `library_scan` service reaches Streamline's library scan, the `drift-check` job that checks every tracked file is still on disk.

```go
st, err := sdk.ScanStatus() // st.Scanning, st.Count, st.LastScan (*time.Time, nil before the first)
err = sdk.StartScan()       // returns once it is started; one already running is not an error
```

`Count` is the number of tracks holding a file on disk. Without the grant both return an error.

## Settings cards

A `settings` card is a set of per-user controls for an admin, each field a switch or a whole number between `Min` and `Max`:

```go
AccountCards: []sdk.AccountCard{{
	ID: "permissions", Kind: sdk.CardSettings, Title: "Music app permissions",
	Fields: []sdk.CardField{
		{ID: "can_download", Kind: sdk.FieldSwitch, Label: "Download"},
		{ID: "max_bit_rate", Kind: sdk.FieldNumber, Label: "Maximum bit rate", Min: 0, Max: 320},
	},
}},
```

Streamline calls your module at `/cards/<id>` on an admin's behalf, with the admin as `Caller` and the account being edited as `TargetUser`:

- `GET`: answer `{"values": {"<field id>": <value>, ...}}` for that user, status 200.
- `PUT`: the body is `{"values": {...}}` with some or all of the fields; store them for that user and answer 204. A field left out stays as it is. Streamline has already checked every key is one of your fields, every switch a boolean and every number a whole number within its bounds.

A settings card is reachable only this way: `/api/v1/extensions/<name>/cards/<id>` answers `404` for it, so every request your module sees for the card carries a `TargetUser` and values Streamline already checked against the card's fields.

## Views

Each granted view appears as `streamline_<name>`, read-only. Columns are stable within API version 1: new ones are added, none renamed or removed.

| View | Grant text | Columns |
| --- | --- | --- |
| `users` | See user names and roles | `id`, `display_name`, `role` |
| `user_logins` | See users' email addresses | `user_id`, `email` (lower-cased) |
| `books` | Read the book library | `id`, `title`, `original_title`, `sort_title`, `author_name`, `kind`, `genre`, `release_year`, `release_date`, `overview`, `preferred_language`, `series_id`, `series_position`, `added_at`, `updated_at` |
| `book_series` | Read the book series | `id`, `title`, `original_title`, `sort_title`, `author_name`, `kind`, `overview`, `ongoing` |
| `authors` | Read book authors | `id`, `name`, `sort_name` |
| `book_contributions` | Read who made each book | `id`, `author_id`, `book_id`, `series_id`, `role`, `language`, `position` (one of `book_id` and `series_id` is set) |
| `book_files` | Read which book files exist | `id`, `book_id`, `slot` (`ebook` or `audiobook`), `format`, `size`, `filename` (the base name, never a path), `added_at`, `updated_at`; files marked missing are left out |
| `artists` | Read the music library | `id`, `mbid`, `name`, `sort_name`, `type`, `origin`, `since`, `genre`, `overview`, `added_at`, `updated_at` |
| `artist_members` | Read the music library | `id`, `artist_id`, `name`, `mbid`, `instruments`, `from_year`, `to_year`, `ordinal` |
| `albums` | Read the music library | `id`, `mbid`, `artist_id` (the album's owner), `title`, `type`, `release_date`, `original_date`, `edition_date`, `genres`, `label`, `country`, `status`, `added_at`, `updated_at` |
| `album_artists` | Read the music library | `album_id`, `artist_id`, `owner` (1 for the owner, 0 for every other artist the album is listed under) |
| `tracks` | Read the music library | `id`, `mbid`, `album_id`, `title`, `disc`, `position`, `duration` (seconds), `bonus`, `artist_credit`, `added_at`, `updated_at` |
| `music_credits` | Read the music library | `id`, `album_id`, `track_id`, `kind`, `role`, `name`, `mbid`, `instruments`, `guest`, `ordinal` |
| `music_files` | Read the music library | `id`, `track_id`, `path` (absolute, on the server), `format`, `quality`, `size`, `duration` (seconds), `bitrate`, `sample_rate` and `bit_depth` (always NULL), `channels`, `missing` (1 when the file is gone from disk) |
| `genres` | Read the music library | `name`, `album_count` |

The API groups views in bundles for the admin (`music`, `books`); your manifest still lists each view by name. There is no view of password hashes, sessions, API keys or requests. Two views carry what others never do: `music_files.path`, so you can open and send the file, and `legacy_subsonic_users.password`, the one readable password, so a Subsonic extension can carry its accounts over.

## Limits

| | |
| --- | --- |
| Memory per instance | 64 MiB |
| Time per request | 30 s |
| Time per SQL statement | 5 s |
| Request and response body | 16 MiB each |
| One SQL value, and one query's result | 16 MiB each; page a larger read |
| Module size | 32 MiB |
| Cache folder | 1 GiB, oldest files deleted past it (hourly) |
| ffmpeg runs at once | 8 per extension |
| `sdk.FFmpeg` output | 16 MiB of stdout, the last 64 KiB of stderr |

## What a guest cannot do

- Open a network connection, or read a file outside its granted folders: the only ways out are the host calls above (`Query`, `Exec`, `Log`, `FFmpeg`, `ScanStatus`, `StartScan`) and the four directives. An extension granted `ffmpeg` is the exception: ffmpeg itself can reach any file and the network.
- Read core outside its views, or write to core or to a read-only folder at all.
- Keep memory between requests, or run in the background: it runs only while answering a request.
- Set cookies, or see the visitor's cookies, bearer token or API key (except `Authorization` on a `self` mount).
- Run script in a visitor's browser.
- Claim a prefix Streamline or another extension owns, or ask for a view, folder or service that does not exist.
