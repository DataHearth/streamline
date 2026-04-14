# Streamline Design System — MASTER

**Codename:** Cinematic Ops
**Scope:** global source of truth for all web UI. Page-specific overrides live under `design-system/pages/<page>.md`. When building a page, first check for a page file; if absent, use this document exclusively.

**Product surfaces covered (now + planned):**
Movies, TV, Music, Books, Transcoding jobs, Video/Audio player, Requests, Activity/Queue, Settings.

---

## 1. Identity

Hybrid: cinematic dark (content: posters, backdrops, playback) + real-time monitoring (ops: queue, grabs, RSS, quality, transcodes).

Tokens drive both. No per-domain re-skin. Only card aspect + column content change across domains.

Principles:
1. Deep layered dark, never pure `#000` (except video letterbox).
2. Indigo accent. Status colors are a separate palette; accent is never used to convey ops status.
3. Mono for any number/ID/size/timestamp/codec — treat data as data.
4. Hairline borders, not heavy strokes. Elevation via background tint, not shadow spam.
5. Every interactive element: visible focus, ≥44px touch target on mobile, reduced-motion aware.
6. Mobile is first-class. Bottom nav primary, sidebar desktop affordance.

---

## 2. Color tokens

### Surfaces
```
--bg-deep:       #0B0B10    /* <html>/<body>, page base */
--bg-base:       #111118    /* main content pane */
--bg-elevated:   #15151F    /* cards, nav rail, sticky bars */
--bg-overlay:    rgba(11,11,16,0.70)    /* modal backdrop */
--bg-player:     #000000    /* video letterbox only */
--bg-card:       #1C1C28    /* card body inside elevated panels */
--bg-hover:      #232333    /* hovered row inside cards */
--bg-0..--bg-4   = bg-deep / bg-base / bg-elevated / bg-card / bg-hover (numeric aliases used in JSX bundle)
--surface:       rgba(255,255,255,0.04) /* hover/selected */
--surface-2:     rgba(255,255,255,0.06) /* pressed */
--border:        rgba(255,255,255,0.08) /* hairline */
--border-strong: rgba(255,255,255,0.14) /* emphasized */
```

### Accent (brand + primary action only)
```
--accent:        #7C8CFF
--accent-hover:  #8B9AFF
--accent-pressed:#6573E6
--accent-glow:   rgba(124,140,255,0.25)
--accent-ring:   rgba(124,140,255,0.40)
--accent-soft:   rgba(124,140,255,0.14)
--accent-line:   rgba(124,140,255,0.40)
--accent-text:   #B8C2FF
```

### Text
```
--fg:            #F5F5F7    /* primary */
--fg-muted:      #C8C8D2    /* secondary (passes 4.5:1 on bg-elevated) */
--fg-subtle:     #8A8A99    /* tertiary, axis labels, helper */
--fg-faint:      #5A5A68    /* faint, uppercase eyebrow / sub-section */
--fg-on-accent:  #0B0B10
```

Never use `--fg-subtle` for body copy. Labels/axis/helper only. `--fg-faint` is reserved for uppercase eyebrows and sub-section dividers.

### Status (ops semantics — used on pills, progress, row highlights)
```
--status-downloading: #3B82F6   /* blue */
--status-grabbing:    #A78BFA   /* violet */
--status-available:   #22C55E   /* green */
--status-wanted:      #F59E0B   /* amber */
--status-missing:     #64748B   /* slate */
--status-failed:      #EF4444   /* red */
--status-paused:      #71717A   /* gray */
--status-warning:     #F59E0B   /* amber reuse */
```

Pill formula: `bg: var(--status-X) / 15%, text: var(--status-X), border: var(--status-X) / 25%`.

Generic aliases (mapped onto the ops palette so generic UI affordances pick the right hue):
```
--ok   = --status-available    --ok-soft   = ok @ 12%
--warn = --status-wanted       --warn-soft = warn @ 12%
--err  = --status-failed       --err-soft  = err @ 12%
--info = --status-downloading  --info-soft = info @ 12%
```

Future aliases (transcoding):
```
--status-queued   = --status-wanted
--status-encoding = --status-downloading
--status-complete = --status-available
```

### Color-use rules
- Accent ≠ status. Primary CTA indigo; success pill green. Don't mix.
- Functional color always paired with icon + text. Never color-only.
- Dark mode variants are this palette. Light mode deferred (not v1).

