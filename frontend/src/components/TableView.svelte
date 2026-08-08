<script module lang="ts">
  // CULL's third sub-layout (⌥3): every value visible, sortable columns, a
  // preview of the focused frame on the right.
  //
  // The table sorts its own view of the frames. It never reorders the array it
  // is given: the grid, the filmstrip and the apply plan all read that array,
  // and a column click must not move a frame under them. So a row carries the
  // index it came from, and every callback speaks in those indices.

  import type { GroupDTO } from "../lib/bindings";
  import { verdictOf } from "../lib/verdict";

  /**
   * The keyboard's end of the table, registered by the instance the way the
   * loupe and the tree register theirs. Only one table is ever mounted; a
   * second instance would take the registration over.
   */
  export interface TableApi {
    /** Move focus by whole rows, in the order the table is showing them. */
    move(delta: number): void;
    /** Jump to the first or last row of the current sort. */
    moveTo(edge: "first" | "last"): void;
    /** `g` in the status bar: advance to the next sortable column. */
    cycleSort(): void;
    /** Reverse the column that is already sorted. */
    reverseSort(): void;
    /** What the sort currently is, for a status line that wants to say so. */
    sort(): { column: SortId; ascending: boolean };
  }

  export const table: TableApi = {
    move: () => {},
    moveTo: () => {},
    cycleSort: () => {},
    reverseSort: () => {},
    sort: () => ({ column: "shot", ascending: false }),
  };

  /** Columns that have something on GroupDTO to sort by. */
  export type SortId = "stem" | "pair" | "shot" | "rating" | "verdict";

  /** Where the table's own sort is remembered across launches. */
  const TABLE_SORT_KEY = "culler.tableSort";

  export interface TableSort {
    column: SortId;
    ascending: boolean;
  }

  interface Column {
    id: string;
    label: string;
    /** Right-aligned, per the design: every numeric column is. */
    numeric?: boolean;
    sort?: SortId;
  }

  /**
   * The drawn column set. Widths live in the stylesheet as custom properties so
   * the header and the rows cannot drift apart; this list is identity, label
   * and sortability only.
   *
   * shutter, ƒ, iso and lens are not on GroupDTO either; they are filled in
   * from the shared EXIF cache once the read for that row lands, which is why
   * they still cannot be sorted — a sort would have to reorder the table under
   * the user as reads came back. raw and jpeg have no source at all yet: file
   * sizes are not carried anywhere, so those two render an em-dash for good.
   * When the backend carries them, each one takes a `sort` id, a case in
   * `compare`, and a value in place of the dash in its cell.
   */
  const COLUMNS: Column[] = [
    { id: "thumb", label: "" },
    { id: "stem", label: "stem", sort: "stem" },
    { id: "pair", label: "pair", sort: "pair" },
    { id: "raw", label: "raw", numeric: true },
    { id: "jpeg", label: "jpeg", numeric: true },
    { id: "shot", label: "shot", numeric: true, sort: "shot" },
    { id: "shutter", label: "shutter", numeric: true },
    { id: "aperture", label: "ƒ", numeric: true },
    { id: "iso", label: "iso", numeric: true },
    { id: "lens", label: "lens" },
    { id: "rating", label: "rating", sort: "rating" },
    { id: "verdict", label: "verdict", sort: "verdict" },
  ];

  /** A value the app does not hold. Never a zero, never a guess. */
  const NO_DATA = "—";

  const SORTABLE: SortId[] = COLUMNS.map((c) => c.sort).filter((id): id is SortId => id !== undefined);

  /** DSCF1201 before DSCF1210, which a plain string sort gets wrong. */
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

  /** Paired frames first: the ones with a decision to make about both halves. */
  function pairRank(kind: string): number {
    switch (kind) {
      case "paired":
        return 0;
      case "raw-only":
        return 1;
      case "jpeg-only":
        return 2;
      default:
        return 3;
    }
  }

  /** Judged frames first, keeps before cuts, everything undecided last. */
  function verdictRank(g: GroupDTO): number {
    switch (verdictOf(g)) {
      case "keep":
        return 0;
      case "cut":
        return 1;
      default:
        return 2;
    }
  }

  /** Frames with no readable timestamp sort last, not to the top. */
  function shotValue(shot: string): number {
    const t = Date.parse(shot);
    return Number.isNaN(t) ? Number.POSITIVE_INFINITY : t;
  }

  function compare(a: GroupDTO, b: GroupDTO, column: SortId): number {
    switch (column) {
      case "stem":
        return collator.compare(a.stem, b.stem);
      case "pair":
        return pairRank(a.kind) - pairRank(b.kind);
      case "shot":
        return shotValue(a.shot) - shotValue(b.shot);
      case "rating":
        return a.rating - b.rating;
      case "verdict":
        return verdictRank(a) - verdictRank(b);
    }
  }

  export interface TableRow {
    /** Where the frame sits in the array the table was given. */
    index: number;
    group: GroupDTO;
  }

  /**
   * sortRows is pure: a new array of rows, the input untouched. Ties fall back
   * to the original order in both directions, so a sort is stable and reversing
   * it is not the same as shuffling the ties.
   */
  export function sortRows(groups: GroupDTO[], sort: TableSort): TableRow[] {
    const rows = groups.map((group, index) => ({ index, group }));
    const direction = sort.ascending ? 1 : -1;
    return rows.sort(
      (a, b) => compare(a.group, b.group, sort.column) * direction || a.index - b.index,
    );
  }

  /**
   * The timestamp is sliced rather than read through Date: it carries the
   * camera's own offset, and a frame shot at dusk must not move to the middle
   * of the afternoon because the laptop is in another zone.
   */
  const STAMP = /^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})/;

  /** The clock the frame was shot at, which is what the 68px column holds. */
  export function clockOf(shot: string): string {
    return STAMP.exec(shot)?.[2] ?? "";
  }

  /** Date and clock together, for the preview pane where there is room. */
  export function stampOf(shot: string): string {
    const m = STAMP.exec(shot);
    return m === null ? "" : `${m[1]} ${m[2]}`;
  }

  function basename(path: string): string {
    const parts = path.split(/[/\\]/);
    return parts[parts.length - 1] || path;
  }
