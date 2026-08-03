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
