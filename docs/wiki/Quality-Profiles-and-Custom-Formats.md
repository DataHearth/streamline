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
  - name: no-scene-junk
    description: Rejects cam/telesync/screener releases
    conditions:
      - type: release_title
        pattern: '(?i)\b(cam(rip)?|hdcam|telesync|screener)\b'
        required: true
        negate: true
```

`description` is optional free text — it has no effect on matching, but shows on the format's row in **Settings → Custom formats** and as a hint wherever the format is scored in a quality profile, the same way a built-in's fixed description does.

| Condition type | Fields | Evaluated against |
| --- | --- | --- |
| `release_title` | `pattern` (regex) | The raw release title |
| `resolution` | `value` (`720p`\|`1080p`\|`2160p`) | Parsed resolution, or probed width for on-disk files |
| `source` | `value` | Parsed source (`bluray`, `web`, `web-dl`, `hdtv`, ...) — matched case-insensitively but not otherwise fuzzed, so it must equal the parser's normalized spelling (`WEB-DL`, not `webdl`) |
| `release_group` | `pattern` (regex) | Parsed release group |
| `codec` | `value` | Parsed codec, or probed video codec for on-disk files |
| `size` | `min_gb` / `max_gb` | Indexer size, or the file's size on disk |
| `seeders` | `min` | Indexer seeders — always absent for an on-disk file, so this condition can never match a file |

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

Thirteen formats compiled into the binary — not seeded into YAML, so they can't be half-edited and stay current across upgrades. They're always available to score in a profile, they're read-only through the API (`builtin: true`; `PUT`/`DELETE` against one is `409`), and a `custom_formats` entry may not reuse a built-in name (`config.Validate` rejects the collision).

| Name | Catches |
| --- | --- |
| `remux` | "remux" in the title |
| `x265` | x265/HEVC/h265 in the title, or probed codec `hevc` |
| `x264` | x264/AVC/h264 in the title, or probed codec `h264` |
| `av1` | "av1" in the title, or probed codec `av1` |
| `hdr` | HDR10/HDR10+/HDR/DV/Dolby Vision in the title |
| `resolution-2160p` / `-1080p` / `-720p` | Parsed resolution equals that exact tier |
| `scene-junk` | CAM/HDCAM/telesync/telecine/DVDScr/screener/workprint |
| `bad-group` | A short list of known bad release groups |
| `re-encode` | "re-encode(d)" in the title |
| `multi-audio` | "multi" or "dual audio" in the title |
| `dubbed` | "dubbed" in the title |

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
      - { name: scene-junk, score: -1000 }
      - { name: bad-group, score: -1000 }
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

- **"Never grab this"** is a very negative score (`-1000`) on a format like `scene-junk` combined with `min_score: 0` — there's no separate blocklist feature, the arithmetic *is* the blocklist.
- **`upgrade_until_score` is "the score I'd be happy to stop at"**, not a target to aim for. `0` means no cap — any strictly higher-scoring release keeps upgrading. A cap nothing can reach just means upgrades never turn off for that profile; see [Upgrades](#upgrades).

**Selection**, among the releases that pass both checks: highest total score wins. Ties are broken by seeders.

Scores are relative order *within one profile* — there is no global scale, and comparing a score of 150 under one profile against a score of 150 under another means nothing.

---

## Upgrades

A profile can replace a file already on disk with a better release, but only through one path: the **RSS feed scanner**, not the interactive search. Interactive search only ever fills a `wanted` item.

Each feed tick, for every monitored movie that already has a file (`upgrade_allowed` on, not currently `downloading` — a movie mid-re-grab isn't re-grabbed again every tick), an incoming release is graded against the movie's current file:

1. The release must pass the profile's band + minimum checks like any other candidate.
2. **The current file's resolution must be in-band** (`Profile.UpgradableFrom`) — at or below `preferred_resolution`, and not unresolvable. A file *above* the band, or one whose resolution can't be determined at all, is never touched: both cases score `0` for the same mechanical reason (the band rejected them before any format was summed), and `0` is not evidence the file is bad — it's evidence the file is untouchable. Replacing it would delete exactly what the profile was protecting.
3. **The current file's score must be below `upgrade_until_score`** (or the cap is `0`, meaning no cap), and the new release's score must be strictly higher.

Only then does Streamline grab the replacement, mark the new download record `replace_existing`, and let the existing import-verification/replace flow do the rest — the same path a manual "replace" grab uses. No new download states, no separate upgrade queue.

**Movies only, in this phase.** The TV feed scanner grabs new episodes but does not evaluate upgrades for episodes already on disk — the scanner has no access to download records from that path, and a season pack is one download record standing in for many episodes, which makes "replace this one episode" a season-level design question that hasn't been answered yet.

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

The new-profile dialog offers three starting points — SPA-side templates, no server involved, prefilling the form so you edit from there:

| Preset | Formats | `min_score` | `upgrade_until_score` |
| --- | --- | --- | --- |
| **Quality first** | remux +200, hdr +100, scene-junk −1000, bad-group −1000 | 0 | 300 |
| **Space saver** | x265 +100, av1 +80, remux −100, scene-junk −1000, bad-group −1000 | 0 | 100 |
| **x265 only** | x265 +100, x264 −1000, scene-junk −1000 | 0 | 100 |

Every name in a preset is a built-in, so a preset applies cleanly to a fresh install with no `custom_formats` defined yet.

---

## Where scores show up

- **Browse releases** (`/movies/{id}/search`, the series browse endpoints, and the RSS/missing-search paths that feed them) sort by score descending, ties by seeders — not by seeders alone. Every `SearchResult` carries `score`, `rejected`, `reject_reason`, and `matched_formats`, all relative to the queried item's own profile. Rejected releases are still listed (the SPA mutes them) — an operator can grab one deliberately; `score`/`rejected`/etc. are ignored if present on a grab request body.
- **Movie detail** (`GET /movies/{id}`) reports `file_score` on each entry of `media_files` — the file's score against the movie's current profile, computed at response time. It's list-response-omitted (the same eager-loaded `media_files` edge that also carries file size and quality elsewhere), and absent entirely when no quality profile is configured at all. There's no `rejected` flag on a file — a file outside the band or below `min_score` simply shows `file_score: 0`, the same number the upgrade decision reads.
- **Episodes carry no `file_score`** — deliberately out of scope alongside the rest of the upgrade-side TV gap (see [Upgrades](#upgrades)).
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
- **No episode upgrades.** See [Upgrades](#upgrades).