</script>

<script lang="ts">
  // Virtualised the same way as the grid: the canvas is sized to the whole
  // table so the scrollbar tells the truth, and only the rows in view plus a
  // screen of overscan exist as DOM.

  import { exifCache, valueOf } from "../lib/exifcache.svelte";
  import { remember, stored } from "../lib/persist";
  import { previewURL } from "../lib/preview";
  import { queuedImage } from "../lib/imageQueue";
  import { groupKey } from "../lib/state.svelte";
  import { halfState, kindLabel, maskOf, verdictWord } from "../lib/verdict";
  import type { CutScope } from "../lib/verdict";

  /** Row pitch, hairline included — every row is box-sized to exactly this. */
  const ROW_H = 44;
  /** The column header, which sticks to the top of its own scroller. */
  const HEAD_H = 26;

  interface Props {
    /** The frames to show, in the order the rest of the app holds them. */
    groups: GroupDTO[];
    /** Which of those frames has focus, as an index into `groups`. */
    focusIndex: number;
    /** Asks for focus to move. The index is into `groups`, never a row number. */
    onFocus: (index: number) => void;
    /** A frame was opened — double click, or ⏎ on the focused row. */
    onActivate?: (index: number) => void;
    /** Selection membership, if the host tracks one. */
    isSelected?: (group: GroupDTO) => boolean;
    /** The 400px preview pane. Off when the shell supplies its own inspector. */
    preview?: boolean;
    /** How far a cut reaches, from the config. Decides what the R/J pair says. */
    cutRemoves?: CutScope;
  }

  let {
    groups,
    focusIndex,
    onFocus,
    onActivate,
    isSelected,
    preview = true,
    cutRemoves = "both",
  }: Props = $props();

  // Shot time, newest first — the grid's default, so switching layouts does
  // not turn the shoot upside down. The column headers re-sort from here, and
  // the chosen column and direction come back on the next mount and the next
  // launch alike; a stored column the table no longer sorts by falls back to
  // the default. Every consumer of table.sort() reads whatever the current
  // sort is, so a restored one needs nothing from them.
  let sort = $state<TableSort>(
    stored(
      TABLE_SORT_KEY,
      (raw) => {
        const [column, direction] = raw.split(":");
        if (!SORTABLE.includes(column as SortId)) return null;
        if (direction !== "asc" && direction !== "desc") return null;
        return { column: column as SortId, ascending: direction === "asc" };
      },
      { column: "shot", ascending: false },
    ),
  );

  function setSort(next: TableSort) {
    sort = next;
    remember(TABLE_SORT_KEY, `${next.column}:${next.ascending ? "asc" : "desc"}`);
  }

  let scroller = $state<HTMLDivElement | null>(null);
  let canvas = $state<HTMLDivElement | null>(null);
  let viewportHeight = $state(0);
  let scrollTop = $state(0);

  let rows = $derived(sortRows(groups, sort));
  /** Where the focused frame sits in the order being shown, not in `groups`. */
  let position = $derived(rows.findIndex((row) => row.index === focusIndex));
  let focused = $derived(groups[focusIndex] ?? null);

  let visible = $derived.by(() => {
    const screenRows = Math.max(1, Math.ceil(viewportHeight / ROW_H));
    const overscan = Math.ceil(screenRows / 2);
    const first = Math.max(0, Math.floor(scrollTop / ROW_H) - overscan);
    const last = Math.min(rows.length, first + screenRows + overscan * 2 + 1);
    const out: { row: TableRow; place: number; y: number }[] = [];
    for (let i = first; i < last; i++) {
      out.push({ row: rows[i], place: i, y: i * ROW_H });
    }
    return out;
  });

  // The four EXIF columns are filled from the cache, and the cache is only
  // asked about rows that exist. A folder of nine hundred frames is not nine
  // hundred file reads on open; it is one read per screenful actually scrolled
  // to, and nothing at all for the eight hundred never looked at.
  $effect(() => {
    exifCache.request(visible.map((v) => v.row.group));
    if (focused) exifCache.request([focused]);
  });

  function focusRow(place: number) {
    const row = rows[Math.max(0, Math.min(place, rows.length - 1))];
    if (row) onFocus(row.index);
  }

  table.move = (delta: number) => {
    if (rows.length === 0) return;
    focusRow((position === -1 ? 0 : position) + delta);
  };

  table.moveTo = (edge: "first" | "last") => {
    if (rows.length === 0) return;
    focusRow(edge === "first" ? 0 : rows.length - 1);
  };

  table.cycleSort = () => {
    const at = SORTABLE.indexOf(sort.column);
    setSort({ column: SORTABLE[(at + 1) % SORTABLE.length], ascending: true });
  };

  table.reverseSort = () => {
    setSort({ column: sort.column, ascending: !sort.ascending });
  };

  table.sort = () => sort;

  /** A header click sorts by that column, or reverses it if it already does. */
  function clickHeader(column: SortId) {
    setSort(column === sort.column ? { column, ascending: !sort.ascending } : { column, ascending: true });
  }

  // Focus has to drag the viewport with it, or arrowing past the last visible
  // row would move focus off screen. The header floats over the top of the
  // scroller, so a row is only clear of it once it is a header's height down.
  $effect(() => {
    const place = position;
    const el = scroller;
    if (!el || !canvas || place < 0) return;
    const top = canvas.offsetTop + place * ROW_H;
    if (top < el.scrollTop + HEAD_H) el.scrollTop = Math.max(0, top - HEAD_H);
    else if (top + ROW_H > el.scrollTop + el.clientHeight) {
      el.scrollTop = top + ROW_H - el.clientHeight;
    }
  });
