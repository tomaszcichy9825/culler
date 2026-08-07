<script lang="ts">
  // The contact sheet: a pane header, then the frames.
  //
  // Hand-rolled virtualisation: the canvas is sized to the full grid so the
  // scrollbar is honest, but only the rows within view plus a screen of
  // overscan are rendered, each one positioned absolutely. A card of 2,000
  // frames therefore costs the same number of DOM nodes as a folder of 30.
  //
  // Columns are chosen rather than measured — the design fixes five, and the
  // − and + keycaps change that number — so the tiles divide whatever width
  // the pane has between them.

  import { library } from "../lib/library.svelte";
  import { app, groupKey } from "../lib/state.svelte";
  import Tile from "./Tile.svelte";

  const GAP = 10;
  const PADDING = 12;
  /** The stem row beneath every thumbnail. */
  const FOOTER_H = 22;
  /** The tile's own 1px border, top and bottom. */
  const BORDER = 2;
  /** The thumbnail's aspect ratio, as a height multiplier. */
  const THUMB_RATIO = 2 / 3;

  let scroller = $state<HTMLDivElement | null>(null);
  let canvas = $state<HTMLDivElement | null>(null);
  let canvasWidth = $state(0);
  let viewportHeight = $state(0);
  let scrollTop = $state(0);

  let cols = $derived(app.gridColumns);
  let colWidth = $derived(Math.max(1, (canvasWidth - GAP * (cols - 1)) / cols));
  // The height is computed rather than left to the aspect ratio alone: the row
  // maths and the DOM have to agree exactly, or the scrollbar starts lying.
  let tileHeight = $derived(Math.round((colWidth - BORDER) * THUMB_RATIO) + FOOTER_H + BORDER);
  let rowHeight = $derived(tileHeight + GAP);
  let rows = $derived(Math.ceil(app.groups.length / cols));
  let screenRows = $derived(Math.max(1, Math.ceil(viewportHeight / rowHeight)));
  /** The last row carries no trailing gap, so the canvas does not claim one. */
  let canvasHeight = $derived(rows === 0 ? 0 : rows * rowHeight - GAP);

  let visible = $derived.by(() => {
    if (canvasWidth === 0) return [];
    const overscan = Math.ceil(screenRows / 2);
    const first = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
    const last = Math.min(rows, first + screenRows + overscan * 2 + 1);
    const out: { index: number; key: string; x: number; y: number }[] = [];
    for (let i = first * cols; i < Math.min(app.groups.length, last * cols); i++) {
      out.push({
        index: i,
        key: groupKey(app.groups[i]),
        x: (i % cols) * (colWidth + GAP),
        y: Math.floor(i / cols) * rowHeight,
      });
    }
    return out;
  });

  let counts = $derived(app.verdictCounts);

  $effect(() => {
    app.cols = cols;
  });

  // Keyboard focus has to drag the viewport along with it, or arrowing past
  // the last visible row would move focus off screen. Rows are positioned
  // within the canvas, which sits below the scroller's own top padding, so the
  // offset has to come from the element rather than from the row maths alone.
  $effect(() => {
    const index = app.focusIndex;
    const columns = cols;
    const step = rowHeight;
    const el = scroller;
    if (!el || !canvas || app.groups.length === 0) return;
    const top = canvas.offsetTop + Math.floor(index / columns) * step;
    const bottom = top + tileHeight;
    if (top < el.scrollTop) el.scrollTop = Math.max(0, top - GAP);
    else if (bottom > el.scrollTop + el.clientHeight) el.scrollTop = bottom + GAP - el.clientHeight;
  });

  function open(index: number) {
    app.setFocus(index);
    app.view = "loupe";
  }

  // While the grid is showing search results it is showing one page of them,
  // and the index holds the rest: nearing the end of the sheet — by scrollbar
  // or by arrowing, which drags the viewport along — asks for the next page,
  // until the catalogue says it is complete.
  //
  // Proximity is measured against the UNFILTERED result list. The canvas
  // height describes the filtered view of the results, but library.complete
  // counts the results themselves — so with a filter hiding rows the bottom
  // of the short sheet stayed "near the end" forever and this effect paged
  // the entire catalogue in a tight loop. Only when the visible list is the
  // full result list does the bottom of the canvas mean "near the end of
  // what is loaded"; under a narrowing filter nothing auto-pages. Lifting the
  // filter re-arms the latch (the mismatch branch resets it), so paging
  // resumes as soon as the full sheet shows its bottom again.
  //
  // And a page is asked for at most once per scroll position: the next one
  // needs the scrollTop to actually move, so a page that comes back without
  // growing the sheet cannot re-arm the condition it just satisfied.
  //
  // The latch is measured against one result list, so it resets when that
  // list is replaced (library.generation — a fresh query keeps the stale
  // canvas and scrollTop for a beat, and a latch left armed there blocked
  // the new search's first auto-page until a manual scroll) and on the
  // branches where its premise is gone: a filter narrowing the sheet or a
  // canvas not laid out yet, where the scroll position it recorded no longer
  // describes the sheet it will next measure. It survives `loading` on
  // purpose — that is the in-flight page it itself asked for, and forgetting
  // it there would re-arm the condition a fruitless page just satisfied.
  let pagedAt = -1;
  let pagedGeneration = -1;
  $effect(() => {
    if (library.generation !== pagedGeneration) {
      pagedGeneration = library.generation;
      pagedAt = -1;
    }
    if (!library.searchOpen || library.loading || library.complete) return;
    if (app.groups.length !== library.results.length) {
      pagedAt = -1;
      return;
    }
    if (canvasHeight === 0) {
      pagedAt = -1;
      return;
    }
    if (scrollTop + viewportHeight < canvasHeight - rowHeight) {
      pagedAt = -1;
      return;
    }
    if (scrollTop === pagedAt) return;
    pagedAt = scrollTop;
    void library.loadMore();
  });
