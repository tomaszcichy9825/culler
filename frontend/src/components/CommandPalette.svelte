<script lang="ts">
  // ⌘K. Every action the application has, with its binding and what it will do
  // to the current selection.
  //
  // The rows come from the action registry rather than a list of their own, so
  // an action that exists is an action that can be found here. Nothing in this
  // file knows what any of them do.

  import { available, noteOf, runAction } from "../lib/actions";
  import type { Action } from "../lib/actions";
  import { formatChord } from "../lib/keymap";
  import { filterIsSet, filterSummary, palette, rank } from "../lib/palette.svelte";
  import { shell } from "../lib/shell.svelte";
  import { app } from "../lib/state.svelte";
  import PaletteFrame from "./PaletteFrame.svelte";

  interface Group {
    title: string;
    rows: Action[];
  }

  /** What a row is found by, in the order the fields count for. */
  function fields(a: Action): string[] {
    return [a.label, noteOf(a), a.id];
  }

  let ranked = $derived(rank(palette.query, available(), fields));

  // Groups keep the design's sectioned body while still respecting the
  // ranking: a group appears where its best row does, and its rows are in
  // score order within it. An empty query scores everything the same, so this
  // is the registry's own order until something is typed.
  let groups = $derived.by(() => {
    const out: Group[] = [];
    const byTitle = new Map<string, Group>();
    for (const action of ranked) {
      let group = byTitle.get(action.group);
      if (group === undefined) {
        group = { title: action.group, rows: [] };
        byTitle.set(action.group, group);
        out.push(group);
      }
      group.rows.push(action);
    }
    return out;
  });

  let flat = $derived(groups.flatMap((g) => g.rows));
  /** The stored cursor can outrun a list that shrank as the query narrowed. */
  let cursor = $derived(Math.min(palette.index, Math.max(0, flat.length - 1)));

  let scope = $derived(
    app.selection.size > 0
      ? `${app.selection.size} frame${app.selection.size === 1 ? "" : "s"} selected`
      : app.focused
        ? "the frame under the cursor"
        : "no frames",
  );

  function run() {
    const action = flat[cursor];
    if (action === undefined) return;
    // Close first: an action that opens another palette has to find the way
    // clear, and the dispatch keeps everything else out while one is up.
    palette.close();
    runAction(action.id);
  }
</script>

<PaletteFrame width={720} label="Command palette" count={flat.length} onrun={run}>
  {#snippet header()}
    <span class="prompt">›</span>
    {#if palette.query === ""}
      <span class="placeholder">search or run a command</span>
    {:else}
      <span class="query">{palette.query}</span>
    {/if}
    <span class="caret" aria-hidden="true"></span>
    <span class="spacer"></span>
    <span class="count">{scope}</span>
  {/snippet}

  {#snippet chips()}
    <span class="chip on">{shell.spec.label}</span>
    <span class="chip">{shell.layoutLabel}</span>
    <span class="chip">{app.selection.size > 0 ? "selection" : "cursor"}</span>
    {#if filterIsSet(palette.filter)}
      <span class="chip">filtered · {filterSummary(palette.filter)}</span>
    {/if}
  {/snippet}

  {#snippet body()}
    {#if flat.length === 0}
      <p class="none">nothing matches “{palette.query}”</p>
    {/if}
    {#each groups as group (group.title)}
      <div class="section">
        <span class="stitle">{group.title}</span>
        <span class="rule"></span>
      </div>
      {#each group.rows as action (action.id)}
        {@const index = flat.indexOf(action)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="row"
          class:at={index === cursor}
          onmouseenter={() => (palette.index = index)}
          onclick={run}
        >
          <span class="icon">{action.icon ?? "·"}</span>
          <span class="name">{action.label}</span>
          <span class="note">{noteOf(action)}</span>
          <span class="chords">
            {#each app.keymap[action.id] ?? [] as chord}
              <span class="cap">{formatChord(chord)}</span>
            {/each}
          </span>
        </div>
      {/each}
    {/each}
  {/snippet}

  {#snippet footer()}
    <div class="foot-row">
      <span><span class="fkey">↑↓</span> pick</span>
      <span><span class="fkey">⏎</span> run</span>
      <span><span class="fkey">⇥</span> run with arguments</span>
      <span class="spacer"></span>
      <span>acts on the <span class="accent">selection</span>, not the cursor</span>
    </div>
  {/snippet}
</PaletteFrame>

<style>
  .prompt {
    flex: 0 0 auto;
    font-size: 15px;
    color: var(--accent);
  }

  .query {
    flex: 0 0 auto;
    font-size: 15px;
    color: var(--text-hi);
    white-space: pre;
  }

  .placeholder {
    flex: 0 0 auto;
    font-size: 15px;
    color: var(--text-ghost);
  }

  .caret {
    flex: 0 0 auto;
    width: 1px;
    height: 17px;
    background: var(--accent);
  }

  .spacer {
    flex: 1;
  }

  .count {
    flex: 0 0 auto;
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .chip {
    padding: 3px 9px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .chip.on {
    background: var(--accent-wash-16);
    border-color: var(--accent);
    color: var(--accent);
  }

  .section {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 9px 16px 5px;
  }

  .stitle {
    font-size: 9.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border-faint);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 34px;
    padding: 0 16px;
    cursor: pointer;
  }

  .row.at {
    background: var(--bg-row-active);
    box-shadow: var(--focus-edge);
  }

  .icon {
    flex: 0 0 14px;
    font-size: 11px;
    color: var(--text-dim);
  }

  .row.at .icon {
    color: var(--accent);
  }

  .name {
    flex: 0 0 auto;
    font-size: 12.5px;
    color: var(--text-2);
    white-space: nowrap;
  }

  .row.at .name {
    color: var(--text-hi);
  }

  .note {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-ghost);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row.at .note {
    color: var(--text-dim);
  }

  .chords {
    flex: 0 0 auto;
    display: flex;
    gap: 3px;
  }

  .cap {
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    font-weight: 600;
    color: var(--text-2);
    white-space: nowrap;
  }

  .none {
    margin: 0;
    padding: 18px 16px;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .foot-row {
    display: flex;
    align-items: center;
    gap: 14px;
    white-space: nowrap;
  }

  .fkey {
    color: var(--text-muted);
  }

  .accent {
    color: var(--accent);
  }
</style>
