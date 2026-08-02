<script lang="ts">
  // The pending-decision summary, and the plan panel that stands between a
  // decision and the disk. The panel takes no DOM focus: the global key layer
  // routes Enter and Esc to it while it is up, so nothing is ever trapped.

  import { cancelApply, confirmApply } from "../lib/actions";
  import { decisionBadge, decisionLabel, formatBytes } from "../lib/preview";
  import { app } from "../lib/state.svelte";
  import NetworkChip from "./NetworkChip.svelte";

  const order = ["keep_all", "drop_raw", "drop_jpeg", "drop_all"];

  let counts = $derived(app.pendingCounts);
  let total = $derived(app.pending.length);

  /** verbCounts is how many files each verb touches, from the planned actions. */
  let verbCounts = $derived.by(() => {
    const out: Record<string, number> = {};
    for (const a of app.plan?.actions ?? []) out[a.verb] = (out[a.verb] ?? 0) + 1;
    return out;
  });
</script>

<div class="bar">
  {#if total === 0}
    <span class="muted">no pending decisions</span>
  {:else}
    <span class="count">{total} pending</span>
    {#each order as decision}
      {#if counts[decision]}
        <span class="chip {decision}">{decisionBadge(decision)} {decisionLabel(decision)} · {counts[decision]}</span>
      {/if}
    {/each}
    <span class="muted">↩ to apply</span>
  {/if}
  <span class="spacer"></span>
  {#if app.folder?.network}<NetworkChip />{/if}
  {#if app.selection.size > 0}
    <span class="muted">{app.selection.size} selected</span>
  {/if}
  {#if app.busy}<span class="muted">working…</span>{/if}
  <span class="muted">? for keys</span>
</div>

{#if app.plan}
  <div class="scrim">
    <div class="panel" role="dialog" aria-label="Confirm apply">
      <h2>Apply</h2>
      <p class="description">{app.plan.description}</p>

      <dl>
        {#each order as decision}
          {#if counts[decision]}
            <div class="row">
              <dt>{decisionLabel(decision)}</dt>
              <dd>{counts[decision]} frames</dd>
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
    color: var(--text-faint);
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
    color: var(--on-decision);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip.keep_all {
    background: var(--keep-all);
  }
  .chip.drop_raw {
    background: var(--drop-raw);
  }
  .chip.drop_jpeg {
    background: var(--drop-jpeg);
  }
  .chip.drop_all {
    background: var(--drop-all);
  }

  .scrim {
    position: fixed;
    inset: 0;
    z-index: 30;
    display: grid;
    place-items: center;
    background: var(--scrim);
  }

  .panel {
    width: min(440px, 86vw);
    padding: 18px 20px;
    border-radius: 10px;
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow-panel);
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
    color: var(--accent-text);
    font-weight: 600;
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
