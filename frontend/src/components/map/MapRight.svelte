<script lang="ts">
  // MAP's right pane: the frame the focused pin is showing.
  //
  // It is the inspector's job on this screen — a preview, what the frame is
  // called and when it was taken, the coordinates the camera recorded, and the
  // rest of the frames standing at the same pin.
  //
  // The Position rows are marked "from camera" because that is the only place
  // they can have come from: this mode reads coordinates and never writes them,
  // and there is no reverse geocode section because naming a coordinate means
  // sending it somewhere, which the mode does not do.

  import { previewURL } from "../../lib/preview";
  import { geotag } from "../../lib/geotag.svelte";
  import {
    formatAltitude,
    formatClock,
    formatCoords,
    formatDate,
    leafName,
    mapState,
    positionToGroup,
  } from "../../lib/map.svelte";
  import type { MapPosition } from "../../lib/map.svelte";

  let copied = $state(false);
  let copyTimer: ReturnType<typeof setTimeout> | null = null;

  let frame = $derived(mapState.frame);
  let cluster = $derived(mapState.cluster);

  function thumb(p: MapPosition): string {
    return previewURL(positionToGroup(p, mapState.hashOf(p)), "grid");
  }

  function preview(p: MapPosition): string {
    return previewURL(positionToGroup(p, mapState.hashOf(p)), "full");
  }

  /**
   * Copying the coordinates is the one action here. Clipboard access can be
   * refused, in which case the pane says nothing rather than claiming a copy
   * that did not happen.
   */
  async function copyCoords() {
    if (frame === null) return;
    try {
      await navigator.clipboard.writeText(formatCoords(frame.latitude, frame.longitude));
      copied = true;
      if (copyTimer !== null) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => {
        copied = false;
        copyTimer = null;
      }, 1400);
    } catch {
      copied = false;
    }
  }
</script>

<div class="pane">
  {#if frame === null || cluster === null}
    <p class="empty">
      {#if mapState.positions.length === 0}
        nothing in this folder carries a position
      {:else}
        no pin focused
      {/if}
    </p>
  {:else}
    <div class="well">
      {#key `${frame.dir}/${frame.stem}`}
        {#if preview(frame) !== ""}
          <img alt="" src={preview(frame)} />
        {/if}
      {/key}
    </div>

    <div class="title">
      <span class="stem">{frame.stem}</span>
      <span class="spacer"></span>
      <span class="here">{cluster.count} {cluster.count === 1 ? "frame" : "frames"} here</span>
    </div>

    <section>
      <div class="label">
        <span>position</span>
        <span class="rule"></span>
        <span class="source">from camera</span>
      </div>

      <button
        type="button"
        class="coords"
        title="copy the coordinates"
        aria-label="Copy the coordinates"
        onclick={() => void copyCoords()}
      >
        <span class="row"><span class="key">latitude</span><span class="val">{frame.latitude.toFixed(6)}</span></span>
        <span class="row"><span class="key">longitude</span><span class="val">{frame.longitude.toFixed(6)}</span></span>
        <span class="copy">{copied ? "copied" : "click to copy"}</span>
      </button>

      {#if frame.hasAltitude}
        <div class="row"><span class="key">altitude</span><span class="val">{formatAltitude(frame.altitude)}</span></div>
      {/if}

      <!-- Borrow this located frame's position onto the selected photos — the
           way a whole shoot from a camera with no GPS gets placed from one
           tagged reference frame. -->
      {#if geotag.targetCount > 0}
        <button
          type="button"
          class="copy-to"
          onclick={() =>
            geotag.copyFrom({
              latitude: frame.latitude,
              longitude: frame.longitude,
              altitude: frame.altitude,
              hasAltitude: frame.hasAltitude,
            })}
        >
          copy this location to {geotag.targetCount} selected
        </button>
      {/if}
    </section>

    <section>
      <div class="label"><span>frame</span><span class="rule"></span></div>
      <div class="row"><span class="key">date</span><span class="val">{formatDate(frame.shot)}</span></div>
      <div class="row"><span class="key">time</span><span class="val">{formatClock(frame.shot)}</span></div>
      <div class="row"><span class="key">files</span><span class="val">{frame.kind}</span></div>
      <div class="row"><span class="key">folder</span><span class="val dim">{leafName(frame.dir)}</span></div>
    </section>

    <section>
      <div class="label"><span>at this pin</span><span class="rule"></span></div>
      <div class="grid">
        {#each cluster.frames as p, i (`${p.dir}/${p.stem}`)}
          <button
            type="button"
            class="cell"
            class:on={i === mapState.frameIndex}
            title={p.stem}
            aria-label={p.stem}
            onclick={() => mapState.focusFrame(i)}
            ondblclick={() => mapState.open(p)}
          >
            {#if thumb(p) !== ""}<img alt="" loading="lazy" src={thumb(p)} />{/if}
          </button>
        {/each}
      </div>
    </section>
  {/if}
</div>

<style>
  .pane {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
  }

  .well {
    aspect-ratio: 3 / 2;
    background: var(--bg-thumb);
    border-bottom: 1px solid var(--border);
    display: grid;
    place-items: center;
    overflow: hidden;
  }

  .well img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }

  .title {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }

  .stem {
    font-family: var(--font-mono);
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .here {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  section {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 7px;
    font-family: var(--font-mono);
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

  .source {
    letter-spacing: 0;
    text-transform: none;
    color: var(--keep);
    white-space: nowrap;
  }

  .row {
    display: flex;
    align-items: baseline;
    gap: 10px;
    height: 20px;
    min-width: 0;
  }

  .key {
    flex: 0 0 78px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .val {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .val.dim {
    color: var(--text-muted);
  }

  .copy-to {
    display: block;
    width: 100%;
    margin-top: 8px;
    padding: 5px 8px;
    border-radius: 5px;
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text-muted);
    font: inherit;
    font-size: 11px;
    cursor: pointer;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .copy-to:hover {
    border-color: var(--accent);
    color: var(--text);
  }

  /* The two coordinate rows are one control, so the whole block is the copy
     target rather than a separate button beside numbers nobody can hit. */
  .coords {
    display: block;
    width: 100%;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .coords .row {
    display: flex;
  }

  .copy {
    display: block;
    padding-top: 3px;
    font-family: var(--font-mono);
    font-size: 10px;
    color: var(--text-ghost);
  }

  .coords:hover .copy {
    color: var(--text-dim);
  }

  .coords:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
    border-radius: 4px;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 4px;
  }

  .cell {
    aspect-ratio: 3 / 2;
    padding: 0;
    border-radius: 2px;
    border: 1px solid var(--border);
    background: var(--bg-thumb);
    overflow: hidden;
    cursor: pointer;
  }

  .cell.on {
    border-color: var(--accent);
  }

  .cell:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .cell img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .empty {
    margin: 0;
    padding: 24px 12px;
    text-align: center;
    font-size: 11px;
    color: var(--text-ghost);
  }
</style>
