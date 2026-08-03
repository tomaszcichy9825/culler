<script lang="ts">
  // IMPORT's left pane: the cards plugged in.
  //
  // A row is a volume the operating system called removable, with a shallow
  // look at what is on it — the app has not walked the card to draw this, and
  // the frame count says so when it was extrapolated rather than counted.
  // Selecting a row is what makes the app read the card properly.

  import CapacityBar from "./CapacityBar.svelte";
  import { formatBytes, formatCount, importState, percent } from "../../lib/import.svelte";

  let container = $state<HTMLDivElement | null>(null);

  // The pane fills itself the first time it is drawn, so IMPORT is populated
  // without the shell having to prime it.
  $effect(() => {
    if (!importState.detected) void importState.refresh();
  });

  function rowEl(index: number): HTMLElement | null {
    return container?.querySelector(`[data-card="${index}"]`) ?? null;
  }

  function holdsFocus(): boolean {
    return container !== null && container.contains(document.activeElement);
  }

  function focusRow(index: number) {
    const clamped = Math.max(0, Math.min(index, importState.cards.length - 1));
    importState.focusCard(clamped);
    if (holdsFocus()) queueMicrotask(() => rowEl(clamped)?.focus());
  }

  function onKeydown(e: KeyboardEvent) {
    if (importState.cards.length === 0) return;
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        focusRow(importState.cardIndex + 1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        focusRow(importState.cardIndex - 1);
        break;
      case "Home":
        e.preventDefault();
        focusRow(0);
        break;
      case "End":
        e.preventDefault();
        focusRow(importState.cards.length - 1);
        break;
      case "Enter":
      case " ":
        e.preventDefault();
        void importState.selectCard(importState.cardIndex);
        break;
      case "Escape":
        e.preventDefault();
        (document.activeElement as HTMLElement | null)?.blur();
        break;
    }
  }

  /** How much of the card is already in the library, for the selected one. */
  function importedShare(path: string): number | null {
    const s = importState.summary;
    if (s === null || s.path !== path || s.sampled === 0) return null;
    return percent(s.imported, s.sampled);
  }
</script>

<div class="pane">
  <section class="grow">
    <div class="label">
      <span>cards</span>
      <span class="rule"></span>
      <button
        type="button"
        class="again"
        disabled={importState.loading}
        onclick={() => void importState.refresh()}>rescan</button
      >
    </div>

    <div
      class="cards"
      bind:this={container}
      data-keys="local"
      role="listbox"
      aria-label="Detected cards"
      tabindex="-1"
      onkeydown={onKeydown}
    >
      {#each importState.cards as card, i (card.path)}
        {@const share = importedShare(card.path)}
        <button
          type="button"
          class="card"
          class:active={importState.cardIndex === i}
          class:selected={importState.selectedPath === card.path}
          data-card={i}
          role="option"
          aria-selected={importState.selectedPath === card.path}
          tabindex={i === importState.cardIndex ? 0 : -1}
          title={card.path}
          onfocus={() => importState.focusCard(i)}
          onclick={() => void importState.selectCard(i)}
        >
          <div class="head">
            <span class="dot"></span>
            <span class="name">{card.name}</span>
            {#if card.hasDcim}<span class="pill">dcim</span>{/if}
          </div>

          <CapacityBar total={card.total} free={card.free} height={6} />

          <div class="stats">
            <span class="frames">
              {card.estimated ? "≈" : ""}{formatCount(card.frames)} frames
            </span>
            <span class="spacer"></span>
            {#if card.total > 0}
              <span class="free">{formatBytes(card.free)} free</span>
            {/if}
          </div>

          {#if card.error !== ""}
            <p class="warn" title={card.error}>could not be read</p>
          {:else if share !== null}
            <p class="note">{share.toFixed(0)}% already in the library</p>
          {:else if card.folders > 1}
            <p class="note">{formatCount(card.folders)} folders</p>
          {/if}
        </button>
      {/each}

      {#if importState.cards.length === 0}
        <p class="empty">
          {importState.detected ? "nothing plugged in" : "looking for cards…"}
        </p>
      {/if}
    </div>
  </section>

  <section>
    <div class="label"><span>review</span><span class="rule"></span></div>
    <button
      type="button"
      class="action"
      disabled={importState.dir === null}
      onclick={() => importState.review()}
    >
      open in cull
    </button>
    <p class="note">routing is done on the frames, in cull — this screen is the overview</p>
  </section>
</div>

<style>
  /* min-width on the pane and every section inside it: the rail is narrow and
     fixed, so a long note or a long volume name has to be cut rather than
     widening the column it sits in. */
  .pane {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  section {
    min-width: 0;
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }

  .grow {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
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

  .again {
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    font-size: 10px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-dim);
    cursor: pointer;
  }

  .again:hover:not(:disabled) {
    color: var(--text);
  }

  .again:disabled {
    color: var(--text-dead);
    cursor: default;
  }

  .cards {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    display: flex;
    flex-direction: column;
    gap: 6px;
    outline: none;
  }

  .card {
    display: block;
    width: 100%;
    padding: 8px 9px;
    border-radius: 5px;
    border: 1px solid var(--border);
    border-left: 2px solid transparent;
    background: var(--bg-tile);
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .card:hover {
    border-color: var(--border-strong);
  }

  .card.selected {
    background: var(--amber-wash-14);
    border-left-color: var(--amber);
  }

  .card.active {
    box-shadow: var(--focus-inset);
  }

  .card:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-bottom: 7px;
    min-width: 0;
  }

  .dot {
    flex: 0 0 auto;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--amber);
  }

  .name {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 12.5px;
    color: var(--text-hi);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pill {
    flex: 0 0 auto;
    padding: 2px 5px;
    border-radius: 4px;
    background: var(--amber-wash-16);
    color: var(--amber);
    font-size: 9px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .stats {
    display: flex;
    align-items: baseline;
    gap: 6px;
    padding-top: 6px;
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .frames,
  .free {
    min-width: 0;
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .free {
    color: var(--text-dim);
  }

  .note {
    margin: 5px 0 0;
    font-size: 10px;
    line-height: 1.5;
    color: var(--text-ghost);
  }

  /* One line on a card row, cut when it will not fit — a row is a fixed
     height and a wrapped note would push the next card down. It carries a
     measured figure rather than a hint, so it is not drawn as faintly. */
  .card .note {
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .warn {
    margin: 5px 0 0;
    font-size: 10px;
    color: var(--amber);
  }

  .empty {
    margin: 0;
    padding: 20px 0;
    text-align: center;
    font-size: 11px;
    color: var(--text-ghost);
  }

  .action {
    width: 100%;
    height: 26px;
    border-radius: 4px;
    border: 1px solid var(--border);
    background: var(--bg-field);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .action:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .action:disabled {
    color: var(--text-dead);
    cursor: default;
  }
</style>
