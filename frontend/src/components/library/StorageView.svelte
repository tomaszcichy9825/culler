<script lang="ts">
  // Storage as a full pane over the shell.
  //
  // It belongs to IMPORT — knowing what a volume is already holding is part of
  // deciding where a card's frames should land — and IMPORT is not built. So
  // rather than leave the one screen that answers "what is on my disks" behind
  // a mode that shows a ghost, it is reachable from the command palette and
  // drawn over everything until Esc.

  import StorageCards from "./StorageCards.svelte";
  import { formatBytes, formatCount, library } from "../../lib/library.svelte";

  // Filled on the way in rather than at startup: it walks everything the
  // catalogue holds, and nothing else on screen needs the answer.
  $effect(() => {
    void library.loadStorage();
  });

  let storage = $derived(library.storage);
</script>

<div class="view" role="dialog" aria-modal="true" aria-label="Storage">
  <header class="head">
    <span class="title">Storage</span>
    <span class="rule"></span>
    {#if storage !== null}
      <span class="totals">
        {formatCount(storage.frames)} frames · {formatBytes(storage.bytes)}
      </span>
    {/if}
    <span class="note">counts are of what is catalogued, not of the disk</span>
    <button type="button" class="close" title="Close" aria-label="Close" onclick={() => (library.storageOpen = false)}>
      esc
    </button>
  </header>

  <StorageCards />
</div>

<style>
  .view {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-window);
  }

  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 38px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-chrome);
    min-width: 0;
  }

  .title {
    flex: 0 0 auto;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .rule {
    flex: 1;
    min-width: 0;
    height: 1px;
    background: var(--border);
  }

  .totals {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text);
  }

  .note {
    flex: 0 1 auto;
    min-width: 0;
    font-size: 10.5px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .close {
    flex: 0 0 auto;
    height: 22px;
    padding: 0 8px;
    border: 1px solid var(--border-strong);
    border-radius: 4px;
    background: var(--bg-field);
    font: inherit;
    font-size: 10.5px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .close:hover {
    border-color: var(--accent);
    color: var(--text-hi);
  }
</style>
