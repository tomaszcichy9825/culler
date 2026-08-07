<script lang="ts">
  // One editable row of the inspector's metadata section, in the four states
  // the design draws for a field.
  //
  // editing — the row has the caret: accent wash, accent border, the value on
  //   a brighter wash with the caret drawn as an inset edge on its right.
  // dirty   — a value is drafted and not written: gold wash, gold border, an
  //   8px gold mark in the left column, and the value on disk struck through
  //   beside the new one so the change reads without opening anything.
  // mixed   — the selected frames disagree; the value reads ⟨mixed⟩ in gold.
  // locked  — this app will not write the tag, or cannot write it to every
  //   covered frame. Dimmed, with a lock marker, and ⇥ steps over it.
  //
  // The input is marked data-keys="local" so the application's global key
  // listener leaves the keyboard alone while a value is being typed.

  import { MIXED, exifState } from "../../lib/exif.svelte";
  import type { FieldRow } from "../../lib/exif.svelte";

  interface Props {
    row: FieldRow;
  }

  let { row }: Props = $props();

  let editing = $derived(exifState.editingTag === row.tag);
  let input = $state<HTMLInputElement | null>(null);

  // The field takes the keyboard the moment the row enters edit, wherever the
  // edit was asked for — a click, ⏎, or ⇥ arriving from the row above.
  $effect(() => {
    if (editing && input !== null) {
      input.focus();
      input.select();
    }
  });

  /**
   * Clicking away commits what was typed, the same as ⏎. The guard matters:
   * ⇥ commits and moves on before the browser has taken the focus off this
   * input, so by the time the blur lands the edit belongs to the next row —
   * and committing again here would clear it.
   */
  function onblur() {
    if (exifState.editingTag === row.tag) exifState.commit();
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      exifState.commit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      exifState.revert();
    } else if (e.key === "Tab") {
      e.preventDefault();
      e.stopPropagation();
      exifState.nextField(e.shiftKey ? -1 : 1);
    }
  }
</script>

<div
  class="field {row.state}"
  class:editing
  data-tag={row.tag}
  data-state={editing ? "editing" : row.state}
>
  <span class="mark" aria-hidden="true">{row.state === "dirty" ? "●" : ""}</span>
  <span class="name">{row.label}</span>

  {#if editing}
    <input
      class="value"
      bind:this={input}
      bind:value={exifState.buffer}
      data-keys="local"
      spellcheck="false"
      aria-label={row.label}
      {onkeydown}
      onblur={onblur}
    />
  {:else}
    <button
      type="button"
      class="value"
      class:placeholder={!row.present && row.value === ""}
      class:mixed={row.value === MIXED}
      disabled={!row.writable}
      onclick={() => exifState.beginEdit(row.tag)}
    >
      {row.value === "" && !row.present ? "—" : row.value}
    </button>
  {/if}

  {#if row.state === "dirty" && row.previous !== ""}
    <span class="was" title="the value on disk">{row.previous}</span>
  {/if}
  {#if !row.writable}
    <span class="lock" title="this app does not write this tag">🔒</span>
  {/if}
</div>

<style>
  .field {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 28px;
    padding: 0 8px;
    border: 1px solid transparent;
    border-radius: 4px;
  }

  .mark {
    flex: 0 0 auto;
    width: 8px;
    font-size: 8px;
    line-height: 1;
    color: var(--gold);
  }

  .name {
    /* Sized for the 296px inspector pane rather than a full editor pane. */
    flex: 0 0 auto;
    width: 88px;
    font-size: 11px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .value {
    flex: 1;
    min-width: 0;
    height: 22px;
    padding: 0 6px;
    margin: 0;
    border: 0;
    border-radius: 3px;
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text);
    text-align: left;
    appearance: none;
  }

  button.value {
    cursor: text;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  button.value:disabled {
    cursor: default;
    color: var(--text-dim);
  }

  button.value.placeholder {
    color: var(--text-ghost);
  }

  button.value.mixed {
    color: var(--gold);
  }

  input.value {
    outline: none;
  }

  /* Editing: the caret is drawn as an inset edge on the value box rather than
     left to the platform, so it reads at a glance which row has the keyboard. */
  .field.editing {
    background: var(--accent-wash-10);
    border-color: var(--accent);
  }

  .field.editing input.value {
    background: var(--accent-wash-16);
    box-shadow: inset -1px 0 0 0 var(--accent);
    color: var(--text-hi);
  }

  .field.dirty {
    background: var(--gold-wash-07);
    border-color: rgba(229, 192, 123, 0.32);
  }

  .field.mixed button.value {
    color: var(--gold);
  }

  .field.locked .name,
  .field.locked button.value {
    color: var(--text-dim);
  }

  /* The value that is about to be replaced, kept visible beside the new one. */
  .was {
    flex: 0 1 auto;
    min-width: 0;
    font-size: 10px;
    color: var(--cut);
    text-decoration: line-through;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .lock {
    flex: 0 0 auto;
    font-size: 9px;
    opacity: 0.45;
  }
</style>