</script>

<header class="sheet-head">
  <!-- The scan returns frames in stem order and nothing yet re-sorts them, so
       this says what the sheet is actually showing rather than what it will
       show once there is a sort control. -->
  <span class="key">sorted by</span><span class="value">name ↑</span>
  <span class="rule"></span>
  <span class="key">verdicts</span>
  <span class="legend"><span class="swatch keep"></span><span class="value">{counts.keep} keep</span></span>
  <span class="legend"><span class="swatch cut"></span><span class="value">{counts.cut} cut</span></span>
  <span class="legend">
    <span class="swatch undecided"></span><span class="value">{counts.undecided} undecided</span>
  </span>
  <span class="spacer"></span>
  <span class="rule"></span>
  <span class="sizer">
    <button type="button" class="cap" onclick={() => app.nudgeColumns(1)} title="Smaller tiles">−</button>
    <button type="button" class="cap" onclick={() => app.nudgeColumns(-1)} title="Bigger tiles">+</button>
    <span>tile size</span>
  </span>
</header>

<div
  class="scroller"
  bind:this={scroller}
  bind:clientHeight={viewportHeight}
  onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
>
  <div
    class="canvas"
    bind:this={canvas}
    bind:clientWidth={canvasWidth}
    style:height="{canvasHeight}px"
    role="listbox"
    aria-label="Frames"
    aria-multiselectable="true"
  >
    {#each visible as tile (tile.key)}
      <Tile
        group={app.groups[tile.index]}
        index={tile.index}
        focused={tile.index === app.focusIndex}
        selected={app.isSelected(app.groups[tile.index])}
        width={colWidth}
        height={tileHeight}
        x={tile.x}
        y={tile.y}
        onfocus={(i) => app.setFocus(i)}
        onopen={open}
      />
    {/each}
  </div>
</div>

<style>
  .sheet-head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 32px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
    overflow: hidden;
  }

  .key {
    color: var(--text-muted);
  }

  .value {
    color: var(--text);
  }

  .rule {
    color: var(--border-strong);
  }

  .rule::before {
    content: "|";
  }

  .legend {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .swatch {
    width: 7px;
    height: 7px;
    border-radius: 2px;
  }

  .swatch.keep {
    background: var(--keep);
  }

  .swatch.cut {
    background: var(--cut);
  }

  .swatch.undecided {
    background: var(--undecided);
  }

  .spacer {
    flex: 1;
  }

  .sizer {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }

  .cap {
    padding: 1px 5px;
    border: none;
    border-radius: 3px;
    background: var(--bg-kbd);
    color: var(--text-muted);
    font: inherit;
    font-size: 11px;
    line-height: 1.4;
    cursor: pointer;
  }

  .cap:hover {
    color: var(--text-hi);
  }

  .scroller {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 12px;
    /* Makes the scroller the canvas's offset parent, so the scroll-into-view
       maths measures the padding rather than the whole page above it. */
    position: relative;
  }

  .canvas {
    position: relative;
    width: 100%;
  }
</style>