---

## 3. Typography

```
--font-sans: "Inter", ui-sans-serif, system-ui, sans-serif
--font-mono: "JetBrains Mono", ui-monospace, "SF Mono", Menlo, monospace
```

Self-hosted fonts. Bundle WOFF2 under `web/static/fonts/`. `font-display: swap`. Preload `Inter 400 + 600` and `JetBrains Mono 400` only.

### Scale (rem, 16px base)
```
xs   0.75   (12)  pill, badge, overline
sm   0.875  (14)  body-secondary, label
base 1.00   (16)  body default (inputs on mobile use this to avoid iOS zoom)
lg   1.125  (18)  card title
xl   1.25   (20)  section heading (h2)
2xl  1.50   (24)  page heading (h1)
3xl  1.875  (30)  hero stat
4xl  2.25   (36)  detail-page title
```

### Weights
```
400  body
500  label, nav item
600  headings, button, active nav
700  hero stat, marketing emphasis (sparingly)
```

### Line-height
```
body   1.5
heading 1.2
data (mono tables) 1.4
```

### Mono reserved for
File sizes, bitrates, resolutions (`1080p`), codecs (`x265 10-bit`), percentages, counts in dense tables, hashes, release names, timestamps, durations (`01:47:32`), ETAs, speeds (`12.4 MB/s`), TMDB/IMDb IDs, release tags.

**Rule:** if the value will sort, compare, or be scanned column-wise → mono. If it's prose → sans.

Sans tabular figures (`font-variant-numeric: tabular-nums`) on stat cards / non-mono numeric displays to prevent reflow.

---

## 4. Layout & spacing

### Spacing scale (4px base)
```
0   0
1   4
2   8
3   12
4   16
5   20
6   24
8   32
10  40
12  48
16  64
```

Use multiples of 4. Component internal padding 8/12/16. Section gaps 24/32/48. Page padding 16 mobile → 24 md → 32 lg.

### Radii
```
--radius-sm:  6px    pills, chips, inputs
--radius-md:  10px   buttons, small cards
--radius-lg:  16px   cards, modals, nav items, hero tiles
--radius-xl:  20px   page-hero panels
--radius-full: 9999  icon buttons, round badges
--radius-player-controls: 12px
```

### Shadows (scale, not freestyle)
```
--shadow-1: 0 1px 2px rgb(0 0 0 / 0.4)                                 /* nav rail */
--shadow-2: 0 4px 12px rgb(0 0 0 / 0.35)                                /* cards hover */
--shadow-3: 0 16px 32px rgb(0 0 0 / 0.45)                               /* modals, sheets */
--shadow-4: 0 24px 64px rgb(0 0 0 / 0.55)                               /* popovers, dropdowns */
--shadow-glow: 0 0 24px var(--accent-glow)                              /* primary CTA hover, active pill */
```

### Z-index scale
```
--z-base:      0
--z-sticky:    10
--z-nav:       20
--z-dropdown:  40
--z-modal:     100
--z-toast:     200
--z-tooltip:   300
```

### Breakpoints
```
sm   375   smallest phone tested
md   768   tablet / landscape phone
lg   1024  desktop minimum
xl   1280  default desktop
2xl  1536  wide
3xl  1600  content max-width cap (library grid cap)
```

Design mobile-first. Every layout verified at 375px landscape + 812px portrait.

### Shell
```
≥ lg   sidebar rail 256px (full labels) + content pane, max 1600px
md     collapsed icon rail 72px + tooltips on hover
< md   top bar (logo + search + user) + bottom tab bar (4 items) + content
```

Bottom tabs: Library · Requests · Activity · Settings (max 5 total; Library opens a segmented sub-view selector for Movies / TV / Music / Books).

Content padding: `p-4 md:p-6 lg:p-8`.
Sticky headers blur background with `backdrop-blur-md saturate-150 bg-[--bg-base]/70`.

---

## 5. Motion

```
--dur-fast:   150ms   hover/press state
--dur-base:   200ms   state transitions (expand/collapse, toggle)
--dur-slow:   300ms   page-level / sheet / modal enter
--dur-exit:   200ms   ~70% of enter; exits snappy
--ease:       cubic-bezier(0.16, 1, 0.3, 1)    /* expo out, "Linear"-feel */
--ease-in:    cubic-bezier(0.4, 0, 1, 1)
--ease-spring: spring physics where available
```

