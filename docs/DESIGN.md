# culler — design

A keyboard-driven desktop app for reviewing photos straight off a card and deciding, per frame,
what survives: both files, the JPEG only, the RAW only, or neither.

Target user: a photographer, not an engineer. Target platform: macOS first, Windows and Linux
as a supported secondary.

---

## 1. Why this exists

Existing open-source cullers (Imagin Raw, RAWviewer, RawCull) treat the RAW as the primary
asset and de-duplicate the JPEG away. For anyone shooting RAW+JPEG where the JPEG is a
first-class deliverable — Fujifilm film simulations, in-camera profiles, straight-out-of-camera
delivery — that is backwards. The unmet need is a **per-frame three-way decision** over a
RAW+JPEG pair, with the JPEG as the thing you actually look at when it exists.

Non-goals, explicitly: no develop/edit module (image pixels are never altered), no AI culling,
no telemetry, no plugin system.

Amended 2026-08-03 with the adopted redesign (`docs/design/DESIGN-SPEC.md`): a local catalogue
(LIBRARY mode) is now in scope, superseding the original "no catalogue" stance; MAP mode makes
two narrow, opt-in exceptions to "no cloud" — map tiles and ask-first reverse geocoding. EXIF
metadata editing (not pixel editing) is in scope via the op engine with journal and undo; since
2026-08-07 it lives in the PHOTOS inspector rather than a mode of its own — every edit is a
draft against the grid's selection or focused frame, and ⌘S puts up the write plan that backs
up, journals and writes, undoable like any other batch (see `docs/design/AMENDMENTS.md`). The
decision model is verdict + mask + star ratings (k/x/r/j, 1–5), superseding the four numeric
decision keys below where they conflict.

---

## 2. Stack

**Go + Wails v3**, frontend in Svelte 5 + TypeScript + Vite.

Rationale:
- Requirement 10 (Windows/Linux) rules out a native Swift/AppKit app.
- Requirement 9 (easy install for non-engineers) rules out Python/PySide, whose macOS
  distribution story is poor.
- The workload is I/O plus JPEG decode, both of which parallelise trivially across goroutines.
- Wails produces a single signed binary per platform with no runtime dependency.
- Svelte over React: less overhead per cell in the virtualised grid.

**Start pure-Go for image decoding.** Do not reach for cgo (libvips, libjpeg-turbo, LibRaw)
until a benchmark proves it necessary — cgo turns a one-command cross-compile into per-platform
dylib bundling and rpath surgery, which directly threatens requirement 9. See §5 for the gate.

License: MIT. Avoiding LibRaw in the default build path also avoids its LGPL/CDDL dual-licence
entanglement.

### Package layout

```
cmd/culler/            main, Wails bootstrap
internal/scan/       directory walk, grouping into PhotoGroups
internal/preview/    embedded-JPEG extraction (IFD, RAF, CR3), thumbnail pipeline
internal/cache/      content-addressed thumbnail cache, LRU eviction
internal/decide/     decision state, SQLite persistence
internal/ops/        Op interface, Plan/apply engine
internal/journal/    append-only JSONL journal, undo/redo
internal/platform/   trash, volume listing, cache dirs — one interface, per-OS impls
internal/config/     extension lists, keymap, collision policy, thresholds
frontend/            Svelte app (grid, loupe, palettes, keymap layer)
```

No `runtime.GOOS` checks outside `internal/platform`.

---

## 3. Core data model

### 3.1 PhotoGroup

The unit of display and decision. Never operate on individual files in the UI.

```go
type Kind int
const (
    KindPaired Kind = iota // RAW + JPEG both present
    KindJPEGOnly
    KindRAWOnly
)

type PhotoGroup struct {
    Dir      string     // absolute directory
    Stem     string     // basename without extension, case-preserved
    Kind     Kind
    Raw      *FileRef   // nil if absent
    Jpeg     *FileRef   // nil if absent
    Sidecars []FileRef  // .xmp, .aae, .json, .dop — follow the RAW on all operations
    Shot     time.Time  // EXIF DateTimeOriginal, falls back to mtime
}
```

Grouping key is `(Dir, lowercase(Stem))`. Case-insensitive because exFAT cards and macOS
default volumes are case-insensitive but ext4 is not — a naive case-sensitive key will split
pairs on Linux.

