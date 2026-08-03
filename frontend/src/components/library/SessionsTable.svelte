<script lang="ts">
  // The sessions table (5b): one row per shoot, newest first.
  //
  // Columns follow the design — date, session, source, frames, kept/cut, on
  // disk, state — minus the "freed" column, which counts bytes an apply has
  // already reclaimed. Nothing records that yet, and a column of zeroes would
  // read as "nothing was freed" rather than "this is not known".

  import StackedBar from "./StackedBar.svelte";
  import {
    formatBytes,
    formatClock,
    formatCount,
    formatDate,
    formatSpan,
    library,
  } from "../../lib/library.svelte";

  /** What the row's state cell says, from the counts alone. */
  function stateOf(kept: number, cut: number, undecided: number): { label: string; tone: string } {
    if (kept + cut === 0) return { label: "untouched", tone: "idle" };
    if (undecided > 0) return { label: `${formatCount(undecided)} to judge`, tone: "open" };
    return { label: "judged", tone: "done" };
  }

  let body = $state<HTMLDivElement | null>(null);

  function focusRow(index: number) {
    if (index < 0 || index >= library.sessions.length) return;
    library.focusSession(index);
    if (body?.contains(document.activeElement) === true) {
      queueMicrotask(() => body?.querySelector<HTMLElement>(`[data-row="${index}"]`)?.focus());
    }
  }

  /**
   * The table's own keys, local so the mode keymap does not also see them. ⏎
   * opens the session's folder in cull, which is the same one-keystroke
   * contract the tree and the results grid keep.
   */
  function onKeydown(e: KeyboardEvent) {
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        focusRow(library.sessionIndex + 1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        focusRow(library.sessionIndex - 1);
        break;
      case "Home":
        e.preventDefault();
        focusRow(0);
        break;
      case "End":
        e.preventDefault();
        focusRow(library.sessions.length - 1);
        break;
      case "Enter": {
        e.preventDefault();
        const session = library.sessions[library.sessionIndex];
        if (session !== undefined) library.openDir(session.dir);
        break;
      }
      case "Escape":
        e.preventDefault();
        (document.activeElement as HTMLElement | null)?.blur();
        break;
    }
  }
</script>

<div class="sessions">
  <div class="head" role="row">
    <span class="cell c-date sorted">date ↓</span>
    <span class="cell c-session">session</span>
    <span class="cell c-source">source</span>
    <span class="cell c-frames num">frames</span>
    <span class="cell c-split">kept / cut</span>
    <span class="cell c-size num">on disk</span>
    <span class="cell c-state">state</span>
  </div>

  <div class="body" bind:this={body} data-keys="local" role="rowgroup">
    {#if library.sessions.length === 0}
      <p class="empty">no sessions — nothing is catalogued yet</p>
    {:else}
      {#each library.sessions as session, i (session.id)}
        {@const state = stateOf(session.kept, session.cut, session.undecided)}
        <button
          type="button"
          class="row"
          class:selected={library.selectedSession === session.id}
          role="row"
          data-row={i}
          data-dir={session.dir}
          tabindex={i === library.sessionIndex ? 0 : -1}
          title="{session.dir} — ⏎ opens it in cull"
          onfocus={() => library.focusSession(i)}
          onkeydown={onKeydown}
          onclick={() => library.selectSession(session.id)}
          ondblclick={() => library.openDir(session.dir)}
        >
          <span class="cell c-date">{formatDate(session.start)}</span>
          <span class="cell c-session">
            {formatClock(session.start)}–{formatClock(session.end)}
            <span class="span">· {formatSpan(session.spanMinutes)}</span>
          </span>
          <span class="cell c-source" title={session.dir}>{session.source}</span>
          <span class="cell c-frames num">{formatCount(session.frames)}</span>
          <span class="cell c-split">
            <StackedBar
              height={5}
              total={session.frames}
              segments={[
                { label: "kept", value: session.kept, colour: "var(--keep)" },
                { label: "cut", value: session.cut, colour: "var(--cut)" },
                { label: "undecided", value: session.undecided, colour: "var(--neutral-bar)" },
              ]}
            />
            <span class="ratio">{session.kept} / {session.cut}</span>
          </span>
          <span class="cell c-size num">{formatBytes(session.bytes)}</span>
          <span class="cell c-state {state.tone}">{state.label}</span>
        </button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .sessions {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .head,
  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 0 12px;
  }

  .head {
    flex: 0 0 26px;
    height: 26px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
    font-size: 10px;
    letter-spacing: 0.1em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .head .sorted {
    color: var(--accent);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    outline: none;
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .row {
    width: 100%;
    height: 38px;
    margin: 0;
    border: none;
    border-bottom: 1px solid var(--border-hair);
    background: transparent;
    font: inherit;
    font-size: 11.5px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
    appearance: none;
    outline: none;
  }

  .row:hover {
    background: var(--bg-row-zebra);
  }

  .row.selected {
    background: var(--bg-row-active);
    box-shadow: var(--focus-edge);
  }

  .row:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .num {
    text-align: right;
    font-family: var(--font-mono);
  }

  .c-date {
    flex: 0 0 84px;
    color: var(--accent);
    font-family: var(--font-mono);
  }
  .c-session {
    flex: 1 1 120px;
    color: var(--text);
  }
  .c-source {
    flex: 0 0 108px;
  }
  .c-frames {
    flex: 0 0 58px;
  }
  .c-split {
    flex: 0 0 120px;
    display: flex;
    flex-direction: column;
    gap: 3px;
    overflow: visible;
  }
  .c-size {
    flex: 0 0 68px;
  }
  .c-state {
    flex: 0 0 104px;
    font-size: 10.5px;
  }

  .span {
    color: var(--text-dim);
  }

  .ratio {
    font-family: var(--font-mono);
    font-size: 9.5px;
    color: var(--text-dim);
  }

  .c-state.done {
    color: var(--keep-text);
  }
  .c-state.open {
    color: var(--gold);
  }
  .c-state.idle {
    color: var(--text-dead);
  }
</style>
