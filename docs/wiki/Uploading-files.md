# Uploading files

If you can reach Streamline only over HTTP, with no shell and no access to the server's disk, you can hand it a finished album, a discography or a book from the web UI or the REST API. The files go through the same importer as a finished download: matched, renamed by your naming template, tagged, refreshed on your media servers and listed in History.

Uploads go onto something **already in the library**. Add the artist or book first through the normal search.

## What you can upload

| Where | What | Result |
|---|---|---|
| Album page (albums, EPs and singles) | A folder or several files | Each file is matched to a track; tracks that already have a file are left alone unless you replace them |
| A track row on the album page | One file | The file lands on that track, whatever its tags or name say |
| Artist page | A discography: one folder per album | Each folder is matched to an album of the artist (monitored or not, any type); folders matching none are skipped |
| Book page | An ebook or an audiobook, per slot | An ebook keeps the one file your profile prefers; an audiobook keeps its folder |

Choose **Upload files** on the page, then drop a folder or files on the sheet, or use the folder or file picker. Before any byte is sent, Streamline shows which file goes where, so a multi-gigabyte upload is never sent for nothing. The preview matches by file name; the import itself matches by tags, so treat it as a preview. **Replace existing files** is offered when the target already has files.

Only audio (for music) or the slot's formats (for a book) are kept, along with `.cue` files, which the importer reads to refuse a CUE disc image. Anything else, such as artwork, `.nfo` files and a PDF booklet in a music folder, is discarded. A file of the wrong kind (a video on an album, an MP3 on an ebook) refuses the whole upload and names the file, and so does a file named as audio that holds none, once everything has arrived. That check reads the file's headers, so it catches a file that was never audio, not one cut short partway through.

## What happens next

The upload is streamed to a staging folder, `<download_path>/.uploads/`, then filed as a download with no client. The album or book slot shows as downloading, and the importer **copies** the files into the library whatever `library.import_mode` says, then removes the staging folder. A held or failed upload keeps its staging folder so you can resolve or retry it from Activity; deleting or rejecting the record removes it. History shows "Upload" in place of an indexer.

An hourly job, `purge-uploads`, removes staging folders that nothing is receiving or waiting on, such as after a dropped connection. The upload is one request; if it is interrupted, send it again.

## Refusals

- `409 upload_conflict`: the album or book slot already has a download in flight or held. Cancel or resolve it first.
- `422 upload_rejected`: an unsafe file name, the wrong kind of media, no media at all, a book whose details have not loaded yet, or nothing in a discography matched. The message says which.
- `422 no_space`: the declared size plus `download.space_reserve_gb` does not fit on the download volume, or the disk filled up during the upload. `download.space_check: false` turns the first check off, as it does for grabs.
- `422 no_quality_profile`: the book has no usable quality profile.

`library.allowed_download_roots`, when set, must cover `<download_path>/.uploads`, or uploads are refused up front.

## Behind a reverse proxy

Uploads can be large, and a proxy in front of Streamline will cut them by default.

- **nginx**: `client_max_body_size 0;` on the location, and `proxy_request_buffering off;` so the body streams instead of being spooled to the proxy's disk first.
- **Traefik**: turn request buffering off (remove the `buffering` middleware, or set `maxRequestBodyBytes` to 0) and raise the entrypoint's `readTimeout`, or set it to 0.
- **Cloudflare and similar**: their body-size caps apply; upload over the LAN address for large files.

Streamline itself keeps the normal 1 MiB body cap everywhere except these routes, and cuts an upload that stalls for two minutes.

## From the API

The endpoints are listed under Music and Books in the [REST API](REST-API). Send `multipart/form-data` with one `file` part per file, each filename the path relative to the folder, and the options in the query string:

```sh
curl -X POST -H "X-API-Key: $KEY" \
  -F "file=@01.flac;filename=Album/CD1/01.flac" \
  -F "file=@02.flac;filename=Album/CD1/02.flac" \
  "https://streamline.example/api/v1/music/albums/42/upload?title=Album&replace_existing=false"
```

A key needs `scope: full`; `read` keys cannot upload. The preview endpoints take a JSON list of `{path, size}` and send nothing.
