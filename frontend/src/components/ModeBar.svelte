<script lang="ts">
  // Four modes, always present, always in this order. The mode bar is the only
  // thing in the shell that never changes shape, so it is what the user
  // navigates by.

  import { MODES, shell } from "../lib/shell.svelte";
</script>

<nav class="modebar" aria-label="Mode">
  {#each MODES as mode, i (mode.id)}
    <button
      class="item"
      class:active={shell.mode === mode.id}
      aria-current={shell.mode === mode.id ? "true" : undefined}
      onclick={() => shell.setMode(mode.id)}
      title="{mode.label} — Control {i + 1}"
    >
      <span class="key" aria-hidden="true">⌃{i + 1}</span>
      <span class="label">{mode.label}</span>
    </button>
  {/each}
</nav>

<style>
  .modebar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 1px;
  }

  .item {
    display: flex;
    align-items: center;
    gap: 5px;
    height: 20px;
    padding: 0 7px;
    border: none;
    border-radius: 3px;
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
    cursor: pointer;
  }

  .item:hover:not(.active) {
    color: var(--text-2);
  }

  .item.active {
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
  }

  .key {
    font-size: 10px;
    opacity: 0.55;
  }

  .item:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
  }
</style>
