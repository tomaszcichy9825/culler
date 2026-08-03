# culler — design specification

Implementation contract extracted from `docs/design/culler-redesign.html` (turns 1–10) and
`docs/design/culler-map.html` (turn 4). Every value below is verbatim from those files. Where a
value could not be recovered, this document says so rather than inventing one.

---

## 0. Provenance, and one thing you must know first

**`culler-redesign.html` is truncated.** The file is exactly 262 144 bytes (256 KiB) and ends
mid-statement inside the `renderVals()` method of the data script that begins at line 2935. All
markup (lines 1–2934) is intact and complete; the *data* that fills the `sc-for` templates is not.

Recovered from the surviving script: `STEMS`, `PLAN`, `KINDS`, the palette constant `C`, the
`METRIC`/`CTL`/`GRP`/`ASIDE` factories, `NAV_ITEMS`, and the complete `SETTINGS_PAGES` array
(screens 10c–10f, every row, description and control label).

Lost to the truncation — structure is known from the markup, contents are not:

| Screen | What was lost |
|---|---|
| 7b token & type sheet | the token *names*, the dark/light value pairs, `typeSpecs`, `themeRules` |
| 8a / 8b / 8c branding | `markRules` construction rules, `iconAssets` list, `brandColors` swatches, `brandNotes` |
| 9a screen index | the 18-row table of screen → mode → entry key → status |
| 9b shell & interaction model | the 7-section shell spec |
| 9c data model | the 5 entity shapes and their fields |
| 9d states we owe | the 10 undesigned-but-specified states |
| 3e build notes | the 6-section implementer hand-off |
| everywhere | list contents: keymap rows, palette rows, destinations, EXIF fields, sessions, facets |

Consequence: sections 1 (tokens) and 5 (branding) below are reconstructed from the **literal inline
styles in the markup**, which is a complete and reliable source for values. The token *names* are
this document's proposal, because the authored names did not survive. If the full HTML can be
re-exported from Claude Design, re-check §1 and §5 against sheet 7b and 8c before treating the names
as settled.

`culler-map.html` (701 lines) is complete.

### The design defines a light theme

The brief for this document assumed the design was dark-only. It is not. Screen **7a** is the cull
screen drawn fully in light, with literal values throughout, and the appearance settings page (10d)
offers `Colour scheme: system (default) · dark · light`. The 10d aside states the governing rule
verbatim:

> Themes only swap token values. No layout, size, or weight changes between light and dark, so a
> screenshot of one maps onto the other.

The light values in §1 are therefore extracted, not invented. What *is* missing is a light value for
tokens whose only appearance is on a dark-only screen (compare, storage, palettes, settings,
map) — those are marked `— derive` and are a genuine open question, listed in §8.

---

## 1. Design tokens

### 1.1 Two accents, not one

The design uses two distinct accent hues and they are not interchangeable:

- **`#56b6c2` (cyan) — brand.** The mark, the wordmark, the "keys" overlay title, the "card
  detected" eyebrow, the `indexed` status word, section titles on spec cards. Never used for focus.
- **`#61afef` (blue) — UI accent.** Focus rings, selection, the active mode in the mode bar, the
  active layout segment, active nav rows, carets, primary blue buttons.

Settings 10d confirms the split: `Accent — used for focus, selection, and the active mode` with
options `blue (default) · cyan · purple`, i.e. blue is the shipped UI accent and cyan is available as
a user choice as well as being the fixed brand colour.

### 1.2 Custom-property block

Paste-ready. Names are proposed (see §0); values are verbatim.

```css
:root {
  /* ---- type ---- */
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
  --font-sans: "Public Sans", system-ui, sans-serif;

  /* ---- surfaces (dark) ---- */
  --bg-app:        #0e1013;  /* deepest: image stage, loupe backdrop, window body behind panes */
  --bg-window:     #14161a;  /* window frame + centre pane background */
  --bg-chrome:     #171a1f;  /* title bar, status bar, side panes, dialogs */
  --bg-raised:     #1a1e24;  /* dialog footers, spec-card label column, focused sidebar */
  --bg-tile:       #1a1d23;  /* grid tile body */
  --bg-field:      #1d2127;  /* inputs, segmented-control shell, secondary buttons, chips */
  --bg-field-alt:  #1c2027;  /* inset value boxes (shift-time, naming preview, GPX row) */
  --bg-kbd:        #262b34;  /* keycap in the title-bar search hint */
  --bg-track:      #20242b;  /* progress + meter track */
  --bg-row-active: #1e242c;  /* focused table/tree row */
  --bg-row-zebra:  #16191e;  /* odd table row */

  /* ---- borders (dark) ---- */
  --border:        #23272f;  /* the default 1px rule between every region */
  --border-hair:   #1b1e24;  /* row separators inside lists and tables */
  --border-faint:  #1f232a;  /* section underline inside palettes and shoot headers */
  --border-strong: #2b313b;  /* controls, chips, buttons, badges */
  --border-window: #262a32;  /* the window's own outer border */
  --border-dialog: #2f3742;  /* dialog and map-pin border */

  /* ---- text (dark), five tiers ---- */
  --text-hi:    #eef1f6;  /* selected/active row, dialog subject, wordmark */
  --text:       #d7dae0;  /* default body */
  --text-2:     #c3c8d1;  /* table cells, tile filename, keycap glyph */
  --text-3:     #a9b0bd;  /* prose paragraphs inside spec cards */
  --text-muted: #8b919e;  /* labels, secondary values — the most-used colour in the doc */
  --text-dim:   #5a606d;  /* hints, paths, inactive status text */
  --text-faint: #4d535f;  /* uppercase section labels */
  --text-ghost: #3f4550;  /* placeholder prose, empty-state text, dead legend swatch */
  --text-dead:  #3a404b;  /* path separators, absent-file glyph */

  /* ---- accents ---- */
  --brand:      #56b6c2;  /* mark, wordmark, brand titles */
  --brand-hi:   #7fd1db;  /* link hover only */
  --brand-dark: #0e8792;  /* the mark's solid square on light backgrounds */
  --accent:     #61afef;  /* focus, selection, active mode/layout/nav */
  --on-accent:  #0e1013;  /* text on any filled accent or status colour */

  /* ---- verdict + status ---- */
  --keep:   #98c379;
  --cut:    #e06c75;
  --amber:  #d19a66;  /* warnings: read-only card, indexing, without-GPS, geotag mode */
  --gold:   #e5c07b;  /* pending counts, dirty-field marks, star ratings, mixed values */
  --violet: #c678dd;  /* search field keys, smart collections, GPX tracks */
  --neutral-bar: #3d4650;  /* losing metric bar in compare, low-density heat bar */

  /* ---- accent washes (the 0.10–0.22 alpha family) ---- */
  --accent-wash-10: rgba(97,175,239,0.10);  /* focused editor pane, recording box */
  --accent-wash-14: rgba(97,175,239,0.14);  /* focused-pane header strip */
  --accent-wash-16: rgba(97,175,239,0.16);  /* mode chip in status bar, active nav row */
  --accent-wash-18: rgba(97,175,239,0.18);  /* MOVE badge, "14 frames selected" pill */
  --keep-wash-10:   rgba(152,195,121,0.10);
  --keep-wash-12:   rgba(152,195,121,0.12);
  --keep-wash-16:   rgba(152,195,121,0.16);
  --keep-wash-20:   rgba(152,195,121,0.20);  /* R/J badge, file kept */
  --cut-wash-09:    rgba(224,108,117,0.09);
  --cut-wash-14:    rgba(224,108,117,0.14);
  --cut-wash-16:    rgba(224,108,117,0.16);
  --cut-wash-22:    rgba(224,108,117,0.22);  /* R/J badge, file cut */
  --amber-wash-14:  rgba(209,154,102,0.14);
  --amber-wash-16:  rgba(209,154,102,0.16);
  --amber-wash-18:  rgba(209,154,102,0.18);
  --gold-wash-07:   rgba(229,192,123,0.07);  /* dirty form row */
  --gold-wash-16:   rgba(229,192,123,0.16);
  --violet-wash-16: rgba(198,120,221,0.16);
  --absent-wash:    rgba(120,130,145,0.14);  /* R/J badge, file not present */

  /* ---- focus + selection ---- */
  --focus-ring:      0 0 0 2px rgba(97,175,239,0.40);   /* focused tile / filmstrip frame */
  --focus-ring-soft: 0 0 0 2px rgba(97,175,239,0.35);   /* focused map pin */
  --focus-inset:     inset 0 0 0 1px rgba(97,175,239,0.30);  /* focused pane */
  --focus-inset-2:   inset 0 0 0 1px rgba(97,175,239,0.35);  /* focused sidebar */
  --focus-edge:      inset 2px 0 0 #61afef;             /* focused table row */
  --border-focus:    #61afef;
  --border-selected: #2d4360;   /* selected-but-not-focused tile */
  --border-pane-focus: #2f5a7a; /* focused pane's outer border */
  --text-on-focus-hint: #5a7fa0; /* the "esc → grid" hint inside a focused-pane header */

  /* ---- scrims + shadows ---- */
  --scrim-palette:  rgba(9,10,12,0.68);
  --scrim-plan:     rgba(9,10,12,0.72);
  --scrim-keymap:   rgba(9,10,12,0.80);
  --scrim-loupe:    rgba(9,10,12,0.82);
  --glass:          rgba(14,16,19,0.86);  /* floating chips over an image or map */
  --shadow-window:  0 30px 80px rgba(0,0,0,0.5);
  --shadow-dialog:  0 24px 70px rgba(0,0,0,0.6);
  --shadow-palette: 0 30px 90px rgba(0,0,0,0.66);
  --shadow-float:   0 8px 24px rgba(0,0,0,0.55);   /* compare crop inset */
  --shadow-pin:     0 4px 14px rgba(0,0,0,0.5);
  --shadow-icon:    0 12px 30px rgba(0,0,0,0.5);
}
```

