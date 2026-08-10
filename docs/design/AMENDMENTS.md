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

- **2026-08-08 — The catalogue indexes in two phases: list first, hash behind.** Adding a
  root used to hash every frame before the folder was usable, which on a cloud-synced folder
  forces a download per file and takes minutes with next to no feedback. An index pass now
  writes every row from the directory listing alone — paths, sizes, mtimes, kind,
  shot-from-mtime, an empty hash — so the tree, the counts and the search have the root within
  seconds; a second phase then re-reads only the unidentified frames, batched, and fills each
  row's identity in place, under the `NetworkHashWorkers` cap on network volumes. A reindex
  whose sizes and mtimes still match the rows reads no content, exactly as before. A frame the
  hashing has not reached is viewable and searchable but has no identity yet: recording a
  verdict on it is refused with the existing "no frame identity" message, and opening its
  folder hashes it immediately. Progress reports both phases on the catalogue event — the
  listing counts climb with no total, the hashing reports read-of-total as a measurement — and
  the UI shows them: the status-bar chip reads INDEXING n/m with a small bar, and a root's
  tree count dims until its listing lands.

- **2026-08-10 — `m` and `c` are two decisions, not two names for one.** The verb a routed
  frame travels by is recorded per frame beside its destination, in a `verb` column on the
  decisions table (`'' | move | copy`, cleared with the destination and with the verdict). Until
  now both keys wrote the same routing and the apply read `Behaviour.MoveOnImport` to decide
  what it meant, so pressing `m` under the default settings copied — the palette said move and
  the files stayed. The setting survives as the fallback for a route that names no verb, which
  is what every route recorded before this means. A plan therefore groups by destination *and*
  verb: one folder can be both copied and moved into, and shows as two rows in the apply summary
  and on the IMPORT routing table, ordered copies-first at the same destination. The import
  screen reads its verb off the plan rather than the setting and reports `mixed` when a card
  holds both; the backup leg is ordered by whether the plan actually moves anything, so a card
  routed with `m` under a copying default still has its second copy written before the original
  leaves. Free space is still weighed per destination folder, since that does not depend on the
  verb. The tile chip names the verb — amber and `⇥` for a move, accent and `→` for a copy — and
  the palette's "the card is never modified" chip becomes a warning when the palette is a move.