</script>

<div class="table">
  <div
    class="scroller"
    bind:this={scroller}
    bind:clientHeight={viewportHeight}
    onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
  >
    <div class="sheet" role="grid" aria-label="Frames" aria-rowcount={rows.length + 1}>
      <div class="head" role="row" aria-rowindex="1" style:height="{HEAD_H}px">
        {#each COLUMNS as column (column.id)}
          <span
            class="cell c-{column.id}"
            class:num={column.numeric}
            class:sorted={column.sort !== undefined && column.sort === sort.column}
            role="columnheader"
            aria-sort={column.sort === sort.column ? (sort.ascending ? "ascending" : "descending") : "none"}
          >
            {#if column.sort !== undefined}
              {@const id = column.sort}
              <button type="button" class="sort" onclick={() => clickHeader(id)}>
                {column.label}{#if id === sort.column}{sort.ascending ? " ↑" : " ↓"}{/if}
              </button>
            {:else}
              {column.label}
            {/if}
          </span>
        {/each}
      </div>

      <div class="canvas" bind:this={canvas} style:height="{rows.length * ROW_H}px">
        {#each visible as { row, place, y } (groupKey(row.group))}
          {@const g = row.group}
          {@const url = previewURL(g)}
          {@const ex = exifCache.get(groupKey(g))}
          <!-- The camera's own capture time when the read has landed, and the
               time the scan recorded until it has. -->
          {@const clock = clockOf(valueOf(ex, "DateTimeOriginal") || g.shot)}
          {@const shutter = valueOf(ex, "ExposureTime")}
          {@const aperture = valueOf(ex, "FNumber")}
          {@const iso = valueOf(ex, "ISO")}
          {@const lens = valueOf(ex, "LensModel")}
          {@const r = halfState(g, "r", cutRemoves)}
          {@const j = halfState(g, "j", cutRemoves)}
          {@const verdict = verdictOf(g)}
          <button
            type="button"
            class="row"
            class:zebra={place % 2 === 1}
            class:focused={row.index === focusIndex}
            class:selected={isSelected?.(g) ?? false}
            style:height="{ROW_H}px"
            style:transform="translateY({y}px)"
            role="row"
            aria-rowindex={place + 2}
            aria-selected={isSelected?.(g) ?? false}
            tabindex="-1"
            onclick={() => onFocus(row.index)}
            ondblclick={() => onActivate?.(row.index)}
          >
            <span class="cell c-thumb" role="gridcell">
              <span class="well">
                {#if url !== ""}
                  <img use:queuedImage={url} alt={g.stem} decoding="async" />
                {/if}
              </span>
            </span>
            <span class="cell c-stem" role="gridcell" title={g.stem}>{g.stem}</span>
            <span class="cell c-pair" role="gridcell">
              <span class="half" class:kept={r === "kept"} class:cut={r === "cut"} class:absent={r === "absent"}>R</span>
              <span class="half" class:kept={j === "kept"} class:cut={j === "cut"} class:absent={j === "absent"}>J</span>
            </span>
            <!-- File sizes are not on GroupDTO and are not read; the cell says
                 so. The four EXIF cells say so too until their read lands. -->
            <span class="cell c-raw num absent" role="gridcell">{NO_DATA}</span>
            <span class="cell c-jpeg num absent" role="gridcell">{NO_DATA}</span>
            <span class="cell c-shot num" class:absent={clock === ""} role="gridcell">
              {clock === "" ? NO_DATA : clock}
            </span>
            <span class="cell c-shutter num" class:absent={shutter === ""} role="gridcell">
              {shutter === "" ? NO_DATA : shutter}
            </span>
            <span class="cell c-aperture num" class:absent={aperture === ""} role="gridcell">
              {aperture === "" ? NO_DATA : aperture}
            </span>
            <span class="cell c-iso num" class:absent={iso === ""} role="gridcell">
              {iso === "" ? NO_DATA : iso}
            </span>
            <span class="cell c-lens" class:absent={lens === ""} role="gridcell" title={lens}>
              {lens === "" ? NO_DATA : lens}
            </span>
            <span class="cell c-rating" role="gridcell" aria-label="{g.rating} of 5">
              {#each { length: g.rating } as _, i (i)}
                <span class="star"></span>
              {/each}
            </span>
            <span
              class="cell c-verdict"
              class:keep={verdict === "keep"}
              class:cut={verdict === "cut"}
              role="gridcell"
            >
              {verdictWord(verdict)}
            </span>
          </button>
        {/each}
      </div>
    </div>
  </div>

  {#if preview}
    <aside class="preview">
      <div class="stage">
        {#if focused}
          {@const url = previewURL(focused)}
          {#if url !== ""}
            <img use:queuedImage={url} alt={focused.stem} decoding="async" />
          {/if}
        {/if}
      </div>

      {#if focused}
        {@const stamp = stampOf(valueOf(exifCache.get(groupKey(focused)), "DateTimeOriginal") || focused.shot)}
        <div class="subject">
          <span class="name">{focused.stem}</span>
          <span class="spacer"></span>
          {#if verdictOf(focused) === "keep"}
            <span class="badge keep">KEEP · {maskOf(focused).toUpperCase()}</span>
          {:else if verdictOf(focused) === "cut"}
            <span class="badge cut">CUT</span>
          {/if}
        </div>

        <div class="sections">
          <section>
            <div class="label"><span>frame</span><span class="rule"></span></div>
            <div class="kv">
              <span class="k">shot</span>
              <span class="v" class:absent={stamp === ""}>{stamp === "" ? NO_DATA : stamp}</span>
            </div>
            <div class="kv"><span class="k">kind</span><span class="v">{kindLabel(focused.kind)}</span></div>
            <div class="kv">
              <span class="k">rating</span>
              {#if focused.rating > 0}
                <span class="v stars">
                  {#each { length: focused.rating } as _, i (i)}
                    <span class="star"></span>
                  {/each}
                </span>
              {:else}
                <span class="v absent">{NO_DATA}</span>
              {/if}
            </div>
            <div class="kv"><span class="k">sidecars</span><span class="v">{focused.sidecars}</span></div>
          </section>

          <section>
            <div class="label"><span>files</span><span class="rule"></span></div>
            <div class="kv">
              <span class="k">raw</span>
              <span class="v" class:absent={!focused.hasRaw} title={focused.rawPath}>
                {focused.hasRaw ? basename(focused.rawPath) : NO_DATA}
              </span>
            </div>
            <div class="kv">
              <span class="k">jpeg</span>
              <span class="v" class:absent={!focused.hasJpeg} title={focused.jpegPath}>
                {focused.hasJpeg ? basename(focused.jpegPath) : NO_DATA}
              </span>
            </div>
            <div class="kv"><span class="k">folder</span><span class="v" title={focused.dir}>{focused.dir}</span></div>
          </section>

          {#if (focused.warnings ?? []).length > 0}
            <section>
              <div class="label"><span>warnings</span><span class="rule"></span></div>
              {#each focused.warnings ?? [] as warning (warning)}
                <div class="kv"><span class="v warn" title={warning}>{warning}</span></div>
              {/each}
            </section>
          {/if}
        </div>
      {/if}
    </aside>
  {/if}
</div>

<style>
  .table {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
  }

  .scroller {
    flex: 1;
    min-width: 0;
    min-height: 0;
    overflow: auto;
    /* Makes the canvas measure its offset against this box, so the
       scroll-into-view maths sees the header and nothing above it. */
    position: relative;
  }

  /* One source for the column geometry: the header cells and the row cells
     both size from these, so they cannot drift apart. */
  .sheet {
    --w-thumb: 58px;
    --w-pair: 52px;
    --w-raw: 74px;
    --w-jpeg: 74px;
    --w-shot: 68px;
    --w-shutter: 62px;
    --w-aperture: 44px;
    --w-iso: 52px;
    --w-lens: 118px;
    --w-rating: 58px;
    --w-verdict: 86px;
    /* The fixed columns, their gutters and the padding come to 902px; the rest
       is the floor under the stem. Below this the whole table scrolls sideways
       rather than crushing a column into an ellipsis it was never drawn with.
       The floor stays under what the stem gets at the drawn 1440 window, so
       the design's own width never produces a horizontal scrollbar. */
    min-width: 1022px;
  }

  .head {
    position: sticky;
    top: 0;
    z-index: 1;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .head .sorted {
    color: var(--accent);
  }

  .sort {
    width: 100%;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    color: inherit;
    text-align: inherit;
    cursor: pointer;
  }

  .sort:hover {
    color: var(--text-muted);
  }

  .head .sorted .sort:hover {
    color: var(--accent);
  }

  .canvas {
    position: relative;
  }

  .row {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
    margin: 0;
    border: none;
    border-bottom: 1px solid var(--border-hair);
    background: transparent;
    font: inherit;
    font-size: 11.5px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
    appearance: none;
    outline: none;
  }

  .row.zebra {
    background: var(--bg-row-zebra);
  }

  /* The design draws no selected-but-unfocused row for this screen. This is the
     grid tile's selected border, moved to the edge the focused row marks. */
  .row.selected {
    box-shadow: inset 2px 0 0 var(--border-selected);
  }

  .row.focused {
    background: var(--bg-row-active);
    box-shadow: var(--focus-edge);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .num {
    text-align: right;
  }

  /* A value the app does not have. Never rendered as a zero or a guess. */
  .absent {
    color: var(--text-dead);
  }

  .c-thumb {
    flex: 0 0 var(--w-thumb);
  }
  .c-stem {
    flex: 1 1 120px;
    color: var(--text);
  }
  .c-pair {
    flex: 0 0 var(--w-pair);
    display: flex;
    gap: 2px;
    overflow: visible;
  }
  .c-raw {
    flex: 0 0 var(--w-raw);
  }
  .c-jpeg {
    flex: 0 0 var(--w-jpeg);
  }
  .c-shot {
    flex: 0 0 var(--w-shot);
  }
  .c-shutter {
    flex: 0 0 var(--w-shutter);
  }
  .c-aperture {
    flex: 0 0 var(--w-aperture);
  }
  .c-iso {
    flex: 0 0 var(--w-iso);
  }
  .c-lens {
    flex: 0 0 var(--w-lens);
  }
  .c-rating {
    flex: 0 0 var(--w-rating);
    display: flex;
    align-items: center;
    gap: 2px;
  }
  .c-verdict {
    flex: 0 0 var(--w-verdict);
    font-size: 10px;
    font-weight: 700;
  }

  .c-verdict.keep {
    color: var(--keep);
  }
  .c-verdict.cut {
    color: var(--cut);
  }

  .well {
    display: block;
    width: 48px;
    height: 32px;
    border-radius: 2px;
    overflow: hidden;
    background: var(--bg-thumb);
  }

  .well img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  /* Both halves are always drawn: an absent file is shown, not hidden. */
  .half {
    padding: 2px 3px;
    border-radius: 2px;
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
  }

  .half.kept {
    background: var(--keep-wash-20);
    color: var(--keep);
  }

  .half.cut {
    background: var(--cut-wash-22);
    color: var(--cut);
    text-decoration: line-through;
  }

  .half.absent {
    background: var(--absent-wash);
    color: var(--text-ghost);
  }

  .star {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--gold);
  }

  .preview {
    flex: 0 0 400px;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-pane);
    border-left: 1px solid var(--border);
  }

  .stage {
    flex: 0 0 auto;
    aspect-ratio: 3 / 2;
    display: grid;
    place-items: center;
    overflow: hidden;
    border-bottom: 1px solid var(--border);
    background: repeating-linear-gradient(58deg, #1c2026 0 8px, #23282f 8px 16px);
  }

  :global(:root[data-theme="light"]) .stage {
    background: repeating-linear-gradient(58deg, #dfe3ea 0 8px, #e9ecf1 8px 16px);
  }

  .stage img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .subject {
    flex: 0 0 auto;
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 12px;
    border-bottom: 1px solid var(--border);
  }

  .name {
    font-size: 14px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .badge {
    flex: 0 0 auto;
    font-size: 10.5px;
    font-weight: 700;
    white-space: nowrap;
  }

  .badge.keep {
    color: var(--keep);
  }

  .badge.cut {
    color: var(--cut);
  }

  .sections {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  section {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .kv {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-height: 20px;
    font-size: 11px;
  }

  .k {
    flex: 0 0 72px;
    color: var(--text-dim);
  }

  .v {
    flex: 1;
    min-width: 0;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .v.stars {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .v.warn {
    color: var(--amber);
  }
</style>