### 1.3 Light theme

Extracted verbatim from screen 7a and the on-light branding plates in 8a. Only tokens that appear on
those screens have a recovered value.

```css
@media (prefers-color-scheme: light) {
  :root {
    --bg-app:        #ffffff;   /* centre pane + window body */
    --bg-window:     #ffffff;
    --bg-chrome:     #f2f4f7;   /* title bar, status bar */
    --bg-pane:       #f7f8fa;   /* side panes, tile body */
    --bg-tile:       #f7f8fa;
    --bg-thumb:      #e7eaf0;   /* empty thumbnail well */
    --bg-field:      #ffffff;   /* search field */
    --bg-field-alt:  #e7eaf0;   /* segmented-control shell */
    --bg-kbd:        #eceef2;   /* keycaps, ± tile-size buttons */

    --border:        #dfe2e8;
    --border-strong: #d6dae2;
    --border-window: #cbd0d9;

    --text-hi:    #1b1e25;
    --text:       #1b1e25;
    --text-muted: #5a616f;
    --text-dim:   #8e95a3;
    --text-faint: #8e95a3;
    --text-dead:  #adb3bf;   /* path separators */
    --undecided:  #cbd0d9;   /* the undecided legend swatch */

    --brand:      #0e8792;   /* the mark's solid square on light */
    --mark-stroke: #b6bdc9;  /* the mark's outlined square on light */
    --wordmark:   #12151b;

    --keep:       #45903a;   /* swatch + border */
    --keep-text:  #3d7f33;   /* keep text and "frees 21.7 GB" */
    --cut:        #cc4550;   /* swatch + border */
    --cut-text:   #c03b46;
    --gold:       #a8781c;   /* stars, pending count, "read-only card" */

    --keep-wash:  rgba(69,144,58,0.10);
    --cut-wash:   rgba(204,69,80,0.09);

    --shadow-window: 0 30px 80px rgba(20,26,40,0.28);

    /* accent (--accent, --focus-ring, all accent washes) is NOT drawn on any light
       screen — see §8, open question 3. */
  }
}
```

Additional light plates from 8a: pure-white plate `#ffffff`, tinted plate `#eceef2`, monochrome mark
on light `#12151b`, monochrome mark on dark `#eef1f6`.

### 1.4 Type

Two families, no exceptions. Loaded from Google Fonts (see §7 — this must change for a desktop app):

```
https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Public+Sans:wght@400;500;600;700&display=swap
```

**JetBrains Mono is the entire application chrome** — every label, value, path, badge, button,
status line, table cell and section heading. **Public Sans appears in-app exactly once**: the
cold-start headline on 6c (`26px / 600 / letter-spacing -0.01em / #eef1f6`). It is otherwise the
document's own body font.

Size scale, all observed values:

| px | Used for |
|---|---|
| 9 | tile R/J badges in the library shoot grid |
| 9.5 | palette group titles, filmstrip captions, stacked EXIF keys |
| 10 | uppercase section labels, key hints, meta, mode-bar key prefixes |
| 10.5 | descriptions, chips, secondary values, prose in asides |
| 11 | **the default** — status bar, most labels and values |
| 11.5 | list rows, tree rows, settings nav, table cells, prose lines |
| 12 | sidebar rows, breadcrumb path, settings row name |
| 12.5 | compare pane stem, command-palette row name |
| 13 | inspector filename, dialog title, doc-header wordmark |
| 14 | move-palette destination path, shift-capture-time value |
| 14–15 | command-palette query, loupe-first filename, recording chord |
| 26 | cold-start headline (Public Sans 600) |

Weights: 400 default, 500 emphasis on paths and filenames, 600 headings and keycaps, 700 badges,
buttons, active mode, wordmark.

Letter-spacing patterns — these are load-bearing and consistent:

| Tracking | Applied to |
|---|---|
| `0.2em` uppercase | the two hero eyebrows: "card detected" (11px), "keys" overlay title (13px) |
| `0.18em` uppercase | the in-app wordmark, 13px / 700 / `--brand` |
| `0.14em` uppercase | **every section label** — 10px / `--text-faint`. The single most repeated pattern in the design |
| `0.12em` uppercase | rail labels flanking a strip ("Comparing", "Without GPS") |
| `0.1em` uppercase | table column headers, 10px / `--text-faint` |
| `0.08em` uppercase | spec-card title column, 11px / 700 / `--brand` |
| `0.04em` | stacked EXIF key, 9.5px |
| `-0.01em` | the 26px Public Sans headline |
| `-0.02em` | the wordmark at 20px and above |
| `+0.02em` | the wordmark at 15px in the stacked lockup |

Line-height: `1.5` for dense two-line descriptions, `1.55` for aside prose, `1.6` for spec-card
prose, `1.7` for centred empty-state text, `1` for badges. Wrapping prose uses `text-wrap: pretty`;
values that may not break use `white-space: nowrap`; keys that must break use
`overflow-wrap: anywhere`.

### 1.5 Spacing, radii, borders

Radii climb with the size of the thing:

| Radius | Applied to |
|---|---|
| 1px | square source dots (5×5) |
| 2px | R/J badges, strip thumbnails, table thumbnails, verdict pills on light, legend swatches |
| 3px | keycaps, filmstrip and compare thumbnails, progress and meter bars, small square icons |
| 4px | chips, pills, badges, segmented-control segment, floating glass chips, section cards |
| 5px | grid tile, ingest step, list card, all buttons, destination row, mark at 76px |
| 6px | segmented-control shell, search field, ingest progress card, brand swatch |
| 7px | storage volume card |
| 8px | write-plan dialog, on-light brand plate |
| 9px | command / move palette dialog |
| 10px | the window frame itself |
| 50% | dots, stars, map circle markers |

Borders: `1px solid` everywhere. Exceptions — `2px` left mark on an active nav/tree/source row
(colour `--accent`, or `--amber` for a detected card, or `--violet` for a track); `3px` top stripe on
a filmstrip frame carrying a verdict; `1px dashed --accent` on the chord-recording box; `1px dashed`
amber on an untagged map pin and on without-GPS strip thumbnails.

Gaps: `1px` between mode-bar items, `2–3px` inside badge clusters, `5–6px` between chips, `8–10px`
between a label and its value, `10px` grid gutter, `12–14px` between status-bar groups, `14px`
between a settings row's text and its controls, `20–22px` between settings groups.

Padding: `0 12px` for a sidebar row, `0 14px` for a title/status bar, `0 16px` for a table or palette
row, `10–12px` for an aside block, `14px 16px` for a spec-card cell, `12px` for the grid canvas,
`14px 18px 22px` for a settings body.

### 1.6 Scrollbars

Both documents specify the same, and it is the only scrollbar styling given:

```css
::-webkit-scrollbar       { width: 9px; height: 9px; }
::-webkit-scrollbar-thumb { background: #2b313b; border-radius: 5px; }
::-webkit-scrollbar-track { background: transparent; }
```

### 1.7 Placeholder imagery

Every thumbnail in both mocks is a generated hatch, not a photo. Reproduce it only if you need
loading placeholders:

```js
tone(i) => `repeating-linear-gradient(${24 + (i*67)%120}deg,
             hsl(218 9% ${15 + (i*41)%24}%) 0 8px,
             hsl(218 9% ${15 + (i*41)%24 + 8}%) 8px 16px)`
```

The inspector's empty preview well uses `repeating-linear-gradient(58deg,#1c2026 0 8px,#23282f 8px
16px)` (light: `58deg,#dfe3ea 0 8px,#e9ecf1 8px 16px`); the loupe stage uses
`repeating-linear-gradient(61deg,#1b1f25 0 10px,#242a32 10px 20px)`.

---

## 2. Shell and navigation model

Every application screen is the same shell. Build the shell once; each mode is a set of pane bodies.

