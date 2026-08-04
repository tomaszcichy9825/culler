<script lang="ts">
  // One frame on the contact sheet.
  //
  // Both halves of the pair are always drawn. A frame with no RAW says so with
  // a dead R rather than by leaving a gap, because a missing badge and a badge
  // that is not there read the same at a glance and mean opposite things.

  import { queuedImage } from "../lib/imageQueue";
  import { leafOf } from "../lib/palette.svelte";
  import { previewURL } from "../lib/preview";
  import { app } from "../lib/state.svelte";
  import { halfState, HALVES, maskOf, verdictOf, verdictWord } from "../lib/verdict";
  import type { GroupDTO } from "../lib/bindings";

  interface Props {
    group: GroupDTO;
    /** Position in the grid, for the click handlers to report back. */
    index: number;
    focused: boolean;
    selected: boolean;
    width: number;
    height: number;
    /** Where the tile sits in the virtualised canvas. */
    x: number;
    y: number;
    onfocus: (index: number) => void;
    onopen: (index: number) => void;
  }

  let { group, index, focused, selected, width, height, x, y, onfocus, onopen }: Props = $props();

  let url = $derived(previewURL(group, "grid"));
  let loaded = $state(false);
  let verdict = $derived(verdictOf(group));
  let stars = $derived(Math.max(0, group.rating));
  let warnings = $derived(group.warnings ?? []);
  // Where this frame is going, if anywhere. The leaf is all a tile has room
  // for; the full path is on the title, which is also what a screenshot of a
  // routed sheet needs to be checkable.
  let destination = $derived(group.destination ?? "");
</script>

<button
  type="button"
  class="tile"
  class:focused
  class:selected
  style:width="{width}px"
  style:height="{height}px"
  style:transform="translate({x}px, {y}px)"
  role="option"
  tabindex="-1"
  aria-selected={selected}
  data-verdict={verdict}
  data-mask={maskOf(group)}
  data-rating={stars}
  onclick={() => onfocus(index)}
  ondblclick={() => onopen(index)}
>
  <div class="thumb">
    {#if url !== ""}
      {#if !loaded}
        <span class="fetching" aria-hidden="true"></span>
      {/if}
      <img
        use:queuedImage={url}
        alt={group.stem}
        decoding="async"
        class:ready={loaded}
        onload={() => (loaded = true)}
        onerror={() => (loaded = true)}
      />
    {:else}
      <span class="missing">no preview</span>
    {/if}

    <div class="overlay">
      <span class="pair">
        {#each HALVES as half (half)}
          {@const state = halfState(group, half, app.cutRemoves)}
          <span class="half {state}" data-half={half} data-state={state}>{half.toUpperCase()}</span>
        {/each}
      </span>
      {#if warnings.length > 0}
        <span class="warn" title={warnings.join("; ")}>!</span>
      {/if}
      <span class="spacer"></span>
      <span class="verdict {verdict}">{verdictWord(verdict)}</span>
    </div>

    {#if stars > 0}
      <span class="stars" aria-label="{stars} of 5">
        {#each { length: stars } as _, i (i)}
          <span class="star"></span>
        {/each}
      </span>
    {/if}
  </div>

  <div class="footer">
    <span class="stem" title={group.stem}>{group.stem}</span>
    {#if destination !== ""}
      <span class="dest" title={destination}>→ {leafOf(destination)}</span>
    {/if}
  </div>
</button>

<style>
  .tile {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    border-radius: 5px;
    overflow: hidden;
    background: var(--bg-tile);
    border: 1px solid var(--border);
    cursor: pointer;
    padding: 0;
    margin: 0;
    font: inherit;
    color: inherit;
    text-align: left;
    appearance: none;
    outline: none;
  }

  .fetching {
    position: absolute;
    inset: 0;
    background: linear-gradient(100deg, transparent 30%, var(--bg-raised) 50%, transparent 70%);
    background-size: 250% 100%;
    animation: fetching 1.2s ease-in-out infinite;
  }

  @keyframes fetching {
    from {
      background-position: 130% 0;
    }
    to {
      background-position: -30% 0;
    }
  }

  img {
    opacity: 0;
    transition: opacity 120ms ease-out;
  }

  img.ready {
    opacity: 1;
  }

  /* Selection has to read from across the room: a wash over the whole
     thumbnail and a filled corner tick, not just a border. */
  .tile.selected .thumb::after {
    content: "";
    position: absolute;
    inset: 0;
    background: var(--accent-wash-18, rgba(97, 175, 239, 0.18));
    pointer-events: none;
  }

  .tile.selected .footer::before {
    content: "✓";
    display: inline-grid;
    place-items: center;
    width: 13px;
    height: 13px;
    border-radius: 3px;
    background: var(--accent);
    color: var(--on-accent);
    font-size: 9px;
    font-weight: 700;
    flex: 0 0 auto;
  }

  .tile.selected {
    box-shadow: inset 0 0 0 2px var(--accent);
    border-color: var(--border-selected);
  }

  .tile.focused {
    border-color: var(--accent);
    box-shadow: var(--focus-ring);
  }

  .thumb {
    position: relative;
    aspect-ratio: 3 / 2;
    display: grid;
    place-items: center;
    background: var(--bg-thumb);
    overflow: hidden;
  }

  .thumb img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .missing {
    font-size: 10.5px;
    color: var(--text-ghost);
    padding: 0 8px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The gradient exists so the badges stay legible over a bright sky; it fades
     out entirely rather than tinting the top of the photograph. */
  .overlay {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 22px;
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 0 5px;
    background: linear-gradient(to bottom, rgba(14, 16, 19, 0.82), rgba(14, 16, 19, 0));
  }

  .pair {
    display: inline-flex;
    gap: 2px;
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
  }

  .half {
    padding: 2px 3px;
    border-radius: 2px;
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

  .warn {
    display: inline-grid;
    place-items: center;
    min-width: 13px;
    height: 13px;
    padding: 0 3px;
    border-radius: 2px;
    background: var(--amber);
    color: var(--on-accent);
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
  }

  .spacer {
    flex: 1;
  }

  .verdict {
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
    white-space: nowrap;
  }

  .verdict.keep {
    color: var(--keep);
  }

  .verdict.cut {
    color: var(--cut);
  }

  .stars {
    position: absolute;
    bottom: 4px;
    left: 5px;
    display: flex;
    gap: 2px;
  }

  .star {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--gold);
  }

  .footer {
    flex: 0 0 auto;
    height: 22px;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    background: var(--bg-tile);
    font-size: 10.5px;
  }

  /* The ellipsis has to live on the text's own box: a flex container will not
     truncate an anonymous text item. */
  .stem {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The chip takes the width it needs before the stem does: which frames are
     routed where is the thing being scanned for on a sheet mid-import. */
  .dest {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 60%;
    padding: 1px 4px;
    border-radius: 2px;
    background: var(--accent-wash-16);
    color: var(--accent);
    font-size: 9.5px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
