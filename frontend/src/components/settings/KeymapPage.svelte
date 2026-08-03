<script lang="ts">
  // The keymap page: every action, the chords bound to it, and the recorder.
  //
  // A binding is a config line like any other, so recording one edits the same
  // draft as every other page and is written by the same save. The recorder
  // refuses a chord another action already owns, because a config with one
  // chord in two places is one the backend rejects whole — taking it from the
  // other action is offered instead, and is the only way to move a binding
  // without opening the file.

  import { settings } from "../../lib/settings.svelte";
  import { ACTION_GROUPS, chordFromEvent, defaultChords, isConfigDefault } from "../../lib/keymapCatalogue";
  import type { ActionSpec } from "../../lib/keymapCatalogue";
  import { DEFAULT_KEYMAP } from "../../lib/keymapCatalogue";
  import Aside from "./Aside.svelte";
  import Keycap from "./Keycap.svelte";
  import PageShell from "./PageShell.svelte";
  import SectionLabel from "./SectionLabel.svelte";
  import { matchesFilter } from "./context";

  interface Row {
    spec: ActionSpec;
    /** What the config binds. Empty when the config is silent about it. */
    chords: string[];
    /** True when the config says nothing and the shell's fallback applies. */
    fallback: boolean;
    changed: boolean;
  }

  let container = $state<HTMLElement | null>(null);
  let selected = $state("");

  /** Actions the config names that this build has never heard of. */
  let unknown = $derived.by(() => {
    const known = new Set(ACTION_GROUPS.flatMap((g) => g.actions.map((a) => a.id)));
    return Object.keys(settings.draft?.keymap ?? {})
      .filter((id) => !known.has(id))
      .sort()
      .map(
        (id): ActionSpec => ({
          id,
          label: id,
          note: "not an action this version answers to",
          scope: "unknown",
          source: "config",
          chords: [],
        }),
      );
  });

  let groups = $derived.by(() => {
    const all = [...ACTION_GROUPS, ...(unknown.length > 0 ? [{ title: "Not recognised", actions: unknown }] : [])];
    return all
      .map((group) => ({
        title: group.title,
        rows: group.actions
          .filter((spec) => matchesFilter(settings.filter, spec.id, spec.label, spec.note))
          .map((spec): Row => {
            const bound = settings.draft?.keymap?.[spec.id];
            return {
              spec,
              chords: bound ?? [],
              fallback: bound === undefined && spec.source === "shell",
              changed: settings.isChanged(spec.id),
            };
          }),
      }))
      .filter((group) => group.rows.length > 0);
  });

  let flat = $derived(groups.flatMap((g) => g.rows));
  let current = $derived(flat.find((r) => r.spec.id === selected) ?? flat[0]);
  let recording = $derived(settings.recording);

  /** Keeps a selection that the filter has hidden from stranding the aside. */
  $effect(() => {
    if (flat.length > 0 && !flat.some((r) => r.spec.id === selected)) selected = flat[0].spec.id;
  });

  function focusRow(id: string) {
    selected = id;
    queueMicrotask(() => container?.querySelector<HTMLElement>(`[data-action="${id}"]`)?.focus());
  }

  function step(delta: number) {
    const at = flat.findIndex((r) => r.spec.id === selected);
    const next = flat[Math.max(0, Math.min(at + delta, flat.length - 1))];
    if (next) focusRow(next.spec.id);
  }

  function record(id: string) {
    selected = id;
    settings.startRecording(id);
  }

  /**
   * While the recorder is open every press is a chord rather than a shortcut,
   * so it is taken in the capture phase before any other handler — the app's
   * own keymap included. Enter and Esc drive the recorder itself and cannot be
   * recorded from here; the config file can still bind them.
   */
  function onRecordKey(e: KeyboardEvent) {
    if (settings.recording === null) return;
    e.preventDefault();
    e.stopPropagation();

    if (e.key === "Escape") {
      settings.cancelRecording();
      return;
    }
    if (e.key === "Enter") {
      settings.commit();
      return;
    }
    const chord = chordFromEvent(e);
    if (chord !== null) settings.capture(chord);
  }

  function onListKey(e: KeyboardEvent) {
    if (settings.recording !== null) return;
    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        e.stopPropagation();
        step(1);
        break;
      case "ArrowUp":
        e.preventDefault();
        e.stopPropagation();
        step(-1);
        break;
      case "Enter":
        if (selected === "") break;
        e.preventDefault();
        e.stopPropagation();
        record(selected);
        break;
      case "Backspace":
      case "Delete":
        if (selected === "") break;
        e.preventDefault();
        e.stopPropagation();
        settings.resetAction(selected);
        break;
    }
  }

  function restoreStock() {
    for (const action of Object.keys({ ...DEFAULT_KEYMAP, ...(settings.draft?.keymap ?? {}) })) {
      settings.resetAction(action);
    }
  }
