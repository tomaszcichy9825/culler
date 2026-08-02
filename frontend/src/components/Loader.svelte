<script lang="ts">
  // What the grid area shows while a folder is being scanned. A card read over
  // SMB can take a long while, so this says which folder it is waiting on
  // rather than leaving an empty grid that looks like a broken app.

  import { app } from "../lib/state.svelte";

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }
</script>

<div class="loading" role="status" aria-live="polite">
  <div class="spinner" aria-hidden="true"></div>
  <p class="what" title={app.scanning ?? ""}>
    Scanning <span class="folder">{basename(app.scanning ?? "")}</span>…
  </p>
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