```
┌──────────────────────────────────────────────────────────────┐
│ title bar            40px   padding: 0 14px 0 78px           │  ← 78px left inset clears
├────────┬────────────────────────────────────┬────────────────┤     the macOS traffic lights
│ left   │ centre pane                        │ right pane     │
│ pane   │  ├ pane header  30–32px            │ (inspector)    │
│ 208px  │  └ body (scrolls)                  │ 296px          │
├────────┴────────────────────────────────────┴────────────────┤
│ [optional strip 78–104px: filmstrip / compare rail / geotag] │
├──────────────────────────────────────────────────────────────┤
│ status bar           30px                                    │
└──────────────────────────────────────────────────────────────┘
window 1440×900 · radius 10px · border 1px #262a32 · shadow 0 30px 80px rgba(0,0,0,0.5)
settings window 1200×820
```

**Title bar** (40px, `--bg-chrome`, `border-bottom: 1px solid --border`), left to right:
segmented layout control · context (breadcrumb path, or a count, or a query field) · flexible
spacer · `⌘K` search hint · flexible spacer · right-hand status chips. The two flexible spacers
centre the ⌘K hint in the window.

**Status bar** (30px, `--bg-chrome`, `border-top`), left to right: the mode bar · a mode chip naming
the current state · inline key hints · right-hand counters.

**Mode bar** — always four items, always in this order, `⌃1–4`:

| Key | Mode | Sub-layouts (`⌥1–3`) |
|---|---|---|
| `⌃1` | CULL | contact sheet · loupe-first · table |
| `⌃2` | EXIF | (segmented control present, contents lost to truncation) |
| `⌃3` | MAP | pins · heat · track |
| `⌃4` | LIBRARY | search · sessions · storage |

Active mode: `background: --accent; color: --on-accent; font-weight: 700`. Inactive:
`color: --text-muted`. The `⌃n` prefix inside each item is `10px` at `opacity: 0.55`.

**Panes** are `⌘1` (left) `⌘2` (centre) `⌘3` (right); `esc` returns focus to the grid. Verbatim from
the 2a status bar: `⌘1–3 panes · ⌃1–4 modes`, `esc back to grid`, `space loupe`, `⌥1–3 layout`.

**Focused-pane treatment** (screen 2a) — the only place focus is shown at pane scale:
background lifts to `--bg-raised`, outer border becomes `--border-pane-focus` (`#2f5a7a`),
`box-shadow: inset 0 0 0 1px rgba(97,175,239,0.35)`, and a 22px header strip appears with
`background: rgba(97,175,239,0.14)`, a 5px accent dot, the pane name plus `· focused` in
`--accent` at 10px, and a right-aligned `esc → grid` hint in `#5a7fa0`.

**Mode chip** in the status bar: `padding: 2px 7px; border-radius: 3px` with a wash background and
matching text — accent wash for normal states (`LOUPE`, `SEARCH`, `SESSIONS`, `STORAGE`, `COMPARE ·
side A`, `BATCH · 14`, `RECORDING`, `HEAT · all imports`), gold wash for `INSERT · DateTimeOriginal`,
amber wash for `INDEXING` and `GEOTAG · from photo`, violet wash for `TRACK · 19:42:07`.

---

## 3. Screen inventory

27 application screens are drawn, plus 9 specification cards. Reference IDs are the design's own
anchors.

### CULL mode (⌃1)

**1a · contact sheet** — the default screen.
Sidebar 208px: three labelled groups, `Sources` (rows with a 5×5 square status dot, name, count),
`Collections`, `Filters` (rows carry a 14×14 key-cap square instead of a dot). Centre: a 32px pane
header — `sorted by / shot time ↑ | verdicts` then three legend swatches (`18 keep`, `9 cut`, `21
undecided`, swatches 7×7 radius 2 in keep/cut/`#3a404b`), spacer, `frees 21.7 GB`, then `−` `+` tile
size keycaps. Body: `grid-template-columns: repeat(5,1fr); gap: 10px; padding: 12px`. Inspector
296px: 3/2 preview well, filename + `7 of 48`, a two-up RAF/JPG pair card, an `Exposure` histogram
(56px tall, 40 bars, `gap: 1px`, `align-items: flex-end`), then EXIF sections as 72px-key /
flexible-value rows at 20px each.

**1b · loupe-first** — one frame at a time.
Left rail widens to 250px and becomes the frame's own panel: filename 15px/500, date-time,
five 10px rating dots with a `1–5 to rate` hint; a `Verdict` block of two half-width cards (KEEP
active = `--keep-wash-12` + 1px `--keep` border; CUT inactive = `--bg-field-alt` + `--border-strong`)
each showing its key beneath; a `Files kept` block of two rows (R kept, J cut with strikethrough and
a `−11.8 MB` delta in `--cut`); then stacked EXIF sections (key above value, not beside).
Centre: `--bg-app` stage, `padding: 22px`, with a top-left verdict badge (`KEEP · RAW ONLY`, glass
background, 1px `rgba(152,195,121,0.6)` border) and an exposure strip beneath it, and bottom-right
hint chips `Z 1:1 · C compare · Tab grid`. Filmstrip 104px: 112×72 frames with a 3px verdict stripe
across the top, an R/J badge pair bottom-left, and the stem caption below.

**1c · table** — every value visible.
No sidebar. Header row 26px, `10px / 0.1em / uppercase / --text-faint`, sorted column in `--accent`.
Columns: thumb `58` · stem `flex` · pair `52` · raw `74` · jpeg `74` · shot `68` · shutter `62` ·
`ƒ` `44` · iso `52` · lens `118` · rating `58` · verdict `86` (all numerics right-aligned). Rows
44px, `border-bottom: 1px solid --border-hair`, zebra `--bg-row-zebra` on odd rows, focused row
`--bg-row-active` + `inset 2px 0 0 #61afef`, 48×32 thumbnail radius 2. Preview pane 400px on the
right. Status keys here are different: `: command line · / search · g sort column · v visual select ·
⏎ apply`.

**2a · sidebar focused (⌘1)** — 1a with the focused-pane treatment on the left pane and the centre
and right panes dimmed to `opacity: 0.72`.

**2b · loupe over the grid (space)** — a full-bleed overlay on `--scrim-loupe`, `padding: 26px 30px
20px`. Left: the image stage with the same badge cluster as 1b and hint chips `z 1:1 · c compare ·
space close`. Right: a 268px card (`--bg-chrome`, 1px `--border-strong`, radius 5, padding 12) with
filename 15px, date, five 10px rating dots, the two kept/cut file rows, then EXIF sections.
Bottom: a 78px filmstrip of 96×62 frames, each with a 3px verdict stripe.

**10a · compare** — two panes side by side, `comparing 2 of 4 selected`.
Title bar right side reads `zoom 1:1 | panning locked` (the lock state in `--brand`). Each pane:
a 34px header with a 19×19 A/B tag square, the stem at 12.5px, the timestamp, and a verdict pill;
the image filling `padding: 16px` with an optional focus ring; a 150×150 crop inset at
`bottom: 26px; right: 26px` (radius 3, 1px `--border-strong`, `--shadow-float`) and a crop note chip
bottom-left; then a metrics block of 26px rows — `96px` key · value · a 4px meter · a delta. Winning
metric: value and bar `--keep`, delta `--keep`; losing delta `--cut`; neutral bar `#3d4650`.
Bottom: a 78px rail labelled `Comparing / 4 frames · burst 19:42:0x` (label in `--accent`) with 82×54
thumbnails. Keys: `⇥ switch side · w this one wins, cut the rest · k/x verdict on this side ·
l unlock panning · c back to grid`.

### EXIF mode (⌃2)

**3a · single frame** — form pane focused, one field in edit.
Title bar carries `3 unwritten` (gold wash chip with a dot) and a green `write ⌘S` button.
Left 208px: a 22px `FRAMES / ⌘1` header, then 34px rows of 34×23 thumbnail + stem + a 5px dirty dot.
Centre: focused-pane treatment — `box-shadow: inset 0 0 0 1px rgba(97,175,239,0.30)` and a 30px
header on `--accent-wash-10` reading `EDITOR · focused` with `⇥ next field · ⏎ commit · esc revert ·
⌘Z undo` in `#5a7fa0`. Form sections: a `0.14em` label, a hairline rule, an optional right-aligned
note. Field rows are 28px minimum, `padding: 0 8px`, radius 4: an 8px dirty column (`●` in
`--gold`), a 210px tag name in `--text-dim`, the value, a struck-through previous value, and a lock
marker. Row states — editing: `background: --accent-wash-10`, `border: 1px solid --accent`, value on
`--accent-wash-16` with `box-shadow: inset -1px 0 0 0 #61afef` as the caret, value colour
`--text-hi`; dirty: `background: rgba(229,192,123,0.07)`, `border: rgba(229,192,123,0.32)`; locked
value `--text-dim`; mixed value `--gold`.
Right 296px: `Targets` (three rows, each a 15×15 chip + name + mode), `Pending diff` (tag,
`− was` in `--cut`, `+ now` in `--keep`), `Presets`.