</script>

<svelte:window onkeydowncapture={onRecordKey} />

<PageShell>
  {#snippet content()}
    <div class="head">
      <span class="here">Keymap</span>
      <span class="sep">|</span>
      <span>click a chord or press <span class="key">↩</span> on a row to record</span>
      <span class="spacer"></span>
      {#if settings.changedActions.length > 0}
        <span class="changed">{settings.changedActions.length} changed from defaults</span>
      {/if}
    </div>

    <div class="list" bind:this={container} onkeydown={onListKey} role="listbox" tabindex="-1" aria-label="Key bindings">
      {#each groups as group (group.title)}
        <div class="group">
          <SectionLabel title={group.title} />
          <div class="rows">
            {#each group.rows as row (row.spec.id)}
              {@const active = recording?.action === row.spec.id}
              <button
                type="button"
                class="row"
                class:selected={row.spec.id === selected}
                class:recording={active}
                data-action={row.spec.id}
                role="option"
                aria-selected={row.spec.id === selected}
                tabindex={row.spec.id === selected ? 0 : -1}
                onfocus={() => (selected = row.spec.id)}
                onclick={() => record(row.spec.id)}
              >
                <span class="mark">{row.changed ? "•" : ""}</span>
                <span class="action">{row.spec.label}</span>
                <span class="chords">
                  {#if active}
                    <span class="listening">press a key</span>
                  {:else if row.chords.length > 0}
                    {#each row.chords as chord (chord)}
                      <Keycap {chord} />
                    {/each}
                  {:else if row.fallback}
                    {#each defaultChords(row.spec.id) as chord (chord)}
                      <Keycap {chord} faint />
                    {/each}
                  {:else}
                    <span class="unbound">unbound</span>
                  {/if}
                </span>
                <span class="note">
                  {row.fallback ? "built-in fallback — recording writes it into the file" : row.spec.note}
                </span>
                <span class="scope">{row.spec.scope}</span>
              </button>
            {/each}
          </div>
        </div>
      {/each}

      {#if flat.length === 0}
        <p class="empty">No action matches “{settings.filter}”.</p>
      {/if}
    </div>
  {/snippet}

  {#snippet aside()}
    {#if current}
      <div class="subject">
        <div class="id">{current.spec.id}</div>
        <p class="desc">{current.spec.label}{current.spec.note === "" ? "" : ` — ${current.spec.note}`}</p>
      </div>
    {/if}

    <div class="record">
      <SectionLabel title="Recording" />
      {#if recording}
        <div class="box" class:clash={recording.conflict !== null}>
          {#if recording.chord === null}
            <span class="waiting">press a key</span>
          {:else}
            <Keycap chord={recording.chord} big />
          {/if}
        </div>

        {#if recording.conflict !== null}
          <div class="clash-note" role="alert">
            <span class="bang">!</span>
            <span>already bound to {recording.conflict} — binding it here takes it from that action</span>
          </div>
        {/if}

        <div class="buttons">
          <button type="button" class="secondary" onclick={() => settings.cancelRecording()}>esc cancel</button>
          {#if recording.conflict !== null}
            <button
              type="button"
              class="primary"
              disabled={recording.chord === null}
              onclick={() => settings.commit({ steal: true })}
            >
              take it ↩
            </button>
          {:else}
            <button
              type="button"
              class="primary"
              disabled={recording.chord === null}
              onclick={() => settings.commit()}
            >
              ↩ bind
            </button>
          {/if}
        </div>
        <p class="hint">
          Every key is a chord while this is open. ↩ and esc drive the recorder itself, so binding those two means
          editing the file.
        </p>
      {:else}
        <p class="hint">Pick a row and press ↩, or click its chord.</p>
      {/if}
    </div>

    <Aside title="This action">
      <div class="actions">
        <button
          type="button"
          class="wide"
          disabled={current === undefined || !current.changed}
          onclick={() => current && settings.resetAction(current.spec.id)}
        >
          ⌫ reset to default
        </button>
        <button
          type="button"
          class="wide"
          disabled={current === undefined || current.chords.length === 0}
          onclick={() => current && settings.unbind(current.spec.id)}
        >
          unbind
        </button>
      </div>
      {#if current && !isConfigDefault(current.spec.id)}
        <p class="hint">
          The stock config file does not carry this action, so resetting it removes the line rather than writing the
          fallback out.
        </p>
      {/if}
    </Aside>

    <Aside title="Presets">
      <div class="actions">
        <button type="button" class="wide" onclick={restoreStock}>restore the stock keymap</button>
      </div>
      <p class="hint">Every binding lives in config.json — a preset just rewrites that file.</p>
    </Aside>
  {/snippet}
</PageShell>

<style>
  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 30px;
    margin: -14px -18px 12px;
    padding: 0 16px;
    border-bottom: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .here {
    color: var(--text);
  }

  .sep {
    color: var(--border-strong);
  }

  .key {
    color: var(--text-muted);
  }

  .spacer {
    flex: 1;
  }

  .changed {
    color: var(--gold);
  }

  .list {
    outline: none;
  }

  .group {
    margin-bottom: 16px;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 28px;
    padding: 0 8px;
    border: 1px solid transparent;
    border-radius: 4px;
    background: none;
    font: inherit;
    text-align: left;
    color: inherit;
    cursor: pointer;
  }

  .row:hover {
    background: var(--bg-raised);
  }

  .row.selected {
    background: var(--bg-row-active);
    border-color: var(--border-selected);
  }

  .row.recording {
    background: var(--accent-wash-10);
    border-color: var(--accent);
    border-style: dashed;
  }

  .row:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .mark {
    flex: 0 0 8px;
    font-size: 11px;
    color: var(--gold);
  }

  .action {
    flex: 0 0 200px;
    font-size: 11.5px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chords {
    flex: 0 0 130px;
    display: flex;
    align-items: center;
    gap: 3px;
    overflow: hidden;
  }

  .listening {
    font-size: 11px;
    color: var(--accent);
  }

  .unbound {
    font-size: 11px;
    color: var(--text-ghost);
  }

  .note {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .scope {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .empty {
    margin: 10px 2px;
    font-size: 11.5px;
    color: var(--text-dim);
  }

  .subject {
    padding: 11px 12px;
    border-bottom: 1px solid var(--border);
  }

  .id {
    font-size: 12px;
    color: var(--text);
  }

  .desc {
    margin: 4px 0 0;
    font-size: 10.5px;
    color: var(--text-dim);
    line-height: 1.55;
    text-wrap: pretty;
  }

  .record {
    padding: 11px 12px;
    border-bottom: 1px solid var(--border);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .box {
    display: grid;
    place-items: center;
    height: 52px;
    border-radius: 6px;
    background: var(--accent-wash-10);
    border: 1px dashed var(--accent);
  }

  .box.clash {
    background: var(--cut-wash-09);
    border-color: var(--cut);
  }

  .waiting {
    font-size: 11px;
    color: var(--accent);
  }

  .clash-note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 7px 9px;
    border-radius: 5px;
    background: var(--cut-wash-09);
    border: 1px solid var(--cut);
    font-size: 10.5px;
    color: var(--cut);
    line-height: 1.5;
    text-wrap: pretty;
  }

  .bang {
    flex: 0 0 auto;
    font-size: 11px;
    font-weight: 700;
  }

  .buttons {
    display: flex;
    gap: 6px;
  }

  .buttons button {
    flex: 1;
    padding: 6px 8px;
    border-radius: 5px;
    font: inherit;
    font-size: 11px;
    white-space: nowrap;
    cursor: pointer;
  }

  .secondary {
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    color: var(--text-muted);
  }

  .primary {
    background: var(--keep);
    border: 1px solid var(--keep);
    color: var(--on-accent);
    font-weight: 700;
  }

  .buttons button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .actions {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .wide {
    width: 100%;
    padding: 6px 9px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    text-align: center;
    cursor: pointer;
  }

  .wide:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .wide:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .hint {
    margin: 9px 0 0;
    font-size: 10.5px;
    color: var(--text-dim);
    line-height: 1.55;
    text-wrap: pretty;
  }
</style>