Edge cases that must be handled and tested:
- `DSCF1234.RAF` + `DSCF1234.JPG` → paired.
- `_DSF1234.RAF` + `DSCF1234.JPG` → **not** paired. Different stems. Do not attempt to be clever.
- `DSCF1234.RAF` + `DSCF1234.JPG` + `DSCF1234.RAF.xmp` → sidecar attaches to the RAW.
- `IMG_1234.JPG` + `IMG_1234.HEIC` → two JPEG-class files, same stem. Treat the RAW slot as
  empty and pick one as primary by a configured extension priority list; surface a warning badge.
- Same stem in two different directories → two separate groups. Never merge across directories.

### 3.2 Supported extensions

RAW class: `.raf .arw .cr2 .cr3 .nef .nrw .orf .rw2 .dng .pef .srw .raw .rwl .3fr .iiq .x3f`
JPEG class: `.jpg .jpeg .heic .heif .png .tif .tiff .webp .avif`

The list is config-driven, not hardcoded, so unknown formats can be added by users without a
release. Unrecognised extensions are ignored, never deleted.

### 3.3 Decision state

Decisions are ephemeral — they exist for the minutes between marking and applying. Source of
truth is a SQLite database at the app's config dir, keyed on `(dir, stem)` plus a content hash
of the primary file so a decision survives a rename but not a content change.

```
decision: none | keep_all | drop_raw | drop_jpeg | drop_all | copy_to(dest) | move_to(dest)
```

XMP sidecar writing is an **optional export**, off by default, for interop with Lightroom and
Bridge. Do not make XMP the source of truth: writing sidecars on every keystroke is slow and
litters the folder.

---

## 4. Requirements → implementation

### R1–R3: mixed RAW+JPEG, JPEG-only, RAW-only

The preview pipeline resolves in this order, first hit wins:

| Tier | Source | Used for | Cost |
|---|---|---|---|
| 0 | EXIF thumbnail (~160px) embedded in either file | initial grid fill | microseconds |
| 1 | External JPEG on disk | grid + loupe, when present | one decode |
| 2 | Full-size preview embedded in the RAW | grid + loupe for RAW-only | one seek + one decode |
| 3 | Demosaiced RAW | 1:1 zoom on RAW-only frames only | expensive, on demand, deferred to v0.3 |

Tier 2 is the piece that makes RAW-only work without a demosaicer. Nearly every RAW format
embeds a large JPEG preview. Extraction, in an `internal/preview` package:

- **TIFF/IFD-based** (`CR2 NEF ARW DNG ORF PEF RW2 SRW`): walk the IFD chain, find the largest
  `JPEGInterchangeFormat` / `StripOffsets` entry, slice the bytes out. One parser covers most
  of the list.
- **RAF** (Fuji): custom container, but the header at a fixed offset stores the embedded JPEG's
  offset and length as big-endian uint32s. ~30 lines.
- **CR3** (Canon): ISO-BMFF/MP4 container. Separate box-walking parser. Lower priority.
- **Fallback**: if extraction fails, show a placeholder tile with the filename and a warning
  badge. Never block the grid, never crash. A frame you can't preview is still a frame you can
  move or delete.

An optional `cgo` build tag may enable LibRaw as a Tier-2/3 fallback for exotic formats. It must
not be required for a working default build.

Always apply EXIF orientation. Always honour the embedded ICC profile — pass original bytes
through to the webview for the loupe; only re-encode for cached thumbnails, and tag those sRGB.

### R4: keyboard-only operation

Hard requirement: every action reachable without a mouse, and no modal that traps focus.

| Key | Action |
|---|---|
| `←` `→` `↑` `↓` / `h j k l` | move focus in grid |
| `Tab` | toggle grid ↔ loupe |
| `Space` | toggle selection of focused group |
| `Shift`+arrows | extend selection from anchor |
| `Cmd/Ctrl+A` | select all in current filter |
| `Esc` | clear selection, or exit loupe |
| `1` | keep all |
| `2` | drop RAW, keep JPEG |
| `3` | drop JPEG, keep RAW |
| `4` | drop both |
| `0` | clear decision |
| `C` | copy destination palette |
| `M` | move destination palette |
| `Shift+C` / `Shift+M` then `1`–`9` | copy/move to MRU slot N, no confirmation |
| `F` | filter palette (by kind, decision, rating, flag) |
| `Z` | 1:1 zoom in loupe; hold to compare |
| `Enter` | apply pending decisions |
| `Cmd/Ctrl+Z` / `Shift+Cmd/Ctrl+Z` | undo / redo |
| `Cmd/Ctrl+K` | command palette (everything, fuzzy-searchable) |
| `?` | keymap overlay |