**3b · batch edit** — 14 frames selected.
Title bar shows a `14 frames selected` pill on `--accent-wash-18` and `4 tags × 14 frames` in gold.
Left rail becomes a `repeat(3,1fr)` thumbnail mosaic with per-thumb opacity. Centre header states
the rule verbatim: *"a value you type replaces every frame · ⟨mixed⟩ means the frames disagree ·
empty leaves them alone"*. Right: `Affected`, `Shift capture time` (a 14px `+02:00:00` inset field
marked `relative`, with the note "keeps the interval between frames intact"), `Presets`.
Status: `⇥ next · ⌘⏎ apply to all · ⌘S write to disk · esc leave batch`, and `56 writes queued`.

**3d · write plan (⌘S)** — a 760px dialog on `--scrim-plan`, panes behind dimmed to `opacity: 0.35`.
Header: `Write metadata` 13px/700 + `56 writes · 14 frames · 21 files`. Body: 28px rows —
`14px` sign · `220px` target · `176px` tag · flexible value · `74px` method (right-aligned).
Footer on `--bg-raised`: two green ✓ assurance rows (`back up originals to
.culler/backup/2026-08-03/`, `RAW via sidecar · JPEG in place`), one amber `!` warning row
(`3 frames sit on a read-only volume — they will be skipped`), then `esc cancel` · spacer ·
`dry run ⌘D` (secondary) · `write ⏎` (green primary).

### MAP mode (⌃3) — `culler-map.html`

**4a · pins.** Sidebar 208px: a `PLACES / ⌘1` header, 26px cluster rows (name + count, active row on
`--accent-wash-16` with a 2px `--accent` left mark), a final `no position / 11` row in `--amber`;
a footer block `GPX track` with a 14×2 violet line swatch, the filename, and
`match by timestamp → 11 frames get a position`. Centre: a Leaflet map on `--bg-app` with two
top-left glass chips (`37 of 48 frames placed | 11 pending`, and `drag a frame onto the map to
geotag it`) and bottom-left hint chips `f fit to pins · g geotag selection · ⇧d strip GPS`.
Inspector 296px: preview well, filename + `18 frames here`, `Position` (latitude, longitude,
altitude, accuracy, heading) marked `from camera` in `--keep`, `Reverse geocode` (place, city,
region, country) with the note "written to IPTC City / State / Country on ⌘S", and `At this pin` as a
`repeat(4,1fr)` thumbnail grid. Bottom: a 78px `Without GPS / 11 frames · drag to place` rail of
78×52 dashed-amber thumbnails.

**4b · geotag tool (g).** Two-tab right pane (`drop pin ⌥1` / `from photo ⌥2`, tabs radius `5px 5px 0
0`, active tab `--bg-window` + 1px `--accent` border with `border-bottom: none` over a 1px `--accent`
top rule on the body). Left 232px: a `QUEUE · no position` header on `--amber-wash-10` with `4 / 11`
in `#8b6a45`, then 34px rows with a 13×13 tick box, a thumbnail, the stem and a timestamp.
`from photo` body: a `/rynek` query field, three scope chips (`nearest in time` active), a donor list
(46×31 thumb, `stem · place`, a time delta, coordinates beneath; selected row on `--keep-wash-10`
with `inset 2px 0 0 #98c379`), and a `Result` footer with `copied from DSCF1189` in `--keep` and an
`apply to 4 ⏎` green button.

**4c · heat (⌥2).** Left rail `DENSITY`, ranked place rows. Map: per location four stacked
`L.circle` layers — three `#e06c75` rings at radius ×1.9/×1.35/×1.0 with fill-opacity
0.07/0.11/0.20, plus a `#e5c07b` core at ×0.55 opacity 0.42; radius `26 + √frames × 5.4`.
Inspector: a stats section, a `Density` bar list, and a `Reading it` explainer.
Status: `⏎ select frames in view · −/+ blob scale · f fit`.

**4d · track (⌥3).** Left rail `TRACKS` with loaded GPX files (active row `--violet-wash-14` + 2px
violet mark) and a `Legend`. Map: `L.polyline` in `#c678dd` weight 3 opacity 0.9; matched frames as
4px `#98c379` circle markers with a `#0e1013` 1.5px ring; track start = hollow violet 5px, track end
= filled violet 5px; a scrub head rendered as a selected pin showing the timestamp.
Inspector: `At the head`, `Matching` (frames in window, matched, outside the track, camera clock
offset, tolerance), and a `Note` stating that matching writes nothing on its own.

### LIBRARY mode (⌃4)

**5a · search.** Title bar's centre is a full-width query field (`--bg-field`, 1px `--accent`) with
`/` in accent, `key:` tokens in `--violet`, values in `--text`, a 1px×13px accent caret, then
`184 results · 38 ms`. Left 216px: grouped tree sections, each with a title, a hairline and a
right-aligned hint. Centre: a 30px header (`grouped by shoot | sort newest first | 12 selected across
3 shoots`, then `save as smart collection ⌘⇧S` with `smart collection` in violet); body groups frames
by shoot with a header line (name 12px/600, meta, hairline, state) over a `repeat(6,1fr) gap: 8px`
tile grid. Tiles here are the compact variant: radius 4, 9px R/J badges top-left, 4px star dots
bottom-left, a location caption bottom-right, and a 19px stem footer. Right 288px: faceted lists —
`96px` key, a 5px meter, a `42px` right-aligned count.

**5b · sessions & storage.** Title bar right: `1 session unfinished` on `--cut-wash-16`.
Table header 26px; columns `date ↓` `84` (accent) · session `flex` · source `108` · frames `58` ·
kept/cut `120` · on disk `68` · freed `66` · state `104`. Rows 38px. The kept/cut cell is a 5px
split bar (`--keep` then `--cut` segments) plus a ratio caption. Right 330px: `Volumes` (per volume a
dot, name, free space, a 7px three-segment bar in `--accent`/`--brand`/`#3f4550`, and a raw/jpeg/other
legend), `Health` (dot, label, value, hint `⏎ to fix`), and `Selected session` (a metadata list plus
`resume ⏎` blue primary and `reveal ⌘R` secondary, side by side).

**10b · storage (⌥3).** Title bar right: `24.6 GB reclaimable` on `--keep-wash-16` with a dot.
Centre: one card per volume (radius 7, `--bg-chrome`, 1px `--border`) — a header row (7×7 dot, name
13px, a `kind` pill, used/total, free), a 12px stacked segment bar with a wrapped legend, and a list
of 28px rows (`name` · `76px` frames · `82px` size · `92px` state). Right 330px: `Reclaimable` — a
tick list where each item is a 13×13 tick box, a two-line label + note, and a bold size, with
`ticked 21.0 GB` and a green `review plan ⏎` button beneath; then `By year` bars; then a `Notes`
paragraph: *"Reclaiming goes through the same plan as a cull — nothing here deletes on the spot, and
every line says which volume it touches."*

### Command and keyboard layer

**6a · ⌘K command palette.** Scrim `--scrim-palette`, panes behind at `opacity: 0.30`. Dialog 720px,
`padding: 96px 60px 60px` from the top of the window, radius 9, `--bg-chrome`, 1px `--border-dialog`,
`--shadow-palette`. Header: a 15px `›` in accent, the query in `--text-hi`, a 1px×17px accent caret,
spacer, `3 frames selected`. Scope chip row beneath. Body: grouped rows 34px — a 14px icon column,
the command name at 12.5px, a flexible dimmed note, then right-aligned chord keycaps. Footer on
`--bg-raised`: `↑↓ pick · ⏎ run · ⇥ run with arguments`, spacer, *"acts on the selection, not the
cursor"* with `selection` in accent.

**6b · move / copy palette (m).** Dialog 760px. Header: a `MOVE` badge on `--accent-wash-18`, the
destination path at 14px, an accent caret, then `3 frames · 5 files · 188 MB`. A policy chip row.
A `Destinations` section headed `type any path · ~ works`, rows 36px. Footer: a `Result` block —
`sidecars and JPEG halves follow the RAW` — then per-file `sign / from → to` lines, and the button
row `esc cancel` · spacer · `copy instead ⌥⏎` (secondary) · `move ⏎` (blue primary).

**6c · cold start / empty state.** No folder open. Sidebar shows `Detected` (a card row on
`--amber-wash-14` with a 2px amber left mark, the volume name, and a `card` tag) and `Recent`.
Centre is a 560px centred column: the eyebrow `card detected` (11px / 0.2em / `--brand`), the
headline `FUJI_SD · 1,204 frames` (Public Sans 26px/600), a meta line, then three 44px action rows
each with a 22×22 key square; then a progress card — `building previews`, `412 / 1,204` in accent, a
5px accent bar at 34%, and the reassurance *"you can start culling now — frames appear as they
decode, embedded JPEG first"*. Right pane holds centred placeholder text in `--text-ghost`:
`no frame selected / the inspector fills in / once you pick one`. Status chip: `INDEXING` (amber).

