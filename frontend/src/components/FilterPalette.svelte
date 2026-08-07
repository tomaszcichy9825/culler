<script lang="ts">
  // f. Narrows the contact sheet by what a frame is, what has been decided
  // about it, and how many stars it has.
  //
  // Unlike the other two this one stays open after ⏎: a filter is built from
  // more than one dimension, and the count in the header is the feedback that
  // makes building it worth doing in place. Esc is how it is finished with.
  //
  // A filter is a view. It never changes a verdict, and an apply still acts on
  // every decided frame in the folder, filtered out or not.

  import { countMatching, filterIsSet, NO_FILTER, palette, rank } from "../lib/palette.svelte";
  import type { Filter, KindFilter, VerdictFilter } from "../lib/palette.svelte";
  import { MAX_RATING } from "../lib/verdict";
  import { app } from "../lib/state.svelte";
  import PaletteFrame from "./PaletteFrame.svelte";

  interface Option {
    group: string;
    label: string;
    /** The filter this row would leave in place. */
    next: Filter;
    /** Whether the filter already is that. */
    active: boolean;
  }

  const KIND = "kind";
  const VERDICT = "verdict";
  const RATING = "rating";
  const RESET = "reset";

  const KINDS: { value: KindFilter; label: string }[] = [
    { value: "all", label: "any kind" },
    { value: "pair", label: "pairs — RAW and JPEG" },
    { value: "raw", label: "RAW only" },
    { value: "jpeg", label: "JPEG only" },
  ];

  const VERDICTS: { value: VerdictFilter; label: string }[] = [
    { value: "all", label: "any verdict" },
    { value: "keep", label: "kept" },
    { value: "cut", label: "cut" },
    { value: "undecided", label: "undecided" },
  ];

  let current = $derived(palette.filter);

  let options = $derived.by(() => {
    const out: Option[] = [];
    for (const k of KINDS) {
      out.push({ group: KIND, label: k.label, next: { ...current, kind: k.value }, active: current.kind === k.value });
    }
    for (const v of VERDICTS) {
      out.push({
        group: VERDICT,
        label: v.label,
        next: { ...current, verdict: v.value },
        active: current.verdict === v.value,
      });
    }
    out.push({
      group: RATING,
      label: "any rating",
      next: { ...current, minRating: 0 },
      active: current.minRating === 0,
    });
    for (let n = 1; n <= MAX_RATING; n++) {
      out.push({
        group: RATING,
        label: `${n} star${n === 1 ? "" : "s"} and up`,
        next: { ...current, minRating: n },
        active: current.minRating === n,
      });
    }
    if (filterIsSet(current)) {
      out.push({ group: RESET, label: "clear the filter", next: { ...NO_FILTER }, active: false });
    }
    return out;
  });

  let ranked = $derived(rank(palette.query, options, (o) => [o.label, o.group]));

  let groups = $derived.by(() => {
    const out: { title: string; rows: Option[] }[] = [];
    const byTitle = new Map<string, { title: string; rows: Option[] }>();
    for (const option of ranked) {
      let group = byTitle.get(option.group);
      if (group === undefined) {
        group = { title: option.group, rows: [] };
        byTitle.set(option.group, group);
        out.push(group);
      }
      group.rows.push(option);
    }
    return out;
  });

  let flat = $derived(groups.flatMap((g) => g.rows));
  let cursor = $derived(Math.min(palette.index, Math.max(0, flat.length - 1)));

  let showing = $derived(countMatching(current));

  function run() {
    const option = flat[cursor];
    if (option === undefined) return;
    palette.setFilter(option.next);
    // The query has done its job once a row has been picked, and clearing it
    // puts the whole list back for the next dimension.
    palette.clearQuery();
  }
</script>

<PaletteFrame width={720} label="Filter the grid" count={flat.length} onrun={run}>
  {#snippet header()}
    <span class="badge">FILTER</span>
    {#if palette.query === ""}
      <span class="placeholder">narrow by kind, verdict or rating</span>
    {:else}
      <span class="query">{palette.query}</span>
    {/if}
    <span class="caret" aria-hidden="true"></span>
    <span class="spacer"></span>
    <!-- Counted against everything the grid could show, not app.groups: that
         is the filtered list, and a set filter would read "12 of 12". -->
    <span class="count">{showing} of {app.allGroups.length} frames</span>
  {/snippet}

  {#snippet chips()}
    <span class="chip" class:on={current.kind !== "all"}>{current.kind === "all" ? "any kind" : current.kind}</span>
    <span class="chip" class:on={current.verdict !== "all"}>
      {current.verdict === "all" ? "any verdict" : current.verdict}
    </span>
    <span class="chip" class:on={current.minRating > 0}>
      {current.minRating === 0 ? "any rating" : `${current.minRating}★ and up`}
    </span>
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
      {#each group.rows as option (option.group + option.label)}
        {@const index = flat.indexOf(option)}
        {@const left = countMatching(option.next)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="row" class:at={index === cursor} onmouseenter={() => (palette.index = index)} onclick={run}>
          <span class="box" class:ticked={option.active}>{option.active ? "✓" : ""}</span>
          <span class="name">{option.label}</span>
          <span class="bar">
            <span class="fill" style="width: {app.allGroups.length === 0 ? 0 : (left / app.allGroups.length) * 100}%"></span>
          </span>
          <span class="left">{left}</span>
        </div>
      {/each}
    {/each}
  {/snippet}

  {#snippet footer()}
    <div class="foot-row">
      <span><span class="fkey">↑↓</span> pick</span>
      <span><span class="fkey">⏎</span> set</span>
      <span><span class="fkey">esc</span> done</span>
      <span class="spacer"></span>
      <span>narrows the <span class="accent">grid</span>; an apply still acts on every decided frame</span>
    </div>
  {/snippet}
</PaletteFrame>

<style>
  .badge {
    flex: 0 0 auto;
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--violet-wash-16);
    color: var(--violet);
    font-size: 11px;
    font-weight: 700;
    white-space: nowrap;
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

  .box {
    flex: 0 0 14px;
    height: 14px;
    display: inline-grid;
    place-items: center;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 9px;
    font-weight: 700;
    color: var(--text-dim);
  }

  .box.ticked {
    background: var(--accent);
    border-color: var(--accent);
    color: var(--on-accent);
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

  .bar {
    flex: 1;
    min-width: 0;
    height: 4px;
    border-radius: 2px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
  }

  .left {
    flex: 0 0 auto;
    min-width: 34px;
    text-align: right;
    font-size: 10.5px;
    color: var(--text-dim);
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
