<script lang="ts">
  // The strip of frames along the bottom of every loupe.
  //
  // One component, two sizes: the loupe-first layout gives it 104px and wants
  // captions and the R/J pair, the overlay gives it 78px and wants neither.
  // Everything that differs between them is a prop, because they are the same
  // strip — a different strip that merely looked similar would drift apart the
  // first time one of them was touched.
  //
  // Thumbnails go through the shared image queue rather than being handed to
  // the browser as a row of <img src>: a strip of 2,000 frames is exactly the
  // pile-up the queue exists to prevent, and a frame scrolled past before its
  // turn comes never costs a request at all.

  import type { GroupDTO } from "../lib/bindings";
  import { app, groupKey } from "../lib/state.svelte";
  import { HALVES, halfState, verdictOf } from "../lib/verdict";
  import type { CutScope } from "../lib/verdict";
  import { previewURL } from "../lib/preview";
  import { queuedImage } from "../lib/imageQueue";

  interface Props {
    /** The frames to draw, in the order they are drawn. */
    groups: GroupDTO[];
    /** Index of the frame the app is on, which the strip keeps in view. */
    index: number;
    /**
     * Asked for a new index. The caller decides whether focus actually moves.
     * A click passes its event with it, so shift and ⌘ mean the same here as
     * they do on the contact sheet; the arrows pass none.
     */
    onselect: (index: number, e?: MouseEvent) => void;
    /**
     * Selection membership, if the host tracks one. Without it the strip draws
     * focus alone, which is what the screens with no selection want.
     */
    isSelected?: (group: GroupDTO) => boolean;
    /** Height of the strip itself: 104 in the loupe-first layout, 78 in the overlay. */
    height?: number;
    thumbWidth?: number;
    thumbHeight?: number;
    /** The stem beneath each frame. */
    caption?: boolean;
    /** The R/J pair in the corner of each frame. */
    badges?: boolean;
    /** Panel background and top rule. Off when the strip sits on a scrim. */
    surface?: boolean;
    /**
     * Whether left/right traverse when the strip holds the keyboard. Screens
     * turn this off while the image is zoomed, where the arrows pan instead.
     */
    keyboard?: boolean;
    label?: string;
    /** How far a cut reaches. Defaults to the configured behaviour. */
    cutRemoves?: CutScope;
  }

  let {
    groups,
    index,
    onselect,
    isSelected,
    height = 104,
    thumbWidth = 112,
    thumbHeight = 72,
    caption = true,
    badges = true,
    surface = true,
    keyboard = true,
    label = "Frames",
    cutRemoves,
  }: Props = $props();

  let cut = $derived(cutRemoves ?? app.cutRemoves);

  /** Matches the gap between frames, so a scrolled-to frame is not flush. */
  const GAP = 6;

  let strip = $state<HTMLDivElement | null>(null);
  let frameEls = $state<(HTMLElement | null)[]>([]);

  // The strip follows the app's focus wherever it came from — a key press, a
  // click in the grid, a decision advancing to the next frame — so the current
  // frame is never off the end of a strip the user is looking at.
  $effect(() => {
    const el = frameEls[index];
    const box = strip;
    if (!el || !box) return;
    const left = el.offsetLeft;
    const right = left + el.offsetWidth;
    if (left < box.scrollLeft) box.scrollLeft = Math.max(0, left - GAP);
    else if (right > box.scrollLeft + box.clientWidth) box.scrollLeft = right + GAP - box.clientWidth;
  });

  function step(to: number) {
    if (groups.length === 0) return;
    onselect(Math.max(0, Math.min(to, groups.length - 1)));
  }

  /**
   * Arrows are handled here only while the strip itself holds the keyboard,
   * and the event is stopped rather than left to bubble: the application's own
   * key listener sits on the window and binds the same arrows, so letting both
   * run would move focus two frames at a time.
   */
  function onkeydown(e: KeyboardEvent) {
    if (!keyboard) return;
    let to: number | null = null;
    if (e.key === "ArrowLeft") to = index - 1;
    else if (e.key === "ArrowRight") to = index + 1;
    else if (e.key === "Home") to = 0;
    else if (e.key === "End") to = groups.length - 1;
    if (to === null) return;
    e.preventDefault();
    e.stopPropagation();
    step(to);
  }
