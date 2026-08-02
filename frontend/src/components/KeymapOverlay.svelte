<script lang="ts">
  // Every binding the app is running with, read from the config rather than
  // hardcoded, so a remapped keymap documents itself.

  import { formatChord } from "../lib/keymap";
  import { app } from "../lib/state.svelte";

  /** Actions in the order they are learned, rather than alphabetically. */
  const order = [
    "focus-left",
    "focus-right",
    "focus-up",
    "focus-down",
    "toggle-loupe",
    "zoom",
    "toggle-select",
    "select-all",
    "escape",
    "keep-all",
    "drop-raw",
    "drop-jpeg",
    "drop-both",
    "clear-decision",
    "apply",
    "undo",
    "redo",
    "toggle-sidebar",
    "focus-path",
    "focus-tree",
    "add-root",
    "copy-path",
    "keymap-overlay",
    "copy-palette",
    "move-palette",
    "filter-palette",
    "command-palette",
  ];

  const labels: Record<string, string> = {
    "focus-left": "move focus left",
    "focus-right": "move focus right",
    "focus-up": "move focus up",
    "focus-down": "move focus down",
    "toggle-loupe": "grid ↔ loupe",
    zoom: "1:1 zoom (loupe)",
    "toggle-select": "toggle selection",
    "select-all": "select all",
    escape: "clear selection / leave loupe",
    "keep-all": "keep all",
    "drop-raw": "drop RAW, keep JPEG",
    "drop-jpeg": "drop JPEG, keep RAW",
    "drop-both": "drop both",
    "clear-decision": "clear decision",
    apply: "apply pending decisions",
    undo: "undo last batch",
    redo: "redo",
    "toggle-sidebar": "show or hide the sidebar",
    "focus-path": "jump to the folder path box",
    "focus-tree": "jump to the folder tree (arrows move, ↩ opens)",
    "add-root": "add a folder to the sidebar",
    "copy-path": "copy the open folder's path",
    "keymap-overlay": "this overlay",
    "copy-palette": "copy destinations",
    "move-palette": "move destinations",
    "filter-palette": "filters",
    "command-palette": "command palette",
  };

  /** v0.2 actions are listed so the keys are not a surprise when pressed. */
  const later = new Set(["copy-palette", "move-palette", "filter-palette", "command-palette", "redo"]);

  let actions = $derived.by(() => {
    const known = Object.keys(app.keymap);
    const sorted = order.filter((a) => known.includes(a));
    for (const a of known.sort()) if (!sorted.includes(a)) sorted.push(a);
    return sorted;
  });
</script>

<div class="scrim">
  <div class="panel" role="dialog" aria-label="Keyboard shortcuts">
    <h2>Keys</h2>
    <table>
      <tbody>
        {#each actions as action}
          <tr class:later={later.has(action)}>
            <td class="keys">
              {#each app.keymap[action] ?? [] as chord}
                <kbd>{formatChord(chord)}</kbd>
              {/each}
            </td>
            <td class="label">
              {labels[action] ?? action}
              {#if later.has(action)}<span class="tag">v0.2</span>{/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="foot">Esc or ? to close</p>
  </div>
</div>

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: 35;
    display: grid;
    place-items: center;
    background: var(--scrim);
  }

  .panel {
    width: min(520px, 88vw);
    max-height: 82vh;
    overflow-y: auto;
    padding: 18px 20px;
    border-radius: 10px;
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow-panel);
  }

  h2 {
    margin: 0 0 12px;
    font-size: 15px;
    color: var(--text);
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12px;
  }

  tr.later {
    opacity: 0.5;
  }

  td {
    padding: 4px 0;
    vertical-align: top;
  }

  .keys {
    width: 140px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .label {
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }

  kbd {
    display: inline-block;
    margin-right: 4px;
    padding: 1px 6px;
    border-radius: 4px;
    background: var(--bg-raised);
    border: 1px solid var(--border-strong);
    color: var(--text);
    font-family: inherit;
    font-size: 11px;
  }

  .tag {
    margin-left: 6px;
    font-size: 10px;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .foot {
    margin: 14px 0 0;
    font-size: 11px;
    color: var(--text-faint);
  }
</style>