Keymap must be remappable via config. The command palette is the escape hatch that guarantees
R4 holds for any feature added later — if an action isn't in the palette, it doesn't ship.

### R5: system folders and network drives

- **Do not sandbox, do not ship via the Mac App Store.** Sandboxing plus security-scoped
  bookmarks makes arbitrary filesystem access painful and network volumes worse. Distribute a
  notarized DMG instead.
- Folder tree sidebar plus a path input that accepts typed absolute paths and `~`.
- Volume enumeration is platform-specific: `/Volumes` on macOS, drive letters and UNC paths on
  Windows, `/media` + `/mnt` + `gio mounts` on Linux. Abstract behind a `VolumeLister` interface.
- **Network drives are slow and can vanish mid-operation.** Non-negotiable consequences:
  - Every filesystem call is async with a timeout; the UI thread never blocks on `stat`.
  - Directory scanning is streamed and incremental — render tiles as they arrive, never wait
    for a full walk.
  - Hash verification defaults to **off** for non-local sources; reading every byte back over
    SMB is unusable at card sizes.
  - A disappeared volume produces a recoverable error and a banner, not a crash. In-flight
    operations halt and the journal records partial completion.
- Detect removable/read-only media and refuse to write to it by default. Never write a cache,
  a database, or an XMP sidecar to the card.

### R6: individual and bulk operations

Standard anchor/extend selection model. Action target resolution:

1. If a selection exists → the action applies to the whole selection.
2. If no selection exists → the action applies to the focused group only, and focus advances.

Rule 2 is what makes fast solo culling feel right: tap `2`, tap `2`, tap `4` and you're moving.

Bulk actions must show a confirmation summary when the selection exceeds a configurable
threshold (default 20) — but the confirmation is dismissible with `Enter`, keeping R4 intact.

A filter must be combinable with select-all, so "select every RAW-only frame with no decision
and drop the RAW" is three keystrokes.

### R7: operations

All operations route through one interface so that journaling, undo, and progress reporting are
implemented exactly once:

```go
type Op interface {
    Plan(groups []PhotoGroup) ([]FileAction, error) // pure, no side effects
    Describe() string                                // for the confirmation UI and journal
}

type FileAction struct {
    Verb   Verb   // Copy | Move | Trash
    Src    string
    Dst    string // "" for Trash
}
```

Ops to implement: `DropRAW`, `DropJPEG`, `DropBoth`, `KeepAll`, `CopyTo(dest)`, `MoveTo(dest)`,
`Rename(template)`. Adding an op later means implementing `Plan`, nothing else.

**Deletion is never `os.Remove`.** Default is the OS trash — Finder Trash on macOS, Recycle Bin
on Windows, XDG trash on Linux — so recovery is possible outside the app. A configurable
alternative moves rejects to a `_Rejected/` subfolder, which some users prefer for transparency.
A separate explicit "empty rejects" command does the only real destruction in the app, and it
shows a count breakdown (n RAW, n JPEG, n pairs, total bytes) before proceeding.

**Journal.** Every applied batch appends a record to an append-only JSON-lines journal:
batch id, timestamp, every `FileAction` with source and destination, and per-action outcome.
Undo replays the journal backwards. This is what makes it safe to cull fast at midnight, and it
is the single most important reliability feature in the app. Build it before building the UI.

Sidecars follow their parent file on every verb, always.

Collision policy on copy/move is explicit and configurable: `skip | rename-suffix | overwrite`,
defaulting to `rename-suffix`. Never silently overwrite.

### R8: destination memory

A `destinations` table in SQLite:

```
path, label, last_used_at, use_count, pinned (bool), slot (int 1-9, nullable)
```

Behaviour:
- The `C`/`M` palette lists pinned destinations first, then MRU by `last_used_at`, all
  fuzzy-searchable by typing. Typing a path that doesn't exist offers to create it.
- The nine most recent destinations auto-bind to slots 1–9 unless a slot is manually pinned.
  `Shift+C` `3` copies to slot 3 immediately. This is the feature that makes repeat sorting fast.
- Destinations support token templates expanded at execution time:
  `{date:2006-01-02}` `{camera}` `{lens}` `{stem}` `{ext}` `{shoot}` — Go time layout syntax.
  So a pinned destination can be `~/Pictures/{date:2006}/{date:2006-01-02}/` and stay useful forever.
