# Design amendments

Decisions made after the spec was extracted; these override DESIGN-SPEC.md where they conflict.

- **2026-08-03 — LIBRARY merges into CULL; ⌃4 becomes IMPORT.** The catalogue stays but stops
  being a room: one catalogue-backed tree in the CULL sidebar (Sources), `/` searches the index
  in place, sessions are a sidebar Collections group. The mode bar becomes CULL · EXIF · MAP ·
  IMPORT. The governing workflow: navigate the library; plug in a card; review the card in CULL,
  routing frames to per-frame destinations (`m` palette, `⇧M`+digit slots — different frames go
  to different library folders, or everything to one); apply executes journaled, hash-verified
  copies into the library. IMPORT mode is that flow's overview: detected cards, routing summary,
  backup second copy, execute, storage view. Tickets #45–#49.

- **2026-08-03 — Tab cycles sub-layouts in every mode.** The `cycle-layout` action (Tab)
  cycles the current mode's `⌥1–3` sub-layouts in order, wrapping: CULL contact sheet →
  loupe-first → table; MAP pins → heat → track; LIBRARY search → sessions → storage; EXIF
  likewise once its sub-layouts exist. The cycle must read the active mode's layout list from
  shell state so new modes inherit it. `⌥1–3` still jump directly. The space loupe overlay is
  unchanged.

- **2026-08-07 — EXIF stops being a mode; editing moves into the PHOTOS inspector.** The
  mode bar becomes PHOTOS · MAP · IMPORT, `⌃1–3`, with MAP and IMPORT shifting up a slot.
  The inspector gains an Edit metadata section: the same four editable fields (capture time,
  artist, copyright, strip GPS) beside the read-only rows it already carries, editing the
  grid's action targets — the selection when there is one, the focused frame alone otherwise,
  with disagreeing values reading ⟨mixed⟩. Nothing about the write changes: every edit is a
  draft, the "N unwritten" chip counts them in the title bar, and `⌘S` (now answered in
  PHOTOS) puts up the same write plan — backup, journal, RAW via sidecar, JPEG in place,
  undoable — that screens 3a–3d specified. Draft scoping is unchanged too: a folder change
  discards drafts, a search opened and closed keeps them, drafts typed during a search go
  with it. Geotagging stays in MAP. The `write-metadata` binding keeps `mod+s`; a saved
  config still naming the retired `mode-exif` action loads fine and the dead entry is
  dropped so stock `⌃2` reaches MAP.
