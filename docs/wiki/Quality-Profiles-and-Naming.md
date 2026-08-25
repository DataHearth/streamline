# Quality Profiles and Naming

Two mechanisms, both driven by parsing the release title: what Streamline is willing to accept, and what it calls the file afterwards.

- [Release parsing](#release-parsing)
- [Quality profiles](#quality-profiles)
- [The acceptance rule](#the-acceptance-rule)
- [File naming](#file-naming)
- [Renaming existing files](#renaming-existing-files)

---

## Release parsing

Everything starts here. Streamline extracts structured fields from a release title with regexes:

| Field | Recognised as |
| --- | --- |
| **Title** | Everything before the year / season marker |
| **Year** | `(19\|20)\d\d` |
| **Season / Episode** | `S01E02` |
| **Season pack** | `S01` with no episode marker |
| **Multi-season / complete** | `S01-S05`, `S01.S02`, `complete`, `intégrale` |
| **Absolute number** | ` - 18 ` style anime numbering. Deliberately conservative; used only as a fallback for anime |
| **Air date** | `2024.03.15` and separator variants, for dailies |
| **Resolution** | `720p`, `1080p`, `2160p`, `4K` |
| **Source** | `BluRay`, `WEB-DL`, `WEBDL`, `WEBRip`, `HDTV`, `DVDRip`, `BDRip`, `BRRip`, `Remux`, `WEB` |
| **Codec** | `x264`, `x265`, `H.264`, `H.265`, `H264`, `H265`, `HEVC`, `AV1`, `MPEG2`, `VC-1`, `AVC` |
| **Group** | Trailing `-GROUP`, or a trailing `.GROUP` when there's no dash form. Known technical tags (`MULTI`, `COMPLETE`, resolutions) are excluded |

The consequence worth remembering: **anything the parser can't read, Streamline won't accept.** That's a deliberate posture — silently grabbing an unknown-quality release is worse than grabbing nothing.

---

## Quality profiles

A profile is three fields. There is no scoring, no ranked custom-format system, no preferred-words list.

| Field | Meaning |
| --- | --- |
| `preferred_resolution` | What you want |
| `min_resolution` | Hard floor |
| `upgrade_allowed` | Whether anything above the floor is acceptable |

Resolutions rank:

| Resolution | Rank |
| --- | --- |
| *(unparseable)* | 0 |
| `720p` | 1 |
| `1080p` | 2 |
| `2160p` / `4K` | 3 |

```yaml
quality_profiles:
  - name: default
    preferred_resolution: 1080p
    min_resolution: 1080p
    upgrade_allowed: true
  - name: 4k
    preferred_resolution: 2160p
    min_resolution: 1080p
    upgrade_allowed: true
  - name: exactly-1080p
    preferred_resolution: 1080p
    min_resolution: 1080p
    upgrade_allowed: false

quality_default_profile: default
```

Profiles are also fully manageable at **Settings → Quality profiles** and via `/api/v1/quality-profiles`.

Movies and shows reference a profile by name. An empty reference resolves to `quality_default_profile`.

Profiles are resolved **per item at search time**, not cached at add time — so editing a profile takes effect on the next search with no restart.

---

## The acceptance rule

In full:

```
parsed = Parse(releaseTitle)

if parsed.Resolution is empty          → REJECT
if rank(parsed) == 0                   → REJECT
if rank(parsed) < rank(min_resolution) → REJECT
if not upgrade_allowed
   and rank(parsed) != rank(preferred) → REJECT
otherwise                              → ACCEPT
```

Three consequences that account for nearly every "why won't it grab this" question:

**1. No resolution in the title means rejection.** Not a downgrade, not a warning — a refusal.

**2. `upgrade_allowed: false` rejects things *above* your preference too.** It means "exactly this resolution", not "at most this resolution". A 2160p release fails a 1080p-preferred profile with upgrades off.

**3. With no profiles configured at all, everything is rejected.** `qualityFor` logs `no quality profile configured, rejecting every release` and returns a zero-value filter that nothing satisfies.

Source and codec are parsed and stored, and are available for naming — but they are **not** part of the acceptance decision. There is currently no way to express "prefer BluRay over WEBRip" declaratively; use manual search when you want a specific release.

**Manual grabs bypass the profile entirely.** That's the escape hatch.

---

## File naming

Two templates:

```yaml
library:
  movie_naming: '{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}'
  series_naming: '{title} ({year})/Season {season}/{title} - S{season:2}E{episode:2} - {episode_title} [{quality}].{ext}'
```

Templates include directory separators — the whole relative path under `movie_path` / `series_path` is templated, not just the filename.

### Tokens

**Movies:**

| Token | Value |
| --- | --- |
| `{title}` | Movie title from TMDB |
| `{year}` | Release year — omitted if unknown |
| `{tmdb_id}` | TMDB ID |
| `{imdb_id}` | IMDb ID, when known |
| `{quality}` | Parsed resolution |
| `{source}` | Parsed source |
| `{codec}` | Parsed codec |
| `{group}` | Release group |
| `{ext}` | File extension |

**Episodes:**

| Token | Value |
| --- | --- |
| `{title}` | **Show** title |
| `{year}` | Show year |
| `{season}`, `{episode}` | Numbers |
| `{episode_title}` | Episode title |
| `{quality}` | Parsed resolution |
| `{absolute}` | Absolute episode number, when parsed |
| `{air_date}` | `YYYY-MM-DD`, when parsed |
| `{ext}` | File extension |

Note that episode templates do **not** get `{source}`, `{codec}` or `{group}` — those are movie-only.

### Zero-padding

`{token:N}` zero-pads a numeric value to width N:

| Template | Season 1, Episode 2 |
| --- | --- |
| `S{season}E{episode}` | `S1E2` |
| `S{season:2}E{episode:2}` | `S01E02` |
| `S{season:3}E{episode:3}` | `S001E002` |

Padding applies only when the value parses as a number; otherwise it's rendered as-is.

### Unknown tokens render empty

An unrecognised token, or one whose value isn't populated, becomes an empty string rather than an error or a literal `{token}`. That's what lets optional segments stay clean:

```
{title} ({year}) [{imdb_id}]     →  The Matrix (1999) []      # when unknown
```

Note the brackets survive. If a segment should disappear entirely when its token is empty, don't wrap it in literal punctuation.

> **A quirk in the shipped default.** The movie default contains `{tmdb-{tmdb_id}}` — nested braces. The parser matches `{tmdb_id}` inside it, so this renders as `{tmdb-603}` rather than `tmdb-603`: the literal outer braces stay in the directory name. Plex and Jellyfin both read `{tmdb-603}` as an ID hint, so this is intentional and works — but if you write your own template, know that the braces are literal text, not template syntax.

### Sanitisation

Rendered paths are sanitised for filesystem safety:

| Character | Becomes |
| --- | --- |
| `:` | ` -` (space-hyphen) |
| `/` `\` | `-` |
| `<` `>` `"` `\|` `?` `*` | *removed* |

Runs of whitespace are then collapsed and the ends trimmed, so a title that
already had a space beside the character doesn't end up with two: `Alien:
Romulus` becomes `Alien - Romulus`, and `Mission : Impossible` — which is how
TMDB writes it in some languages — becomes `Mission - Impossible`, not
`Mission  - Impossible`.

Sanitisation applies to the **values** substituted into the template, not to the
rendered path, so a `/` inside a title (`In/Spectre`, `Face/Off`) becomes a dash
rather than a directory separator. The `/` in the template itself still marks a
directory.

> If you are upgrading, existing folders keep their old spelling until you
> re-run a rename. `POST /movies/{id}/rename` moves the file and removes the
> directory it emptied.

### Examples

```yaml
# Plex-friendly, IDs in the folder name
movie_naming: '{title} ({year}) {tmdb-{tmdb_id}}/{title} ({year}) [{quality}].{ext}'
# → The Matrix (1999) {tmdb-603}/The Matrix (1999) [1080p].mkv

# Flat, quality-rich
movie_naming: '{title} ({year}) [{quality}] [{source}] [{codec}]-{group}.{ext}'
# → The Matrix (1999) [1080p] [BluRay] [x265]-GROUP.mkv

# Anime, absolute numbering
series_naming: '{title}/{title} - {absolute:3} - {episode_title} [{quality}].{ext}'
# → Cowboy Bebop/Cowboy Bebop - 018 - Speak Like a Child [1080p].mkv

# Daily shows
series_naming: '{title}/{title} - {air_date} - {episode_title}.{ext}'
```

---

## Renaming existing files

Changing a template does **not** rewrite files already on disk. Apply it with the per-title **Rename** action, or:

```bash
curl -X POST -H "X-API-Key: $KEY" \
  https://streamline.example.com/api/v1/movies/42/rename

curl -X POST -H "X-API-Key: $KEY" \
  https://streamline.example.com/api/v1/series/7/rename
```

This is also how you normalise files adopted in place by an [import scan](Importing-an-Existing-Library) — adoption records paths as-is; renaming is a deliberate, separate step.

If you're re-rooting a whole library rather than renaming within it, use [path migration](GitOps-and-Kubernetes#library-path-migration) instead.
