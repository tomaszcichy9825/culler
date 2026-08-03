<script lang="ts">
  // The image, large, with 1:1 zoom and panning.
  //
  // Internal to the two loupe screens rather than part of their contract: both
  // draw the same stage and differ only in what floats on top of it, so the
  // zoom maths lives here once. Whatever the parent puts in its children is
  // rendered over the image — the verdict badge, the hint chips — inside a
  // layer that takes no pointer events, so a drag that starts on a chip still
  // pans the photograph underneath it.

  import type { Snippet } from "svelte";
  import type { GroupDTO } from "../lib/bindings";
  import { app, loupe } from "../lib/state.svelte";
  import { previewURL } from "../lib/preview";

  interface Props {
    group: GroupDTO | null;
    /** Breathing room between the image and the edge of the stage. */
    padding?: number;
    /** A border and radius around the stage, which the overlay draws and the pane does not. */
    framed?: boolean;
    children?: Snippet;
  }

  let { group, padding = 22, framed = false, children }: Props = $props();

  let imgEl = $state<HTMLImageElement | null>(null);
  let stage = $state<HTMLDivElement | null>(null);
  let scale = $state(1);
  let dragging = $state(false);
  let dragX = 0;
  let dragY = 0;

  let url = $derived(group ? previewURL(group) : "");

  // 1:1 means one image pixel per screen pixel, so the factor is however much
  // the preview was shrunk to fit the stage.
  function measure() {
    if (!app.zoom || !imgEl || imgEl.clientWidth === 0) {
      scale = 1;
      return;
    }
    scale = Math.max(1, imgEl.naturalWidth / imgEl.clientWidth);
  }

  $effect(() => {
    // Re-measure whenever the zoom is toggled or the frame changes.
    app.zoom;
    url;
    measure();
  });

  /** clamp keeps the pan within the part of the image that is off stage. */
  function clamp() {
    if (!imgEl || !stage || scale <= 1) {
      app.panX = 0;
      app.panY = 0;
      return;
    }
    const limitX = Math.max(0, (imgEl.clientWidth * scale - stage.clientWidth) / 2 / scale);
    const limitY = Math.max(0, (imgEl.clientHeight * scale - stage.clientHeight) / 2 / scale);
    app.panX = Math.max(-limitX, Math.min(limitX, app.panX));
    app.panY = Math.max(-limitY, Math.min(limitY, app.panY));
  }

  // Arrow keys pan while zoomed, and the key layer has no way to clamp on its
  // own. Handing the handler back on teardown matters: a stage that has left
  // the document must not go on receiving pans through a stale closure.
  $effect(() => {
    loupe.pan = (dx: number, dy: number) => {
      if (!app.zoom) return;
      app.panX += dx / scale;
      app.panY += dy / scale;
      clamp();
    };
    return () => {
      loupe.pan = () => {};
    };
  });

  function onPointerDown(e: PointerEvent) {
    if (!app.zoom) return;
    dragging = true;
    dragX = e.clientX;
    dragY = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: PointerEvent) {
    if (!dragging) return;
    app.panX += (e.clientX - dragX) / scale;
    app.panY += (e.clientY - dragY) / scale;
    dragX = e.clientX;
    dragY = e.clientY;
    clamp();
  }

  function onPointerUp(e: PointerEvent) {
    if (!dragging) return;
    dragging = false;
    (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
  }
</script>

<div
  class="stage"
  class:framed
  bind:this={stage}
  class:zoomed={app.zoom}
  class:dragging
  style:padding="{padding}px"
  role="img"
  aria-label={group ? group.stem : "no frame"}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerUp}
  onpointercancel={onPointerUp}
>
  {#if group && url !== ""}
    <img
      bind:this={imgEl}
      src={url}
      alt={group.stem}
      onload={measure}
      style:transform="scale({scale}) translate({app.panX}px, {app.panY}px)"
    />
  {:else if group}
    <span class="missing">no preview for {group.stem}</span>
  {:else}
    <span class="missing">nothing to show</span>
  {/if}

  {#if children}
    <div class="float">{@render children()}</div>
  {/if}
</div>

<style>
  /* Flex, not grid, and the distinction is the whole reason the image used to
     hang off the bottom of the stage. A grid item's `max-height: 100%`
     resolves against its track, and the single implicit row was auto-sized to
     the photograph's own height — so the track grew past the stage, the
     percentage ceiling never bit, and the image was laid out from the top-left
     of a box taller than the one clipping it. A flex item's percentage
     resolves against the flex container, which is the box that does the
     clipping, so the image can never be larger than what is on screen. */
  .stage {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    background: var(--bg-app);
  }

  .stage.framed {
    border-radius: 4px;
    border: 1px solid var(--border-strong);
  }

  .stage.zoomed {
    cursor: grab;
  }

  .stage.dragging {
    cursor: grabbing;
  }

  .stage img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    display: block;
    transform-origin: center center;
  }

  .missing {
    color: var(--text-dim);
    font-size: 13px;
    max-width: 90%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Informational only: everything the parent floats here is readable, and
     none of it is in the way of a drag. */
  .float {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }
</style>