**6d · keymap overlay (?).** Scrim `--scrim-keymap`, panes behind at `opacity: 0.22`, no dialog
frame — content sits directly on the scrim at `padding: 44px 52px`. Header: `keys` (13px / 700 /
0.2em / `--brand`), then `CULL mode · read from ~/.config/culler/config.toml`, spacer,
`? or esc to close`. Body: `grid-template-columns: repeat(3,1fr); gap: 0 40px; align-content: start`.
Each group: a `0.14em` title with a hairline, then 25px rows of a 92px right-aligned keycap cluster
and a label.

### Settings (⌘,) — 1200×820

Shared frame. Title bar: `Settings` 12px/600, a flexible `filter settings` field with `⌘F`, then
`~/.config/culler/config.toml` and an `edit file ⌘E` pill. Body: nav 190px / content flexible /
aside 280px. Status bar: the page chip, `⌘E open config.toml`, `⌫ reset to default`, `esc close`,
spacer, `saved automatically`.

Nav items, fixed order: `General · Keymap · Culling · Files & writes · Catalogue · Appearance ·
Advanced`. Keymap carries a badge `2`. Active row: `background: --accent-wash-16`,
`border-left: 2px solid --accent`, text `--text-hi`; inactive text `--text-muted`.

Content is groups of rows: a `0.14em` group title (`--text-faint`, or `--cut` for a destructive
group) with a hairline, then rows of `name (12px) + description (10.5px, --text-muted)` on the left
and a right-aligned cluster of control chips.

**5c · keymap.** Content is chord rows: an 8px changed-marker column in `--gold`, a 200px action
name, a 130px chord cluster, a note, a right-aligned scope. Header states `click a chord or press ⏎
on a row to record` and `2 changed from defaults` in gold. Aside: the selected action and its
description; a `Recording` block — a 52px box on `--accent-wash-10` with a `1px dashed --accent`
border showing `⌘ ⇧ X` at 14px/700 in accent, then a conflict callout on `rgba(224,108,117,0.10)`
with a `rgba(224,108,117,0.45)` border reading *"already bound to library.exclude — recording again
will unbind it"*, then `esc cancel` / `⏎ bind` (green); then `Presets` with the note *"Every binding
lives in config.toml — presets just rewrite that file."* Status chip: `RECORDING`.

**5d · files & writes.** Standard settings body. Aside: `Naming preview` (three inset sample strings
on `--bg-field-alt`), `Safety net` (13×13 icon + label rows), and `Storage used by culler` with a
full-width `purge previews older than 90 days` button.

**10c · culling.** Groups and rows, verbatim:
- *Verdicts* — `Default verdict for a new frame` (undecided•/keep); `Advance after a verdict`
  (on•/off); `A verdict on a selection` (whole selection•/cursor only); `Second press`
  (clears•/no-op).
- *Pairs* — `Default mask for keep` (both•/raw only/jpeg only); `Cut removes` (both halves•/masked
  only).
- *Ratings* — `Scale` (1–5 stars•/3-step); `Rating implies keep` (on•/off).
- *Applying* (destructive, title in `--cut`) — `Confirm above` (50 files); `Apply on folder change`
  (hold•/prompt).
Aside: `Current session` (frames 48, decided 21 gold, keep 18, cut 9, would free 21.7 GB green) and
an `Effect of these settings` note.

**10d · appearance.** *Theme* — `Colour scheme` (system•/dark/light); `Accent` (blue•/cyan/purple).
*Density* — `Row height` (compact•/comfortable); `UI text size` (11px/12px•/13px);
`Monospace face` (JetBrains Mono•). *Tiles* — `Show on a tile` (verdict•, rating•, size•, time) with
the note "Filename and the R/J pair are always shown"; `Thumbnail fit` (fill•/fit); `Grid columns`
(auto•/fixed). *Chrome* — `Show the keys in the status line` (on•/off); `Inspector on launch`
(open•/collapsed). Aside `Preview`: scheme `system → dark`, accent `#61afef`, row height `26 px`,
tile footer `stem + size`.

**10e · catalogue.** *Location* — catalogue file `~/.local/share/culler/db`, preview cache
`~/.cache/culler`. *Indexing* — watch folders; watched roots; read metadata on index (on•/lazy);
network volumes (on demand•/always). *Cache* — preview budget (20 GB•/50 GB/no limit); keep previews
for (90 days). *Maintenance* (destructive) — re-index everything; rebuild previews; forget missing
files (danger-styled `run`). Aside `Catalogue`: 41,208 frames, 612 MB on disk, 18.4 GB previews
(gold), last index 6 min ago, 0 missing (green); plus a `Safety` note.

**10f · general & advanced.** *On launch* — open (last folder•/library/nothing); when a card is
inserted (offer to cull•/ignore); restore pending verdicts. *Tools* — external editor; exiftool
(bundled•/system path); reveal in. *Advanced* — decode with GPU; decode threads (auto•/set);
log level (warn•/info/debug); reload config on save. *Privacy* — `Network calls: never•` with the
description *"culler makes none of its own. Reverse geocoding is the one exception."*;
`Reverse geocoding` (ask first•/off). Aside `Build`: version 0.4.1, platform `macOS 15.4 · arm64`,
exiftool `13.02 · bundled`; plus an `Escape hatches` note: *"Everything on every settings page is a
line in config.toml. ⌘E opens it and it reloads on save — these panels are a convenience, not the
source of truth."*

### Light theme

**7a · cull screen, light.** Structurally identical to 2a. All values in §1.3. The caption is the
design intent: *"paper, not a washed-out dark theme."* Two divergences from the dark tile worth
noting: the top overlay bar has no gradient scrim (it uses `padding: 4px 5px 0` on a light thumbnail
well) and the verdict badge gains a background chip rather than being bare text.

### Specification cards (not screens)

`3e` build notes · `7b` token & type sheet · `8a` mark & wordmark · `8b` app icon · `8c` brand rules ·
`9a` screen index · `9b` shell & interaction model · `9c` data model · `9d` states we owe.
All are 940–1020px wide cards with a `190px` label column on `--bg-raised` and a content column.
**All of their row contents were lost to the truncation** (§0).

---

## 4. Component specifications

### 4.1 Grid tile

```
container   display:flex; flex-direction:column; border-radius:5px; overflow:hidden;
            background:--bg-tile; cursor:pointer;
            border:1px solid  #23272f | selected #2d4360 | focused #61afef
            box-shadow:       none    | focused 0 0 0 2px rgba(97,175,239,0.40)

thumbnail   position:relative; aspect-ratio:3/2; background:--bg-app; overflow:hidden
  image     position:absolute; inset:0
  overlay   position:absolute; top/left/right:0; height:22px; display:flex;
            align-items:center; gap:5px; padding:0 5px;
            background:linear-gradient(to bottom, rgba(14,16,19,0.82), rgba(14,16,19,0))
    R/J     inline-flex; gap:2px; mono 10px/700; line-height:1
            each: padding:2px 3px; border-radius:2px
    spacer  flex:1
    verdict mono 10px/700 — "KEEP" --keep | "CUT" --cut | "" undecided
  stars     position:absolute; bottom:4px; left:5px; flex; gap:2px
            each: 5×5; border-radius:50%; background:--gold

footer      height:22px; padding:0 6px; display:flex; align-items:center; gap:6px;
            background:--bg-tile; mono 10.5px
  stem      flex:1 1 auto; min-width:0; ellipsis; color:--text-2
  size      flex:0 0 auto; nowrap; color:--text-muted
```

R/J badge states (both halves are always drawn — absence is shown, not hidden):

| State | Background | Foreground | Decoration |
|---|---|---|---|
| present, kept | `rgba(152,195,121,0.20)` | `#98c379` | none |
| present, cut | `rgba(224,108,117,0.22)` | `#e06c75` | `line-through` |
| not present | `rgba(120,130,145,0.14)` | `#3f4550` | none |

Compact variant (library shoot grid, 5a): radius 4, badges at 9px positioned `top:3px; left:3px`,
star dots 4px at `bottom:3px; left:4px`, a location caption at `bottom:3px; right:4px`, footer 19px
with the stem only.

### 4.2 Buttons

| Variant | Style |
|---|---|
| Primary, destructive-safe (green) | `padding:6px 14px; border-radius:5px; background:#98c379; color:#0e1013; mono 11.5px/700` |
| Primary, navigational (blue) | same box, `background:#61afef; color:#0e1013; mono 11px/700` |
| Secondary | `padding:6px 12px; border-radius:5px; background:#1d2127; border:1px solid #2b313b; color:#8b919e; mono 11–11.5px/400` |
| Full-width action | `padding:6px 9px; border-radius:5px; background:#1d2127; border:1px solid #2b313b; text-align:center` |
| Title-bar pill | `padding:3px 8–9px; border-radius:4px` — green filled for `write ⌘S`, `--bg-field` + `--border-strong` for `edit file ⌘E` |

