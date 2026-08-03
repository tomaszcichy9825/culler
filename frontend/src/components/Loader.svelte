<script lang="ts">
  // What the centre pane shows while a folder is being read. It is the same
  // 560px column as the cold start, so pressing ⏎ replaces the offer with its
  // own progress rather than throwing the screen away.
  //
  // The design's line here is "you can start culling now — frames appear as
  // they decode, embedded JPEG first". That is not true of this backend:
  // OpenFolder walks the whole directory, hashes every group and returns the
  // folder in one piece, so nothing is cullable until it finishes and no
  // preview is decoded during the wait. The progress events count frames
  // hashed, which is why the total is trustworthy the moment the first one
  // lands. The reassurance below says what is actually happening; the design's
  // line goes back in when the backend streams groups as it finds them.
  //
  // A card read over SMB can take a long while, which is why this says which
  // folder it is waiting on rather than leaving an empty grid.

  import { app } from "../lib/state.svelte";

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }

  // A total of zero would divide by zero and means nothing to show anyway.
  let progress = $derived(app.scanProgress !== null && app.scanProgress.total > 0 ? app.scanProgress : null);
  let percent = $derived(progress === null ? 0 : Math.min(100, (progress.done / progress.total) * 100));

  let dir = $derived(app.scanning ?? "");
  let count = $derived(progress === null ? "" : `${progress.done.toLocaleString()} / ${progress.total.toLocaleString()}`);
</script>

<div class="scan" role="status" aria-live="polite">
  <div class="column">
    <div class="hero">
      <!-- Amber is the design's indexing hue, and the status chip on this
           screen is an amber INDEXING. Brand cyan belongs to the cold start's
           card-detected eyebrow, which this replaces. -->
      <span class="eyebrow">indexing</span>
      <h1 class="headline" title={dir}>
        {basename(dir)}{progress === null ? "" : ` · ${progress.total.toLocaleString()} frames`}
      </h1>
      <p class="meta" title={dir}>{dir}</p>
    </div>

    <div class="card">
      <div class="head">
        <span class="label">identifying frames</span>
        <span class="spacer"></span>
        {#if progress === null}
          <span class="count pending">counting…</span>
        {:else}
          <span class="count">{count}</span>
        {/if}
      </div>

      <!-- Indeterminate until the backend's first progress event: it has to
           finish walking the directory before it knows how many frames. -->
      <div
        class="track"
        role="progressbar"
        aria-label="Scan progress"
        aria-valuemin={progress === null ? undefined : 0}
        aria-valuemax={progress === null ? undefined : progress.total}
        aria-valuenow={progress === null ? undefined : progress.done}
      >
        {#if progress === null}
          <div class="fill sweep"></div>
        {:else}
          <div class="fill" style:width="{percent}%"></div>
        {/if}
      </div>

      <p class="note">RAW and JPEG halves are paired as they are read — nothing is written to the card.</p>

      {#if app.scanSlow}
        <p class="slow">large or slow folder — still scanning</p>
      {/if}
    </div>
  </div>
</div>

<style>
  .scan {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: grid;
    place-items: center;
    padding: 40px;
    overflow: auto;
  }

  .column {
    width: 560px;
    max-width: 100%;
    display: flex;
    flex-direction: column;
    gap: 22px;
    min-width: 0;
  }

  .hero {
    display: flex;
    flex-direction: column;
    gap: 7px;
    min-width: 0;
  }

  .eyebrow {
    font-size: 11px;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--amber);
  }

  /* Public Sans, at the one size the design gives it. */
  .headline {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 26px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text-hi);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    margin: 0;
    font-size: 12px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 13px 14px;
    border-radius: 6px;
    background: var(--bg-chrome);
    border: 1px solid var(--border);
    min-width: 0;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 9px;
    min-width: 0;
  }

  .label {
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
    min-width: 0;
  }

  .count {
    font-size: 11px;
    color: var(--accent);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .count.pending {
    color: var(--text-dim);
  }

  .track {
    height: 5px;
    border-radius: 3px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    transition: width 0.18s ease-out;
  }

  /* The indeterminate state used to be a spinner. A bar that sweeps says the
     same thing in the shape the determinate one will take, so the card does
     not change height the moment the first count arrives. */
  .sweep {
    width: 34%;
    transition: none;
    animation: sweep 1.4s ease-in-out infinite;
  }

  @keyframes sweep {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(294%);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .sweep {
      animation-duration: 4.2s;
    }
  }

  .note,
  .slow {
    margin: 0;
    font-size: 10.5px;
    line-height: 1.5;
    text-wrap: pretty;
    color: var(--text-dim);
  }

  .slow {
    color: var(--amber);
  }
</style>