- Drag-and-drop of a folder onto the window registers it as a destination. Non-essential, but
  it's how users discover the feature.

### R9: installability

This is a real cost line, not an afterthought. Budget for it:

- **macOS**: signed and notarized `.dmg`. Requires an Apple Developer account (~$99/yr).
  Unsigned builds are effectively unopenable for a non-technical user on current macOS.
- **Windows**: NSIS or MSI installer. Code-signing certificate needed or SmartScreen will warn
  on every download. An unsigned build is acceptable for v0.1 with a documented workaround.
- **Linux**: AppImage as primary, `.deb` secondary.
- **Engineers**: Homebrew cask on macOS, `winget` on Windows.
- **Auto-update**: a signed release feed checked on launch, with a visible changelog and an
  opt-out. Without this, users stay on v0.1 forever.

CI builds and signs all three platforms on tag push. Reproducible `make release`.

### R10: cross-platform

Wails covers the shell. The parts that will actually break:
- Path separators and case sensitivity — never string-concatenate paths, always `filepath.Join`,
  always normalise case for the grouping key only.
- Trash APIs differ per platform; one interface, three implementations, each tested.
- Volume enumeration differs per platform (see R5).
- Font rendering and default key modifiers (`Cmd` vs `Ctrl`) — abstract in the keymap layer.

Keep every one of these behind an interface in `internal/platform`. No `runtime.GOOS` checks
scattered through business logic.

### R11: geotagging (setting a location)

A frame off a camera with no GPS has no place on the map. This is how a user gives it one, in
the spirit of Immich: drop a pin, or copy the location from a frame that already has one. It sets
a location; it does not name one — there is no reverse geocoding here, and none is added.

Two ways to set a location, both acting on the current selection (or the focused frame when
nothing is selected):

- **Drop a pin in MAP.** Click a point on the map, or drag a frame's existing pin, and the
  selected frames take those coordinates. Setting a coordinate is entirely local — the map's two
  opt-in cloud exceptions are tiles and ask-first reverse geocoding, and neither is touched by
  writing a position the user chose.
- **Copy from another photo.** Pick a frame that already carries GPS and borrow its
  latitude/longitude/altitude onto the selection. This is how a whole shoot from a camera with no
  GPS gets placed from one tagged reference frame — a phone shot at the same spot, or an earlier
  frame that was tagged.

Writing goes through the **existing EXIF write plan** — the same `Plan`/`Apply`, journal, backup
and undo as every other metadata edit. A geotag is not special-cased:

- **JPEG**: the GPS tags are written into the file's EXIF in place. A JPEG with no GPS sub-IFD
  gets one created and IFD0's GPS pointer set to it — the same shape of change the writer already
  makes to add the EXIF sub-IFD for a capture time.
- **RAW**: never rewritten. The coordinates go to the frame's `.xmp` sidecar (`exif:GPSLatitude`,
  `exif:GPSLongitude`, `exif:GPSAltitude`), the same sidecar the rest of RAW metadata editing
  writes to.
- The card is never written to directly: the op engine stages the edited bytes, moves the
  original into backup, then copies the new bytes into place, in one journaled batch — so a
  geotag is undoable byte-for-byte like any apply.

The bulk of the work is in `internal/exif`, which today can only *strip* GPS. Setting it needs a
rational-value encoder (deg/min/sec as three `RATIONAL`s, ASCII `N/S/E/W` refs, the altitude
`RATIONAL` plus its `BYTE` above/below-sea-level ref) and the ability to create the GPS sub-IFD
on a frame that lacks one. `exif.Changes` gains GPS fields, `ExifEditDTO` and the editable-field
table gain the Location row, and the package's write and fuzz tests gate all of it. Removing a
location stays the existing `StripGPS` path. APP1 stays under its 64 KB ceiling — a GPS IFD is
small.

Testing: write coordinates then read back the same value; create the GPS IFD where none existed;
JPEG-in-place and RAW-sidecar parity; copy-from-another-frame produces the source's exact
coordinates; apply→undo restores byte-identical files; the fuzz gate stays green.

---

## 5. Performance

The thumbnail pipeline is the only place where performance is a design constraint rather than
an afterthought. Everything else can be naive.

Design:
- Worker pool sized to `runtime.NumCPU()`, decoding into a **content-addressed disk cache**
  keyed on `(sha256 of first 64KB + file size + mtime)`. Cache lives in the OS cache dir, has a
  configurable size cap, and evicts LRU.
