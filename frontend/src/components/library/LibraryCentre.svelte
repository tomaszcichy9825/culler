<script lang="ts">
  // LIBRARY's centre pane. The sub-layout decides what it holds: the search
  // results (⌥1), the sessions table (⌥2) or the storage cards (⌥3).
  //
  // The header line is the design's: what the view is grouped and sorted by on
  // the left, and what it is offering on the right.

  import FacetChips from "./FacetChips.svelte";
  import SearchResults from "./SearchResults.svelte";
  import SessionsTable from "./SessionsTable.svelte";
  import StorageCards from "./StorageCards.svelte";
  import { formatCount, library } from "../../lib/library.svelte";

  interface Props {
    /** The sub-layout index: 0 search, 1 sessions, 2 storage. */
    layout: number;
  }

  let { layout }: Props = $props();

  const GAPS = [1, 2, 4, 8, 24];
</script>

<div class="centre">
  {#if layout === 0}
    <div class="header">
      <span>grouped by shoot</span>
      <span class="sep">·</span>
      <span>newest first</span>
      <span class="spacer"></span>
      <span class="loaded">
        {formatCount(library.results.length)} of {formatCount(library.total)} loaded
      </span>
    </div>
    <FacetChips />
    <SearchResults />
  {:else if layout === 1}
    <div class="header">
      <span>sessions</span>
      <span class="sep">·</span>
      <span>newest first</span>
      <span class="spacer"></span>
      <span class="gap">
        break
        {#each GAPS as hours (hours)}
          <button
            type="button"
            class="step"
            class:on={library.sessionGapHours === hours}
            onclick={() => library.setSessionGap(hours)}>{hours}h</button
          >
        {/each}
      </span>
    </div>
    <SessionsTable />
  {:else}
    <div class="header">
      <span>volumes</span>
      <span class="sep">·</span>
      <span>largest first</span>
      <span class="spacer"></span>
      <span class="loaded">counts are of what is catalogued, not of the disk</span>
    </div>
    <StorageCards />
  {/if}
</div>

<style>
  .centre {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-pane);
  }

  .header {
    flex: 0 0 30px;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-chrome);
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .sep {
    color: var(--text-faint);
  }

  .spacer {
    flex: 1;
  }

  .loaded {
    color: var(--text-dim);
  }

  .gap {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--text-dim);
  }

  .step {
    padding: 2px 6px;
    border-radius: 3px;
    border: 1px solid transparent;
    background: none;
    font: inherit;
    font-size: 10px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .step:hover {
    color: var(--text);
  }

  .step.on {
    border-color: var(--accent);
    background: var(--accent-wash-16);
    color: var(--accent);
  }
</style>
