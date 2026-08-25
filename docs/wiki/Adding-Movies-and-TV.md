# Adding Movies and TV

How a title gets from "I'd like to watch that" to a file on disk.

- [Adding a movie](#adding-a-movie)
- [Adding a TV show](#adding-a-tv-show)
- [What the statuses mean](#what-the-statuses-mean)
- [How automatic grabbing works](#how-automatic-grabbing-works)
- [Searching manually](#searching-manually)
- [Monitoring and unmonitoring](#monitoring-and-unmonitoring)
- [Fixing a wrong match](#fixing-a-wrong-match)
- [Renaming and deleting](#renaming-and-deleting)

---

## Adding a movie

Click the **+** button (or press it in the bottom bar on mobile) → **Movie**. Type a title. Results come from TMDB.

Pick the right one — check the year, posters help — and you'll be asked for two things:

- **Quality profile** — leave it on the default unless this particular film deserves different treatment
- **Monitored** — on by default. A monitored film is one Streamline actively hunts for

Confirm, and the film lands in your library as **Wanted**.

Nothing happens instantly. Streamline searches on a schedule, not on the spot. If you're impatient, open the film and hit **Search now**.

---

## Adding a TV show

Same **+** button → **Series**. Results come from TVDB.

Shows have one extra decision, and it's the one worth reading carefully — **which episodes to monitor**:

| Option | Monitors |
| --- | --- |
| **All episodes** | Everything, past and future |
| **Missing episodes** | Only what you don't already have a file for |
| **Existing episodes** | Only episodes you already have — useful for upgrades, not acquisition |
| **Future episodes** | Nothing aired yet; only what airs from now on |
| **Pilot only** | Season 1 Episode 1, to try a show before committing |

**Specials (season 0) are not monitored by default.** Most people don't want them, and they're the most poorly-named releases on any tracker. Turn them on globally at Settings → Series (`library.monitor_specials`), or per show from the show's page.

The Settings → Series page also holds the defaults applied to *new* shows and to seasons discovered later. Changing it doesn't retroactively touch shows already in your library — there's an explicit **Apply to existing series** action for that.

Once added, the show page shows every season and episode, what's on disk, what's missing, and what's still to air.

---

## What the statuses mean

**Movies** carry one of four:

| Status | Meaning |
| --- | --- |
| **Wanted** | Monitored, no file yet, being searched for |
| **Downloading** | A release was grabbed and is in your download client |
| **Available** | Imported and sitting in your library |
| **Failed** | Grabs kept failing — see below |

**Episodes** have a fifth, **Paused**, for episodes deliberately held back, and **Skipped** for ones you've told Streamline to ignore.

A film goes **Failed** after `library.max_grab_failures` (default 3) consecutive failed grab attempts. This is a circuit breaker, not a permanent verdict — it stops Streamline hammering a tracker for something that isn't working. Open the film and hit **Search now** to try again.

Separately, when a search finds *nothing acceptable*, that title goes quiet for `library.no_match_cooldown` (default 6 hours) before being searched again. That's why a just-added obscure film may sit at Wanted for a while without visible activity.

---

## How automatic grabbing works

Two independent jobs hunt for things, and it helps to know which one is doing what.

**RSS sync** (every 15 minutes by default) reads the newest releases from your indexers and checks whether any match something you want. This is what catches new releases quickly — usually within minutes of them being posted.

**Missing search** (every 12 hours) takes the opposite approach: it walks through everything still marked Wanted and actively searches for it. This is what eventually finds older titles that will never appear in an RSS feed.

Both exist separately for movies and TV (`movie_rss_sync`, `tv_rss_sync`, `movie_missing_search`, `tv_missing_search`) and all four can be retimed, paused or run on demand from **Settings → Schedules**.

When a release passes the quality bar, Streamline sends it to the highest-priority enabled download client, tagged `streamline`. When it finishes, the download monitor (every 30 seconds) notices, and the importer hardlinks it into your library under the naming template, then nudges your media server to rescan.

So the expected latency for a brand-new release is *minutes*, and for something obscure and old it can be *hours* — that's not a fault, it's the missing-search interval.

---

## Searching manually

Automation not finding it? Open the title and click **Search** — this runs a live query against every enabled indexer and shows you what came back.

The results list gives you release name, size, seeders, indexer and detected quality, and you can filter by indexer or release group. Click a release to grab it directly, bypassing the quality profile entirely. That's the escape hatch for the case where you want *this specific release* and don't care what the rules say.

There's a **Replace existing files** toggle on the grab dialog. Off, Streamline refuses to import over a file that's already there. On, it overwrites. Use it when you're deliberately upgrading.

For TV you can search at three levels:

- **Whole series** — for complete-series packs
- **A season** — for season packs. Note that Streamline *hides* complete-series and multi-season packs from a season search by default, since they rarely do what you want; there's a toggle to show them
- **A single episode** — narrowest, most reliable

---

## Monitoring and unmonitoring

Monitoring is Streamline's on/off switch for automation. Unmonitored things are never searched for and never grabbed — they just sit in your library.

Unmonitor a film you've decided you don't want after all, and it stops appearing in searches without you having to delete it. For shows, monitoring cascades down the tree: unmonitor a season and its episodes go with it.

This is the right tool for "I have seasons 1-3 and don't want 4 onwards" — unmonitor season 4, and Streamline stops hunting for it.

---

## Fixing a wrong match

Sometimes a title ends up pointing at the wrong entry — an import matched
*The Matrix* to *The Matrix Reloaded*, or a series folder landed on the wrong
show. **Change match…** in the actions menu repairs it without losing anything.
Admin only.

Pick the correct title from the same TMDB/TVDB search the add flow uses, confirm,
and the entry is repointed **in place**: it keeps its id, its files, its download
history and any requests attached to it. Only the provider identity changes.
Metadata is refreshed from the new entry and the files are renamed into the new
title's folder.

**For a series** the season and episode list is rebuilt from the new show, and
each file is re-attached to the episode with the same season and episode number.
Files whose numbering has no counterpart in the new show are **left on disk** and
listed back to you — a provider numbering seasons differently is not evidence the
media is unwanted. Their database rows are dropped, so an orphan scan can re-adopt
them once you have decided where they belong.

Re-identifying a series also re-infers its **type** (standard/anime/daily). That is
the one case where an override you set by hand is deliberately discarded: it was
about the old show.

There is no undo, because none is needed — changing the match back is the same
action in reverse, files and all.

**You cannot re-identify onto a title already in your library.** `tmdb_id` and
`tvdb_id` are unique, and merging two entries is a different operation than this
one. Delete the wrong entry first.

---

## Renaming and deleting

**Rename** re-applies your naming template to files already on disk. Useful after you've changed `library.movie_naming` or `library.series_naming`, or after importing files under their original names. It's available per-title from the actions menu.

**Delete** removes the title from Streamline. You'll be asked whether to also delete the files on disk — the two are separate decisions, and Streamline will happily forget about a film while leaving the file exactly where it is.

Deleting a title also clears its cached posters and any related download records.

---

**Next:** [Activity and Calendar](Activity-and-Calendar) — watching what's happening, and dealing with things that get stuck.