- Two cached sizes: grid tile (long edge 512px) and loupe preview (long edge 2560px).
- The grid is virtualised — only visible tiles plus a screen of overscan are ever in memory.
- Decode priority follows the viewport: visible first, then scroll direction, cancel work for
  tiles that scrolled away.

**Benchmark gate for cgo.** Write the pure-Go pipeline first, then measure cold-cache
time-to-first-full-grid on 2,000 40MP JPEGs from a local SSD. If that exceeds 45 seconds, or
sustained scroll drops below 50fps with a warm cache, introduce libvips via `govips` for
shrink-on-load decoding (a 5–10× win on the decode step) behind a build tag, and accept the
packaging cost. Not before.

Acceptance targets:
- Cold open of a 2,000-frame folder: first tiles visible in under 1 second.
- Warm-cache scroll: 60fps sustained.
- Keystroke to decision-badge render: under 16ms. Decisions are database writes batched on a
  ticker, never synchronous with the keystroke.
- Apply cost follows the decisions, not the folder. Planning identifies only the frames the
  store holds a verdict or a destination for, so culling six frames out of two thousand costs
  six identity reads per pass and not two thousand. Both halves report progress, and the grid
  is patched from what the batch consumed rather than by reopening the folder.

---

## 6. Milestones

**v0.1 — the thing you actually use**
Journal and op engine (with tests) → directory scan and grouping → thumbnail pipeline and cache
→ virtualised grid → loupe → the four decision keys → apply → undo. Trash-based deletion only.
No copy/move destinations yet, no ingest, no filters.

Ship this, use it on one real card, then continue.

**v0.2** — copy/move with the destination palette and MRU slots (R8), filters, selection model
bulk ops (R6), EXIF panel, `_Rejected` folder mode.

**v0.3** — ingest from card with folder templates, verified copy, and simultaneous second-copy
to a backup destination. 1:1 zoom with Tier-3 demosaic for RAW-only frames. Compare mode.

**v0.4** — Windows and Linux builds, auto-update, XMP export. Geotagging (R11): drop-a-pin and
copy-from-another-frame, written through the EXIF plan.

---

## 7. Testing

The op engine and grouping logic must have real test coverage — this is code that deletes
people's photographs.

- Table-driven tests for grouping against a fixture tree covering every edge case in §3.1.
- `Plan()` is pure and returns a `[]FileAction`; assert the plan, not the filesystem.
- Filesystem integration tests in a temp dir: apply, assert, undo, assert the tree is byte-identical.
- Fuzz the preview extractor against truncated and corrupt RAW files. It must return an error,
  never panic, never read out of bounds.
- Simulate a vanishing volume mid-batch and assert the journal records partial completion and
  undo still works.

---

## 8. Resolved decisions

Previously open, now decided:

1. **Frontend: Svelte 5** with TypeScript and Vite. Preferred by the spec for lower per-cell
   overhead in the virtualised grid; no React dependency needed.
2. **`drop_jpeg` on `KindRAWOnly` frames: hide the key.** Pressing `3` on a RAW-only frame
   shows a brief toast ("no JPEG in this frame") instead of silently doing nothing, so muscle
   memory gets feedback. Same for `2` on JPEG-only frames.
3. **No rating/flag system in v0.1.** The four decision keys are the product. Revisit only if
   real use proves them insufficient.

---

## 9. CI and releases

GitHub Actions, two workflows:

**`ci.yml`** — on push to `main` and on pull requests:
- `go vet ./...` and `go test ./...` with race detector on Linux and macOS runners.
- Frontend: `npm ci && npm run check && npm run build`.
- No artefacts. Fast, required for merge.

**`release.yml`** — on tag push matching `v*`:
- Matrix: `macos-latest` (arm64 + amd64), `windows-latest` (amd64), `ubuntu-latest` (amd64).
- Each leg: install Go + Node + wails3 CLI, `wails3 package` for its platform.
- macOS: `.dmg` (unsigned until an Apple Developer account is set up; signing + notarisation
  step is stubbed behind a repo secret check so it activates when `APPLE_CERT` secrets exist).
- Windows: NSIS installer, unsigned for now with a documented SmartScreen workaround.
- Linux: AppImage.
- Artefacts uploaded to a **draft** GitHub release for the tag; publishing is manual.

Version comes from the tag. `make release` reproduces a local unsigned build of the current
platform.
