<script lang="ts">
  // The compact tile the search grid is drawn with (5a): radius 4, small R/J
  // badges top-left, star dots bottom-left, a caption bottom-right and a 19px
  // stem footer.
  //
  // It is not components/Tile.svelte. That tile is the contact sheet's — a
  // GroupDTO, the full 22px badge row, no caption — and the design draws this
  // one smaller and differently. Editing the contact sheet's tile to serve both
  // would change CULL to suit LIBRARY, so this is its own component.

  import { queuedImage } from "../../lib/imageQueue";
  import { basename, type CatalogFrame } from "../../lib/library.svelte";

  interface Props {
    frame: CatalogFrame;
    index: number;
    focused: boolean;
    width: number;
    height: number;
    x: number;
    y: number;
    onfocus: (index: number) => void;
    onopen: (index: number) => void;
  }

  let { frame, index, focused, width, height, x, y, onfocus, onopen }: Props = $props();

  /**
   * The same URL the grid uses, built from the paths the catalogue recorded.
   * The grid tier is served from the thumbnail cache keyed on the content hash,
   * so a result the user has already seen in CULL appears without touching the
   * card at all.
   */
  let url = $derived.by(() => {
    const cache = frame.hash !== "" ? `&size=grid&hash=${encodeURIComponent(frame.hash)}` : "";
    if (frame.hasJpeg && frame.jpegPath !== "") {
      return `/preview?path=${encodeURIComponent(frame.jpegPath)}&tier=jpeg${cache}`;
    }
    if (frame.hasRaw && frame.rawPath !== "") {
      return `/preview?path=${encodeURIComponent(frame.rawPath)}&tier=embedded${cache}`;
    }
    return "";
  });

  let stars = $derived(Math.max(0, frame.rating));
  /**
   * The design captions the tile with where the frame was taken. There is no
   * GPS in the catalogue, so the caption names the folder instead — true, and
   * the thing the user is most likely to be looking for in a result.
   */
  let caption = $derived(basename(frame.dir));
</script>

<button
  type="button"
  class="tile"
  class:focused
  style:width="{width}px"
  style:height="{height}px"
  style:transform="translate({x}px, {y}px)"
  role="option"
  data-row={index}
  data-stem={frame.stem}
  tabindex={focused ? 0 : -1}
  aria-selected={focused}
  data-verdict={frame.verdict}
  onfocus={() => onfocus(index)}
  onclick={() => onfocus(index)}
  ondblclick={() => onopen(index)}
>
  <div class="thumb">
    {#if url !== ""}
      <img use:queuedImage={url} alt={frame.stem} decoding="async" />
    {:else}
      <span class="missing">no preview</span>
    {/if}

    <span class="pair">
      <span class="half" class:absent={!frame.hasRaw} class:cut={frame.verdict === "cut"}>R</span>
      <span class="half" class:absent={!frame.hasJpeg} class:cut={frame.verdict === "cut"}>J</span>
    </span>

    {#if stars > 0}
      <span class="stars" aria-label="{stars} of 5">
        {#each { length: stars } as _, i (i)}
          <span class="star"></span>
        {/each}
      </span>
    {/if}

    <span class="caption" title={frame.dir}>{caption}</span>
  </div>

  <div class="footer">
    <span class="stem" title={frame.stem}>{frame.stem}</span>
  </div>
</button>

<style>
  .tile {
    position: absolute;
    top: 0;
    left: 0;
    display: flex;
    flex-direction: column;
    border-radius: 4px;
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

  .tile.focused {
    border-color: var(--accent);
    box-shadow: var(--focus-ring);
  }

  .thumb {
    position: relative;
    flex: 1 1 auto;
    min-height: 0;
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
    object-fit: cover;
    display: block;
  }

  .missing {
    font-size: 9.5px;
    color: var(--text-ghost);
  }

  .pair {
    position: absolute;
    top: 4px;
    left: 4px;
    display: inline-flex;
    gap: 2px;
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
  }

  /* Both halves are always drawn: a missing badge and a badge that says the
     half is absent read the same at a glance and mean opposite things. */
  .half {
    padding: 2px 3px;
    border-radius: 2px;
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
    text-decoration: none;
  }

  .stars {
    position: absolute;
    bottom: 4px;
    left: 4px;
    display: flex;
    gap: 2px;
  }

  .star {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--gold);
  }

  .caption {
    position: absolute;
    right: 4px;
    bottom: 4px;
    max-width: 70%;
    padding: 2px 5px;
    border-radius: 3px;
    background: var(--glass);
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 9.5px;
    line-height: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .footer {
    flex: 0 0 19px;
    display: flex;
    align-items: center;
    padding: 0 5px;
    background: var(--bg-tile);
    font-size: 10px;
  }

  .stem {
    flex: 1 1 auto;
    min-width: 0;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
