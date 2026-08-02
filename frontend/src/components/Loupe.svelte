<script lang="ts">
  // The focused frame, large. The same preview URL as the grid tile, so a
  // frame already fetched for the grid is served from the webview cache.

  import { app, loupe } from "../lib/state.svelte";
  import { decisionBadge, decisionLabel, kindBadge, previewURL } from "../lib/preview";

  let imgEl = $state<HTMLImageElement | null>(null);
  let stage = $state<HTMLDivElement | null>(null);
  let scale = $state(1);
  let dragging = $state(false);
  let dragX = 0;
  let dragY = 0;

  let group = $derived(app.focused);
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

  // Arrow keys pan while zoomed; the key layer has no way to clamp on its own.
  loupe.pan = (dx: number, dy: number) => {
    if (!app.zoom) return;
    app.panX += dx / scale;
    app.panY += dy / scale;
    clamp();
  };

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

<div class="loupe">
  <div
    class="stage"
    bind:this={stage}
    class:zoomed={app.zoom}
    class:dragging
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
  </div>

  {#if group}
    <div class="info">
      <span class="stem" title={group.stem}>{group.stem}</span>
      <span class="chip">{kindBadge(group.kind)} · {group.kind}</span>
      {#if group.decision !== "" && group.decision !== "none"}
        <span class="chip decision {group.decision}">{decisionBadge(group.decision)} {decisionLabel(group.decision)}</span>
      {/if}
      {#if app.zoom}<span class="chip">1:1 · {scale.toFixed(1)}×</span>{/if}
      {#each group.warnings ?? [] as warning}
        <span class="chip warn" title={warning}>{warning}</span>
      {/each}
      <span class="spacer"></span>
      <span class="hint">Tab or Esc back to the grid · Z for 1:1</span>
    </div>
  {/if}
</div>

<style>
  .loupe {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .stage {
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    overflow: hidden;
    background: var(--bg-sunken);
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
    color: var(--text-faint);
    font-size: 13px;
    max-width: 90%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .info {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 14px;
    border-top: 1px solid var(--border);
    font-size: 12px;
    color: var(--text-muted);
    min-width: 0;
    overflow: hidden;
  }

  .stem {
    /* Shrinks before the chips do, since the chips are already short. */
    flex: 0 1 auto;
    min-width: 0;
    font-weight: 600;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1 1 auto;
  }

  .hint {
    flex: 0 1 auto;
    min-width: 0;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip {
    flex: 0 1 auto;
    min-width: 0;
    max-width: 40%;
    padding: 2px 7px;
    border-radius: 4px;
    background: var(--bg-chip);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip.warn {
    background: var(--warn-bg);
    color: var(--warn);
  }

  .chip.decision {
    color: var(--on-decision);
    font-weight: 600;
  }

  .chip.keep_all {
    background: var(--keep-all);
  }
  .chip.drop_raw {
    background: var(--drop-raw);
  }
  .chip.drop_jpeg {
    background: var(--drop-jpeg);
  }
  .chip.drop_all {
    background: var(--drop-all);
  }
</style>
