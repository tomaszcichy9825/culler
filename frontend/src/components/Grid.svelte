<script lang="ts">
  // Hand-rolled virtualisation: the canvas is sized to the full grid so the
  // scrollbar is honest, but only the rows within view plus a screen of
  // overscan are rendered, each one positioned absolutely. A card of 2,000
  // frames therefore costs the same number of DOM nodes as a folder of 30.

  import { app, groupKey } from "../lib/state.svelte";
  import { decisionBadge, kindBadge, previewURL } from "../lib/preview";

  /** Target tile width; the real width divides the container evenly. */
  const TILE_W = 200;
  const IMG_H = 150;
  const LABEL_H = 30;
  const GAP = 10;
  const TILE_H = IMG_H + LABEL_H;
  const ROW_H = TILE_H + GAP;

  let scroller = $state<HTMLDivElement | null>(null);
  let canvas = $state<HTMLDivElement | null>(null);
  let canvasWidth = $state(0);
  let viewportHeight = $state(0);
  let scrollTop = $state(0);

  let cols = $derived(Math.max(1, Math.floor((canvasWidth + GAP) / (TILE_W + GAP))));
  let colWidth = $derived((canvasWidth - GAP * (cols - 1)) / cols);
  let rows = $derived(Math.ceil(app.groups.length / cols));
  let screenRows = $derived(Math.max(1, Math.ceil(viewportHeight / ROW_H)));

  let visible = $derived.by(() => {
    if (canvasWidth === 0) return [];
    const overscan = Math.ceil(screenRows / 2);
    const first = Math.max(0, Math.floor(scrollTop / ROW_H) - overscan);
    const last = Math.min(rows, first + screenRows + overscan * 2 + 1);
    const out: { index: number; key: string; x: number; y: number }[] = [];
    for (let i = first * cols; i < Math.min(app.groups.length, last * cols); i++) {
      out.push({
        index: i,
        key: groupKey(app.groups[i]),
        x: (i % cols) * (colWidth + GAP),
        y: Math.floor(i / cols) * ROW_H,
      });
    }
    return out;
  });

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
    const el = scroller;
    if (!el || !canvas || app.groups.length === 0) return;
    const top = canvas.offsetTop + Math.floor(index / columns) * ROW_H;
    const bottom = top + TILE_H;
    if (top < el.scrollTop) el.scrollTop = Math.max(0, top - GAP);
    else if (bottom > el.scrollTop + el.clientHeight) el.scrollTop = bottom + GAP - el.clientHeight;
  });
</script>

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
    style:height="{rows * ROW_H}px"
    role="listbox"
    aria-label="Frames"
    aria-multiselectable="true"
  >
    {#each visible as tile (tile.key)}
      {@const g = app.groups[tile.index]}
      {@const url = previewURL(g)}
      <button
        type="button"
        class="tile"
        class:focused={tile.index === app.focusIndex}
        class:selected={app.isSelected(g)}
        style:width="{colWidth}px"
        style:height="{TILE_H}px"
        style:transform="translate({tile.x}px, {tile.y}px)"
        role="option"
        tabindex="-1"
        aria-selected={app.isSelected(g)}
        onclick={() => app.setFocus(tile.index)}
        ondblclick={() => {
          app.setFocus(tile.index);
          app.view = "loupe";
        }}
      >
        <div class="thumb" style:height="{IMG_H}px">
          {#if url !== ""}
            <img src={url} alt={g.stem} loading="lazy" decoding="async" />
          {:else}
            <span class="missing">no preview</span>
          {/if}
          <span class="badge kind">{kindBadge(g.kind)}</span>
          {#if g.decision !== "" && g.decision !== "none"}
            <span class="badge decision {g.decision}">{decisionBadge(g.decision)}</span>
          {/if}
          {#if (g.warnings ?? []).length > 0}
            <span class="badge warn" title={(g.warnings ?? []).join("; ")}>!</span>
          {/if}
        </div>
        <div class="label" style:height="{LABEL_H}px" title={g.stem}><span>{g.stem}</span></div>
      </button>
    {/each}
  </div>
</div>

<style>
  .scroller {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 10px 14px 14px;
    /* Makes the scroller the canvas's offset parent, so the scroll-into-view
       maths measures the padding rather than the whole page above it. */
    position: relative;
  }

  .canvas {
    position: relative;
    width: 100%;
  }

  .tile {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    border-radius: 6px;
    overflow: hidden;
    background: var(--bg-tile);
    border: 1px solid transparent;
    cursor: pointer;
    padding: 0;
    margin: 0;
    font: inherit;
    color: inherit;
    text-align: left;
    appearance: none;
    outline: none;
  }

  .tile.selected {
    background: var(--selected-bg);
    border-color: var(--selected-border);
  }

  .tile.focused {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--focus-ring);
  }

  .thumb {
    position: relative;
    display: grid;
    place-items: center;
    background: var(--bg-sunken);
    overflow: hidden;
  }

  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .missing {
    font-size: 11px;
    color: var(--text-faint);
    padding: 0 8px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge {
    position: absolute;
    display: inline-grid;
    place-items: center;
    min-width: 17px;
    /* A badge never grows past its corner of the tile, whatever it holds. */
    max-width: calc(50% - 8px);
    height: 17px;
    padding: 0 4px;
    border-radius: 4px;
    font-size: 11px;
    font-weight: 700;
    line-height: 1;
    color: var(--on-decision);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kind {
    top: 4px;
    left: 4px;
    /* Inverted rather than a fixed grey, so it reads on either theme. */
    background: var(--text-muted);
    color: var(--bg);
  }

  .warn {
    top: 4px;
    left: 25px;
    background: var(--warn);
  }

  .decision {
    top: 4px;
    right: 4px;
  }

  .decision.keep_all {
    background: var(--keep-all);
  }
  .decision.drop_raw {
    background: var(--drop-raw);
  }
  .decision.drop_jpeg {
    background: var(--drop-jpeg);
  }
  .decision.drop_all {
    background: var(--drop-all);
  }

  .label {
    display: flex;
    align-items: center;
    padding: 0 7px;
    min-width: 0;
    font-size: 11px;
    color: var(--text-muted);
  }

  /* The ellipsis has to live on the text's own box: a flex container will not
     truncate an anonymous text item. */
  .label span {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
