# Quality Profiles and Custom Formats

How Streamline scores a release against what you want, decides whether to grab it, and decides whether to replace a file you already have. For how a release title gets parsed into a resolution/source/codec/group in the first place, and how the resulting file gets named, see [Quality Profiles and Naming](Quality-Profiles-and-Naming).

- [Custom formats](#custom-formats)
- [The built-in library](#the-built-in-library)
- [Scoring profiles](#scoring-profiles)
- [The scoring mental model](#the-scoring-mental-model)
- [Upgrades](#upgrades)
- [The tester](#the-tester)
- [Presets](#presets)
- [Where scores show up](#where-scores-show-up)
- [Behavior changes from the old filter](#behavior-changes-from-the-old-filter)
- [Not in this phase](#not-in-this-phase)

---

## Custom formats

A custom format is a named set of conditions matched against a release (or, for on-disk files, a filename + probed technical data). It doesn't grab or reject anything by itself — a [quality profile](#scoring-profiles) attaches a score to it, and *that* score feeds the accept/reject/upgrade decision.

Config-backed like indexers and download clients: a top-level `custom_formats[]` list, name-keyed, hot-editable, no restart needed.

```yaml
custom_formats:
  - name: scene-junk
    description: Cam/telesync/screener rips
    conditions:
      - type: release_title
        pattern: '(?i)\b(cam(rip)?|hdcam|telesync|screener)\b'
        required: true

  - name: bad-group
    description: Groups I don't want
    conditions:
      - type: release_group
        pattern: '(?i)^(yify|yts|axxo)$'
        required: true
```

Both of these are *examples*, not defaults — nothing like them ships built in, deliberately (see [The built-in library](#the-built-in-library)). Score either one very negative in a profile and it becomes a blocklist. The release-group editor in **Settings → Custom formats** writes that second pattern for you: type group names as chips and it compiles the anchored alternation.

`description` is optional free text — it has no effect on matching, but shows on the format's row in **Settings → Custom formats** and as a hint wherever the format is scored in a quality profile, the same way a built-in's fixed description does.

| Condition type | Fields | Evaluated against |
| --- | --- | --- |
| `release_title` | `pattern` (regex) | The raw release title |
| `resolution` | `value` (`720p`\|`1080p`\|`2160p`) | Parsed resolution, or probed width for on-disk files |
| `source` | `value` | Parsed source (`BluRay`, `WEB-DL`, `WEBRip`, `HDTV`, `DVDRip`, `Remux`) — matched case-insensitively but not otherwise fuzzed, so it must equal the parser's normalized spelling. A bare `WEB` tag normalizes to `WEB-DL`, so write `WEB-DL` to catch both spellings; `WEBRip` stays distinct, since that one really is a re-encode |
| `release_group` | `pattern` (regex) | Parsed release group — the UI edits this one as a chip list, see below |
| `codec` | `value` | Parsed codec, or probed video codec for on-disk files |
| `size` | `min_gb` / `max_gb` | Indexer size, or the file's size on disk — **per episode**, see below |
| `seeders` | `min` | Indexer seeders — always absent for an on-disk file, so this condition can never match a file |

### Size is per episode, not per release

`min_gb` and `max_gb` are budgets **for one episode**. Streamline multiplies both by the number of episodes the release carries, so a single threshold means the same thing whether the release is one episode, a season pack, or a whole-series integral:

```yaml
# custom_formats[]
- name: oversized
  conditions:
    - { type: size, min_gb: 5, required: true }   # 5 GB *per episode*
```

| Release | Size | Episodes | Measured against | Matches `min_gb: 5` |
| --- | --- | --- | --- | --- |
| `Show.S03E01.1080p.WEB` | 1.4 GB | 1 | 5 GB | no |
| `Show.S03.1080p.WEB` (12 ep) | 17 GB | 12 | 60 GB | no |
| `Show.S03.1080p.BluRay.REMUX` (12 ep) | 155 GB | 12 | 60 GB | **yes** |
| `Show.COMPLETE.1080p.WEB` (60 ep) | 84 GB | 60 | 300 GB | no |

Score that format very negative (`-5000`) and the remux is rejected at every scope, while the ordinary season and series packs pass untouched. Without the scaling you would have to pick between a cap that lets the remux through and one that rejects every legitimate pack.

**Where the episode count comes from.** It is looked up in *your library*, per season, from the scope the release name claims — `S03` is season 3, `S01-S05` is those five seasons summed, `COMPLETE`/`INTEGRALE` is every season the show has. It is **not** the torrent's file count: no indexer reports that, and the torrent's contents are only knowable after grabbing it, which is too late to reject anything.

Two consequences worth knowing:

- **A season Streamline tracks no episodes for scales by nothing.** The bound is then applied to the whole release, exactly as it behaved before. Better a bound that is too generous than one invented from a count we don't have.
- **Movies always count 1**, so a `size` condition on a movie profile is unchanged by any of this.

The condition editor spells the arithmetic out as you type it — enter `1` and `5` and it tells you a 12-episode pack is judged against 12–60 GB. The format tester takes an **Episodes** field for the same reason: leave it at 1 to test a single file, set it to a season's length to see how a pack would score.

### Release groups without the regex

A `release_group` row in **Settings → Custom formats** is a chip list, not a text box: type a group name and press Enter (or type several separated by commas), click a chip's × to drop it. The editor compiles the chips to `(?i)^(name1|name2)$` — anchored, so `YTS` never matches `YTSAGAIN`, case-insensitive, and every name regex-escaped, so a group called `YTS.MX` matches that literal and not "YTS" plus any character. Score the format **negative to exclude** those groups and **positive to prefer** them; there is no separate blocklist, the arithmetic is the blocklist.

Nothing is hidden from you: **Edit as regex** switches the row back to the raw pattern, and an existing pattern the chip list could not have written (anything that is not exactly that anchored alternation of literals) opens as raw regex on its own, so a hand-written pattern is never silently rewritten. A raw pattern that *does* match the chip shape shows as chips again.

**Matching semantics are Radarr-compatible:** every `required` condition must pass, and if the format has any non-required conditions, at least one of those must also pass (a format that is all-required needs nothing else — "at least one of zero" is vacuously true). `negate` inverts a single condition's result before it's combined. A condition whose input is missing — seeders on a file, a release group nobody could parse — evaluates false, negated or not.

Two required conditions is "AND". Two non-required conditions is "OR" (either matches the format). Mixing both is "these must all hold, plus at least one of these":

```yaml
conditions:
  - { type: release_title, pattern: '(?i)\bx265\b', required: false }
  - { type: codec, value: hevc, required: false }
```

matches a release that says "x265" in the title *or* one whose probed codec is `hevc` — the pattern the built-in `x265` format itself uses, so a probed file matches it even when the filename never says so.

---

## The built-in library

Ten formats compiled into the binary — not seeded into YAML, so they can't be half-edited and stay current across upgrades. They're always available to score in a profile, they're read-only through the API (`builtin: true`; `PUT`/`DELETE` against one is `409`), and a `custom_formats` entry may not reuse a built-in name (`config.Validate` rejects the collision).

| Name | Catches |
| --- | --- |
| `remux` | "remux" in the title |
| `x265` | x265/HEVC/h265 in the title, or probed codec `hevc` |
| `x264` | x264/AVC/h264 in the title, or probed codec `h264` |
| `av1` | "av1" in the title, or probed codec `av1` |
| `hdr` | HDR10/HDR10+/HDR/DV/Dolby Vision in the title |
| `resolution-2160p` / `-1080p` / `-720p` | Parsed resolution equals that exact tier |
| `multi-audio` | "multi" or "dual audio" in the title |
| `dubbed` | "dubbed" in the title |

Every built-in *describes* a release — what codec, what resolution, what source. None of them judges one. Opinions about which release groups or which rip sources you'll accept are yours to write as `custom_formats`, because they don't generalize: the group list one person blocks is the group list another prefers, and a screener is worthless to most people and the only available copy to some. The `scene-junk` and `bad-group` examples above, and the release-group chips in **Settings → Custom formats**, are there to make writing your own a two-minute job.

`GET /api/v1/custom-formats` returns both libraries in one list; check `builtin` to tell them apart.

---

## Scoring profiles

Four fields were added to the existing profile shape; everything that was there before still means what it meant before:

```yaml
quality_profiles:
  - name: default
    min_resolution: 1080p         # unchanged: hard floor
    preferred_resolution: 2160p   # now the hard CEILING of the accepted band
    upgrade_allowed: true         # unchanged: master switch for replacing files
    allowed_codecs: []            # unchanged: drives import holds only, not grab decisions
    formats:                      # NEW: score per format, by name
      - { name: x265, score: 100 }
      - { name: hdr, score: 50 }
      - { name: scene-junk, score: -1000 }   # a custom_formats entry of your own
    min_score: 0                  # NEW: total below this -> release rejected
    upgrade_until_score: 500      # NEW: stop upgrading once the current file reaches this

quality_default_profile: default
```

`formats[].name` must resolve to a built-in name or a `custom_formats` entry — an unknown name is a `422` on save. A profile with no `formats` at all still works: nothing is scored, so any in-band release passes with a score of `0`.

**`preferred_resolution` changed meaning.** It used to be the exact target when upgrades were off. It's now the ceiling of the accepted `min_resolution`..`preferred_resolution` band — resolution *preference* inside that band is expressed by scoring the built-in `resolution-*` formats instead. A release outside the band (too low, or above `preferred_resolution`) is rejected before any format is even evaluated.

Because it is the ceiling, a `min_resolution` *above* `preferred_resolution` describes an empty band that rejects every release. Config validation refuses it outright rather than letting the profile look configured while grabbing nothing.

---

## The scoring mental model

For one release against one profile:

1. **Band check.** Resolution below `min_resolution` or above `preferred_resolution` → rejected, reason "resolution outside profile band". No score is computed.
2. **Sum.** Add the profile's score for every format that matches. Formats the profile doesn't list contribute nothing. Overlapping formats stack — `x265` (100) + `hdr` (50) scores 150, by design; every surface that shows a score also shows which formats matched, so the arithmetic is visible.
3. **Minimum check.** Total below `min_score` → rejected, reason "score below profile minimum".

Two idioms fall out of this:

- **"Never grab this"** is a very negative score (`-1000`) on a custom format like the `scene-junk` example above, combined with `min_score: 0` — there's no separate blocklist feature, the arithmetic *is* the blocklist.
- **`upgrade_until_score` is "the score I'd be happy to stop at"**, not a target to aim for. `0` means no cap — any strictly higher-scoring release keeps upgrading. A cap nothing can reach just means upgrades never turn off for that profile; see [Upgrades](#upgrades).

**Selection**, among the releases that pass both checks: highest total score wins. Ties are broken by seeders.

Scores are relative order *within one profile* — there is no global scale, and comparing a score of 150 under one profile against a score of 150 under another means nothing.

---

## Upgrades

A profile can replace a file already on disk with a better release, but only through one path: the **RSS feed scanner**, not the interactive search. Interactive search only ever fills a `wanted` item.

Each feed tick, for every monitored movie or episode that already has a file (`upgrade_allowed` on, nothing already in flight for it — an item mid-re-grab isn't re-grabbed again every tick), an incoming release is graded against the current file:

1. The release must pass the profile's band + minimum checks like any other candidate.
2. **The current file's resolution must be in-band** (`Profile.UpgradableFrom`) — at or below `preferred_resolution`, and not unresolvable. A file *above* the band, or one whose resolution can't be determined at all, is never touched: both cases score `0` for the same mechanical reason (the band rejected them before any format was summed), and `0` is not evidence the file is bad — it's evidence the file is untouchable. Replacing it would delete exactly what the profile was protecting.
3. **The current file's score must be below `upgrade_until_score`** (or the cap is `0`, meaning no cap), and the new release's score must be strictly higher.

Only then does Streamline grab the replacement, mark the new download record's replace mode `upgrades`, and let the existing import-verification/replace flow do the rest — the same path a manual "replace" grab uses. No new download states, no separate upgrade queue.

### Series, seasons and episodes

Episodes upgrade by the same three rules, but a season pack is judged **episode by episode** against each episode's own file, not against the season as a whole:

- **Each episode in the pack is compared to its own file.** A release that beats episode 3 but not episode 7 replaces episode 3 and leaves episode 7 alone — there's no season-wide veto, so one strong episode no longer blocks a pack that would improve the rest. At least one episode has to qualify for the pack to be grabbed at all.
- **Filling a gap and upgrading happen in the same grab.** If a pack covers episodes you're missing, it's grabbed to fill them, and any episode you already have that the release beats is replaced in that same download — you don't need to re-run it once the season is complete.
A single-episode release is judged against that one episode alone.

Series upgrades reuse the identical import path: verification runs before anything on disk is touched, scoped to the episodes the import actually plans to replace (see [Import verification](Configuration-Reference#import-verification)) — a season pack doesn't verify episodes it isn't going to touch. That per-episode plan, and the probe re-check behind it, is a season-pack thing: a single-episode release was already judged once by the scanner, and the importer's import just carries that decision through.

**On-disk scores are never stored.** Every comparison rebuilds a `ReleaseContext` from the file's row on demand — basename parsed the same way a release title is, resolution from the probed width (falling back to the filename parse), codec from the probe, size from the row. Editing a profile re-ranks your whole library instantly, with no migration and no cached score to invalidate.

---

## The tester

`POST /api/v1/custom-formats/test` evaluates a **draft** set of conditions — saved or not — against a synthetic sample, and reports pass/fail per condition plus the format's overall match. It's how the create/edit form in **Settings → Custom formats** lets you check a pattern before committing it.

```bash
curl -X POST -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{
    "conditions": [
      { "type": "release_title", "pattern": "(?i)\\bremux\\b", "required": true }
    ],
    "sample": { "title": "Movie.Title.2024.2160p.UHD.BluRay.REMUX.mkv", "size": 45000000000, "seeders": 40 }
  }' \
  https://streamline.example.com/api/v1/custom-formats/test
```

```json
{ "matched": true, "conditions": [{ "index": 0, "passed": true }] }
```

An empty `conditions` array is `422`. A condition that does not compile — an uncompilable regex, an unknown `type`, a resolution outside the three buckets — is `422` with `code: invalid_condition`, and the same code comes back from creating or updating a format. Its `message` names the offending condition ("condition 0: error parsing regexp: …") and the UI shows it verbatim, so it is the only place the regexp diagnostic appears.

---

## Presets

The new-profile dialog offers three starting points — SPA-side templates, no server involved, prefilling the *whole* form (name, both resolutions, allowed codecs, scores and thresholds) so you edit from there:

| Preset | Name | Resolution band | Allowed codecs | Formats | `min_score` | `upgrade_until_score` |
| --- | --- | --- | --- | --- | --- | --- |
| **Quality first** | `Quality first` | 1080p – 2160p | any | remux +200, hdr +100 | 0 | 300 |
| **Space saver** | `Space saver` | 720p – 1080p | HEVC, AV1 | x265 +100, av1 +80, remux −100 | 0 | 100 |
| **x265 only** | `x265 only` | 720p – 1080p | HEVC | x265 +100, x264 −1000 | 0 | 100 |

Applying one never saves — it fills the form and you edit from there, including the name it suggests. Every format name in a preset is a built-in, so a preset applies cleanly to a fresh install with no `custom_formats` defined yet; that is also why no preset carries a group blocklist or a junk-source penalty, which would have to name a format only you can write.

---

## Where scores show up

- **Browse releases** (`/movies/{id}/search`, the series browse endpoints, and the RSS/missing-search paths that feed them) sort by score descending, ties by seeders — not by seeders alone. Every `SearchResult` carries `score`, `rejected`, `reject_reason`, and `matched_formats`, all relative to the queried item's own profile. Rejected releases are still listed (the SPA mutes them) — an operator can grab one deliberately; `score`/`rejected`/etc. are ignored if present on a grab request body.
- **Movie detail** (`GET /movies/{id}`) reports `file_score` on each entry of `media_files` — the file's score against the movie's current profile, computed at response time. It's list-response-omitted (the same eager-loaded `media_files` edge that also carries file size and quality elsewhere), and absent entirely when no quality profile is configured at all. There's no `rejected` flag on a file — a file outside the band or below `min_score` simply shows `file_score: 0`, the same number the upgrade decision reads.
- **Series detail** (`GET /series/{id}`) reports `file_score` on each episode that has a file, against the series' profile, on the same terms. The series *list* never carries episodes at all, so there is no list/detail split to think about there.
- `min_score` is omitted from API responses when it's `0` — the handler only sets the pointer when the value is non-zero, not a signal that the profile has no minimum. Absent means `0`.

---

## Behavior changes from the old filter

Three, all a consequence of moving from "first release that passes" to score-then-select — worth knowing if you're tuning an install that predates this feature:

1. **`pickBest` is now score-ranked, then seeders-ranked** — previously it was first-hit (whichever the indexer listed first). A profile with an empty `formats` list still improves: it becomes seeders-ranked instead of first-hit.
2. **A profile with `upgrade_allowed: false` and `min_resolution` below `preferred_resolution` can now grab anywhere in that band.** Before, "upgrades off" meant "accept only exactly `preferred_resolution`". Now it means "accept the whole band, just don't replace a file already there."
3. **`preferred_resolution` is a hard ceiling, everywhere a profile is evaluated** — including the RSS/missing-search feed scanners, not just interactive search. An install whose `preferred_resolution` sits below its media's actual resolution will stop grabbing anything above it until the profile is raised. If your library already has files above a profile's `preferred_resolution`, review your profiles before relying on automatic search after upgrading.

---

## Not in this phase

- **No active backlog search for upgrade-eligible items.** Upgrades happen when the RSS feed happens to see a better release; there's no scheduled sweep that goes looking for one after you retune a profile. Re-run search manually on items you want re-evaluated right away.
- **No per-file score caching.** Every score above is computed on the fly; that's fine at today's cost, but means there's no SQL-queryable "show me everything below its upgrade cap" view yet.
- **No community format import/sync** (TRaSH Guides, Dictionarry, etc.). Custom formats here are entirely local.
