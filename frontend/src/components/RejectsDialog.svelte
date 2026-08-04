<script lang="ts">
  // Emptying the rejects: the only destruction in the application, and the one
  // dialog whose job is to slow the user down.
  //
  // It lists every rejected folder it found and what is in it — RAW, JPEG,
  // matched pairs, sidecars, anything else, and the bytes — before anything is
  // touched, then asks for the word to be typed out. Nothing has happened while
  // this is on screen; the run starts on ⏎ with the word complete and not
  // before.
  //
  // Containment is the established data-keys="local" pattern rather than a
  // focus trap: the panel takes the keyboard while it is up, keeps its own key
  // events away from the window listener, and gives everything back on Esc. Tab
  // is swallowed rather than trapped.

  import { CONFIRM_WORD, formatSize, rejects } from "../lib/rejects.svelte";

  let survey = $derived(rejects.survey);
  let panel = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (rejects.open) panel?.focus();
  });

  /** The letters of the word, drawn as caps that fill in as they are typed. */
  let letters = $derived(CONFIRM_WORD.split(""));

  let percent = $derived.by(() => {
    const p = rejects.progress;
    if (p === null || p.total <= 0) return 0;
    return Math.min(100, Math.round((p.done / p.total) * 100));
  });

  function onkeydown(e: KeyboardEvent) {
    // Nothing typed into this dialog belongs to the window listener — least of
    // all the Enter that applies verdicts and the letters that pass judgement
    // on frames.
    e.stopPropagation();
    if (e.key === "Escape") {
      e.preventDefault();
      rejects.cancel();
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      void rejects.confirm();
      return;
    }
    if (e.key === "Backspace") {
      e.preventDefault();
      rejects.backspace();
      return;
    }
    if (e.key === "Tab") {
      // Nothing in here is reachable by Tab, and letting focus walk out from
      // behind a dialog that deletes photographs is worse than swallowing it.
      e.preventDefault();
      return;
    }
    if (e.key.length === 1 && !e.metaKey && !e.ctrlKey) {
      e.preventDefault();
      rejects.type(e.key);
    }
  }
</script>

