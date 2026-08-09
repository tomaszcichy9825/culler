<script lang="ts">
  // The pending-decision summary, and the plan panel that stands between a
  // decision and the disk. The panel takes no DOM focus: the global key layer
  // routes Enter and Esc to it while it is up, so nothing is ever trapped.

  import { cancelApply, confirmApply, exportXMP, filterByVerdict } from "../lib/actions";
  import { filterIsSet, filterSummary, palette } from "../lib/palette.svelte";
  import { formatBytes } from "../lib/preview";
  import { app } from "../lib/state.svelte";
  import { PLAN_ORDER, PLAN_TERMS } from "../lib/verdict";

  let counts = $derived(app.verdictCounts);
  let total = $derived(app.pending.length);
  let filterVerdict = $derived(palette.filter.verdict);

  // A narrowing filter needs an escape hatch that survives everything: the
  // verdict pills vanish once their count hits zero — uncut every cut and the
  // "cut" pill that set the filter is gone while the filter it set stays on —
  // so the way out cannot live on the pills. This chip shows whenever anything
  // is narrowing the grid, whatever set it.
  let filterActive = $derived(filterIsSet(palette.filter));

  /**
   * The plan's own counts still arrive keyed in the pre-verdict vocabulary, so
   * the dialog names them rather than showing the keys.
   */
  let planCounts = $derived(app.plan?.counts ?? {});

  /** verbCounts is how many files each verb touches, from the planned actions. */
  let verbCounts = $derived.by(() => {
    const out: Record<string, number> = {};
    for (const a of app.plan?.actions ?? []) out[a.verb] = (out[a.verb] ?? 0) + 1;
    return out;
  });

  /**
   * Where the plan sends frames, one row per destination. This is the part of
   * the summary worth reading twice: everything else in the dialog is about
   * files leaving, and this is about files arriving somewhere the user named
   * possibly several minutes and several hundred frames ago.
   */
  let routes = $derived(app.plan?.destinations ?? []);

  /** How many frames in the sheet are routed, for the pending strip. */
  let routed = $derived(app.pending.filter((g) => (g.destination ?? "") !== "").length);

  /**
   * What the confirm button says. An apply moves real files and can take a
   * while over a share, so once it is running the button stops offering to
   * start one and starts reporting how far it has got. Without a count yet it
   * still says it is working: the wait is the thing that needed explaining.
   */
  let applyLabel = $derived.by(() => {
    const p = app.applyProgress;
    if (p === null) return "↩ — apply";
    if (p.phase === "planning") return "planning…";
    if (p.total > 0) return `applying… ${p.done}/${p.total}`;
    return "applying…";
  });
</script>