Every button embeds its own key hint in the label (`review plan ⏎`, `resume ⏎`, `move ⏎`,
`copy instead ⌥⏎`, `dry run ⌘D`, `apply to 4 ⏎`, `reveal ⌘R`), separated by one or two spaces. Keep
this — it is the design's most consistent affordance.

### 4.3 Settings control chip (`CTL`)

```
padding:5px 10px; border-radius:5px; mono 11px; white-space:nowrap
  on       background:#98c379            border:#98c379                  color:#0e1013  weight:700
  danger   background:rgba(224,108,117,0.14) border:rgba(224,108,117,0.5) color:#e06c75  weight:400
  default  background:#1d2127            border:#2b313b                  color:#8b919e  weight:400
```

An `on` chip is the selected member of its group. Groups are rendered as a flex row, `gap: 7px`.

### 4.4 Keycaps

| Context | Style |
|---|---|
| Title-bar ⌘K hint | `padding:1px 4px; border-radius:3px; background:#262b34; color:#8b919e` |
| Keymap overlay, palette rows | `padding:2px 6px; border-radius:3px; background:#1d2127; border:1px solid #2b313b; mono 10.5px/600; color:#c3c8d1` |
| Status-bar inline hint | no box — the key glyph in `--text-muted`, its label in `--text-dim` |
| Mode-bar prefix | 10px at `opacity:0.55`, inherits the item's colour |
| Ingest step | `22×22; border-radius:5px; display:inline-grid; place-items:center; mono 11px/700` |
| Filter row | `14×14; border-radius:3px; mono 9px/700` |

### 4.5 Inputs

**Search field (title bar, resting):** `height:24px; padding:0 9px; border-radius:6px;
background:#1d2127; border:1px solid #2b313b; mono 11px; color:#5a606d` with the keycap cluster
right-aligned.

**Query field (active, 5a):** `height:26px; padding:0 10px; border-radius:6px; background:#1d2127;
border:1px solid #61afef; mono 12px`. Syntax colouring: `/` and the caret `--accent`, `key:` prefixes
`--violet`, values `--text`, the trailing result count `--text-dim`. The caret is a
`1px × 13px` `--accent` block, not a text cursor.

**Settings filter:** `height:26px; padding:0 10px; border-radius:6px; background:#1d2127;
border:1px solid #2b313b; mono 11.5px` with `⌘F` right-aligned.

**Inset value box:** `padding:6–7px 8px; border-radius:4–5px; background:#1c2027;
border:1px solid #2b313b`.

### 4.6 Segmented control (layout toggle)

```
shell    height:24px; padding:2px; border-radius:6px; background:#1d2127; border:1px solid #2b313b
segment  height:100%; padding:0 9px; border-radius:4px; display:flex; gap:6px; mono 11px
  active   background:#61afef; color:#0e1013; font-weight:700
  inactive color:#8b919e; font-weight:400
  key      10px at opacity:0.55 (light: 0.6)
```

### 4.7 Rows

**Sidebar / source row:** `height:26px; padding:0 12px; gap:8px; border-left:2px solid <mark>` —
a 5×5 radius-1 status dot, the name (12px, ellipsised), a right-aligned count (10.5px,
`--text-muted`). Active: `background:--accent-wash-16`, mark `--accent`, text `--text-hi`.

**Filter row:** `height:24px` — a 14×14 key square in place of the dot, name at 11.5px.

**Tree / nav row (settings, library):** `height:25–28px; padding:0 12px; gap:8–9px;
border-left:2px solid <mark>`, name 11.5px, right-aligned badge 10px.

**Palette row:** `height:34px; padding:0 16px; gap:12px` — a 14px icon column, the name at 12.5px,
a flexible ellipsised note, right-aligned chord caps. Selected: a background wash plus a `box-shadow`
edge (the exact selected background is templated and was lost).

**Destination row:** `height:36px; padding:0 16px` — icon, path (12px), meta, chords.

**Table row:** see 1c and 5b above. Height 38–44px, `border-bottom:1px solid #1b1e24`.

**Metric row (compare):** `height:26px; padding:0 14px; gap:12px; border-bottom:1px solid #1b1e24` —
a `96px` key at 10.5px `--text-dim`, the value at 11.5px, a flexible 4px meter
(`background:#20242b; border-radius:2px`), a right-aligned delta at 10px.

**Key/value aside row:** `min-height:20–21px; gap:10px` — key `flex:1` at 11px `--text-muted`,
value right-aligned at 11px, colour varies by significance (`--text` default, `--keep` good,
`--gold` attention).

### 4.8 Section label

The most repeated component in the design:

```html
<div style="display:flex;align-items:center;gap:8px;padding-bottom:7px">
  <span style="font-family:var(--font-mono);font-size:10px;letter-spacing:0.14em;
               text-transform:uppercase;color:#4d535f">Title</span>
  <span style="flex:1;height:1px;background:#23272f"></span>
  <!-- optional right-aligned hint, 10px, #3f4550 -->
</div>
```

### 4.9 Dialog frame

```
scrim     position:absolute; inset:0; background: rgba(9,10,12, 0.68 | 0.72 | 0.80 | 0.82)
          panes behind drop to opacity 0.22 (keymap) / 0.30 (palette) / 0.35 (write plan)
panel     border-radius:8px (plan) | 9px (palette)
          background:#171a1f; border:1px solid #2f3742
          box-shadow:0 24px 70px rgba(0,0,0,0.6) | 0 30px 90px rgba(0,0,0,0.66)
          width:720px (⌘K) | 760px (move, write plan)
header    padding:13–14px 16px; border-bottom:1px solid #23272f
body      flex:1; min-height:0; overflow-y:auto
footer    padding:9–12px 16px; border-top:1px solid #23272f; background:#1a1e24
```

Placement: the ⌘K and move palettes are top-anchored (`padding:96px 60px 60px`,
`justify-content:center`); the write plan is centred (`display:grid; place-items:center;
padding:60px`).

### 4.10 Progress and meters

**Progress bar:** `height:5px; border-radius:3px; background:#20242b; overflow:hidden`, fill
`background:#61afef`. Paired with a `done / total` count in `--accent` above it.

**Meter (facets, years, metrics):** `height:4–6px; border-radius:2–3px; background:#20242b`, fill
coloured by meaning.

**Stacked bar (volumes, kept/cut):** `display:flex` inside the track, each segment a percentage-width
span — `--accent` raw / `--brand` jpeg / `#3f4550` other, or `--keep` then `--cut`.

**Volume bar (storage):** 12px tall, radius 3, arbitrary segment count with a wrapped legend beneath
(7×7 radius-2 swatch, key in `--text-muted`, value in `--text`).

### 4.11 Status chips (title bar, right)

`display:inline-flex; align-items:center; gap:6px; padding:3px 8px; border-radius:4px` with a
5×5 round dot in the same hue as the text, on a 0.16-alpha wash of that hue. Instances:
`24.6 GB reclaimable` (keep), `1 session unfinished` (cut), `3 unwritten` / `4 tags × 14 frames`
(gold), `11 without GPS` (amber).

### 4.12 Glass chip (over an image or map)

`padding:4–5px 8–10px; border-radius:4–5px; background:rgba(14,16,19,0.78–0.88);
border:1px solid #2b313b; mono 10.5–11px; color:#8b919e; white-space:nowrap`. Used for loupe hint
chips, map hint chips, the crop note in compare, and the map's status overlays. Container sets
`pointer-events:none` where the chips are informational.

### 4.13 Map pin (`culler-map.html`)

```css
.pin           { display:flex; align-items:center; gap:5px; padding:3px 3px 3px 4px;
                 border-radius:5px; background:rgba(14,16,19,0.9); border:1px solid #2f3742;
                 box-shadow:0 4px 14px rgba(0,0,0,0.5); cursor:pointer; white-space:nowrap; }
.pin .sw       { width:26px; height:18px; border-radius:2px; }        /* thumbnail swatch */
.pin .ct       { font-family:"JetBrains Mono"; font-size:10.5px; font-weight:700;
                 color:#c3c8d1; padding-right:3px; }                  /* count */
.pin.sel       { border-color:#61afef;
                 box-shadow:0 0 0 2px rgba(97,175,239,0.35), 0 4px 14px rgba(0,0,0,0.5); }
.pin.sel .ct   { color:#eef1f6; }
.pin.untagged  { border-color:#d19a66; border-style:dashed; }
.pin.untagged .ct { color:#d19a66; }
```

Leaflet chrome overrides (dark basemap by filter, not by tile provider):

```css
.leaflet-container { background:#0e1013; font-family:"JetBrains Mono",monospace; }
.leaflet-tile-pane { filter: invert(1) hue-rotate(185deg) saturate(0.55)
                             brightness(0.82) contrast(1.06); }
.leaflet-control-attribution { background:rgba(14,16,19,0.78)!important; color:#5a606d!important;
                               font-size:9.5px!important; border:none!important; }
.leaflet-bar   { border:1px solid #2b313b!important; }
.leaflet-bar a { background:#171a1f!important; color:#8b919e!important;
                 border-bottom-color:#2b313b!important; }
.leaflet-bar a:hover { background:#1f242b!important; color:#d7dae0!important; }
```