Rules:
- Only animate `transform` + `opacity` + `filter`. Never `width/height/top/left`.
- Exits faster than enters.
- Stagger list reveals 30–50ms.
- Every animation interruptible.
- `@media (prefers-reduced-motion: reduce)` disables blobs/glow/parallax/stagger; keeps instant opacity + color for feedback.
- Pressed scale 0.97 on tap cards; 1.00 on release.

---

## 6. Effects

- **Blur surfaces** (top bar, modals, filter sticky bar): `backdrop-filter: blur(16px) saturate(150%); background: rgb(10 10 15 / 0.70);`
- **Accent glow** behind primary button hover, live "downloading" pill, now-playing indicator. Subtle — opacity 0.25.
- **Hairline borders** everywhere. `1px solid var(--border)`. Never 2px default.
- **No skeuomorphism.** No gradients except backdrop darkening on detail hero.
- **Detail-hero backdrop gradient:** `linear-gradient(to top, var(--bg-deep) 0%, transparent 60%)` over TMDB backdrop image.

---

## 7. Component vocabulary

### Buttons
| Tier | Background | Text | Border | Use |
|---|---|---|---|---|
| **Primary** | `--accent` | `--fg-on-accent` | — | Save, Sign in, Add, primary CTA |
| **Secondary** | `--surface` | `--fg` | `--border-strong` | Cancel, Back, secondary action |
| **Ghost** | transparent | `--fg-muted` | — | Nav items, row actions |
| **Destructive** | transparent | `--status-failed` | `--status-failed/40` | Delete/unmonitor (confirm step flips to solid red) |
| **Icon-only** | transparent | `--fg-muted` | — | Requires `aria-label`; 40×40 tap area min |

Sizes:
- `sm` h-8 px-3 text-sm (desktop secondary, dense rows)
- `md` h-10 px-4 text-sm (default desktop)
- `lg` h-11 px-5 text-base (mobile, primary CTA)

All buttons: `hover:` opacity/bg shift + `active:` scale 0.97 + `focus-visible:outline-2 outline-[--accent] outline-offset-2` + `disabled:opacity-50 disabled:pointer-events-none`.

### Inputs
- Height: 44 mobile / 40 desktop
- Font-size: `text-base md:text-sm`
- Bg: `--bg-elevated`
- Border: `--border` → focus `--accent` + `ring-2 ring-[--accent-ring]`
- Labels: above input, `text-sm text-[--fg-muted]`, not placeholder-only
- Helper text: below, `text-xs text-[--fg-subtle]`
- Error: border `--status-failed`, message below in `--status-failed`
- Search input variant: leading icon + trailing clear button (shown when value non-empty)

### Cards
```
Base:       bg-[--bg-elevated] border border-[--border] rounded-lg
Hover:      hover:border-[--border-strong] hover:shadow-2 transition-[border,box-shadow] duration-[--dur-fast]
Interactive:above + cursor-pointer + active:scale-[0.99]
```

### Media card (Movie/Book poster, Album square)
- Aspect variants: `--card-aspect-poster: 2/3`, `--card-aspect-album: 1/1`, `--card-aspect-book: 2/3`, `--card-aspect-backdrop: 16/9`
- Status pill absolute top-right
- Title (sans, `text-sm font-medium truncate`) + metadata (`text-xs mono text-[--fg-muted]`) below
- `group-hover` reveals quick-action bar (monitor toggle, search now, menu)
- Lazy-load poster with `aspect-ratio` reserved to prevent CLS
- `<img width height>` or `aspect-ratio` CSS always declared

### Status pill
```html
<span class="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium
             bg-[--status-X]/15 text-[--status-X] border border-[--status-X]/25">
  <span class="h-1.5 w-1.5 rounded-full bg-[--status-X]"></span>
  Downloading
</span>
```
Label Title-case, never raw enum value. Live/animated states add `animate-pulse` to the dot only.

### Navigation
- **Sidebar rail** (≥md): logo + grouped sections with uppercase tracking labels; active item has left accent bar + filled-variant icon + `bg-[--surface]`
- **Bottom tabs** (<md): 4–5 items, icon + label, active tint accent + indicator line on top edge, `min-h-14`
- **Sub-nav** (Settings, Library categories): pill tabs, accent underline on active
- Breadcrumbs on ≥3-level hierarchies, sans, `text-sm text-[--fg-muted]`
- Active state announced via `aria-current="page"`