<div class="bar">
  {#if total === 0}
    <span class="muted">nothing judged yet</span>
  {:else}
    <span class="count">{total} pending</span>
    <!-- The pills are the review list: clicking one shows just those frames
         in the grid; clicking the active one shows everything again. -->
    {#if counts.keep > 0}
      <button
        class="chip keep"
        class:active={filterVerdict === "keep"}
        onclick={() => filterByVerdict("keep")}
        title="Show only the keeps"
      >
        k keep · {counts.keep}
      </button>
    {/if}
    {#if counts.cut > 0}
      <button
        class="chip cut"
        class:active={filterVerdict === "cut"}
        onclick={() => filterByVerdict("cut")}
        title="Show only the cuts"
      >
        x cut · {counts.cut}
      </button>
    {/if}
    {#if routed > 0}
      <span class="chip routed">→ routed · {routed}</span>
    {/if}
    <span class="muted">↩ to apply</span>
  {/if}
  <!-- Outside the pending guard on purpose: a filter can strand the grid on an
       empty set with nothing judged, and the way back has to be here for that. -->
  {#if filterActive}
    <button
      class="chip filter-clear"
      onclick={() => palette.clearFilter()}
      title="Show every frame again"
    >
      ✕ {filterSummary(palette.filter)}
    </button>
  {/if}
  <!-- The volume, the selection, the busy flag and the key hints all live in
       the title bar and the status bar now; this strip is only the pending
       decisions and the way to apply them. -->
  <span class="spacer"></span>
  {#if app.folder !== null}
    <button class="xmp" onclick={() => void exportXMP()} disabled={app.busy} title="Write XMP sidecars for Lightroom and Bridge">
      ✧ Write XMP
    </button>
  {/if}
</div>

{#if app.plan}
  <div class="scrim">
    <div class="panel" role="dialog" aria-label="Confirm apply">
      <h2>Apply</h2>
      <p class="description">{app.plan.description}</p>

      {#if routes.length > 0}
        <dl class="routes" aria-label="Destinations">
          {#each routes as route (route.path)}
            <div class="row">
              <dt title={route.path}>{route.verb} → {route.path}</dt>
              <dd>{route.frames} frames · {route.files} files · {formatBytes(route.bytes)}</dd>
            </div>
          {/each}
        </dl>
      {/if}

      <dl>
        {#each PLAN_ORDER as key (key)}
          {#if planCounts[key]}
            <div class="row">
              <dt>{PLAN_TERMS[key]}</dt>
              <dd>{planCounts[key]} frames</dd>
            </div>
          {/if}
        {/each}
        {#each Object.entries(verbCounts) as [verb, n]}
          <div class="row">
            <dt>{verb}</dt>
            <dd>{n} files</dd>
          </div>
        {/each}
        <div class="row total">
          <dt>total</dt>
          <dd>{formatBytes(app.plan.totalBytes)}</dd>
        </div>
      </dl>

      <div class="buttons">
        <button class="ghost" onclick={cancelApply} disabled={app.busy}>Esc — cancel</button>
        <button class="primary" class:working={app.busy} onclick={confirmApply} disabled={app.busy}>
          {applyLabel}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .xmp {
    flex: 0 0 auto;
    font: inherit;
    font-size: 11px;
    padding: 3px 9px;
    border-radius: 5px;
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }

  .xmp:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--text);
  }

  .xmp:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 14px;
    border-top: 1px solid var(--border);
    background: var(--bg-chrome);
    font-size: 12px;
    color: var(--text-muted);
    flex: 0 0 auto;
    min-width: 0;
    overflow: hidden;
  }

  .count {
    flex: 0 0 auto;
    font-weight: 600;
    color: var(--text);
    white-space: nowrap;
  }

  .muted {
    flex: 0 1 auto;
    min-width: 0;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1 1 auto;
  }

  .chip {
    flex: 0 1 auto;
    min-width: 0;
    padding: 2px 7px;
    border-radius: 4px;
    color: var(--on-accent);
    font: inherit;
    font-weight: 600;
    border: 1px solid transparent;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The keep and cut chips are buttons; the routed one is still a span. */
  button.chip {
    cursor: pointer;
  }

  button.chip:hover {
    filter: brightness(1.12);
  }

  /* The active review filter is ringed so it is clear which list is showing. */
  button.chip.active {
    border-color: var(--on-accent);
    box-shadow: 0 0 0 1px var(--on-accent);
  }

  .chip.keep {
    background: var(--keep);
  }

  .chip.cut {
    background: var(--cut);
  }

  .chip.routed {
    background: var(--accent);
  }

  /* The filter escape hatch is a quiet outline, not a coloured verdict pill:
     it is a state to leave, not a decision to read. */
  button.chip.filter-clear {
    flex: 0 1 auto;
    background: var(--bg-field);
    color: var(--text-muted);
    border-color: var(--border-strong);
  }

  button.chip.filter-clear:hover {
    filter: none;
    border-color: var(--accent);
    color: var(--text);
  }

  /* Destinations lead the summary and are marked off from the verdict counts:
     they are the only rows describing files arriving rather than leaving. */
  .routes {
    margin: 0 0 12px;
    padding: 8px 10px;
    border-radius: 6px;
    background: var(--accent-wash-16);
    font-size: 12px;
  }

  .routes .row {
    border-bottom-color: var(--border-faint);
  }

  .routes .row:last-child {
    border-bottom: none;
  }

  .routes dt {
    color: var(--accent);
    font-weight: 600;
  }

  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    display: grid;
    place-items: center;
    background: var(--scrim-plan);
  }

  .panel {
    width: min(440px, 86vw);
    padding: 18px 20px;
    border-radius: 10px;
    background: var(--bg-chrome);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow-dialog);
  }

  h2 {
    margin: 0 0 4px;
    font-size: 15px;
    color: var(--text);
  }

  .description {
    margin: 0 0 14px;
    font-size: 12px;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }

  dl {
    margin: 0 0 16px;
    font-size: 12px;
  }

  .row {
    display: flex;
    justify-content: space-between;
    padding: 4px 0;
    border-bottom: 1px solid var(--border);
    gap: 12px;
    min-width: 0;
  }

  .row.total {
    border-bottom: none;
    color: var(--text);
    font-weight: 600;
  }

  dt,
  dd {
    margin: 0;
    min-width: 0;
    color: inherit;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  dt {
    color: var(--text-muted);
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  button {
    font: inherit;
    font-size: 12px;
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    border: 1px solid var(--border-strong);
    white-space: nowrap;
  }

  .ghost {
    background: transparent;
    color: var(--text-muted);
  }

  .primary {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  /* A working apply is not the same as a disabled one: it is doing what it was
     asked to. It keeps its colour and pulses so the dialog cannot be mistaken
     for one that swallowed the keystroke. */
  .primary.working:disabled {
    opacity: 1;
    animation: pulse 1.4s ease-in-out infinite;
  }

  @keyframes pulse {
    50% {
      opacity: 0.62;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .primary.working:disabled {
      animation: none;
      opacity: 0.8;
    }
  }

  .ghost:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
