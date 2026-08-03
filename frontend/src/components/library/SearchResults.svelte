<script module lang="ts">
  // The search results grid (5a): frames grouped by shoot, newest first, in a
  // six-column tile grid.
  //
  // Virtualised the way the contact sheet and the table are: the canvas is
  // sized to the whole result so the scrollbar tells the truth, and only the
  // rows in view plus a screen of overscan exist as DOM. A row here is either a
  // group header or one line of tiles, which is what lets the grouping survive
  // virtualisation — the alternative, a grid of everything with sticky headers,
  // would need every frame in the DOM to know where the headers go.

  import type { CatalogFrame } from "../../lib/library.svelte";

  /**
   * The keyboard's end of the results grid, registered by the instance the way
   * the table registers its own. Only one grid is ever mounted.
   */
  export interface ResultsApi {
    /** Move focus by whole tiles, in the order the grid is showing them. */
    move(delta: number): void;
    /** Move focus by whole rows. */
    moveRow(delta: number): void;
    /** Jump to the first or last result. */
    moveTo(edge: "first" | "last"): void;
    /** How many tiles are on a row, for a caller that wants to say so. */
    columns(): number;
  }

  export const results: ResultsApi = {
    move: () => {},
    moveRow: () => {},
    moveTo: () => {},
    columns: () => 1,
  };

  /** Header height, tile footer height and the gaps, all from the design. */
  const HEADER_H = 30;
  const GAP = 8;
  const FOOTER_H = 19;
  /** Below this a tile is too small to judge a photograph from. */
  const MIN_TILE = 118;
  /** The design draws six columns at the window it was drawn at. */
  const MAX_COLUMNS = 6;

  interface HeaderRow {
    kind: "header";
    label: string;
    meta: string;
    y: number;
    height: number;
  }

  interface TileRow {
    kind: "tiles";
    /** Indices into the flat result list, left to right. */
    indices: number[];
    y: number;
    height: number;
  }

  export type Row = HeaderRow | TileRow;

  export interface Layout {
    rows: Row[];
    height: number;
    columns: number;
    tileWidth: number;
    tileHeight: number;
  }

  /** The date part of an RFC3339 stamp, which is what a shoot is grouped on. */
  function dayOf(shot: string): string {
    return shot.slice(0, 10);
  }

  /**
   * layOut turns a flat result list into positioned rows. Pure, so the maths
   * can be reasoned about without a DOM: given the same width and frames it
   * always produces the same layout.
   *
   * Frames are grouped by the day they were shot. The results arrive newest
   * first, so the days are already contiguous and a single pass finds them.
   */
  export function layOut(frames: CatalogFrame[], width: number): Layout {
    const columns = Math.max(1, Math.min(MAX_COLUMNS, Math.floor((width + GAP) / (MIN_TILE + GAP))));
    const tileWidth = Math.max(MIN_TILE, Math.floor((width - GAP * (columns - 1)) / columns));
    const tileHeight = Math.round(tileWidth / 1.5) + FOOTER_H;

    const perDay = new Map<string, number>();
    for (const frame of frames) {
      const key = dayOf(frame.shot);
      perDay.set(key, (perDay.get(key) ?? 0) + 1);
    }

    const rows: Row[] = [];
    let y = 0;
    let day = "";
    let run: number[] = [];

    const flushRun = () => {
      while (run.length > 0) {
        const indices = run.splice(0, columns);
        rows.push({ kind: "tiles", indices, y, height: tileHeight });
        y += tileHeight + GAP;
      }
    };

    frames.forEach((frame, index) => {
      const shotDay = dayOf(frame.shot);
      if (shotDay !== day) {
        flushRun();
        day = shotDay;
        const count = perDay.get(shotDay) ?? 0;
        rows.push({
          kind: "header",
          label: shotDay === "" ? "no date" : shotDay,
          meta: `${count} frame${count === 1 ? "" : "s"}`,
          y,
          height: HEADER_H,
        });
        y += HEADER_H;
      }
      run.push(index);
    });
    flushRun();

    return { rows, height: Math.max(0, y - GAP), columns, tileWidth, tileHeight };
  }
</script>

<script lang="ts">
  import LibraryTile from "./LibraryTile.svelte";
  import { library } from "../../lib/library.svelte";

  let scroller = $state<HTMLDivElement | null>(null);
  let width = $state(0);
  let viewportHeight = $state(0);
  let scrollTop = $state(0);

  let layout = $derived(layOut(library.results, Math.max(1, width)));

  let visible = $derived.by(() => {
    const overscan = viewportHeight;
    const top = scrollTop - overscan;
    const bottom = scrollTop + viewportHeight + overscan;
    return layout.rows.filter((row) => row.y + row.height >= top && row.y <= bottom);
  });

  results.columns = () => layout.columns;

  results.move = (delta: number) => {
    library.focus(library.focusIndex + delta);
  };

  results.moveRow = (delta: number) => {
    library.focus(library.focusIndex + delta * layout.columns);
  };

  results.moveTo = (edge: "first" | "last") => {
    library.focus(edge === "first" ? 0 : library.results.length - 1);
  };

  /**
   * The next page is asked for a screen before the end, so the grid fills as
   * the user arrives rather than after they have stopped.
   */
  function onScroll(event: Event & { currentTarget: HTMLDivElement }) {
    scrollTop = event.currentTarget.scrollTop;
    if (scrollTop + viewportHeight > layout.height - viewportHeight) {
      void library.loadMore();
    }
  }

  // Focus drags the viewport with it, or arrowing past the last visible tile
  // would move focus off screen.
  $effect(() => {
    const index = library.focusIndex;
    const el = scroller;
    if (!el) return;
    const row = layout.rows.find((r) => r.kind === "tiles" && r.indices.includes(index));
    if (!row) return;
    if (row.y < el.scrollTop) el.scrollTop = Math.max(0, row.y - HEADER_H);
    else if (row.y + row.height > el.scrollTop + el.clientHeight) {
      el.scrollTop = row.y + row.height - el.clientHeight;
    }
  });
</script>

<div
  class="results"
  bind:this={scroller}
  bind:clientWidth={width}
  bind:clientHeight={viewportHeight}
  onscroll={onScroll}
>
  {#if library.results.length === 0}
    <p class="empty">
      {#if library.error !== null}
        {library.error}
      {:else if !library.searched}
        the catalogue has not been searched yet
      {:else if library.roots.length === 0}
        no folders are catalogued — add one on the left
      {:else}
        nothing matches
      {/if}
    </p>
  {:else}
    <div class="canvas" style:height="{layout.height}px" role="listbox" aria-label="Results">
      {#each visible as row (row.kind === "header" ? `h${row.y}` : `r${row.y}`)}
        {#if row.kind === "header"}
          <div class="group" style:transform="translateY({row.y}px)" style:height="{row.height}px">
            <span class="name">{row.label}</span>
            <span class="meta">{row.meta}</span>
            <span class="rule"></span>
          </div>
        {:else}
          {#each row.indices as index, column (library.results[index].hash)}
            <LibraryTile
              frame={library.results[index]}
              {index}
              focused={index === library.focusIndex}
              width={layout.tileWidth}
              height={layout.tileHeight}
              x={column * (layout.tileWidth + GAP)}
              y={row.y}
              onfocus={(i) => library.focus(i)}
              onopen={(i) => library.open(library.results[i])}
            />
          {/each}
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .results {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 10px 12px;
  }

  .canvas {
    position: relative;
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .group {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .name {
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
  }

  .meta {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }
</style>
