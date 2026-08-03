<script lang="ts">
  // The title bar: the layout control, where you are, the way into the command
  // palette, and the state worth flagging. 40px, and it doubles as the window's
  // drag region because the native title bar is hidden.

  import { isMac } from "../lib/keymap";
  import { shell } from "../lib/shell.svelte";
  import { app } from "../lib/state.svelte";
  import NetworkChip from "./NetworkChip.svelte";

  let {
    onlayout,
    oncommand,
    onpath,
  }: {
    /** Choose a sub-layout of the current mode, as ⌥1–3 would. */
    onlayout: (index: number) => void;
    oncommand: () => void;
    onpath: () => void;
  } = $props();

  let crumbs = $derived(app.folder === null ? [] : app.folder.dir.split("/").filter((s) => s !== ""));
</script>

<header class="titlebar" class:mac={isMac}>
  <div class="segmented" role="group" aria-label="Layout">
    {#each shell.spec.layouts as layout, i (layout)}
      <button
        class="segment"
        class:active={shell.layout === i}
        onclick={() => onlayout(i)}
        title="{layout} — Option {i + 1}"
      >
        <span class="segkey" aria-hidden="true">⌥{i + 1}</span>
        <span>{layout}</span>
      </button>
    {/each}
  </div>

  <div class="context">
    {#if app.folder}
      <button class="crumbs" onclick={onpath} title="{app.folder.dir}&#10;Click to copy this path">
        {#each crumbs as crumb, i (i)}
          <span class="sep" aria-hidden="true">/</span><span
            class="crumb"
            class:leaf={i === crumbs.length - 1}>{crumb}</span
          >
        {/each}
      </button>
    {/if}
  </div>

  <button class="search" onclick={oncommand} title="Command palette">
    <span class="placeholder">search</span>
    <span class="caps"><kbd>⌘</kbd><kbd>K</kbd></span>
  </button>

  <div class="chips">
    {#if app.busy}
      <span class="chip working"><span class="dot" aria-hidden="true"></span>working</span>
    {/if}
    {#if app.folder?.network}<NetworkChip />{/if}
  </div>
</header>

<style>
  .titlebar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 14px;
    height: 40px;
    padding: 0 14px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
    min-width: 0;
    --wails-draggable: drag;
  }

  /* Clear of the macOS traffic lights, which sit over the top-left corner. */
  .titlebar.mac {
    padding-left: 78px;
  }

  .titlebar button {
    --wails-draggable: no-drag;
  }

  .segmented {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    height: 24px;
    padding: 2px;
    border-radius: 6px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
  }

  .segment {
    display: flex;
    align-items: center;
    gap: 6px;
    height: 100%;
    padding: 0 9px;
    border: none;
    border-radius: 4px;
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    cursor: pointer;
  }

  .segment.active {
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
  }

  .segkey {
    font-size: 10px;
    opacity: 0.55;
  }

  .context {
    flex: 0 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
  }

  .crumbs {
    min-width: 0;
    display: block;
    padding: 2px 5px;
    margin-left: -5px;
    border: none;
    border-radius: 4px;
    background: none;
    font: inherit;
    font-size: 12px;
    text-align: left;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }

  .crumbs:hover {
    background: var(--bg-raised);
  }

  .sep {
    color: var(--text-dead);
  }

  .crumb {
    color: var(--text-muted);
  }

  .crumb.leaf {
    color: var(--text-2);
    font-weight: 500;
  }

  /* The two flexible spacers either side of the search hint centre it in the
     window, which is why the context area and the chips both grow. */
  .search {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 24px;
    min-width: 210px;
    padding: 0 9px;
    margin: 0 auto;
    border-radius: 6px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-dim);
    cursor: pointer;
  }

  .search:hover {
    border-color: var(--border-focus);
  }

  .placeholder {
    flex: 1;
    text-align: left;
  }

  .caps {
    display: flex;
    gap: 2px;
  }

  kbd {
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--bg-kbd);
    color: var(--text-muted);
    font: inherit;
    font-size: 10px;
  }

  .chips {
    flex: 0 1 auto;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 6px;
    min-width: 0;
  }

  .chip {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 8px;
    border-radius: 4px;
    font-size: 10.5px;
    font-weight: 600;
    white-space: nowrap;
  }

  .chip.working {
    background: var(--accent-wash-16);
    color: var(--accent);
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  .titlebar button:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
  }
</style>
