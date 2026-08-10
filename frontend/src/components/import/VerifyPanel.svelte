<script lang="ts">
  // IMPORT · verify: the last screen before the files move.
  //
  // Everything above the button is what is about to happen stated plainly —
  // how many frames, where they land, whether a second copy is being written,
  // and whether it all fits. Everything below it is what happened. The bar is
  // driven by the backend's own events rather than by a timer, and a moving
  // import says so instead of pretending to a percentage it cannot measure.

  import {
    canReveal,
    formatBytes,
    formatCount,
    importState,
    phaseLabel,
    PHASE_MOVE,
  } from "../../lib/import.svelte";

  let plan = $derived(importState.plan);
  let progress = $derived(importState.progress);
  let done = $derived(importState.batch);
  // Anything that leaves the card counts as moving, including a card whose
  // frames were routed both ways: the warning is about whether the card stops
  // being a copy, and under a mixed plan it does.
  let moving = $derived(plan?.verb === "move" || plan?.verb === "mixed");
  let mixed = $derived(plan?.verb === "mixed");
</script>

<div class="verify">
  {#if plan === null}
    {#if importState.error !== null}
      <!-- Mirrors RoutePanel: a folder that would not read leaves no plan,
           and the reason beats "select a card" about a card just selected. -->
      <p class="empty">{importState.error}</p>
    {:else}
      <p class="empty">select a card on the left</p>
    {/if}
  {:else}
    <header>
      <span class="eyebrow">verify</span>
      <h2>
        {formatCount(plan.routed)}
        {plan.routed === 1 ? "frame" : "frames"} into the library
      </h2>
      <p class="path">
        {formatCount(plan.files)} files · {formatBytes(plan.bytes)}
        {#if importState.backup}· written twice{/if}
      </p>
    </header>

    <section>
      <div class="label"><span>second copy</span><span class="rule"></span></div>
      <label class="toggle">
        <input
          type="checkbox"
          checked={importState.backup}
          onchange={(e) => importState.setBackup(e.currentTarget.checked)}
        />
        <span class="toggle-name">write a second copy to a backup folder</span>
      </label>
      <input
        class="field"
        type="text"
        placeholder="/Volumes/Backup/2026"
        spellcheck="false"
        autocomplete="off"
        data-keys="local"
        aria-label="Backup folder"
        disabled={!importState.backup}
        value={importState.backupPath}
        oninput={(e) => importState.setBackupPath(e.currentTarget.value)}
      />
      <p class="note">
        the backup keeps the library's own layout, so a frame is in the same place on both — and
        both copies are one batch, so one undo takes the whole import back
      </p>
    </section>

    <section>
      <div class="label"><span>landing</span><span class="rule"></span></div>
      <div class="rows">
        {#each importState.space as space (space.destination)}
          <div class="row" class:tight={!space.fits} title={space.path}>
            <span class="cell c-dest">{space.destination}</span>
            <span class="cell c-vol">{space.volumeName || "unknown volume"}</span>
            <span class="cell c-num">{formatBytes(space.bytes)}</span>
            <span class="cell c-free">
              {space.total > 0 ? `${formatBytes(space.free)} free` : "capacity unknown"}
            </span>
          </div>
        {/each}
        {#if importState.space.length === 0}
          <p class="empty">nothing is routed in this folder</p>
        {/if}
      </div>
      {#if importState.overfull.length > 0}
        <p class="warn">
          {formatCount(importState.overfull.length)}
          {importState.overfull.length === 1 ? "destination has" : "destinations have"} less room than
          this import needs
        </p>
      {/if}
    </section>

    {#if importState.error !== null}
      <p class="error">{importState.error}</p>
    {/if}

    {#if importState.running}
      <section class="progress">
        <div class="label">
          <span>{phaseLabel(progress?.phase ?? "")}</span>
          <span class="rule"></span>
          <span class="hint">
            {progress !== null && progress.total > 0
              ? `${formatCount(progress.files)} / ${formatCount(progress.total)}`
              : "counting…"}
          </span>
        </div>
        <div
          class="track"
          role="progressbar"
          aria-label="Import progress"
          aria-valuemin="0"
          aria-valuemax={progress?.total ?? 0}
          aria-valuenow={importState.percentDone === null ? undefined : progress?.files}
        >
          {#if importState.percentDone === null}
            <span class="fill sweep"></span>
          {:else}
            <span class="fill" style:width="{importState.percentDone}%"></span>
          {/if}
        </div>
        <p class="note">
          {progress !== null && progress.phase === PHASE_MOVE
            ? "frames are being taken off the card, which the file counter cannot see"
            : `${formatBytes(progress?.bytes ?? 0)} written`}
        </p>
      </section>
    {:else if done !== null}
      <section class="done" class:failed={importState.failed > 0}>
        <div class="label">
          <span>{importState.failed > 0 ? "partly done" : "done"}</span>
          <span class="rule"></span>
        </div>
        <p class="summary">{done.description}</p>
        {#if importState.failed > 0}
          <p class="warn">
            {formatCount(importState.failed)} of {formatCount(done.actions.length)} files did not land — the
            frames they belong to kept their routing, so the import can be run again
          </p>
        {/if}
        {#if canReveal()}
          <div class="opens">
            {#each importState.space as space (space.destination)}
              <button type="button" class="open" onclick={() => importState.reveal(space.path)}>
                open {space.destination}
              </button>
            {/each}
          </div>
        {/if}
      </section>
    {/if}

    <button
      type="button"
      class="run"
      class:moving
      disabled={!importState.ready}
      onclick={() => void importState.execute()}
    >
      <span class="run-key">⏎</span>
      <span class="run-name">
        {#if importState.running}
          importing…
        {:else if plan.routed === 0}
          nothing routed to import
        {:else}
          {mixed ? "import" : moving ? "move" : "copy"}
          {formatCount(plan.routed)}
          {plan.routed === 1 ? "frame" : "frames"} into the library
        {/if}
      </span>
    </button>
    {#if moving}
      <p class="note centre">
        {mixed
          ? "some of these frames are routed to move — those come off the card, and for them the card stops being a copy"
          : "a moving import takes the frames off the card — the card stops being a copy"}
      </p>
    {/if}
  {/if}
</div>

<style>
  .verify {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    padding: 16px;
  }

  header {
    padding-bottom: 6px;
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
    font-size: 11px;
    color: var(--text-dim);
  }

  section {
    padding-top: 16px;
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
    color: var(--accent);
  }

  .toggle {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 8px;
    cursor: pointer;
  }

  .toggle input {
    width: 13px;
    height: 13px;
    accent-color: var(--accent);
  }

  .toggle-name {
    font-size: 11.5px;
    color: var(--text-2);
  }

  .field {
    width: 100%;
    height: 26px;
    padding: 0 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    font: inherit;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text);
    outline: none;
  }

  .field:focus {
    border-color: var(--border-focus);
  }

  .field:disabled {
    color: var(--text-dead);
    background: var(--bg-field-alt);
  }

  .note {
    margin: 6px 0 0;
    font-size: 10px;
    line-height: 1.5;
    color: var(--text-ghost);
  }

  .note.centre {
    text-align: center;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 28px;
    border-top: 1px solid var(--border-hair);
    font-size: 11px;
    color: var(--text-muted);
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

  .c-vol {
    flex: 0 0 120px;
    color: var(--text-dim);
  }

  .c-num {
    flex: 0 0 78px;
    text-align: right;
    font-family: var(--font-mono);
  }

  .c-free {
    flex: 0 0 116px;
    text-align: right;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-dim);
  }

  .row.tight .c-free {
    color: var(--cut-text);
  }

  .warn {
    margin: 8px 0 0;
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--amber);
  }

  .error {
    margin: 14px 0 0;
    padding: 8px 10px;
    border-radius: 5px;
    background: var(--cut-wash-14);
    font-size: 11px;
    color: var(--cut-text);
  }

  .track {
    display: flex;
    height: 5px;
    border-radius: 3px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  /* An indeterminate bar sweeps rather than sitting at zero: the card is being
     read and the app has no honest fraction to draw yet. */
  .fill.sweep {
    width: 34%;
    animation: sweep 1.1s ease-in-out infinite;
  }

  @keyframes sweep {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(300%);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .fill.sweep {
      animation: none;
      width: 100%;
      opacity: 0.4;
    }
  }

  .done .summary {
    margin: 0;
    font-size: 11.5px;
    color: var(--keep-text);
  }

  .done.failed .summary {
    color: var(--amber);
  }

  .opens {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding-top: 8px;
  }

  .open {
    height: 24px;
    padding: 0 10px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    font: inherit;
    font-size: 10.5px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .open:hover {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .run {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    width: 100%;
    margin-top: 18px;
    height: 38px;
    border-radius: 5px;
    border: 1px solid var(--keep-wash-20);
    background: var(--keep-wash-12);
    font: inherit;
    font-size: 12.5px;
    color: var(--keep-text);
    cursor: pointer;
  }

  .run:hover:not(:disabled) {
    background: var(--keep-wash-16);
  }

  .run.moving {
    border-color: var(--amber-wash-18);
    background: var(--amber-wash-14);
    color: var(--amber);
  }

  .run:disabled {
    border-color: var(--border);
    background: none;
    color: var(--text-dead);
    cursor: default;
  }

  .run:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .run-key {
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

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-ghost);
  }
</style>
