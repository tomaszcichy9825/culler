<script lang="ts">
  // LIBRARY's left pane: the folder tree over what the catalogue covers, and
  // the one place to add or drop a root.
  //
  // A root is a folder the user has asked the catalogue to remember. Removing
  // one forgets its rows; it never touches the disk, and the pane says so
  // rather than making the user find out.

  import LibraryTree from "./LibraryTree.svelte";
  import { formatCount, library } from "../../lib/library.svelte";

  let draft = $state("");

  function add(event: SubmitEvent) {
    event.preventDefault();
    const path = draft.trim();
    if (path === "") return;
    draft = "";
    void library.addRoot(path);
  }
</script>

<div class="pane">
  <section class="grow">
    <div class="label">
      <span>folders</span>
      <span class="rule"></span>
      <span class="hint">{formatCount(library.roots.length)}</span>
    </div>
    <LibraryTree />
    <p class="note">⏎ opens the folder in cull</p>
  </section>

  <section>
    <div class="label"><span>add</span><span class="rule"></span></div>
    <form onsubmit={add}>
      <input
        type="text"
        placeholder="~/Pictures"
        spellcheck="false"
        autocomplete="off"
        bind:value={draft}
      />
    </form>
    <p class="note">indexing reads the folder and writes nothing to it</p>
  </section>

  <section>
    <div class="label"><span>index</span><span class="rule"></span></div>
    <button
      type="button"
      class="action"
      disabled={library.indexing !== null || library.roots.length === 0}
      onclick={() => library.reindex()}
    >
      {library.indexing === null ? "reindex everything" : "indexing…"}
    </button>
    {#if library.indexing !== null}
      <p class="note" title={library.indexing.dir}>
        {formatCount(library.indexing.frames)} frames · {formatCount(library.indexing.dirs)} folders
      </p>
    {/if}
  </section>
</div>

<style>
  .pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  section {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }

  /* The tree takes the slack and scrolls; the add and index blocks keep their
     height, so the one control that grows is the one holding the folders. */
  .grow {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
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

  .hint {
    font-family: var(--font-mono);
    letter-spacing: 0;
    color: var(--text-dim);
  }

  input {
    width: 100%;
    height: 24px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    font: inherit;
    font-size: 11px;
    color: var(--text);
    outline: none;
  }

  input:focus {
    border-color: var(--border-focus);
  }

  .note {
    margin: 6px 0 0;
    font-size: 10px;
    color: var(--text-ghost);
  }

  .action {
    width: 100%;
    height: 26px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .action:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .action:disabled {
    color: var(--text-dead);
    cursor: default;
  }
</style>
