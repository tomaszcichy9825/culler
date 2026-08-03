<script lang="ts">
  // The title bar's right-hand cluster while EXIF mode is showing: a gold chip
  // counting the drafted changes, and the green pill that writes them.
  //
  // The chip is the whole of the mode's honesty. Every edit is a draft until
  // ⌘S, so a number sitting in the title bar is the only thing telling the
  // user that closing the app now would lose work. It disappears at zero
  // rather than reading "0 unwritten", which would be noise.

  import { exifState } from "../../lib/exif.svelte";

  let count = $derived(exifState.unwritten);
  let batch = $derived(exifState.batch);
  let frames = $derived(exifState.targets.length);
  let tags = $derived(new Set(exifState.rows.filter((r) => r.state === "dirty").map((r) => r.tag)).size);
</script>

<div class="cluster" data-testid="exif-unwritten">
  {#if batch && frames > 0}
    <span class="pill">{frames} frame{frames === 1 ? "" : "s"} selected</span>
    {#if tags > 0}
      <span class="tally">{tags} tag{tags === 1 ? "" : "s"} × {frames} frame{frames === 1 ? "" : "s"}</span>
    {/if}
  {/if}

  {#if count > 0}
    <span class="chip" title="edits that are not on disk yet">
      <span class="dot" aria-hidden="true"></span>
      {count} unwritten
    </span>
    <button type="button" class="write" disabled={exifState.writing} onclick={() => void exifState.requestWrite()}>
      write ⌘S
    </button>
  {/if}
</div>

<style>
  .cluster {
    display: inline-flex;
    align-items: center;
    gap: 8px;
  }

  /* Status chip, §4.11: a 5px dot in the text's own hue on a 0.16 wash. */
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--gold-wash-16);
    font-size: 10.5px;
    color: var(--gold);
    white-space: nowrap;
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--gold);
  }

  .pill {
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--accent-wash-18);
    font-size: 10.5px;
    color: var(--accent);
    white-space: nowrap;
  }

  .tally {
    font-size: 10.5px;
    color: var(--gold);
    white-space: nowrap;
  }

  .write {
    padding: 3px 9px;
    border-radius: 4px;
    background: var(--keep);
    border: 1px solid var(--keep);
    font: inherit;
    font-size: 11px;
    font-weight: 700;
    color: var(--on-accent);
    cursor: pointer;
    white-space: nowrap;
    appearance: none;
  }

  .write:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
