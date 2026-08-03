<script lang="ts">
  // A typed path is the whole folder picker for now: absolute paths and ~ are
  // both accepted, and the backend does the expanding so the rules match what
  // every other path in the app goes through.

  import { addRoot, openFolder } from "../lib/actions";
  import { app, picker } from "../lib/state.svelte";

  let { value = $bindable("") }: { value?: string } = $props();
  let input = $state<HTMLInputElement | null>(null);

  picker.focus = () => {
    input?.focus();
    input?.select();
  };

  // With no folder open there is nothing else to do, so the box takes focus
  // and the app is usable from the keyboard alone on a cold start. It must
  // give focus back the moment a folder arrives: a text field swallows the
  // whole keymap, so holding on to it would leave the grid unable to hear a
  // single decision key.
  $effect(() => {
    if (app.folder === null) input?.focus();
    else if (document.activeElement === input) input?.blur();
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    input?.blur();
    await openFolder(value);
    // A typed folder joins the tree so it can be got back to, but only once
    // it has opened, and only if no root already covers it. The resolved path
    // is used rather than what was typed, since the backend expands ~.
    if (app.error === "" && app.folder !== null) addRoot(app.folder.dir);
  }
</script>

<form class="picker" onsubmit={(e) => void submit(e)}>
  <input
    bind:this={input}
    bind:value
    type="text"
    spellcheck="false"
    autocapitalize="off"
    autocorrect="off"
    placeholder="Open folder by path…  (~ works)"
    title={value}
    aria-label="Open folder by typed path"
  />
  <!-- Never disabled: a disabled default button also blocks the form's
       implicit submission, so Enter would stop working during a slow scan —
       exactly when switching folders matters most. Concurrent scans are made
       safe by sequencing them, not by locking the control. -->
  <button type="submit">Open</button>
</form>

<style>
  .picker {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  input {
    flex: 1;
    /* Without this a long path stretches the flex item past the sidebar. */
    min-width: 0;
    font: inherit;
    font-size: 12px;
    padding: 5px 9px;
    border-radius: 6px;
    border: 1px solid var(--border-strong);
    background: var(--bg-app);
    color: var(--text);
    outline: none;
    text-overflow: ellipsis;
    -webkit-user-select: text;
    user-select: text;
  }

  input:focus {
    border-color: var(--accent);
  }

  input::placeholder {
    color: var(--text-dim);
  }

  button {
    flex: 0 0 auto;
    font: inherit;
    font-size: 12px;
    padding: 5px 11px;
    border-radius: 6px;
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text);
    cursor: pointer;
    white-space: nowrap;
  }

  button:hover {
    border-color: var(--accent);
  }
</style>
