<script lang="ts">
  // The confirmation that stands between a dropped pin and the files. Writing a
  // location is a metadata write like any other, so it is planned first and
  // shown here — how many frames, which files, and the reassurance that the
  // originals are backed up and the whole thing undoes in one step.
  //
  // The panel takes no DOM focus: the global key layer routes Enter and Esc to
  // it while it is up, the same way the apply plan does.

  import { geotag } from "../../lib/geotag.svelte";

  let plan = $derived(geotag.plan);
</script>

{#if plan}
  <div class="scrim">
    <div class="panel" role="dialog" aria-label="Confirm location">
      <h2>Set location</h2>
      <p class="description">{plan.description}</p>

      <dl>
        <div class="row">
          <dt>frames</dt>
          <dd>{plan.frames}</dd>
        </div>
        <div class="row">
          <dt>files written</dt>
          <dd>{plan.writes}</dd>
        </div>
        {#each plan.rows.slice(0, 6) as row (row.target + row.tag)}
          <div class="row">
            <dt title={row.target}>{row.target}</dt>
            <dd>{row.value} · {row.method}</dd>
          </div>
        {/each}
        {#if plan.rows.length > 6}
          <div class="row muted">
            <dt>and more</dt>
            <dd>{plan.rows.length - 6} further</dd>
          </div>
        {/if}
      </dl>

      {#each plan.assurances as line (line)}
        <p class="assure">{line}</p>
      {/each}
      {#each plan.warnings as line (line)}
        <p class="warn">{line}</p>
      {/each}

      <div class="buttons">
        <button class="ghost" onclick={() => geotag.cancel()}>Esc — cancel</button>
        <button class="primary" onclick={() => void geotag.confirm()} disabled={geotag.busy}>↩ — write</button>
      </div>
    </div>
  </div>
{/if}

<style>
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
    margin: 0 0 14px;
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

  .row.muted {
    color: var(--text-dim);
    border-bottom: none;
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

  .assure {
    margin: 0 0 6px;
    font-size: 11px;
    color: var(--text-dim);
  }

  .warn {
    margin: 0 0 6px;
    font-size: 11px;
    color: var(--cut);
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
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