Map init: `zoomControl:false` then `L.control.zoom({position:'bottomright'})`; tiles
`https://tile.openstreetmap.org/{z}/{x}/{y}.png`, `maxZoom:19`,
attribution `© OpenStreetMap contributors`.

---

## 5. Branding

### 5.1 The mark

The mark is the decision itself: **one frame kept, one dropped.** Two equal squares in a square box —
the back square outlined and offset to the top-right, the front square solid `--brand` and offset to
the bottom-left. They overlap.

Geometry, expressed as ratios of the mark's bounding box (verified against all six drawn sizes):

| Property | Ratio | At 76px |
|---|---|---|
| Square side | 68 % of box | 52px |
| Offset (each axis) | 32 % of box | 24px |
| Corner radius | ~10 % of square | 5px |
| Outline stroke | ~4 % of box | 3px |

Positioning is literal: back square `top:0; right:0`; front square `bottom:0; left:0`.

Drawn instances, verbatim:

| Box | Square | Radius | Stroke | Stroke colour | Solid colour |
|---|---|---|---|---|---|
| 76 | 52 | 5 | 3 | `#3a4550` | `#56b6c2` |
| 40 (on light) | 28 | 3 | 2.5 | `#b6bdc9` | `#0e8792` |
| 30 (mono, on light) | 21 | 3 | 2 | `#12151b` | `#12151b` |
| 30 (mono, on dark) | 21 | 3 | 2 | `#eef1f6` | `#eef1f6` |
| 26 | 18 | 2 | 2 | `#3a4550` | `#56b6c2` |

SVG, reproducible at any size (`viewBox="0 0 76 76"`):

```svg
<svg viewBox="0 0 76 76" xmlns="http://www.w3.org/2000/svg">
  <rect x="25.5" y="1.5" width="49" height="49" rx="5"
        fill="none" stroke="#3a4550" stroke-width="3"/>
  <rect x="0"    y="24"  width="52" height="52" rx="5" fill="#56b6c2"/>
</svg>
```

(The outlined rect is inset by half its stroke so the stroke sits inside the 52px footprint.)

### 5.2 The wordmark

Always lowercase `culler`. Always JetBrains Mono 700. Never title case, never all caps except in the
in-app lockup.

| Lockup | Font | Size | Tracking | Colour | Gap to mark |
|---|---|---|---|---|---|
| Hero, horizontal | JetBrains Mono 700 | 56px | `-0.02em` | `#eef1f6` | 22px, mark 76px |
| Horizontal, small | JetBrains Mono 700 | 20px | `-0.02em` | `#eef1f6` | 11px, mark 26px |
| Stacked | JetBrains Mono 700 | 15px | `+0.02em` | `#eef1f6` | 7px below a 26px mark |
| In-app / title bar | JetBrains Mono 700 | 13px | `0.18em`, **uppercase** | `#56b6c2` | — |
| On light | JetBrains Mono 700 | 30px | `-0.02em` | `#12151b` | 14px, mark 40px |

`line-height: 1` on every lockup. The in-app variant is the only uppercase treatment and the only one
in brand cyan; it is what appears in the application's own chrome.

### 5.3 App icon

Same mark in each platform's own container shape.

| Platform | Container | Background | Mark box | Square | Stroke | Stroke colour |
|---|---|---|---|---|---|---|
| macOS | 132px, radius 30px (`22.4%`), 1px `#2b313b`, `0 12px 30px rgba(0,0,0,0.5)` | `linear-gradient(160deg,#1e232b,#12151a)` | 70 | 48 | 3 | `#4a5560` |
| Windows | 132px, radius 2px, full bleed | `#12151a` | 82 | 56 | 3.5 | `#4a5560` |
| Linux | 132px, radius 50%, circle-safe, 16 % inset | `#12151a` | 64 | 44 | 3 | `#4a5560` |

Note the icon's outlined square is `#4a5560`, one step lighter than the `#3a4550` used in the
wordmark lockup — it needs the extra contrast against the icon's darker plate.

Downscales (macOS ramp): 64px → radius 15, mark 34, squares 23, stroke 2 · 32px → radius 7, mark 18,
squares 12, stroke 1.5 · 16px → radius 4, flat `#12151a` background, mark 11, squares 7, stroke 1.

The 16px caveat, verbatim from 8b:

> At 16px the outlined frame loses its inner hole — that is expected. Ship a separate 16px asset
> where the back frame becomes a solid 40%-opacity block instead of a stroke.

### 5.4 Monochrome

Two single-colour versions exist "for menu bars, tray, and print": both squares solid `#12151b` on
light, both solid `#eef1f6` on dark. The outlined square becomes a filled square — the overlap still
reads.

The `iconAssets`, `markRules`, `brandColors` and `brandNotes` lists (the authored construction rules,
the cut list, and the voice guidelines) were lost to the truncation.

---

## 6. Delta versus the current frontend

Current app: `frontend/src/` — Svelte 5, one `App.svelte` shell, 10 components, tokens in
`frontend/public/style.css`.

### 6.1 Structural additions (nothing like these exists today)

| New | Notes |
|---|---|
| Mode bar and four modes | Today `app.view` is `"grid" \| "loupe"` only (`state.svelte.ts:12`). CULL/EXIF/MAP/LIBRARY is a new top-level axis, plus `⌥1–3` sub-layouts within each mode. |
| Three-pane shell with pane focus | Today: sidebar + main + a bottom bar. The design adds a persistent right inspector (296px) and a `⌘1–3` pane-focus model with a visible focused-pane treatment. |
| Compare mode (10a) | Absent. |
| Table layout (1c) | Absent. |
| Loupe-first layout (1b) | Absent. |
| Settings UI (5c, 5d, 10c–10f) | Absent. The frontend only *reads* config (`actions.ts:161`, `ConfigService.Get()`); there is no writer and no settings screen. Six pages must be built plus a keymap recorder with conflict detection. |
| MAP mode (4a–4d) | Absent. Needs Leaflet, geotagging, GPX parsing, reverse geocoding. |
| LIBRARY mode (5a, 5b, 10b) | Absent — and directly contradicts `docs/DESIGN.md:19` (see §8). |
| EXIF mode (3a, 3b, 3d) | Absent. Needs an editable form model with dirty/mixed/locked field states and a write-plan dialog. |
| Command palette (⌘K), move palette (m) | Bound but unbuilt — `command-palette`, `copy-palette`, `move-palette`, `filter-palette` currently toast *"comes in v0.2"* (`App.svelte:37-43`). |
| Cold-start / ingest screen (6c) | Absent. Today an unopened folder just auto-focuses the path input (`FolderPicker.svelte:22-25`). |
| Ratings (1–5 stars) | No rating model exists at all. |
| Inspector with histogram + EXIF sections | Absent. `GroupDTO.shot` exists (`models.ts:97`) and is never rendered. |
| Filmstrip | Absent. |
| Sources / Collections / Filters sidebar sections | Today the sidebar is a folder tree only. |

### 6.2 Restyled, and one model change

**The decision model changes shape.** Today: four decisions bound to `1`/`2`/`3`/`4`
(`keep-all`, `drop-raw`, `drop-jpeg`, `drop-both`, `config.go:88-109`), rendered as a numeric badge.
The design uses a **verdict plus a mask**: `k` = keep, `x` = cut, `r`/`j` toggle which half survives,
and `1`–`5` are reassigned to star ratings. Settings 10c makes the semantics explicit
(`Default mask for keep: both•/raw only/jpeg only`, `Cut removes: both halves•/masked only`).
This is a data-model change, not a restyle — the four existing decisions map onto
(verdict=keep, mask=RJ), (keep, J), (keep, R), (cut, —) but the UI, the keymap and the badge
rendering all change.

**Tile.** Today: a kind badge (`P`/`J`/`R`/`?`), a warning `!`, a decision digit, and a stem label
(`Grid.svelte:104-114`). The design: an R/J pair badge showing both halves with strike-through for
the cut half, a `KEEP`/`CUT` word, star dots, and a footer of stem **plus file size**. Geometry
changes too — today `TILE_W 200 / IMG_H 150 / LABEL_H 30 / GAP 10 / ROW_H 190`
(`Grid.svelte:12-17`) against the design's `aspect-ratio 3/2`, 22px footer, 10px gap, 5 columns.

**Tokens.** Every colour changes. The current palette is a blue-violet dark theme
(`--bg #0b0c11`, `--accent #7aa2f7`, `--keep-all #6fcf7f`); the design is a warmer, greyer dark
(`--bg-app #0e1013`, accent `#61afef`, keep `#98c379`) with a second brand cyan. The existing
variable *structure* in `frontend/public/style.css` is sound and maps cleanly onto §1 — this is a
value swap plus roughly 25 new tokens, not a rewrite.

