# Quality Profiles and Naming

Two mechanisms, both driven by parsing the release title: what Streamline is willing to accept, and what it calls the file afterwards.

This page is the first half of quality: release parsing, the resolution band and file naming. Its companion, [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats), covers the scoring layer on top of the band — custom formats, `min_score`, upgrades and the transcode policy. Music and book profiles are tiers and formats rather than resolutions, and are described on [Music and Books](Music-and-Books#searching-and-grabbing); their file naming is [below](#music-and-book-naming).

- [Release parsing](#release-parsing)
- [Quality profiles](#quality-profiles)
- [The acceptance rule](#the-acceptance-rule)
- [File naming](#file-naming)
- [Music and book naming](#music-and-book-naming)
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

> [!IMPORTANT]
> Anything the parser can't read, Streamline won't accept. That's a deliberate posture — silently grabbing an unknown-quality release is worse than grabbing nothing.

---

## Quality profiles

A profile's core is three fields, all resolution-driven:

| Field | Meaning |
| --- | --- |
| `preferred_resolution` | Hard ceiling of the accepted band |
| `min_resolution` | Hard floor |
| `upgrade_allowed` | Whether a file already on disk can be replaced by a better release |

Profiles also carry a custom-format scoring layer — matched formats, a minimum score, an upgrade cutoff — covered in full on [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats). This page sticks to resolution and naming; that one covers "which release wins" and "when does Streamline replace a file you already have".

Resolutions rank:

| Resolution | Rank |
| --- | --- |
| *(unparseable)* | 0 |
| `480p` | 1 |
| `720p` | 2 |
| `1080p` | 3 |
| `2160p` / `4K` | 4 |

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

movie_quality_default_profile: default
series_quality_default_profile: default
```

Profiles are also fully manageable at **Settings → Quality profiles** and via `/api/v1/quality-profiles`.

Movies and shows reference a profile by name. An empty reference resolves to `movie_quality_default_profile` for a movie and `series_quality_default_profile` for a show, so movies and series can each have their own default. A profile cannot be deleted while it is either default.

A config file may carry the single key `quality_default_profile` instead. It is read and seeds both keys when neither is set; the next time Streamline saves the config it is replaced by the two keys.

Profiles are resolved **per item at search time**, not cached at add time — so editing a profile takes effect on the next search with no restart.

---

## The acceptance rule

The resolution part, in full:

```
parsed = Parse(releaseTitle)

if parsed.Resolution is empty              → REJECT
if rank(parsed) == 0                       → REJECT
if rank(parsed) < rank(min_resolution)     → REJECT
if rank(parsed) > rank(preferred_resolution) → REJECT
otherwise                                  → continue to custom-format scoring
```

Three consequences that account for most "why won't it grab this" questions:

**1. No resolution in the title means rejection.** Not a downgrade, not a warning — a refusal.

**2. `preferred_resolution` is a ceiling, not a target.** A release above it is rejected regardless of `upgrade_allowed` — that switch only governs whether an already-downloaded file can be *replaced*, not which resolutions are acceptable to grab in the first place. Everything from `min_resolution` through `preferred_resolution` is fair game.

**3. With no profiles configured at all, everything is rejected.** `qualityFor` logs `no quality profile configured, rejecting every release` and returns a zero-value filter that nothing satisfies.

A release that survives the resolution band is then scored against the profile's custom formats — that part, plus source/codec preference, upgrade behaviour and the whole scoring model, is [Quality Profiles and Custom Formats](Quality-Profiles-and-Custom-Formats).

> [!TIP]
> Manual grabs bypass the profile entirely. That's the escape hatch.

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

| Token | Expands to | Example |
| --- | --- | --- |
| `{title}` | Movie title from TMDB | `The Matrix` |
| `{year}` | Release year — omitted if unknown | `1999` |
| `{tmdb_id}` | TMDB ID | `603` |
| `{quality}` | Parsed resolution | `1080p` |
| `{source}` | Parsed source | `BluRay` |
| `{codec}` | Parsed codec | `x265` |
| `{group}` | Release group | `GROUP` |
| `{ext}` | File extension | `mkv` |

**Episodes:**

| Token | Expands to | Example |
| --- | --- | --- |
| `{title}` | **Show** title | `Show` |
| `{year}` | Show year | `2019` |
| `{tvdb_id}` | TVDB ID | `12345` |
| `{season}`, `{episode}` | Numbers | `1`, `2` |
| `{episode_title}` | Episode title | `Pilot` |
| `{quality}` | Parsed resolution | `1080p` |
| `{source}` | Parsed source | `WEB-DL` |
| `{codec}` | Parsed codec | `x264` |
| `{group}` | Release group | `GROUP` |
| `{absolute}` | Absolute episode number, when parsed | `018` |
| `{air_date}` | `YYYY-MM-DD`, when parsed | `2024-03-15` |
| `{ext}` | File extension | `mkv` |

`{tmdb_id}` is movie-only and `{tvdb_id}` series-only (they are the same token in different namespaces); `{absolute}` and `{air_date}` are episode-only. Everything else is available to both.

Write `{tvdb_id}` as `{tvdb-{tvdb_id}}` for the same Plex/Jellyfin ID hint the movie default gets from `{tmdb-{tmdb_id}}`. Streamline reads that marker back when scanning an existing library, so a folder carrying it is matched by ID rather than by title.

**Where `{quality}`, `{source}`, `{codec}` and `{group}` come from.** On import, from the release name. On a **rename**, from what Streamline recorded about the file — not from the name on disk, which the previous rename wrote and which carries only the tokens your template kept. `{quality}` additionally falls back to the probed video width, so a file whose release name never stated a resolution still gets one once [ffmpeg](Configuration-Reference#ffmpeg) has probed it. The name on disk is consulted last, for a file adopted in place that Streamline never parsed at import time.

`{codec}` keeps the release's spelling (`x265`) where there is one and only falls back to the probe's (`hevc`) when nothing claimed a codec — the two are different vocabularies and a template means the first.

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
{title} ({year}) [{group}]      →  The Matrix (1999)      # when unparsed
{title} - {group}               →  The Matrix -           # when unparsed
```

An empty token wrapped in `[...]` or `(...)` takes the brackets **and the space before them** with it (a pair holding several tokens goes when all of them are empty) — punctuation in a template is there to delimit a value, and with no value there is nothing to delimit. A bare token has no pair to remove, so a separator around one survives; keep an optional token in brackets if you want the segment to disappear cleanly.

> [!NOTE]
> The movie default contains `{tmdb-{tmdb_id}}` — nested braces. The parser matches `{tmdb_id}` inside it, so this renders as `{tmdb-603}` rather than `tmdb-603`: the literal outer braces stay in the directory name. Plex and Jellyfin both read `{tmdb-603}` as an ID hint, so this is intentional and works — but if you write your own template, know that the braces are literal text, not template syntax.

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

> [!WARNING]
> Existing folders keep their old spelling until you re-run a rename — changing a template or fixing sanitisation doesn't touch files already on disk. `POST /movies/{id}/rename` moves the file and removes the directory it emptied.

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

## Music and book naming

Music and books have templates of their own, with **capitalised** tokens — a token is looked up exactly as written, so `{artist}` in a music template renders empty. Like the movie and series templates they are edited on Settings → Library and apply to the next import or rename.

```yaml
library:
  music_naming: '{Artist}/{Album} ({Year})/{Disc}{Track:02} - {Title} [{BitDepth}-{SampleRate}].{ext}'
  ebook_naming: '{Author}/{Title} ({Year})'
  audiobook_naming: '{Author}/{Title} ({Year})'
  book_series_naming: '{Author}/{Series}/{Series} - Vol. {Volume:02}'
```

**Music** (`library.music_naming`, under `library.music_path`):

| Token | Expands to | Example |
| --- | --- | --- |
| `{Artist}` | The album's artist (for a release shared by several artists, the one that owns it) | `Daft Punk` |
| `{Album}` | Album title | `Discovery` |
| `{Year}` | Release year — omitted if unknown | `2001` |
| `{Disc}` | Disc number followed by `-`, **only on a multi-disc album**; absent otherwise, so a single-disc file carries no prefix | `2-` |
| `{Track}` | Track number; `{Track:02}` pads it | `03` |
| `{Title}` | Track title | `Digital Love` |
| `{ext}` | File extension | `flac` |
| `{Quality}` | The file's quality tier: `hires`, `lossless`, `high`, `standard` or `low` — omitted when it could not be established | `lossless` |
| `{BitDepth}` | Bit depth of a lossless file — omitted for a lossy file and for one that was never measured | `24` |
| `{SampleRate}` | Sample rate of a lossless file in kHz — omitted like `{BitDepth}` | `96`, `44.1` |

`{BitDepth}` and `{SampleRate}` are measured with ffprobe when a file is imported or adopted, so they need [`ffmpeg.enabled`](Configuration-Reference#ffmpeg). Files already in the library are measured in the background by the `media-probe` job, 25 per run; until a file is measured its bracket is simply left out, and **Rename** on the artist adds it afterwards. The default template ends `[{BitDepth}-{SampleRate}]`: a bracket pair whose tokens are all empty disappears whole, so an MP3 is named `03 - Digital Love.mp3` and a FLAC `03 - Digital Love [16-44.1].flac`.

These three are per file. In a folder segment (`{Album} [{Quality}]`) an album whose tracks differ would be split across two folders, so keep them in the file name.

**Books** (`library.ebook_naming` and `library.audiobook_naming`, under `library.ebook_path` and `library.audiobook_path`; an ebook volume of a series uses `library.book_series_naming` instead):

| Token | Expands to | Example |
| --- | --- | --- |
| `{Author}` | The book's display author: its authors, else writers, else artists, joined with ` & ` | `Ursula K. Le Guin` |
| `{Title}` | The title of the slot's edition, so a French edition is filed under its French title | `Le Sorcier de Terremer` |
| `{Year}` | Release year — omitted if unknown | `1968` |
| `{Language}` | The slot edition's language | `fr` |
| `{Series}` | Series name, for a volume | `Terremer` |
| `{Volume}` | Position in the series; `{Volume:02}` pads a whole number and leaves a fractional one (`1.5`) as it is | `01` |

The book templates name the file **without its extension**: an ebook's own extension is appended, and an audiobook template names a **folder**, into which the release's files are placed under their original names. Not `library.series_naming`, which is the TV template.

Sanitisation and the empty-token rule above apply to both.

---

## Renaming existing files

Changing a template does **not** rewrite files already on disk. Apply it with the per-title **Rename files…** action, or across a selection: pick titles on the Movies or Series page, then **⋯ → Rename files…** in the bulk bar. The bulk action skips titles with nothing on disk and applies without a preview — use the per-title action when you want to see the exact moves first. Or over the API:

```bash
curl -X POST -H "X-API-Key: $KEY" \
  https://streamline.example.com/api/v1/movies/42/rename

curl -X POST -H "X-API-Key: $KEY" \
  https://streamline.example.com/api/v1/series/7/rename
```

This is also how you normalise files adopted in place by an [import scan](Importing-an-Existing-Library) — adoption records paths as-is; renaming is a deliberate, separate step.

Music and books rename the same way, from the artist, book or series page (**Rename files**) or `POST /music/artists/{id}/rename`, `/books/{id}/rename` and `/books/series/{id}/rename` (`?preview=true` returns the plan without applying it). A music rename never touches the files' tags, and a book rename never moves onto a file that already exists.

If you're re-rooting a whole library rather than renaming within it, use [path migration](GitOps-and-Kubernetes#library-path-migration) instead.