{#if rejects.open}
  <!-- The scrim is not a click target here: this dialog is dismissed
       deliberately, not by a stray click beside it. -->
  <div class="scrim" data-keys="local" data-testid="empty-rejects">
    <div
      class="panel"
      bind:this={panel}
      role="dialog"
      aria-modal="true"
      aria-label="Empty rejects"
      tabindex="-1"
      {onkeydown}
    >
      <header>
        <span class="subject">Empty rejects</span>
        {#if survey !== null && survey.folder !== ""}
          <span class="folder">{survey.folder}</span>
        {/if}
        <span class="spacer"></span>
        {#if survey !== null}
          <span class="tally">
            {survey.files} file{survey.files === 1 ? "" : "s"} · {formatSize(survey.totalBytes)}
          </span>
        {/if}
      </header>

      <div class="body">
        {#if rejects.surveying}
          <p class="quiet">counting…</p>
        {:else if survey === null}
          <p class="quiet">nothing surveyed</p>
        {:else if survey.files === 0}
          <p class="quiet">no rejected files in the open folder or any catalogued folder</p>
        {:else}
          <div class="totals">
            <div class="figure">
              <span class="n">{survey.raw}</span>
              <span class="k">RAW</span>
            </div>
            <div class="figure">
              <span class="n">{survey.jpeg}</span>
              <span class="k">JPEG</span>
            </div>
            <div class="figure">
              <span class="n">{survey.pairs}</span>
              <span class="k">pair{survey.pairs === 1 ? "" : "s"}</span>
            </div>
            <div class="figure">
              <span class="n">{survey.sidecars}</span>
              <span class="k">sidecar{survey.sidecars === 1 ? "" : "s"}</span>
            </div>
            {#if survey.other > 0}
              <div class="figure">
                <span class="n">{survey.other}</span>
                <span class="k">other</span>
              </div>
            {/if}
            <div class="figure bytes">
              <span class="n">{formatSize(survey.totalBytes)}</span>
              <span class="k">total</span>
            </div>
          </div>

          <div class="rows" role="table" aria-label="Rejected folders">
            <div class="line head" role="row">
              <span class="path" role="columnheader">folder</span>
              <span class="num" role="columnheader">raw</span>
              <span class="num" role="columnheader">jpeg</span>
              <span class="num" role="columnheader">pairs</span>
              <span class="num" role="columnheader">files</span>
              <span class="size" role="columnheader">bytes</span>
            </div>
            {#each survey.dirs as row (row.path)}
              <div class="line" role="row">
                <span class="path" role="cell" title={row.path}>{row.path}</span>
                <span class="num" role="cell">{row.raw}</span>
                <span class="num" role="cell">{row.jpeg}</span>
                <span class="num" role="cell">{row.pairs}</span>
                <span class="num" role="cell">{row.files}</span>
                <span class="size" role="cell">{formatSize(row.bytes)}</span>
              </div>
            {/each}
          </div>
        {/if}

        {#if rejects.result !== null && rejects.result.errors.length > 0}
          <div class="failures">
            {#each rejects.result.errors as line (line)}
              <p class="fail">{line}</p>
            {/each}
          </div>
        {/if}
      </div>

      <footer>
        <p class="warn">
          <span aria-hidden="true">!</span>
          These files are deleted permanently. They do not go to the trash, and undo cannot bring them
          back.
        </p>
        {#if rejects.error !== ""}
          <p class="error" role="alert">{rejects.error}</p>
        {/if}

        {#if rejects.running}
          <div class="progress" role="status">
            <span class="count">
              {rejects.progress?.done ?? 0} / {rejects.progress?.total ?? 0}
            </span>
            <span class="track"><span class="fill" style="width: {percent}%"></span></span>
          </div>
        {/if}

        <div class="confirm">
          <span class="ask">type <strong>{CONFIRM_WORD}</strong> to allow it</span>
          <span class="word" aria-hidden="true">
            {#each letters as letter, i (i)}
              <span class="cap" class:on={i < rejects.typed.length}>{letter}</span>
            {/each}
          </span>
          <label class="sr" for="rejects-typed">confirmation word</label>
          <input
            id="rejects-typed"
            class="sr"
            type="text"
            readonly
            tabindex="-1"
            value={rejects.typed}
          />
        </div>

        <div class="buttons">
          <button type="button" class="secondary" onclick={() => rejects.cancel()} disabled={rejects.running}>
            esc cancel
          </button>
          <span class="spacer"></span>
          <button type="button" class="destroy" disabled={!rejects.armed} onclick={() => void rejects.confirm()}>
            {rejects.running ? "destroying…" : "delete permanently ⏎"}
          </button>
        </div>
      </footer>
    </div>
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 45;
    display: grid;
    place-items: center;
    padding: 60px;
    background: var(--scrim-plan);
  }

  .panel {
    display: flex;
    flex-direction: column;
    width: 760px;
    max-width: 100%;
    max-height: 100%;
    border-radius: 8px;
    background: var(--bg-chrome);
    /* The one dialog that is outlined in the cut colour: it is the only thing
       in the app that removes a file for good. */
    border: 1px solid var(--cut);
    box-shadow: var(--shadow-dialog);
    outline: none;
  }

  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 13px 16px;
    border-bottom: 1px solid var(--border);
  }

  .subject {
    font-size: 13px;
    font-weight: 700;
    color: var(--text-hi);
  }

  .folder {
    padding: 2px 7px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .tally {
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .quiet {
    margin: 0;
    padding: 18px 16px;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .totals {
    display: flex;
    flex-wrap: wrap;
    gap: 22px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
  }

  .figure {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .figure .n {
    font-size: 17px;
    font-weight: 700;
    color: var(--text-hi);
  }

  .figure.bytes .n {
    color: var(--cut);
  }

  .figure .k {
    font-size: 9.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  /* The per-folder breakdown scrolls sideways on its own rather than making
     the panel do it: a deep path must not push the counts off the edge. */
  .rows {
    overflow-x: auto;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 520px;
    height: 28px;
    padding: 0 16px;
    font-size: 11px;
    border-bottom: 1px solid var(--border-faint);
  }

  .line.head {
    height: 24px;
    font-size: 9.5px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .path {
    flex: 1;
    min-width: 0;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .num {
    flex: 0 0 46px;
    text-align: right;
    color: var(--text-muted);
  }

  .size {
    flex: 0 0 74px;
    text-align: right;
    color: var(--text);
  }

  .line.head .num,
  .line.head .size,
  .line.head .path {
    color: var(--text-faint);
  }

  .failures {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 10px 16px;
  }

  .fail {
    margin: 0;
    font-size: 10.5px;
    color: var(--cut);
    overflow-wrap: anywhere;
  }

  footer {
    padding: 10px 16px 12px;
    border-top: 1px solid var(--border);
    background: var(--bg-raised);
  }

  .warn {
    display: flex;
    gap: 8px;
    margin: 0 0 8px;
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--amber);
    text-wrap: pretty;
  }

  .error {
    margin: 0 0 8px;
    font-size: 10.5px;
    color: var(--cut);
    overflow-wrap: anywhere;
  }

  .progress {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-bottom: 10px;
  }

  .progress .count {
    font-size: 10.5px;
    color: var(--accent);
    white-space: nowrap;
  }

  .track {
    flex: 1;
    height: 5px;
    border-radius: 3px;
    background: var(--bg-field);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--cut);
  }

  .confirm {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-bottom: 10px;
  }

  .ask {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .ask strong {
    color: var(--cut);
    letter-spacing: 0.08em;
  }

  .word {
    display: flex;
    gap: 4px;
  }

  .cap {
    width: 18px;
    height: 21px;
    display: grid;
    place-items: center;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 11px;
    font-weight: 700;
    color: var(--text-ghost);
  }

  .cap.on {
    background: var(--cut-wash-14);
    border-color: var(--cut);
    color: var(--cut);
  }

  /* The field carries the typed value for assistive technology; the caps above
     are what is actually read at a glance. */
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    border: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 8px;
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

  .destroy {
    padding: 6px 14px;
    border-radius: 5px;
    background: var(--cut);
    border: 1px solid var(--cut);
    font: inherit;
    font-size: 11.5px;
    font-weight: 700;
    color: var(--on-accent);
    cursor: pointer;
    appearance: none;
  }

  .destroy:disabled,
  .secondary:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
