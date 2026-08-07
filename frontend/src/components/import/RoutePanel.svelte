<script lang="ts">
  // IMPORT · route: where the folder's frames are going, and how much of it is
  // going nowhere.
  //
  // The unrouted count is the whole point of the screen. A frame with no
  // destination stays on the card when the import runs, and the difference
  // between "I decided to leave it" and "I never got to it" is the difference
  // between a cull and a card you will find again in a year — so the two are
  // counted apart, and the warning names the one that is an oversight.

  import {
    basename,
    formatBytes,
    formatCount,
    importState,
    percent,
  } from "../../lib/import.svelte";

  interface Props {
    /** What the jump-to-CULL hint does. The shell passes its own openFolder. */
    onreview?: (dir: string) => void;
  }

  let { onreview }: Props = $props();

  let plan = $derived(importState.plan);

  function review() {
    const dir = importState.dir ?? "";
    if (dir === "") return;
    if (onreview !== undefined) onreview(dir);
    else importState.review(dir);
  }
</script>

<div class="route">
  {#if plan === null}
    {#if importState.error !== null}
      <!-- A folder that would not read leaves no plan; the reason has to show
           here, or the panel says "select a card" about a card just selected. -->
      <p class="empty">{importState.error}</p>
    {:else}
      <p class="empty">select a card on the left</p>
    {/if}
  {:else}
    <header>
      <span class="eyebrow">routing</span>
      <h2 title={plan.dir}>{basename(plan.dir)}</h2>
      <p class="path">
        {formatCount(plan.frames)}
        {plan.frames === 1 ? "frame" : "frames"} · {plan.verb === "move"
          ? "frames are taken off the card"
          : "the card is left as it was found"}
      </p>
    </header>

    <div class="split">
      <span class="part routed" style:width="{percent(plan.routed, plan.frames)}%"></span>
      <span class="part cut" style:width="{percent(plan.cut, plan.frames)}%"></span>
      <span class="part left" style:width="{percent(plan.unrouted, plan.frames)}%"></span>
    </div>
    <div class="legend">
      <span class="tag routed">{formatCount(plan.routed)} routed</span>
      <span class="tag cut">{formatCount(plan.cut)} cut</span>
      <span class="tag left">{formatCount(plan.unrouted)} staying on the card</span>
    </div>

    {#if importState.hasUnrouted}
      <button type="button" class="warning" onclick={review}>
        <span class="bang">!</span>
        <span class="warning-body">
          <span class="warning-name">
            {formatCount(plan.unrouted)}
            {plan.unrouted === 1 ? "frame is" : "frames are"} routed nowhere
          </span>
          <span class="warning-note">
            {plan.undecided > 0
              ? `${formatCount(plan.undecided)} of them nobody has looked at — open the folder in cull and filter to undecided`
              : "judged and left where they are, which the import will honour"}
          </span>
        </span>
        <span class="warning-key">⏎</span>
      </button>
    {/if}

    <section>
      <div class="label">
        <span>destinations</span>
        <span class="rule"></span>
        <span class="hint" title={plan.libraryRoot}>{plan.libraryRoot}</span>
      </div>
      <div class="rows">
        <div class="row head">
          <span class="cell c-dest">destination</span>
          <span class="cell c-num">frames</span>
          <span class="cell c-num">files</span>
          <span class="cell c-num">size</span>
          <span class="cell c-share">share</span>
        </div>
        {#each plan.routes as route (route.destination)}
          <div class="row" title={route.path}>
            <span class="cell c-dest">{route.destination}</span>
            <span class="cell c-num">{formatCount(route.frames)}</span>
            <span class="cell c-num">{formatCount(route.files)}</span>
            <span class="cell c-num">{formatBytes(route.bytes)}</span>
            <span class="cell c-share">{percent(route.frames, plan.routed).toFixed(0)}%</span>
          </div>
        {/each}
        {#if plan.routes.length === 0}
          <p class="empty">
            nothing in this folder is routed yet — press <span class="key">m</span> on a frame in cull
          </p>
        {/if}
      </div>
    </section>
  {/if}
</div>

<style>
  .route {
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
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .path {
    margin: 3px 0 0;
    font-size: 11px;
    color: var(--text-dim);
  }

  .split {
    display: flex;
    height: 10px;
    border-radius: 3px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .part {
    display: block;
    height: 100%;
  }

  .part.routed {
    background: var(--accent);
  }
  .part.cut {
    background: var(--cut);
  }
  .part.left {
    background: var(--undecided);
  }

  .legend {
    display: flex;
    gap: 14px;
    padding-top: 7px;
    font-size: 10.5px;
  }

  .tag {
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }

  .tag::before {
    content: "";
    width: 7px;
    height: 7px;
    border-radius: 2px;
  }
  .tag.routed::before {
    background: var(--accent);
  }
  .tag.cut::before {
    background: var(--cut);
  }
  .tag.left::before {
    background: var(--undecided);
  }

  .warning {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    margin-top: 14px;
    padding: 10px 12px;
    border-radius: 5px;
    border: 1px solid var(--amber-wash-18);
    background: var(--amber-wash-14);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .warning:hover {
    border-color: var(--amber);
  }

  .warning:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .bang {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 18px;
    height: 18px;
    border-radius: 4px;
    background: var(--amber-wash-18);
    color: var(--amber);
    font-size: 11px;
    font-weight: 700;
  }

  .warning-body {
    flex: 1 1 auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .warning-name {
    font-size: 12px;
    color: var(--amber);
  }

  .warning-note {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .warning-key {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 4px;
    background: var(--bg-kbd);
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 10.5px;
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
    max-width: 40%;
    font-family: var(--font-mono);
    letter-spacing: 0;
    text-transform: none;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 28px;
    border-top: 1px solid var(--border-hair);
    font-size: 11px;
    color: var(--text-muted);
  }

  .row.head {
    height: 22px;
    border-top: none;
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .c-dest {
    flex: 1 1 auto;
    font-family: var(--font-mono);
    color: var(--text);
  }

  .row.head .c-dest {
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

  .c-share {
    flex: 0 0 64px;
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

  .key {
    display: inline-grid;
    place-items: center;
    min-width: 18px;
    height: 18px;
    padding: 0 4px;
    border-radius: 4px;
    background: var(--bg-kbd);
    color: var(--text-muted);
    font-family: var(--font-mono);
    font-size: 10px;
  }
</style>
