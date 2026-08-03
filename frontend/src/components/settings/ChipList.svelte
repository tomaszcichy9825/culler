<script lang="ts">
  // An editable list of short strings — the extension classes.
  //
  // Order is meaning here: when several files share a stem, the first
  // extension in the list is the one treated as the primary. So entries are
  // appended where they are typed and never sorted behind the user's back.

  interface Props {
    values: string[];
    label: string;
    placeholder?: string;
    /** Folds an entry to the form the backend stores, or returns "" to refuse it. */
    normalise?: (raw: string) => string;
    onchange: (values: string[]) => void;
  }

  let { values, label, placeholder = "add", normalise = (raw) => raw.trim(), onchange }: Props = $props();

  let typed = $state("");
  let note = $state("");

  function add() {
    const entry = normalise(typed);
    if (entry === "") {
      note = typed.trim() === "" ? "" : "not a usable entry";
      return;
    }
    if (values.includes(entry)) {
      note = `${entry} is already listed`;
      return;
    }
    onchange([...values, entry]);
    typed = "";
    note = "";
  }

  function remove(entry: string) {
    onchange(values.filter((v) => v !== entry));
    note = "";
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter" || e.key === "," || e.key === " ") {
      e.preventDefault();
      add();
      return;
    }
    if (e.key === "Backspace" && typed === "" && values.length > 0) {
      e.preventDefault();
      remove(values[values.length - 1]);
    }
  }
</script>

<div class="list" role="group" aria-label={label}>
  <div class="entries">
    {#each values as value (value)}
      <span class="entry">
        <span class="text">{value}</span>
        <button type="button" aria-label="Remove {value}" title="Remove {value}" onclick={() => remove(value)}>
          ×
        </button>
      </span>
    {/each}

    <input
      class="add"
      aria-label="Add to {label}"
      {placeholder}
      bind:value={typed}
      {onkeydown}
      onblur={add}
      oninput={() => (note = "")}
    />
  </div>
  {#if note !== ""}<span class="note" role="alert">{note}</span>{/if}
</div>

<style>
  .list {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    /* Wide lists wrap rather than pushing the row's description out. */
    max-width: 420px;
  }

  .entries {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 5px;
  }

  .entry {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 3px 4px 3px 8px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 11px;
    color: var(--text-2);
    white-space: nowrap;
  }

  button {
    width: 14px;
    height: 14px;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    color: var(--text-dim);
    font: inherit;
    font-size: 12px;
    line-height: 1;
    cursor: pointer;
  }

  button:hover {
    background: var(--bg-raised);
    color: var(--cut);
  }

  button:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .add {
    width: 8ch;
    padding: 3px 8px;
    border-radius: 5px;
    background: var(--bg-field-alt);
    border: 1px dashed var(--border-strong);
    outline: none;
    font-family: inherit;
    font-size: 11px;
    color: var(--text);
  }

  .add:focus {
    border-style: solid;
    border-color: var(--border-focus);
  }

  .note {
    font-size: 10.5px;
    color: var(--gold);
  }
</style>