### Dialog / Modal
Global CSS reset:
```css
dialog {
  margin: auto;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  max-width: min(92vw, 32rem);
  max-height: 85dvh;
}
dialog::backdrop {
  background: rgb(2 2 3 / 0.70);
  backdrop-filter: blur(8px);
}
```
Surface: `rounded-xl bg-[--bg-elevated] border border-[--border] shadow-3`.
Header: flex, title `text-lg font-semibold` + close icon-button (40×40, `aria-label="Close"`).
`aria-labelledby` wired to title id.
Backdrop click closes (explicit handler); Esc native.
Body scrollable if exceeds max-height.

### Sheet (mobile variant of modal)
Slides from bottom `<md`. Drag handle 4×32 rounded at top. Swipe-down to dismiss.

### Toast
Bottom-right stack desktop, bottom-center mobile. `bg-[--bg-elevated]` + 3px left stripe by status color. `role="status"` + `aria-live="polite"`. 4s auto-dismiss, pause on hover. Max 3 visible; queue rest.

### Tables (activity/queue/history)
- Row height 48 (comfortable) / 40 (compact toggle)
- Sticky header with filter controls
- Mono columns for size/speed/eta/duration
- Progress bar inline (see below)
- Expand caret → full log / source detail panel
- Row hover `bg-[--surface]`; active grab pulses `bg-[--status-downloading]/5`
- Sort via `aria-sort`

### Progress bar
```
Track:  --progress-track  (rgba(255,255,255,0.08))
Fill:   --progress-fill   (var(--accent)) OR status color if ops-tied
Buffer: --progress-buffer (rgba(255,255,255,0.16))
```
Heights: `h-1` inline row / `h-2` detail view / `h-1.5` player scrubber.
Determinate: fill width %. Indeterminate: shimmer animation (skip on reduced-motion).

### Skeleton
`bg-[--surface]` with subtle shimmer gradient; respects reduced-motion (static tint only).

### Tooltip / Popover
Floating on `--bg-elevated` + `shadow-4`. Tooltip = `role="tooltip"`, pointer-only, delayed 500ms. Popover keyboard-reachable.

### Chips / Filter tags
Pill `h-7 px-3 text-xs`, removable chips get trailing `×` button.

### Empty state
Centered, icon 48×48 `--fg-subtle`, title `text-base font-medium --fg`, description `text-sm --fg-muted`, primary CTA below. Never a bare "No data" string.

### Avatar
Square rounded (`--radius-md`) 32/40/48 variants. Initials in sans `font-medium`, bg generated from email hash mapped to a fixed palette subset (no random colors).

---

## 8. Iconography

- **Lucide only.** Stroke 1.5 default, 2 for emphasis. Size tokens: `icon-sm 16`, `icon-md 20`, `icon-lg 24`, `icon-xl 32`.
- Filled variant (where available) reserved for active nav item / selected state.
- **No emoji ever.**
- Every icon-only interactive element gets `aria-label`.
- Decorative icons get `aria-hidden="true"`.

---

## 9. Accessibility (non-negotiable)

1. All text ≥ 4.5:1 contrast on its background. Large text (≥24px or 18.66px bold) ≥ 3:1.
2. Focus-visible outline always (global rule, see §10 implementation).
3. Touch targets ≥44×44pt on mobile.
4. Keyboard reachable: every interactive element, logical tab order, skip-to-main link.
5. Screen reader labels on icon-only controls.
6. Forms: visible label, helper text, inline error below field, first-invalid-field focus on submit error, `aria-live` error region.
7. Color never sole conveyor (always + icon or text).
8. Respect `prefers-reduced-motion`.
9. Dialog: `aria-labelledby` + `aria-modal="true"`, ESC/close returns focus to trigger.
10. Dynamic announcements via `aria-live` regions (toasts, progress completion, route change).

---

## 10. Implementation notes

