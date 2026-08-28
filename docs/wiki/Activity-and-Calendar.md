# Activity and Calendar

Where to look when you want to know what Streamline is doing, what it did, and what's coming.

- [Dashboard](#dashboard)
- [The event feed](#the-event-feed)
- [The queue](#the-queue)
- [History](#history)
- [Adopted torrents](#adopted-torrents)
- [Torrents (built-in client)](#torrents-built-in-client)
- [Calendar](#calendar)

---

## Dashboard

The landing page. A summary of library counts, what's downloading right now, recent activity, and anything needing your attention.

The health indicator in the top bar reflects real state — disk usage on your data and media volumes, and whether you're serving over plain HTTP. If it's not green, something on the [Troubleshooting](Troubleshooting) page probably applies.

---

## The event feed

The **Recent activity** panel on the dashboard, and `GET /api/v1/activity` behind it,
is the history of what happened to each title. Events cover movies, episodes and
series:

| Event | When it fires |
| --- | --- |
| `grabbed` | A release was sent to the download client |
| `download_completed` / `download_failed` | That download finished or failed |
| `imported` | A file landed in the library |
| `import_failed` | A bulk-import entry failed |
| `drift_detected` | A tracked file went missing from disk |
| `drift_confirmed` | It stayed missing past the grace window and the row was reverted |
| `searched` | A search-and-grab pass ran |

Every event names exactly one owner — `movie`, `episode` or `series`. Episode rows
render as *Show · S01E03*.

**`searched` is recorded once per search, not once per episode.** Asking Streamline
to search a series writes one event for the whole pass, with the seasons it
touched, how many episodes it searched and how many it grabbed in the payload.
A pass over one season reads as *Show · Season 3*. Without that, a
`tv-missing-search` tick over a large library would write thousands of rows an hour.

Browsing releases in the manual-grab dialog records nothing — no grab happened,
and the results are already on screen. The grab that follows still fires
`grabbed`.

Rows are purged by the `cleanup` job after `events.retention` (default 90 days).

---

## The queue

**Activity → Queue.** A live snapshot of everything currently downloading, refreshed every 30 seconds by the `download-monitor` job.

Each row shows progress, speed, ETA, size, which download client is handling it, and what it's for.

You can **pause**, **resume** and **remove** items directly — these act on your actual download client, so pausing here pauses in qBittorrent. Removing asks whether to delete the downloaded data too.

A queue entry is in one of five states:

| Status | Meaning |
| --- | --- |
| **Downloading** | Normal progress |
| **Importing** | Downloaded; Streamline is moving it into your library |
| **Held** | Downloaded, but it failed import verification and is waiting on your decision |
| **Paused** | Suspended, by you or by the client |
| **Error** | Something failed — the entry carries a failure reason |

**Held** entries are the only ones whose next move is yours. Streamline probed
the finished file and it disagrees with what the release claimed — a 720p file
sold as 1080p, a truncated runtime, a codec your profile doesn't allow, or a
file ffprobe cannot read at all. Nothing was moved into your library. Open the
entry and choose: **import anyway**, or delete it — with or without searching
for a replacement. A season pack is held whole, listing every file that failed.

See [Configuration Reference](Configuration-Reference#import-verification) for
the checks and how to turn them up or off (including `always_ask`, which holds
every import for review).

A torrent that shows progress stuck at the same percentage with no speed has no peers. It'll sit there forever — remove it and let Streamline find another release, or search manually and pick one with more seeders.

---

## History

**Activity → History.** Everything that's happened, newest first: grabs, imports, failures, deletions, renames, metadata refreshes.

Filter by event type or by title, and page back through time. This is where you find *why* something failed — an import error records the actual reason (permission denied, cross-device link, destination exists), which is far more useful than the status word on the queue.

**Clear completed** tidies out successful entries and leaves the failures. Events also age out on their own after `events.retention` (default 90 days).

---

## Adopted torrents

This one is worth understanding because it looks like a bug the first time you see it.

Streamline only manages torrents tagged with the category/label `streamline`. If you add a torrent to that category **yourself**, Streamline notices it and tries to work out what it is.

When it can safely tell — and the file is a clean upgrade or a straightforward addition — it just imports it. When it *can't* be sure, it doesn't guess. It parks it as a **proposal** awaiting your decision, and the sidebar flags *"Adopted torrents need attention"*.

Proposals come up when:

- The resolution is below your quality profile's minimum
- A file already exists for that title
- The match is ambiguous
- It's a season pack that doesn't map cleanly onto what you're missing

Three actions per proposal:

| Action | Effect |
| --- | --- |
| **Import** | Accept it and import as-is |
| **Replace** | Delete the existing file, then import this one |
| **Ignore** | Dismiss it. Optionally also remove the torrent from your download client |

Ignore is permanent for that proposal — it won't be offered again.

### Torrents for something you don't track yet

You don't have to add the movie or series first. A torrent matching nothing in your library shows up as a proposal too, marked *unidentified*, with a single **Identify** action:

1. Say whether it's a movie or a series — Streamline guesses from the release name.
2. Pick the title from the usual TMDB/TVDB search, pre-filled with whatever the name parsed to.

The title is added to your library if it isn't there already, and the download is matched to it. Nothing is imported at that point — the proposal stays put, now showing Import or Replace like any other.

Adding a series this way monitors it, exactly as adding it by hand would, so Streamline will start looking for its other missing episodes. Unmonitor the seasons you don't want if that's not what you're after.

---

## Torrents (built-in client)

**Activity → Torrents.** Only relevant if you're running Streamline's built-in torrent engine (`client_type: builtin`).

Live view of the engine's torrents: peers, pieces, ratio, up/down rates. You can pause and resume individual torrents, and toggle individual **files** within a torrent on and off — handy for a season pack where you only want three episodes.

If you're using qBittorrent or Transmission, this page will be empty; manage those torrents in their own UI or from the Queue.

---

## Calendar

**Calendar.** A month grid plus an agenda view of what's on the horizon, covering two kinds of event:

- **Movie digital releases** — for wanted films, the date they become available digitally in your `metadata.tmdb_region`. This is why setting the region correctly matters: leave it wrong and your calendar shows another country's release schedule.
- **Episode air dates** — upcoming episodes of the shows you're tracking.

Filter chips switch between all / movies / episodes. Click any day to see what lands on it; click an entry to jump to the title.

The calendar is a *forecast*, not a queue. An entry appearing today doesn't mean Streamline has grabbed it — it means the release date has arrived and the next RSS sync or missing search is now likely to find something.

Weeks start on Monday regardless of locale.

---

**Next:** [Requests and Users](Requests-and-Users), or [Troubleshooting](Troubleshooting) if something's stuck.
