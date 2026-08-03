<script lang="ts">
  // LIBRARY's left pane: the roots the catalogue covers, and the one place to
  // add or drop one.
  //
  // A root is a folder the user has asked the catalogue to remember. Removing
  // one forgets its rows; it never touches the disk, and the pane says so
  // rather than making the user find out.

  import { basename, formatBytes, formatCount, library } from "../../lib/library.svelte";

  let draft = $state("");

  function add(event: SubmitEvent) {
    event.preventDefault();
    const path = draft.trim();
    if (path === "") return;
    draft = "";
    void library.addRoot(path);
  }
</script>

<div class="tree">
  <section>
    <div class="label">
      <span>folders</span>
      <span class="rule"></span>
      <span class="hint">{formatCount(library.roots.length)}</span>
    </div>

    {#each library.roots as root (root.path)}
      <div class="root" class:on={library.facets.root === root.path}>
        <button
          type="button"
          class="pick"
          title={root.path}
          onclick={() => library.setFacet("root", root.path)}
        >
          <span class="name">{basename(root.path)}</span>
          <span class="meta">
            {#if root.lastIndexed === ""}
              never indexed
            {:else}
              {formatCount(root.frames)} · {formatBytes(root.bytes)}
            {/if}
          </span>
        </button>
        <button
          type="button"
          class="drop"
          title="forget {root.path} — nothing on disk is touched"
          aria-label="Forget {root.path}"
          onclick={() => library.removeRoot(root.path)}>×</button
        >
      </div>
    {:else}
      <p class="empty">no folders yet</p>
    {/each}
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
  .tree {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  section {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
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

  .root {
    display: flex;
    align-items: center;
    gap: 4px;
    border-radius: 3px;
  }

  .root.on {
    background: var(--accent-wash-16);
    box-shadow: inset 2px 0 0 var(--accent);
  }

  .pick {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 4px 6px;
    border: none;
    background: none;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .name {
    font-size: 11.5px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .root.on .name {
    color: var(--accent);
  }

  .meta {
    font-size: 10px;
    color: var(--text-dim);
  }

  .drop {
    flex: 0 0 auto;
    width: 18px;
    height: 18px;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-ghost);
    font: inherit;
    font-size: 13px;
    line-height: 1;
    cursor: pointer;
  }

  .drop:hover {
    background: var(--cut-wash-16);
    color: var(--cut-text);
  }

  .empty {
    margin: 0;
    font-size: 10.5px;
    color: var(--text-ghost);
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
