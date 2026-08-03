<script lang="ts">
  // LIBRARY's title-bar context slot (5a): a full-width query field with `/` in
  // accent and the result count beside it, or, in the sessions and storage
  // sub-layouts, what those views count instead.
  //
  // The shell owns the title bar; this is only what goes in its centre. The
  // `key:` tokens are real — the backend parses kind:, verdict:, rating: and
  // root: out of the field — so they are coloured to say so.

  import { formatBytes, formatCount, library } from "../../lib/library.svelte";

  interface Props {
    /** The sub-layout index: 0 search, 1 sessions, 2 storage. */
    layout: number;
  }

  let { layout }: Props = $props();

  let field = $state<HTMLInputElement | null>(null);

  /** focus is exported so ⌘F or `/` can put the caret in the field. */
  export function focus() {
    field?.focus();
    field?.select();
  }

  let unfinished = $derived(library.sessions.filter((s) => s.undecided > 0).length);
</script>

<div class="context">
  {#if layout === 0}
    <label class="query" for="library-query">
      <span class="slash">/</span>
      <input
        id="library-query"
        bind:this={field}
        type="text"
        placeholder="search the catalogue — try kind:raw-only or rating:4"
        spellcheck="false"
        autocomplete="off"
        value={library.query}
        oninput={(e) => library.setQuery(e.currentTarget.value)}
      />
    </label>
    <span class="count">
      {#if library.error !== null}
        <span class="error">{library.error}</span>
      {:else if library.searched}
        {formatCount(library.total)}
        {library.total === 1 ? "result" : "results"} · {library.elapsed} ms
      {/if}
    </span>
  {:else if layout === 1}
    <span class="count">
      {formatCount(library.sessions.length)}
      {library.sessions.length === 1 ? "session" : "sessions"}
    </span>
    {#if unfinished > 0}
      <span class="chip cut">
        <span class="dot"></span>
        {formatCount(unfinished)}
        {unfinished === 1 ? "session unfinished" : "sessions unfinished"}
      </span>
    {/if}
  {:else}
    <span class="count">
      {formatCount(library.storage?.frames ?? 0)} frames catalogued
    </span>
    <span class="chip keep">
      <span class="dot"></span>
      {formatBytes(library.storage?.bytes ?? 0)} indexed
    </span>
  {/if}

  {#if library.indexing !== null}
    <span class="chip amber">
      <span class="dot"></span>
      INDEXING · {formatCount(library.indexing.frames)}
    </span>
  {/if}
</div>

<style>
  .context {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .query {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 8px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--accent);
  }

  .slash {
    color: var(--accent);
    font-family: var(--font-mono);
    font-size: 11px;
  }

  input {
    flex: 1;
    min-width: 0;
    border: none;
    background: none;
    outline: none;
    font: inherit;
    font-size: 11.5px;
    color: var(--text);
  }

  input::placeholder {
    color: var(--text-ghost);
  }

  .count {
    flex: 0 0 auto;
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .error {
    color: var(--cut-text);
  }

  .chip {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 8px;
    border-radius: 4px;
    font-size: 10.5px;
    white-space: nowrap;
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  .chip.keep {
    background: var(--keep-wash-16);
    color: var(--keep-text);
  }

  .chip.cut {
    background: var(--cut-wash-16);
    color: var(--cut-text);
  }

  .chip.amber {
    background: var(--amber-wash-16);
    color: var(--amber);
  }
</style>
