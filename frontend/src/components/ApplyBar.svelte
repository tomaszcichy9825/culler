<script lang="ts">
  // The pending-decision summary, and the plan panel that stands between a
  // decision and the disk. The panel takes no DOM focus: the global key layer
  // routes Enter and Esc to it while it is up, so nothing is ever trapped.

  import { cancelApply, confirmApply, exportXMP } from "../lib/actions";
  import { formatBytes } from "../lib/preview";
  import { app } from "../lib/state.svelte";
  import { PLAN_ORDER, PLAN_TERMS } from "../lib/verdict";

  let counts = $derived(app.verdictCounts);
  let total = $derived(app.pending.length);

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
</script>

<div class="bar">
  {#if total === 0}
    <span class="muted">nothing judged yet</span>
  {:else}
    <span class="count">{total} pending</span>
    {#if counts.keep > 0}
      <span class="chip keep">k keep · {counts.keep}</span>
    {/if}
    {#if counts.cut > 0}
      <span class="chip cut">x cut · {counts.cut}</span>
    {/if}
    {#if routed > 0}
      <span class="chip routed">→ routed · {routed}</span>
    {/if}
    <span class="muted">↩ to apply</span>
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
        <button class="ghost" onclick={cancelApply}>Esc — cancel</button>
        <button class="primary" onclick={confirmApply} disabled={app.busy}>↩ — apply</button>
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
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
</style>
