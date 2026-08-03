<script lang="ts">
  // EXIF mode's left pane: the frames the editor is working on.
  //
  // One component, two shapes, because they are one rail. Screen 3a lists the
  // frames as 34px rows — thumbnail, stem, and a dirty dot for a frame with
  // drafted changes. Screen 3b, editing a batch, drops the names and becomes a
  // three-column mosaic of the same frames, dimming the ones with nothing
  // drafted on them. Nothing but the drawing differs, so nothing but the
  // drawing is duplicated.
  //
  // Thumbnails go through the shared image queue rather than being handed to
  // the browser as a wall of <img src>: a selection of two thousand frames is
  // exactly the pile-up the queue exists to prevent.

  import type { GroupDTO } from "../../lib/bindings";
  import { app } from "../../lib/state.svelte";
  import { exifState } from "../../lib/exif.svelte";
  import { previewURL } from "../../lib/preview";
  import { queuedImage } from "../../lib/imageQueue";

  interface Props {
    /** Three-column mosaic instead of named rows. Set by the batch layout. */
    mosaic?: boolean;
  }

  let { mosaic = false }: Props = $props();

  /**
   * A frame is known here by the path of the file being edited, which is one
   * of the two files a group can hold. Both are indexed so either resolves.
   */
  let byPath = $derived.by(() => {
    const map = new Map<string, GroupDTO>();
    for (const g of app.allGroups) {
      if (g.jpegPath !== "") map.set(g.jpegPath, g);
      if (g.rawPath !== "") map.set(g.rawPath, g);
    }
    return map;
  });

  let rowEls = $state<(HTMLElement | null)[]>([]);

  // The rail follows the cursor wherever it was moved from, so the frame being
  // edited is never off the end of a list the user is looking at.
  $effect(() => {
    rowEls[exifState.index]?.scrollIntoView({ block: "nearest" });
  });
</script>

<div class="rail" class:mosaic data-testid="exif-frames-rail">
  <div class="head">
    <span class="title">Frames</span>
    <span class="rule"></span>
    <span class="hint">⌘1</span>
  </div>

  {#if exifState.frames.length === 0}
    <p class="empty">no frames selected</p>
  {:else}
    <div class="list" class:mosaic role="listbox" aria-label="Frames" aria-orientation="vertical">
      {#each exifState.frames as frame, i (frame.path)}
        {@const group = byPath.get(frame.path)}
        {@const url = group ? previewURL(group, "grid") : ""}
        {@const dirty = exifState.isDirty(frame.path)}
        <button
          type="button"
          class="row"
          class:focused={i === exifState.index}
          class:dim={mosaic && !dirty}
          bind:this={rowEls[i]}
          role="option"
          aria-selected={i === exifState.index}
          title={frame.path}
          onclick={() => exifState.setIndex(i)}
        >
          <span class="thumb">
            {#if url !== ""}
              <img use:queuedImage={url} alt={frame.stem} decoding="async" />
            {/if}
            {#if mosaic && dirty}<span class="dot" aria-label="unwritten changes"></span>{/if}
          </span>
          {#if !mosaic}
            <span class="stem">{frame.stem}</span>
            {#if frame.kind === "raw"}<span class="kind">R</span>{/if}
            <span class="dot" class:on={dirty} aria-label={dirty ? "unwritten changes" : ""}></span>
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .rail {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
    background: var(--bg-pane);
  }

  /* The 22px section header of screen 3a: a label, a hairline, and the pane's
     own key on the right. */
  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 22px;
    padding: 0 12px;
  }

  .title {
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

  .hint {
    font-size: 10px;
    color: var(--text-ghost);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding-bottom: 8px;
  }

  .list.mosaic {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 4px;
    padding: 4px 8px 8px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    height: 34px;
    padding: 0 12px;
    margin: 0;
    border: 0;
    border-left: 2px solid transparent;
    background: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    appearance: none;
  }

  .row.focused {
    background: var(--accent-wash-16);
    border-left-color: var(--accent);
  }

  .row.focused .stem {
    color: var(--text-hi);
  }

  .mosaic .row {
    height: auto;
    padding: 0;
    border-left: 0;
  }

  .mosaic .row.dim {
    opacity: 0.45;
  }

  .mosaic .row.focused {
    background: none;
  }

  .thumb {
    position: relative;
    flex: 0 0 auto;
    width: 34px;
    height: 23px;
    border-radius: 2px;
    overflow: hidden;
    background: var(--bg-thumb);
  }

  .mosaic .thumb {
    width: 100%;
    height: auto;
    aspect-ratio: 3 / 2;
    border-radius: 3px;
    border: 1px solid transparent;
  }

  .mosaic .row.focused .thumb {
    border-color: var(--border-focus);
  }

  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .stem {
    flex: 1;
    min-width: 0;
    font-size: 11.5px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kind {
    flex: 0 0 auto;
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
    padding: 2px 3px;
    border-radius: 2px;
    background: var(--absent-wash);
    color: var(--text-muted);
  }

  /* The dirty mark: 5px of gold, and the only thing on the row that says a
     frame has changes nobody has written yet. */
  .dot {
    flex: 0 0 auto;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: transparent;
  }

  .dot.on {
    background: var(--gold);
  }

  .mosaic .dot {
    position: absolute;
    top: 3px;
    right: 3px;
    background: var(--gold);
    box-shadow: 0 0 0 1px rgba(14, 16, 19, 0.6);
  }

  .empty {
    margin: 0;
    padding: 12px;
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--text-ghost);
  }
</style>
