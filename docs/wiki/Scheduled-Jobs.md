# Scheduled Jobs

Everything Streamline does on its own is a named job on a fixed interval. All of them are visible, retimeable, pausable and runnable on demand at **Settings → Schedules** or via `/api/v1/schedules`.

- [The jobs](#the-jobs)
- [Driving them](#driving-them)
- [Job state](#job-state)
- [Tuning intervals](#tuning-intervals)
- [Deprecated config keys](#deprecated-config-keys)

---

## The jobs

| Job | Default | Config key | What it does |
| --- | --- | --- | --- |
| `download-monitor` | `30s` | `schedules.download_monitor` | Polls download clients for progress; hands finished torrents to the importer; adopts manually-added `streamline`-tagged torrents |
| `import-scan` | `60s` | `schedules.import_scan` | Recovery sweep: re-queues any download record stuck in `importing` state, so work isn't lost across a restart or transient failure |
| `movie-rss-sync` | `15m` | `schedules.movie_rss_sync` | Reads indexer RSS feeds, grabs matching wanted movies |
| `tv-rss-sync` | `15m` | `schedules.tv_rss_sync` | Same, for episodes |
| `movie-missing-search` | `12h` | `schedules.movie_missing_search` | Actively searches indexers for every still-wanted movie |
| `tv-missing-search` | `12h` | `schedules.tv_missing_search` | Same, for episodes |
| `movie-metadata-refresh` | `24h` | `schedules.movie_metadata_refresh` | Re-pulls TMDB metadata, posters, release dates |
| `tv-metadata-refresh` | `24h` | `schedules.tv_metadata_refresh` | Re-pulls TVDB metadata; discovers new seasons and episodes |
| `movie-orphan-scan` | `6h` | `schedules.movie_orphan_scan` | Finds untracked video files under `movie_path` and queues them for review |
| `tv-orphan-scan` | `6h` | `schedules.tv_orphan_scan` | Same, under `series_path` |
| `drift-check` | `15m` | `schedules.drift_check` | Detects tracked files that have vanished from disk |
| `media-probe` | `15m` | `schedules.media_probe` | Backfills ffprobe technical info (`media_info`) onto `MediaFile` rows the importer didn't probe inline — adoption, orphan scan, bulk import. 25 rows/tick, oldest first. No-ops when `ffmpeg.enabled` is false or ffprobe isn't found |
| `file-selection` | `30s` | `schedules.file_selection` | Resolves magnet-sourced [selective file downloads](First-Run-Setup#selective-file-download) still waiting to learn what's inside the torrent. While records are actually pending the job re-checks every 5 seconds from inside its own run, so a magnet resolves at that cadence rather than this one; the interval here is only how often it looks for work when there is none. It keeps draining pending records regardless of `download.selective_files` — turning the setting off stops new pending records being created, but records already waiting still have to be resolved |
| `cleanup` | `24h` | `schedules.cleanup` | Prunes completed download records and aged-out events |
| `purge-sessions` | `1h` | — | **System job.** Deletes expired sessions. Not configurable, not controllable |

### RSS sync vs missing search

These are complementary, not redundant, and the distinction explains most latency questions.

**RSS sync** is *reactive*: it reads the newest items from each indexer's feed and checks them against what you want. Cheap, frequent, catches new releases within minutes. But a feed only carries recent items — an RSS sync will never find a film from 2011.

**Missing search** is *active*: it walks everything still marked Wanted and issues a real search query per item. Expensive, infrequent, and it's what eventually finds back-catalogue titles.

So a new release typically lands in minutes; something old can take up to 12 hours unless you hit **Search now**.

### Orphan scan and drift check

Two halves of keeping the database honest against the filesystem.

**Orphan scan** finds files on disk that Streamline doesn't know about, and queues them for review rather than importing blind — you get the same accept/match/exclude flow as an import scan.

**Drift check** finds records whose files have gone. It doesn't act immediately: a file must be missing for `library.drift_grace_ticks` consecutive checks (default 3) before its row is deleted. At the default 15-minute interval that's 45 minutes of tolerance, which is what stops a brief NFS outage from erasing your library metadata.

If your storage is flaky, raise `drift_grace_ticks` (max 20) rather than lengthening the interval — you keep prompt detection while widening the tolerance.

### Download monitor

The busiest job, and the one that does the most. Every 30 seconds it polls each enabled client for the state of `streamline`-tagged torrents, updates the queue, hands anything complete to the importer, and notices torrents *you* added into the `streamline` category — producing the proposals described in [Activity and Calendar](Activity-and-Calendar#adopted-torrents).

Raising the interval above 30s makes imports feel sluggish. Lowering it mostly generates API traffic against your download client.

---

## Driving them

**In the UI:** Settings → Schedules.

**Via the API** (all admin-only):

```bash
# List every job with its state
curl -H "X-API-Key: $KEY" https://streamline.example.com/api/v1/schedules

# One job
curl -H "X-API-Key: $KEY" https://streamline.example.com/api/v1/schedules/movie-rss-sync

# Retime it — minimum 10s
curl -X PATCH -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"interval":"5m"}' \
  https://streamline.example.com/api/v1/schedules/movie-rss-sync

# Pause / resume
curl -X POST -H "X-API-Key: $KEY" .../api/v1/schedules/movie-rss-sync/pause
curl -X POST -H "X-API-Key: $KEY" .../api/v1/schedules/movie-rss-sync/resume

# Run once, right now
curl -X POST -H "X-API-Key: $KEY" .../api/v1/schedules/movie-missing-search/run
```

Intervals have a **10 second floor**. Pause state is persisted to the database, so a paused job stays paused across restarts.

`purge-sessions` is flagged `system: true` and rejects PATCH, pause, resume and run.

---

## Job state

Each job reports:

| Field | Meaning |
| --- | --- |
| `name` | Job identifier |
| `interval` | Current interval |
| `paused` | Whether it's suspended |
| `system` | Read-only system job |
| `running` | Currently executing |
| `status` | `never` \| `success` \| `error` \| `skipped` |
| `last_started_at`, `last_finished_at` | Timestamps |
| `next_run_at` | Next scheduled fire |
| `last_duration_ms` | How long the last run took |
| `last_error` | Failure message, when `status` is `error` |

**`skipped` is the status worth watching for.** It means the job ran but bailed out because a precondition wasn't met — most often **no enabled download client**, which causes all three acquisition jobs (`*-rss-sync`, `*-missing-search`) to give up immediately. A library that never grabs anything, with jobs reporting `skipped`, is almost always this.

A rising `last_duration_ms` on `*-missing-search` is normal as your wanted list grows; on `drift-check` it usually means slow storage.

---

## Tuning intervals

Sensible directions, if the defaults don't suit:

| Situation | Change |
| --- | --- |
| Want new releases faster | `movie_rss_sync` / `tv_rss_sync` → `5m`. Check your indexers' rate limits first |
| Large library, missing-search takes too long | Lengthen to `24h`; RSS still catches new releases |
| Aggressive indexer rate limits | Lengthen RSS sync; consider pausing missing-search and running it manually |
| Slow or networked storage | Lengthen `drift_check`, or raise `library.drift_grace_ticks` |
| Library never changes outside Streamline | Pause both orphan scans entirely |
| Metadata churn matters (new seasons) | Shorten `tv_metadata_refresh` to `12h` |

Pausing a job is a legitimate long-term configuration, not just a debugging step. If nothing ever touches your library outside Streamline, the orphan scans are pure I/O for no benefit.

---

## Deprecated config keys

Four older keys are still honoured, with a warning logged at boot:

| Old key | Now |
| --- | --- |
| `schedules.rss_sync` | `schedules.movie_rss_sync` |
| `schedules.missing_search` | Applied to **both** `movie_missing_search` and `tv_missing_search` |
| `schedules.metadata_refresh` | Applied to **both** `movie_metadata_refresh` and `tv_metadata_refresh` |
| `schedules.orphan_scan` | Applied to **both** `movie_orphan_scan` and `tv_orphan_scan` |

Migrate to the split keys — you almost certainly want TV metadata refreshed more often than movie metadata, and the merged keys can't express that.