**Typography.** Today `system-ui` throughout (`style.css:11`). The design is JetBrains Mono for
essentially everything. This is the single most visible change and it affects every layout
measurement, because the monospace advance width drives the column widths quoted in §3.

**Chrome.** The top bar gains the segmented layout control, the breadcrumb path, the ⌘K hint and the
status chips. The bottom bar gains the mode bar and the mode chip. `KeymapOverlay` restyles to the
three-column grid on a scrim with no panel (6d). `Toast` and `NetworkChip` have no direct equivalent
drawn — see §8.

### 6.3 Behaviour that must survive the rebuild

These already work and the design does not ask for them to change:

- **The single global key listener** (`App.svelte:204`) with signature-based chord resolution
  (`keymap.ts`), `mod` → Cmd on macOS, and `ownsKeys()` yielding to inputs and to
  `[data-keys="local"]` regions. The design's keymap is far larger but the mechanism is right.
- **Escape unwinding in order** (`App.svelte:73-91`): plan → overlay → zoom → loupe → clear
  selection. The design adds modes and panes to the stack; keep the ordered unwind.
- **Keymap sourced from Go config** (`config.go:86-111`, `ConfigService.Get()`). The design doubles
  down: every settings screen states `~/.config/culler/config.toml` is the source of truth and the
  panels are "a convenience, not the source of truth". Settings must write that file, not a
  parallel store.
- **The virtualised grid** (`Grid.svelte`): canvas sized to `rows * ROW_H` for an honest scrollbar,
  half-screen overscan, `transform`-positioned rows, `app.cols` synced by effect, focus scrolled into
  view. Retune the constants; keep the approach.
- **The image queue** (`imageQueue.ts`): IntersectionObserver with `rootMargin: "50% 0px"`,
  `MAX_IN_FLIGHT = 3`, nearest-to-viewport ordering by live `getBoundingClientRect()`, slot release
  on unmount, and the `fetched` short-circuit. The design's filmstrips, compare rails and map strips
  are all new consumers of exactly this.
- **Decision persistence** (`decisions.ts`): 500 ms debounce, `DecisionService.SetBatch`, chained
  promises, `flush()` on blur / beforeunload / folder change / apply. The verdict+mask+rating model
  rides on the same machinery.
- **Scan progress and the slow-scan hint** (`Loader.svelte`, `app.scanProgress`, `slowScanSeconds`).
  The design's ingest screen (6c) is a richer presentation of this same state, and its
  *"you can start culling now — frames appear as they decode, embedded JPEG first"* line describes
  behaviour the backend already has.
- **The network-volume signal** (`app.network`, `NetworkChip`). The design keeps the concern —
  10e has `Network volumes: index on demand•/always`, *"so a sleeping NAS never blocks launch"* —
  but never draws the chip. Keep the behaviour; restyle it as a title-bar status chip (§4.11).
- **Undo** (`ApplyService.Undo`, `actions.ts:327`). `⌘Z` appears in the status bar of five drawn
  screens.
- **The apply-plan modal** (`ApplyBar.svelte`). The design's write plan (3d) is the same idea with a
  richer body; the cull plan is referenced constantly (`⏎ apply`, `review plan ⏎`) but the cull
  plan dialog itself is **not drawn** — see §8.

---

## 7. External dependencies the design assumes

### 7.1 Google Fonts — must be bundled, not fetched

Both documents load:

```html
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Public+Sans:wght@400;500;600;700&display=swap" rel="stylesheet">
```

**This cannot ship.** culler is an offline desktop app; settings 10f states `Network calls: never` and
*"culler makes none of its own"*. A CDN font fetch would make that false at launch, and would leave
the UI unstyled on an offline machine. Self-host both families as WOFF2 under `frontend/public/` with
`@font-face`. Both are OFL — JetBrains Mono (Apache 2.0 in fact) and Public Sans (OFL 1.1) are both
redistributable. Settings 10d confirms the intent: `Monospace face — anything installed; the default
ships with the app.`

Two weight problems to fix while you are there:

- **JetBrains Mono weight 600 is used but not requested.** The link loads `400;500;700`, yet
  `font-weight:600` appears on the Settings title (line 285), shoot names (1342), keycaps (1254) and
  chord chips (1614). Either add 600 to the bundled set or reassign those to 500/700.
- **Public Sans loads four weights for one use.** Only 600 is used, once (the 6c headline). Bundle
  600 alone, or drop Public Sans and set that headline in JetBrains Mono.

### 7.2 Leaflet — and the non-goals problem

`culler-map.html` depends on Leaflet 1.9.4 from unpkg (with SRI) and on OpenStreetMap raster tiles.

- **Licensing.** Leaflet is BSD-2-Clause — fine to bundle. OSM tiles are ODbL and the public
  `tile.openstreetmap.org` endpoint has a usage policy that a bundled desktop application plausibly
  breaches at scale; the attribution `© OpenStreetMap contributors` is mandatory and is already in
  the mock.
- **Offline.** Map mode is the one screen that cannot work offline without a bundled or cached tile
  source. There is no offline story in the design.
- **The non-goals conflict.** `docs/DESIGN.md:19-20` states, verbatim:

  > Non-goals, explicitly: no develop/edit module, no catalogue or library, no AI culling, no
  > cloud, no telemetry, no plugin system.

  The design contradicts three of these: **LIBRARY mode is a catalogue** (an SQLite file at
  `~/.local/share/culler/db`, 41,208 indexed frames, watched roots, smart collections); **map tiles
  are a cloud dependency**; and **reverse geocoding is an outbound network call**. The design is
  aware of the last one and handles it — 10f carves out `Reverse geocoding — sends a coordinate to
  look up a place name` as `ask first•/off`, described as *"the one exception"*. The catalogue and
  the tiles are not reconciled anywhere.

  This is a scope decision, not an implementation detail. Resolve it before building CULL, because
  the mode bar (`⌃1–4`) is drawn on every single screen — if LIBRARY and MAP are out of scope, the
  shell changes shape.

### 7.3 Nothing else

No icon font, no CSS framework, no charting library. Every glyph in the design is either a Unicode
character (`⌘ ⇧ ⌥ ⏎ ⌫ ⇥ ← → ↑ ↓ ↑ ✓ ● ■ ! ? › ·`) or a CSS box. Histograms, meters, stacked bars and
the star ratings are all plain divs. Keep it that way.

---

## 8. Open questions

Ranked by how much they block implementation.

1. **The truncated data script.** Nine specification cards (§0) lost their contents, including the
   authored token names, the screen index, the shell spec, the data model, and the list of undesigned
   states the designer explicitly wrote down "so nobody invents them". Re-export the full HTML from
   Claude Design if at all possible. Everything else in this list is downstream of this.

2. **Non-goals versus LIBRARY and MAP** (§7.2). A scope decision that changes the shell.

3. **Light-theme accent.** No light screen draws focus, selection, an active mode or an active nav
   row — 7a happens to show none of them. The dark accent `#61afef` will not pass contrast on a
   `#ffffff` field. The current app already faces this and answers `--accent: #2f6fe0` with
   `--focus-ring: rgba(47,111,224,0.35)` (`style.css:73-75`); that is a reasonable starting point,
   but it is a value the design did not supply.

4. **Two accents, three accent options.** Brand is fixed cyan `#56b6c2`; the UI accent defaults to
   blue `#61afef` and 10d offers `blue/cyan/purple` as a user setting. If a user picks cyan, brand
   and UI accent collide. Is the brand mark exempt from the accent setting? Presumably yes — say so.

5. **The cull apply-plan dialog is never drawn.** `⏎ apply` appears in the status bar of five
   screens, `review plan ⏎` on storage, and 10c has `Confirm above — 50 files`, but only the *EXIF
   write* plan (3d) is designed. The existing `ApplyBar` modal is the closest thing; restyling it to
   the 3d frame is the obvious move but it is an inference, not a spec.

6. **Toast and error states are not drawn.** The current app has both (`Toast.svelte`,
   the App error banner). Neither appears anywhere in the design, and `9d states we owe` — which
   presumably covered exactly this — is the card that was truncated.

7. **Keyboard conflict on screen 1b.** The loupe-first status bar lists `j/k prev/next` *and*
   `k keep` *and* `r/j toggle file` simultaneously. Three bindings, two keys. Every other screen uses
   `k keep` / `x cut` / `r`/`j` mask with arrows for navigation, so 1b is likely a drafting slip —
   but confirm before implementing that layout.

8. **`⌃2 EXIF`'s sub-layouts.** The segmented control is drawn on 3a and 3b (`exifLayouts`) but its
   contents were templated and lost.

9. **Screen 3c does not exist.** The turn-3 index runs `3a · 3b · 3d · 3e`. Probably a drafting
   artefact rather than a missing screen, but the screen index (9a) that would have confirmed it is
   truncated.

10. **Inspector collapse.** 10d offers `Inspector on launch — open•/collapsed`, but no collapsed
    state is drawn. The current sidebar collapses to a 30px rail (`Sidebar.svelte`); mirroring that
    on the right is the natural reading.
