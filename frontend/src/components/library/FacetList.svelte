<script lang="ts">
  // One faceted list in the right pane (5a): a 96px key, a 5px meter and a 42px
  // right-aligned count. The meter is a share of the largest value in the list
  // rather than of the whole catalogue, because a list where every bar is two
  // pixels wide says nothing.

  import type { CatalogFacetCount } from "../../lib/library.svelte";
  import { formatCount, percent } from "../../lib/library.svelte";

  interface Props {
    title: string;
    rows: CatalogFacetCount[];
    /** Which row is selected, by value. */
    selected: string;
    /** Any CSS colour for the meter fill. */
    colour?: string;
    onpick: (value: string) => void;
  }

  let { title, rows, selected, colour = "var(--accent)", onpick }: Props = $props();

  let peak = $derived(Math.max(1, ...rows.map((r) => r.frames)));
</script>

<section>
  <div class="label"><span>{title}</span><span class="rule"></span></div>
  {#each rows as row (row.value)}
    <button
      type="button"
      class="row"
      class:on={selected === row.value}
      class:empty={row.frames === 0}
      aria-pressed={selected === row.value}
      onclick={() => onpick(row.value)}
    >
      <span class="key">{row.label}</span>
      <span class="meter">
        <span class="fill" style:width="{percent(row.frames, peak)}%" style:background={colour}></span>
      </span>
      <span class="count">{formatCount(row.frames)}</span>
    </button>
  {/each}
</section>

<style>
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

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    min-height: 22px;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
  }

  .row:hover .key {
    color: var(--text);
  }

  .row.on .key,
  .row.on .count {
    color: var(--accent);
  }

  /* A facet holding nothing stays in the list so it does not reflow, but it
     reads as unavailable rather than as a choice. */
  .row.empty {
    color: var(--text-dead);
    cursor: default;
  }

  .key {
    flex: 0 0 96px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meter {
    flex: 1 1 auto;
    min-width: 0;
    height: 5px;
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-track);
  }

  .fill {
    display: block;
    height: 100%;
  }

  .count {
    flex: 0 0 42px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: 10.5px;
  }
</style>
