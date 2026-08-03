<script lang="ts">
  // Screen 3d: the write plan.
  //
  // Nothing has touched a file when this is on screen. It lists every line the
  // write will produce — sign, target, tag, value, method — and then states
  // the two things the user is owed before agreeing: where the originals go,
  // and what is going to be skipped. The dialog is the last point at which a
  // metadata write is free, so it says what it will do in the plainest terms
  // available and puts cancel first.
  //
  // It is marked data-keys="local" so the application's global key listener
  // yields while the dialog owns the keyboard.

  import { exifState } from "../../lib/exif.svelte";

  let plan = $derived(exifState.plan);

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      exifState.cancelWrite();
    } else if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      void exifState.confirmWrite();
    }
  }

  let panel = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (plan !== null) panel?.focus();
  });
</script>

{#if plan !== null}
  <div class="scrim" data-keys="local" data-testid="exif-write-plan">
    <div
      class="panel"
      bind:this={panel}
      role="dialog"
      aria-modal="true"
      aria-label="Write metadata"
      tabindex="-1"
      {onkeydown}
    >
      <header>
        <span class="subject">Write metadata</span>
        <span class="tally">
          {plan.writes} write{plan.writes === 1 ? "" : "s"} · {plan.frames} frame{plan.frames === 1 ? "" : "s"} · {plan.files}
          file{plan.files === 1 ? "" : "s"}
        </span>
      </header>

      <div class="body">
        {#if plan.rows.length === 0}
          <p class="empty">nothing to write</p>
        {:else}
          {#each plan.rows as row, i (`${row.target}-${row.tag}-${i}`)}
            <div class="line">
              <span class="sign {row.sign === '−' ? 'remove' : 'add'}">{row.sign}</span>
              <span class="target" title={row.target}>{row.target}</span>
              <span class="tag">{row.tag}</span>
              <span class="value" title={row.value}>{row.value}</span>
              <span class="method" class:skipped={row.method === "skipped"}>{row.method}</span>
            </div>
          {/each}
        {/if}
      </div>

      <footer>
        <div class="assurances">
          {#each plan.assurances as line (line)}
            <p class="ok"><span aria-hidden="true">✓</span>{line}</p>
          {/each}
          {#each plan.warnings as line (line)}
            <p class="warn"><span aria-hidden="true">!</span>{line}</p>
          {/each}
        </div>
        <div class="buttons">
          <button type="button" class="secondary" onclick={() => exifState.cancelWrite()}>esc cancel</button>
          <span class="spacer"></span>
          <button
            type="button"
            class="primary"
            disabled={plan.writes === 0 || exifState.writing}
            onclick={() => void exifState.confirmWrite()}
          >
            {exifState.writing ? "writing…" : "write ⏎"}
          </button>
        </div>
      </footer>
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 60px;
    background: var(--scrim-plan);
    z-index: 40;
  }

  .panel {
    display: flex;
    flex-direction: column;
    width: 760px;
    max-width: 100%;
    max-height: 100%;
    border-radius: 8px;
    background: var(--bg-chrome);
    border: 1px solid var(--border-dialog);
    box-shadow: var(--shadow-dialog);
    outline: none;
  }

  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 13px 16px;
    border-bottom: 1px solid var(--border);
  }

  .subject {
    font-size: 13px;
    font-weight: 700;
    color: var(--text-hi);
  }

  .tally {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  /* 28px rows: sign · target · tag · value · method, per the design's widths. */
  .line {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 28px;
    padding: 0 16px;
    font-size: 11px;
  }

  .sign {
    flex: 0 0 14px;
    font-weight: 700;
  }

  .sign.add {
    color: var(--keep);
  }

  .sign.remove {
    color: var(--cut);
  }

  .target {
    flex: 0 0 220px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    flex: 0 0 176px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .value {
    flex: 1;
    min-width: 0;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .method {
    flex: 0 0 74px;
    font-size: 10px;
    color: var(--text-dim);
    text-align: right;
  }

  .method.skipped {
    color: var(--amber);
  }

  .empty {
    margin: 0;
    padding: 16px;
    font-size: 10.5px;
    color: var(--text-ghost);
  }

  footer {
    padding: 9px 16px 12px;
    border-top: 1px solid var(--border);
    background: var(--bg-raised);
  }

  .assurances {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding-bottom: 10px;
  }

  .ok,
  .warn {
    display: flex;
    gap: 8px;
    margin: 0;
    font-size: 10.5px;
    line-height: 1.5;
    text-wrap: pretty;
  }

  .ok {
    color: var(--keep);
  }

  .warn {
    color: var(--amber);
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .spacer {
    flex: 1;
  }

  .secondary {
    padding: 6px 12px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    cursor: pointer;
    appearance: none;
  }

  .primary {
    padding: 6px 14px;
    border-radius: 5px;
    background: var(--keep);
    border: 1px solid var(--keep);
    font: inherit;
    font-size: 11.5px;
    font-weight: 700;
    color: var(--on-accent);
    cursor: pointer;
    appearance: none;
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
