<script lang="ts">
  // IMPORT · review: what is on the selected card, and the one thing to do
  // about it.
  //
  // Nothing on this screen routes a frame. Routing happens on the frames
  // themselves in CULL, so the whole panel leads to one affordance — open the
  // card there — and everything above it exists to answer whether this card is
  // worth opening: how much is on it, over how many shoots, and how much of it
  // the library already holds.

  import {
    basename,
    formatBytes,
    formatCount,
    formatSpan,
    importState,
  } from "../../lib/import.svelte";

  interface Props {
    /** What "review in cull" does. The shell passes its own openFolder. */
    onreview?: (dir: string) => void;
  }

  let { onreview }: Props = $props();

  let card = $derived(importState.card);
  let summary = $derived(importState.summary);
  let target = $derived(importState.dir ?? card?.dir ?? "");

  function review() {
    if (target === "") return;
    if (onreview !== undefined) onreview(target);
    else importState.review(target);
  }
</script>

<div class="review">
  {#if card === null}
    <p class="empty">select a card on the left</p>
  {:else}
    <header>
      <span class="eyebrow">card</span>
      <h2>{card.name}</h2>
      <p class="path" title={card.path}>{card.path}</p>
    </header>

    {#if summary === null}
      <p class="empty">reading the card…</p>
    {:else}
      <div class="figures">
        <div class="figure">
          <span class="value">{formatCount(summary.frames)}</span>
          <span class="key">frames</span>
        </div>
        <div class="figure">
          <span class="value">{formatBytes(summary.bytes)}</span>
          <span class="key">on the card</span>
        </div>
        <div class="figure">
          <span class="value">{formatCount(summary.sessions)}</span>
          <span class="key">{summary.sessions === 1 ? "shoot" : "shoots"}</span>
        </div>
        <div class="figure">
          <span class="value" class:known={summary.sampled > 0}>
            {summary.sampled === 0 ? "—" : `${importState.importedPercent.toFixed(0)}%`}
          </span>
          <span class="key">already imported</span>
        </div>
      </div>

      {#if summary.sampled > 0 && summary.sampled < summary.frames}
        <p class="caveat">
          measured on {formatCount(summary.sampled)} frames spread across the card, not all
          {formatCount(summary.frames)}
        </p>
      {/if}

      <button type="button" class="hero" disabled={target === ""} onclick={review}>
        <span class="hero-key">⏎</span>
        <span class="hero-body">
          <span class="hero-name">review in cull</span>
          <span class="hero-note" title={target}>{basename(target)}</span>
        </span>
      </button>

      <section>
        <div class="label">
          <span>folders</span>
          <span class="rule"></span>
          <span class="hint">{formatSpan(summary.first, summary.last)}</span>
        </div>
        <div class="rows">
          <div class="row head">
            <span class="cell c-name">folder</span>
            <span class="cell c-num">frames</span>
            <span class="cell c-num">files</span>
            <span class="cell c-num">size</span>
            <span class="cell c-when">shot</span>
          </div>
          {#each summary.dirs as dir (dir.path)}
            <button
              type="button"
              class="row"
              title={dir.path}
              onclick={() => {
                void importState.loadFolder(dir.path);
                if (onreview !== undefined) onreview(dir.path);
                else importState.review(dir.path);
              }}
            >
              <span class="cell c-name">{dir.name}</span>
              <span class="cell c-num">{formatCount(dir.frames)}</span>
              <span class="cell c-num">{formatCount(dir.files)}</span>
              <span class="cell c-num">{formatBytes(dir.bytes)}</span>
              <span class="cell c-when">{formatSpan(dir.first, dir.last)}</span>
            </button>
          {/each}
          {#if summary.dirs.length === 0}
            <p class="empty">no image folders on this card</p>
          {/if}
        </div>
      </section>
    {/if}
  {/if}
</div>

<style>
  .review {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    padding: 16px;
  }

  header {
    padding-bottom: 14px;
  }

  .eyebrow {
    font-size: 10px;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--brand);
  }

  h2 {
    margin: 6px 0 0;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.2px;
    color: var(--text-hi);
  }

  .path {
    margin: 3px 0 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .figures {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }

  .figure {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 10px 12px;
    border-radius: 5px;
    border: 1px solid var(--border);
    background: var(--bg-chrome);
    min-width: 0;
  }

  .value {
    font-family: var(--font-mono);
    font-size: 17px;
    color: var(--text-hi);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .value.known {
    color: var(--keep-text);
  }

  .key {
    font-size: 10px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .caveat {
    margin: 8px 0 0;
    font-size: 10.5px;
    color: var(--text-ghost);
  }

  .hero {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    margin-top: 14px;
    padding: 12px 14px;
    border-radius: 5px;
    border: 1px solid var(--border-selected);
    background: var(--accent-wash-10);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .hero:hover:not(:disabled) {
    background: var(--accent-wash-14);
  }

  .hero:disabled {
    background: none;
    border-color: var(--border);
    cursor: default;
  }

  .hero:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .hero-key {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 5px;
    background: var(--accent);
    color: var(--on-accent);
    font-family: var(--font-mono);
    font-size: 11px;
    font-weight: 700;
  }

  .hero:disabled .hero-key {
    background: var(--bg-kbd);
    color: var(--text-dead);
  }

  .hero-body {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .hero-name {
    font-size: 12.5px;
    color: var(--text-hi);
  }

  .hero-note {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  section {
    padding-top: 18px;
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
    text-transform: none;
    color: var(--text-dim);
  }

  .rows {
    min-width: 0;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 28px;
    padding: 0;
    border: none;
    border-top: 1px solid var(--border-hair);
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
  }

  .row.head {
    height: 22px;
    border-top: none;
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-faint);
    cursor: default;
  }

  .row:not(.head):hover .c-name {
    color: var(--text);
  }

  .row:focus-visible {
    outline: none;
    box-shadow: var(--focus-inset);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .c-name {
    flex: 1 1 auto;
    font-family: var(--font-mono);
    color: var(--text);
  }

  .row.head .c-name {
    font-family: inherit;
    color: var(--text-faint);
  }

  .c-num {
    flex: 0 0 78px;
    text-align: right;
    font-family: var(--font-mono);
  }

  .row.head .c-num {
    font-family: inherit;
  }

  .c-when {
    flex: 0 0 172px;
    text-align: right;
    font-size: 10.5px;
    color: var(--text-dim);
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-ghost);
  }
</style>