</script>

<div
  class="strip"
  class:surface
  bind:this={strip}
  style:height="{height}px"
  role="listbox"
  aria-label={label}
  aria-orientation="horizontal"
  tabindex="0"
  {onkeydown}
>
  {#each groups as g, i (groupKey(g))}
    {@const url = previewURL(g, "grid")}
    <button
      type="button"
      class="frame"
      class:focused={i === index}
      class:selected={isSelected?.(g) ?? false}
      style:width="{thumbWidth}px"
      bind:this={frameEls[i]}
      role="option"
      tabindex="-1"
      aria-selected={i === index}
      onclick={(e) => onselect(i, e)}
    >
      <span class="thumb" style:height="{thumbHeight}px">
        {#if url !== ""}
          <img use:queuedImage={url} alt={g.stem} decoding="async" />
        {:else}
          <span class="missing">no preview</span>
        {/if}
        <span class="stripe {verdictOf(g) || 'none'}" aria-hidden="true"></span>
        {#if badges}
          <span class="pair">
            {#each HALVES as half}
              <span class="half {halfState(g, half, cut)}">{half.toUpperCase()}</span>
            {/each}
          </span>
        {/if}
      </span>
      {#if caption}
        <span class="stem" title={g.stem}>{g.stem}</span>
      {/if}
    </button>
  {/each}
</div>

<style>
  .strip {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 6px;
    overflow-x: auto;
    overflow-y: hidden;
    outline: none;
  }

  .strip.surface {
    padding: 0 12px;
    background: var(--bg-window);
    border-top: 1px solid var(--border);
  }

  .frame {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 0;
    margin: 0;
    border: 0;
    background: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    appearance: none;
    outline: none;
  }

  .thumb {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-thumb);
    border: 1px solid var(--border-strong);
  }

  .frame.focused .thumb {
    border-color: var(--border-focus);
    box-shadow: var(--focus-ring);
  }

  /* A swept range has to be visible in the strip too, or extending a selection
     from the loupe is a thing you do blind. Same language as the tile: a wash
     over the thumbnail and a solid accent edge. */
  .frame.selected .thumb {
    border-color: var(--accent);
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .frame.selected .thumb::after {
    content: "";
    position: absolute;
    inset: 0;
    background: var(--accent-wash-18);
    pointer-events: none;
  }

  .frame.selected.focused .thumb {
    box-shadow: inset 0 0 0 2px var(--accent), var(--focus-ring);
  }

  .frame.selected .stem {
    color: var(--text);
  }

  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .missing {
    font-size: 9px;
    color: var(--text-dim);
    padding: 0 4px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The verdict reads before anything else in the strip: a bar across the top
     of the frame, the full width of it, in the one hue that means that verdict
     anywhere in the application. */
  .stripe {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 3px;
  }

  .stripe.keep {
    background: var(--keep);
  }

  .stripe.cut {
    background: var(--cut);
  }

  .stripe.none {
    opacity: 0;
  }

  .pair {
    position: absolute;
    bottom: 2px;
    left: 3px;
    display: inline-flex;
    gap: 2px;
    font-size: 9px;
    font-weight: 700;
    line-height: 1;
  }

  .half {
    padding: 2px 3px;
    border-radius: 2px;
  }

  .half.kept {
    background: var(--keep-wash-20);
    color: var(--keep-text);
  }

  .half.cut {
    background: var(--cut-wash-22);
    color: var(--cut-text);
    text-decoration: line-through;
  }

  .half.absent {
    background: var(--absent-wash);
    color: var(--text-dead);
  }

  .stem {
    min-width: 0;
    font-size: 10px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
