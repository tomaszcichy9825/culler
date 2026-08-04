<script lang="ts">
  // The Sessions group in the sidebar: the shoots the catalogue has worked out
  // from the gaps between frames, newest first.
  //
  // A session is not a folder the user made, so it is named by when it
  // happened rather than by a path — the day, how long it ran, and how many
  // frames came out of it. Opening one loads the folder those frames are
  // filed in, which is the only thing CULL can be pointed at.

  import { formatCount, formatSpan, library, sessionLabel } from "../../lib/library.svelte";

  let container = $state<HTMLDivElement | null>(null);

  // Same arrangement as the tree: the group fills itself the first time it is
  // drawn, so the sidebar is populated without anything else asking.
  $effect(() => {
    void library.ensureSessions();
  });

  function rowEl(index: number): HTMLElement | null {
    return container?.querySelector(`[data-session="${index}"]`) ?? null;
  }

  function holdsFocus(): boolean {
    return container !== null && container.contains(document.activeElement);
  }

  function focusRow(index: number) {
    const clamped = Math.max(0, Math.min(index, library.sessions.length - 1));
    library.focusSession(clamped);
    if (holdsFocus()) queueMicrotask(() => rowEl(clamped)?.focus());
  }

  function onKeydown(e: KeyboardEvent) {
    const session = library.sessions[library.sessionIndex];
    if (session === undefined) return;
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
      case "Enter":
      case " ":
        e.preventDefault();
        library.openSession(session);
        break;
      case "Escape":
        e.preventDefault();
        (document.activeElement as HTMLElement | null)?.blur();
        break;
    }
  }
</script>

<div
  class="sessions"
  bind:this={container}
  data-keys="local"
  role="listbox"
  aria-label="Sessions"
  tabindex="-1"
  onkeydown={onKeydown}
>
  {#each library.sessions as session, i (session.id)}
    <button
      class="row"
      class:active={library.sessionIndex === i}
      data-session={i}
      role="option"
      aria-selected={library.sessionIndex === i}
      tabindex={i === library.sessionIndex ? 0 : -1}
      title="{session.dir} — {formatCount(session.frames)} frames over {formatSpan(session.spanMinutes)}"
      onfocus={() => library.focusSession(i)}
      onclick={() => {
        library.focusSession(i);
        library.openSession(session);
      }}
    >
      <span class="label">{sessionLabel(session)}</span>
      <span class="span">{formatSpan(session.spanMinutes)}</span>
      <span class="count">{formatCount(session.frames)}</span>
    </button>
  {/each}

  {#if library.sessions.length === 0}
    <p class="empty">No sessions yet.</p>
  {/if}
</div>

<style>
  /* The tree takes the slack; the sessions take what is left up to a third of
     the rail, so a long history cannot push the folders off the screen. */
  .sessions {
    flex: 0 1 auto;
    max-height: 33%;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 2px 6px 8px;
    outline: none;
  }

  .row {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 5px;
    border: none;
    border-radius: 5px;
    background: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    outline: none;
  }

  .row:hover {
    background: var(--bg-raised);
  }

  .row.active {
    background: var(--bg-row-active);
    box-shadow: inset 0 0 0 1px var(--border-selected);
  }

  .row:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .label {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .span {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--text-dim);
  }

  .count {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 9.5px;
    color: var(--text-dim);
  }

  .empty {
    margin: 6px 8px;
    font-size: 11.5px;
    color: var(--text-dim);
  }
</style>
