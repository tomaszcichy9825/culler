<script lang="ts">
  // What the grid area shows while a folder is being scanned. A card read over
  // SMB can take a long while, so this says which folder it is waiting on
  // rather than leaving an empty grid that looks like a broken app.

  import { app } from "../lib/state.svelte";

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }

  // A total of zero would divide by zero and means nothing to show anyway.
  let progress = $derived(app.scanProgress !== null && app.scanProgress.total > 0 ? app.scanProgress : null);
  let percent = $derived(progress === null ? 0 : Math.min(100, (progress.done / progress.total) * 100));
</script>

<div class="loading" role="status" aria-live="polite">
  {#if progress !== null}
    <div
      class="track"
      role="progressbar"
      aria-valuemin="0"
      aria-valuemax={progress.total}
      aria-valuenow={progress.done}
      aria-label="Scan progress"
    >
      <div class="fill" style:width="{percent}%"></div>
    </div>
  {:else}
    <!-- Indeterminate until the backend's first progress event: the scan has
         to finish walking the directory before it knows how many frames. -->
    <div class="spinner" aria-hidden="true"></div>
  {/if}

  <p class="what" title={app.scanning ?? ""}>
    Scanning <span class="folder">{basename(app.scanning ?? "")}</span>…
  </p>

  {#if progress !== null}
    <p class="count">{progress.done} of {progress.total} frames</p>
  {/if}

  {#if app.scanSlow}
    <p class="slow">large or slow folder — still scanning</p>
  {/if}
</div>

<style>
  .loading {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    padding: 0 24px;
    min-width: 0;
    color: var(--text-muted);
    font-size: 13px;
  }

  .spinner {
    width: 22px;
    height: 22px;
    border-radius: 50%;
    border: 2px solid var(--border-strong);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }

  @media (prefers-reduced-motion: reduce) {
    .spinner {
      animation-duration: 2.4s;
    }
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .track {
    width: min(260px, 60%);
    height: 4px;
    border-radius: 2px;
    background: var(--bg-raised);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    border-radius: 2px;
    transition: width 0.18s ease-out;
  }

  .count {
    margin: 0;
    font-size: 12px;
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }

  .what,
  .slow {
    margin: 0;
    max-width: 100%;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .folder {
    color: var(--text);
    font-weight: 600;
  }

  .slow {
    font-size: 12px;
    color: var(--text-faint);
  }
</style>