### CSS variables live at `:root` in `web/static/css/input.css`:
```css
@import "tailwindcss";

:root {
  /* surfaces */ --bg-deep: #020203; /* ... */
}

/* Dialog global reset (fixes Tailwind Preflight margin wipe) */
dialog { margin: auto; padding: 0; border: 0; background: transparent; }
dialog::backdrop { background: rgb(2 2 3 / 0.7); backdrop-filter: blur(8px); }

/* Global focus-visible */
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 4px; }

/* Tabular numerals on stat displays */
.tabular { font-variant-numeric: tabular-nums; }

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
  }
}

@font-face { font-family: "Fira Sans"; /* ... local woff2 */ }
@font-face { font-family: "JetBrains Mono"; /* ... local woff2 */ }
```

### Tailwind v4 theme extension
Map all tokens to `@theme` block so `bg-accent`, `text-fg-muted`, `border-border` etc. work everywhere without arbitrary values.

### HTML `<html>` attrs
```
<html lang="en" class="h-full" style="color-scheme: dark">
```

### Favicon
Add `<link rel="icon" href="/static/favicon.svg" type="image/svg+xml">` in base layout.

### Skip link
First focusable element inside `<body>`: `<a href="#main" class="sr-only focus:not-sr-only ...">Skip to main content</a>`.

---

## 11. Page patterns (high-level)

| Page | Pattern | Card/aspect | Key components |
|---|---|---|---|
| Auth (login/register/invite) | Centered card + radial accent glow behind | — | form, auth-error partial, OIDC buttons |
| Dashboard | 4-stat hero row + activity feed + upcoming | — | stat card, sparkline, activity row |
| Library (Movies/TV/Books) | Sticky filter bar + poster grid | 2:3 | media card, status pill, filter chips, empty state |
| Library (Music) | Sticky filter bar + album grid + tracks table | 1:1 | album card, track row, play button |
| Detail (any media) | Backdrop hero + poster + tabs (Overview/Files/History/Queue/Manual search) | backdrop 16:9 + 2:3 poster | tab nav, metadata list, action bar |
| Activity (Queue + History) | Live table, row expansion, progress bars | — | table, progress bar, status pill, log panel |
| Transcoding jobs | Same as Activity with codec columns | — | table, progress, codec/bitrate mono columns |
| Player | Full-bleed video + floating controls overlay | `--bg-player` | transport controls, scrubber with chapters, quality menu, subs |
| Requests | Card grid or list + approve/deny inline | — | request card, status pill, actor strip |
| Settings | Left sub-nav + card-per-section | — | section card, inline edit, danger zone |

Each gets its own `design-system/pages/<slug>.md` only when deviating from above.

---

## 12. Mandatory pre-delivery checklist

Copy-paste this block into every PR description that touches UI.

**Visual**
- [ ] No emojis as icons; Lucide SVG only
- [ ] Consistent icon stroke (1.5 or 2 within same hierarchy)
- [ ] Cards use `bg-[--bg-elevated] border border-[--border]`
- [ ] Accent indigo used only for primary action + brand, never status
- [ ] Status pills use status palette with `/15` bg + `/25` border

**Interaction**
- [ ] Every button has `:focus-visible` ring
- [ ] Touch targets ≥44×44 on mobile (`h-11` buttons, `h-11` inputs)
- [ ] Input font-size `text-base md:text-sm` (no iOS zoom)
- [ ] Form errors via inline partial + field border; first-invalid-field focused on submit error
- [ ] `hx-indicator` or `aria-busy` on async actions
- [ ] Disabled states `opacity-50 pointer-events-none`

**Contrast**
- [ ] Primary text `--fg` (≥7:1)
- [ ] Secondary `--fg-muted` (≥4.5:1)
- [ ] `--fg-subtle` only for labels/helpers (≤4.5:1 allowed, not body)

**Layout**
- [ ] Works at 375/768/1024/1440
- [ ] Sidebar hidden `<md`; bottom nav rendered instead
- [ ] `main` has `p-4 md:p-6 lg:p-8`; content capped `max-w-[1600px]`
- [ ] No horizontal scroll anywhere
- [ ] Safe-area insets respected (env() on bottom nav)

**Motion**
- [ ] Only `transform`/`opacity`/`filter` animated
- [ ] `prefers-reduced-motion` kills blobs/glow/parallax/stagger
- [ ] Exits ~70% of enter duration

