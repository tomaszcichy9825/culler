<script lang="ts">
  // The search bar: a thin strip under the title bar that puts the catalogue's
  // index onto the grid in place of the open folder.
  //
  // Search is not a room you go to. `/` opens this, the grid fills with what
  // the index answers, ⏎ opens the frame under the cursor where it lives, and
  // Esc puts the folder back exactly as it was — nothing on disk, and nothing
  // about the open folder, has moved in the meantime. The banner says so,
  // because a grid showing frames from six different folders would otherwise
  // be indistinguishable from one showing a folder.

  import { formatCount, library } from "../../lib/library.svelte";
  import { app } from "../../lib/state.svelte";

  let field = $state<HTMLInputElement | null>(null);

  /** The bar opens with the keyboard already in it: `/` is a typing gesture. */
  $effect(() => {
    field?.focus();
  });

  /** open hands the frame under the grid's cursor back to CULL, in its folder. */
  function open() {
    const focused = app.groups[app.focusIndex];
    if (focused === undefined) return;
    library.openAt(focused.dir, focused.hash);
  }

  function onKeydown(e: KeyboardEvent) {
    switch (e.key) {
      case "Escape":
        e.preventDefault();
        library.closeSearch();
        break;
      case "Enter":
        e.preventDefault();
        open();
        break;
      case "ArrowDown":
        e.preventDefault();
        app.setFocus(app.focusIndex + 1);
        break;
      case "ArrowUp":
        e.preventDefault();
        app.setFocus(app.focusIndex - 1);
        break;
    }
  }
</script>

<div class="bar" data-search-bar>
  <span class="glyph" aria-hidden="true">/</span>
  <input
    bind:this={field}
    type="text"
    class="field"
    placeholder="search the index — name, folder, kind:paired, 4★"
    spellcheck="false"
    autocomplete="off"
    aria-label="Search the catalogue"
    value={library.query}
    oninput={(e) => library.setQuery(e.currentTarget.value)}
    onkeydown={onKeydown}
  />

  <span class="banner" data-search-banner>
    {#if library.error !== null}
      <span class="failed">{library.error}</span>
    {:else if library.loading}
      searching…
    {:else if library.searched}
      <span class="found" data-search-total={library.total}>
        {formatCount(library.total)} in the index
      </span>
      <span class="sep">·</span>
      <span>⏎ opens it where it lives</span>
      <span class="sep">·</span>
      <span>esc returns</span>
    {:else}
      index results · esc returns
    {/if}
  </span>

  <button type="button" class="close" title="Close the search" aria-label="Close the search" onclick={() => library.closeSearch()}>
    ×
  </button>
</div>

<style>
  .bar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 30px;
    padding: 0 10px 0 12px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
    box-shadow: var(--focus-inset-2);
    min-width: 0;
  }

  .glyph {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--accent);
  }

  .field {
    flex: 1 1 auto;
    min-width: 0;
    height: 22px;
    padding: 0 6px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg-field);
    font: inherit;
    font-size: 12px;
    color: var(--text);
    outline: none;
  }

  .field:focus {
    border-color: var(--border-focus);
  }

  .banner {
    flex: 0 1 auto;
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
    overflow: hidden;
  }

  .found {
    color: var(--text-muted);
  }

  .sep {
    color: var(--text-faint);
  }

  .failed {
    color: var(--cut);
  }

  .close {
    flex: 0 0 auto;
    width: 20px;
    height: 20px;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    border-radius: 4px;
    background: none;
    font: inherit;
    font-size: 14px;
    line-height: 1;
    color: var(--text-dim);
    cursor: pointer;
  }

  .close:hover {
    background: var(--bg-raised);
    color: var(--text);
  }
</style>
