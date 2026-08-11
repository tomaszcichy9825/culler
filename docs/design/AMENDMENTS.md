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

- **2026-08-10 — The catalogue catches up at launch, and Sessions has a size floor.** Roots were
  indexed when they were added and never again: nothing in the app called `Reindex` except
  `addRoot`, so the catalogue was a snapshot of the day each root was registered and said so
  nowhere. Every shoot filed since was missing from the tree's counts, from search, and from
  Sessions — the one view whose whole job is to list what happened. A background pass now runs
  over every registered root once at launch, and a `rescan` button beside `+ add` starts one on
  demand; both report through the existing progress chip. The pass is cheap on an unchanged
  folder — the listing phase walks names and the hashing phase re-reads only frames whose size
  or mtime moved — so catching up costs a directory walk. Separately, `Behaviour.MinSessionFrames`
  (default 5, minimum 1) is the smallest shoot the Sessions list shows: at four hours' gap a real
  library turns every stray frame into a session of its own, and on a library of ten thousand
  frames those fragments outnumber the shoots two to one. The floor filters the list and nothing
  else — no frame is hidden from the grid, the search or the tree by it, and changing it costs a
  query rather than a reindex. Because a filtered list that looks complete is exactly how a shoot
  goes missing unnoticed, `Sessions` returns how many it hid and at what floor, and the sidebar
  header shows "+N small" beside the count.

- **2026-08-11 — Shot time is the capture time, not the file's mtime.** `PhotoGroup.Shot` was
  documented as "EXIF DateTimeOriginal, falls back to mtime" and only the fallback was ever
  built: the walk reads no file, so every shot time in the catalogue was the day the file was
  last written. On a library assembled by copying folders about, that is the day of the copy —
  so a shoot from the 2nd read as a shoot on the 8th, and a folder copied in one go collapsed
  into a "session" as long as the copy took (28 photographs over four minutes became a
  39-second session a week later). Sessions cannot work on that, and neither can the grid's
  sort-by-shot. The catalogue now reads the capture time in the hashing phase, where the file is
  already open — the hash wants the whole file and the capture time wants its head, so reading
  them separately would touch every photograph twice. A frame carrying no capture time keeps the
  file's time, because the grid has to sort it somewhere. `frames.shot_source` records which of
  the two a row holds (`exif` | `mtime`), and empty marks a row written before any of this. Those
  rows are repaired on the next pass by a header-only read — the identity in them is still good,
  so re-hashing a whole library over a network share to correct a timestamp is not needed — and
  once a row says where its time came from it is never repaired again. The opened-folder grid
  still sorts on the walk's mtime; that path is next.

- **2026-08-11 — The opened folder gets the capture time too.** The catalogue's shot times were
  corrected the day before; the grid was still sorting a folder you had actually opened by its
  files' mtimes, because that path is the walk's rather than the catalogue's. The identity pass
  already opens every frame's primary file, so the capture time is read there and rides back on
  the same `scan:hashed` patch that carries the hash and the recorded decision — `FrameHash.Shot`,
  RFC3339, empty when the file carried none, in which case the mtime the frame was painted with
  stands. `exif.ReadCaptureTime` is the single reader both the catalogue and the folder open
  call, so "no capture time" means the same thing in both. The same patch was also dropping the
  routing verb on the floor, which it has carried since verbs existed: a reopened folder showed
  a routed frame as a copy however it had been routed.

- **2026-08-11 — A folder loads into the order it will be shown in.** Opening a folder painted
  tiles that then moved: batches arrived name-ascending and were sorted into a newest-first
  sheet, so each one landed above what was already there and pushed the page down, and — since
  the capture-time fix earlier the same day — every frame's sort key changed again when its
  identity landed behind it. Two things settle it. The batch's capture times are read before the
  batch is painted, so a tile arrives carrying the time it will be ordered by rather than its
  file's mtime; this is the one read a streamed open now makes before showing anything, and it
  is a header read across the same workers the hashes use. And the walk takes a direction:
  `StreamOptions.Descending` hands the stems over backwards, which `OpenFolderStream` is told to
  do when the grid is sorted newest-first. The walk cannot know when a photograph was taken, but
  it holds the whole listing before it emits anything and camera filenames run in shooting
  order, so walking them backwards puts the newest frames on screen first and the page grows
  downwards. The identity pass no longer reads the capture time — the frame already carries it,
  so that would be a second read of the same file.
