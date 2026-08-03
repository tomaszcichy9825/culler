<script lang="ts">
  // The facet chip row above the results (5a). A chip is a toggle: clicking the
  // one that is already on clears it, so there is no separate way to undo a
  // filter and no state the user cannot get out of by clicking what they see.

  import { KIND_FACETS, VERDICT_FACETS, library } from "../../lib/library.svelte";

  const RATINGS = [1, 2, 3, 4, 5];
</script>

<div class="chips">
  <span class="label">kind</span>
  {#each KIND_FACETS as kind (kind)}
    <button
      type="button"
      class="chip"
      class:on={library.facets.kind === kind}
      aria-pressed={library.facets.kind === kind}
      onclick={() => library.setFacet("kind", kind)}
    >
      {kind}
    </button>
  {/each}

  <span class="divider"></span>
  <span class="label">verdict</span>
  {#each VERDICT_FACETS as verdict (verdict)}
    <button
      type="button"
      class="chip {verdict}"
      class:on={library.facets.verdict === verdict}
      aria-pressed={library.facets.verdict === verdict}
      onclick={() => library.setFacet("verdict", verdict)}
    >
      {verdict}
    </button>
  {/each}

  <span class="divider"></span>
  <span class="label">rating</span>
  {#each RATINGS as rating (rating)}
    <button
      type="button"
      class="chip star"
      class:on={library.facets.minRating === rating}
      aria-pressed={library.facets.minRating === rating}
      aria-label="{rating} stars and up"
      onclick={() => library.setFacet("minRating", rating)}
    >
      {rating}+
    </button>
  {/each}

  <span class="spacer"></span>
  {#if library.activeFacets > 0}
    <button type="button" class="chip clear" onclick={() => library.clearFacets()}>
      clear {library.activeFacets}
    </button>
  {/if}
</div>

<style>
  .chips {
    flex: 0 0 auto;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    padding: 7px 12px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-chrome);
  }

  .label {
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .divider {
    width: 1px;
    height: 14px;
    margin: 0 4px;
    background: var(--border);
  }

  .spacer {
    flex: 1;
  }

  .chip {
    padding: 3px 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    color: var(--text-muted);
    font: inherit;
    font-size: 10.5px;
    line-height: 1.3;
    cursor: pointer;
  }

  .chip:hover {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .chip.on {
    background: var(--accent-wash-18);
    border-color: var(--accent);
    color: var(--accent);
  }

  .chip.keep.on {
    background: var(--keep-wash-16);
    border-color: var(--keep);
    color: var(--keep-text);
  }

  .chip.cut.on {
    background: var(--cut-wash-16);
    border-color: var(--cut);
    color: var(--cut-text);
  }

  .chip.star.on {
    background: var(--gold-wash-16);
    border-color: var(--gold);
    color: var(--gold);
  }

  .chip.clear {
    color: var(--text-dim);
  }
</style>
