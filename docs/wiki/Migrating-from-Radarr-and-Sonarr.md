# Migrating from Radarr and Sonarr

Streamline can read a running Radarr or Sonarr and take its library over: every title it tracks, whether each one is monitored, which quality profile it uses, and the files already on disk. Optionally it copies the indexers and download clients across too. Nothing is ever written to Radarr or Sonarr.

A migration is an [import scan](Importing-an-Existing-Library) whose titles come from the *arr instead of from a directory walk, so the review and commit steps are the ones you already know. Migrations are **admin-only**.

> [!NOTE]
> In this release a migration is driven through the [REST API](REST-API); the guided screen in the web UI is coming next. Everything below is a `curl` sequence you can paste.

- [What comes across](#what-comes-across)
- [Before you start](#before-you-start)
- [1. Preview the instance](#1-preview-the-instance)
- [2. Check your paths](#2-check-your-paths)
- [3. Copy indexers and download clients (optional)](#3-copy-indexers-and-download-clients-optional)
- [4. Start the migration](#4-start-the-migration)
- [5. Review and commit](#5-review-and-commit)
- [How quality profiles translate](#how-quality-profiles-translate)

---

## What comes across

| From the *arr | In Streamline |
| --- | --- |
| Every movie / series, by TMDB / TVDB id | A library entry — no title matching, the id is taken as given |
| Monitored flag (movie, series, each season, each episode) | The same flags |
| Quality profile | The Streamline profile you map it to |
| The file of each movie / episode | Adopted in place, or imported and renamed |
| Sonarr's series type (standard, daily, anime) | The show's type, set before episodes are matched |
| Titles with no file yet | Added as wanted, so the missing search picks them up |
| Torznab indexers, Prowlarr | Indexers — every Prowlarr-synced indexer becomes **one** Prowlarr entry |
| qBittorrent, Transmission, Deluge | Download clients |

What does **not** come across: usenet indexers and clients (Streamline is torrent-only), history, blocklists, custom-format *definitions*, tags, import lists, notifications, and anything about release *source* (Bluray, WEB-DL, HDTV) in a quality profile — see [below](#how-quality-profiles-translate).

Sonarr already knows which file is which episode, so a migrated show is matched **by episode number**, never by re-reading filenames. A file name no parser could read still lands on the right episode.

---

## Before you start

You need the instance's URL and API key (**Settings → General → Security → API Key** in Radarr and Sonarr). The key is used for the calls below and **never stored** — every request carries it again.

Run one migration per instance: one for Radarr (movies), one for Sonarr (series).

The examples assume:

```bash
SL=https://streamline.example.com
api() { curl -fsS -H "X-API-Key: $STREAMLINE_KEY" -H "Content-Type: application/json" "$@"; }
SRC='"app":"radarr","url":"http://radarr:7878","api_key":"<radarr key>"'
```

---

## 1. Preview the instance

```bash
api -X POST -d "{$SRC}" "$SL/api/v1/library/imports/sources/preview" | jq
```

Nothing is written. The answer lists:

- **`root_folders`** — each with a `sample_path`, one real file under it. You use these in the next step.
- **`quality_profiles`** — each with the instance's `id` and `name`, `in_use` (how many of its titles use it), its translation into a Streamline profile (`translation`), and every lossy step spelled out in `notes`. `existing` names the Streamline profile spelled exactly the same, or is empty when there is none.
- **`indexers`** and **`download_clients`** — with a `reason` on anything that cannot come across (an indexer's `kind` and a client's `client_type` read `unsupported` then), `needs_secret` where the instance did not return the API key or password, and `conflict` where the name is already taken here.
- **`counts`** — titles, how many have a file, how many are monitored.

A wrong URL, a rejected key, or pointing a Radarr migration at a Sonarr answers `422` with a message that says which.

---

## 2. Check your paths

Radarr and Streamline usually see the same disk under different paths — `/movies` in one container, `/data/media/movies` in the other. Tell Streamline how each root folder appears **on its side**, and check it before anything starts:

```bash
api -X POST -d '{"roots":[
  {"from":"/movies","to":"/data/media/movies","sample_path":"/movies/Inception (2010)/Inception.mkv"}
]}' "$SL/api/v1/library/imports/sources/check-paths" | jq
```

Each root comes back `found: true`, or `found: false` with a `reason` (`not found`, `permission denied`, `unreadable`). When the paths are identical, send `from` and `to` the same.

> [!IMPORTANT]
> The mode decides where the roots must point, exactly like a directory scan:
>
> | Mode | Every mapped root must be |
> | --- | --- |
> | **Adopt in place** (`in_place`) | *Inside* `library.movie_path` / `library.series_path` |
> | **Import & rename** (`rename`) | *Outside* it — files are hardlinked, copied or moved in |
>
> Adopting files in place somewhere outside the library would track a library that the orphan scan and drift check never walk, so it is refused. If Radarr's folder is not under your library path, either change `library.movie_path` or use rename mode.

---

## 3. Copy indexers and download clients (optional)

Pick by the `name` the preview reported. Anything flagged `needs_secret` needs its key or password in `secret`:

```bash
api -X POST -d "{$SRC,
  \"indexers\":[{\"name\":\"Prowlarr\"}],
  \"download_clients\":[{\"name\":\"qBittorrent\",\"secret\":\"<qbit password>\"}]
}" "$SL/api/v1/library/imports/sources/apply-config" | jq
```

It is all or nothing: a name that collides, an unsupported entry, or a missing secret answers `422` with `code: migration_rejected` and **nothing** is written. Every colliding name is listed in that one answer, so you can deselect or rename them all in one pass. Run it once per instance, or skip it entirely if Streamline is already set up.

> [!NOTE]
> Streamline adds torrents to qBittorrent under the `streamline` category and expects finished downloads at `library.download_path/<torrent name>`. Radarr's category settings do not carry over — check the client's category configuration after copying it.

---

## 4. Start the migration

Map every source profile id onto a Streamline profile. Either name one that exists, or add `create` (usually the preview's `translation` block) to have it created first:

```bash
scan=$(api -X POST -d "{
  \"source\":\"radarr\",\"source_url\":\"http://radarr:7878\",\"api_key\":\"<radarr key>\",
  \"mode\":\"in_place\",
  \"root_mappings\":[{\"from\":\"/movies\",\"to\":\"/data/media/movies\",
                     \"sample_path\":\"/movies/Inception (2010)/Inception.mkv\"}],
  \"profile_mappings\":[
    {\"source_id\":4,\"target\":\"HD-1080p\"},
    {\"source_id\":5,\"target\":\"Ultra-HD\",\"create\":{\"name\":\"Ultra-HD\",\"preferred_resolution\":\"2160p\",\"min_resolution\":\"1080p\"}}
  ]
}" "$SL/api/v1/library/imports" | jq -r .id)
```

`source: "sonarr"` does the same for series. A title whose profile you did not map gets the default profile.

If another import scan is still running the request answers `409` **before** any profile is created, so nothing is left behind; wait for it and send the same request again.

The request is refused (`422`, `code: migration_rejected`) before any scan starts when a `sample_path` does not resolve through its mapping, a root breaks the rule in the table above, or a profile to create collides with a different one of the same name — every colliding name is listed at once. The message says which, and the web UI shows it as is. Re-sending a start that already created its profiles is fine — an identical profile is not a collision.

The scan then reads the instance in the background. Poll it until `status` is `awaiting_review`:

```bash
until [ "$(api "$SL/api/v1/library/imports/$scan" | jq -r .status)" = awaiting_review ]; do sleep 5; done
```

---

## 5. Review and commit

Rows are classified the way a directory scan's are, with one difference: there is no guesswork, so nothing is *unmatched*.

| Classification | Meaning on a migration |
| --- | --- |
| **Confirmed** | Ready to add |
| **Existing** | Streamline already tracks this title — committing attaches the file, and applies the *arr's monitored flag and profile |
| **Ambiguous** | The *arr reports a file Streamline cannot use: missing at the mapped path, or (in place) outside the library. Accepting it adds the title **without** the files it cannot use |

A movie row with an empty `source_path` is a title Radarr tracks without a file; it commits as a wanted entry.

```bash
api -X POST "$SL/api/v1/library/imports/$scan/commit"
```

Each row's `outcome_message` names anything that did not go fully to plan — a profile deleted since you started, a Sonarr file naming an episode TVDB does not have — without failing the title itself.

> [!WARNING]
> Committing an **Existing** movie whose Streamline entry already has a *different* file deletes that file and replaces it with Radarr's, as described in [Importing an Existing Library](Importing-an-Existing-Library#committing). Look through the Existing rows of a large migration before you commit.

When the migration is done, turn off the *arr's own automation (or shut it down) so the two do not both grab the same releases.

---

## How quality profiles translate

Streamline gates a release on a resolution band and a custom-format score. Radarr and Sonarr profiles carry more than that, and the preview's `notes` say what was lost for each one.

| *arr profile | Streamline profile |
| --- | --- |
| Lowest allowed quality | `min_resolution` — 720p, 1080p or 2160p |
| Cutoff | `preferred_resolution`, the band's ceiling (a cutoff naming a group takes the group's highest resolution) |
| Upgrades allowed | `upgrade_allowed` |
| Minimum custom format score | `min_score` |
| Upgrade until custom format score | `upgrade_until_score` |
| Custom format scores | `formats`, for every format whose **name** exists here (built-in or yours), matched regardless of case and stored under Streamline's spelling (`REMUX` lands as `remux`); the rest are listed in `notes` |
| Anything below 720p (SDTV, DVD) | Floored to 720p — Streamline has no lower band |
| Release source (Bluray vs WEB-DL vs HDTV) | Dropped — a Streamline profile does not gate on source; write a [custom format](Quality-Profiles-and-Custom-Formats) if it matters to you |

Custom formats themselves are not copied. Re-create the ones you rely on under the same names *before* starting the migration and their scores carry across.