**A11y**
- [ ] `aria-label` on every icon-only interactive
- [ ] `aria-labelledby` on dialogs; focus returns on close
- [ ] Skip-to-main link present
- [ ] Keyboard full path: tab through, Esc closes, Enter activates
- [ ] Status announced via `aria-live` where relevant

**Perf**
- [ ] Images have `width`/`height` or `aspect-ratio` (no CLS)
- [ ] `loading="lazy"` on below-fold images
- [ ] Fonts `font-display: swap`
- [ ] Virtualize lists ≥50 items

---

## 13. Roll-out order

Lock sequence. Each stage ships independently, each verifies at 375/768/1440.

1. **Tokens + base layer** — `input.css` vars, `@theme` Tailwind config, fonts bundled, favicon, global focus/dialog/reduced-motion rules
2. **Responsive shell** — base layout responsive, sidebar (≥md) + bottom nav (<md), skip link, sticky blurred top bar
3. **Auth pages** — login + register + invite accept with token partials, button focus, submit indicator, fixed register error container
4. **Dashboard** — stat row with sparklines + tabular-nums, activity feed component, upcoming strip
5. **Movies list + detail** — poster grid, filter bar, media card, detail hero, tabs, manual search, fixed TMDB modal (CR1 + CR2 resolved)
6. **Activity page** — queue/history table, progress bar, expandable row
7. **Settings** — sub-nav + card sections
8. **Requests**
9. **Music / Books / TV** — reuse library pattern, add aspect variants
10. **Transcoding jobs** — extend activity with codec columns
11. **Player** — floating control overlay, scrubber, quality/subs/PiP/Cast

---

## 14. Non-goals (v1)

- Light mode (deferred; color-scheme locked dark)
- Serif editorial accent (deferred; all sans for v1)
- Custom illustrations (empty states use Lucide for now)
- Theming API (single theme shipped)
- Multi-language beyond `lang="en"` hook

---

## 15. Reference artifacts

- Per-page overrides: `design-system/pages/<page>.md` (only when deviating)
- Context retrieval prompt for future sessions:
  > I am building the [Page Name] page. Read `design-system/MASTER.md`. Also check if `design-system/pages/[page-name].md` exists. If the page file exists, prioritize its rules. If not, use Master exclusively.

---

## 16. Drawer / Sheet pattern

Drawers are the slide-in panel for add/edit flows. Use `shared/drawer.html` for every entity edit.

Caller signature mirrors `shared/modal.html`:
- `ID` (required) — DOM id, used by close-OOB swaps and auto-open hook
- `Title` — header text
- `BodyTmpl` (required) — name of `{{define ...}}` for body
- `FooterTmpl` — name of `{{define ...}}` for footer (Cancel / secondary / Save row)
- `Data` — payload
- `Size` — `sm | md (default) | lg` → 360 / 480 / 640 px width on `≥md`, capped at 90vw
- `Attrs` — pass-through (htmx, data, aria); `data-auto-open=""` triggers `.showModal()` on htmx swap

Native `<dialog>` element. `≥md` anchors right (`inset: 0 0 0 auto`); `<md` becomes a bottom sheet (`inset: auto 0 0 0`). Animated close: caller sets `data-closing` (or `app.js` does on Esc / backdrop / explicit close) → wait `--dur-exit` → `dialog.close()`. Reduced-motion users skip the wait.

Use a single mount point `#drawer-host` in `layouts/base.html`. Form submits inside drawers return updated row HTML for the page table (OOB swap) plus an OOB close fragment.

---

## 17. Brand logos

Vendor / connector logos for indexer / download-client / media-server / SSO type pickers and list rows. Single source: `shared/brand_logo.html` — one router `{{define "shared/brand_logo"}}` switching on `.Name`, with one `{{define}}` per glyph.

Names: `plex`, `jellyfin`, `emby`, `qbittorrent`, `transmission`, `deluge`, `sabnzbd`, `nzbget`, `prowlarr`, `jackett`, `nzbhydra2`, `authentik`, `keycloak`, `authelia`, `google`, `okta`, `azure`.

Caller signature: `Name` (required), `Size` (16/20/24 — default 20), `Aria` (`label` for standalone, `hidden` for paired-with-text — caller decides).

SVG glyphs lifted from the design bundle (`docs/plans/2026-05-07-streamline-recommended-bundle/project/brand-logos.jsx`). All glyphs use `fill="currentColor"` so they inherit the surrounding text color.
