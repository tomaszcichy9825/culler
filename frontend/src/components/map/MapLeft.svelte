<script lang="ts">
  // MAP's left pane: the places the open folder's photographs were taken.
  //
  // A row is a cluster, and the clusters are exactly the pins the map is
  // drawing — the same maths, computed once by the map pane and shared here, so
  // the rail cannot list a place that is not on the map or miss one that is.
  // The rail therefore reflows as the map zooms, which is the honest behaviour:
  // two pins that merge on screen are one place at that zoom.
  //
  // What each sub-layout calls the rail differs: pins lists places in capture
  // order, heat ranks them by how many frames they hold, and track lists the
  // day the polyline is drawn from and the legend that reads it.

  import { formatClock, formatDate, mapState, placeName } from "../../lib/map.svelte";
  import type { MapCluster } from "../../lib/map.svelte";

  interface Props {
    /** The mode's sub-layout: 0 pins, 1 heat, 2 track. */
    layout?: number;
  }

  let { layout = 0 }: Props = $props();

  let container = $state<HTMLDivElement | null>(null);

  const TITLES = ["places", "density", "tracks"];
  let title = $derived(TITLES[layout] ?? TITLES[0]);

  /**
   * Heat ranks by count; the other two keep the order the clusters came in,
   * which is capture order. The rows carry their index into `clusters` so a
   * ranked row still focuses the right pin.
   */
  let rows = $derived.by<{ cluster: MapCluster; index: number }[]>(() => {
    const all = mapState.clusters.map((cluster, index) => ({ cluster, index }));
    if (layout !== 1) return all;
    return all.sort((a, b) => b.cluster.count - a.cluster.count);
  });

  /** The largest cluster, which the density meters are drawn against. */
  let busiest = $derived(rows.reduce((most, r) => Math.max(most, r.cluster.count), 0));

  /** The span the track covers, which is the whole folder in capture order. */
  let span = $derived.by(() => {
    const frames = mapState.positions;
    if (frames.length === 0) return null;
    const first = frames[0];
    const last = frames[frames.length - 1];
    return {
      date: formatDate(first.shot),
      from: formatClock(first.shot),
      to: formatClock(last.shot),
      points: frames.length,
    };
  });

  function rowEl(index: number): HTMLElement | null {
    return container?.querySelector(`[data-place="${index}"]`) ?? null;
  }

  function holdsFocus(): boolean {
    return container !== null && container.contains(document.activeElement);
  }

  /** focusAt walks the visible rows, which is not the same order as the pins. */
  function focusAt(position: number) {
    if (rows.length === 0) return;
    const clamped = Math.max(0, Math.min(position, rows.length - 1));
    mapState.focusCluster(rows[clamped].index);
    if (holdsFocus()) queueMicrotask(() => rowEl(rows[clamped].index)?.focus());
  }

  /** Where the focused cluster sits among the rows as they are listed. */
  let position = $derived(Math.max(0, rows.findIndex((r) => r.index === mapState.clusterIndex)));

  function onKeydown(e: KeyboardEvent) {
    if (rows.length === 0) return;
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        focusAt(position + 1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        focusAt(position - 1);
        break;
      case "Home":
        e.preventDefault();
        focusAt(0);
        break;
      case "End":
        e.preventDefault();
        focusAt(rows.length - 1);
        break;
      case "Enter":
        e.preventDefault();
        mapState.open();
        break;
      case "Escape":
        e.preventDefault();
        (document.activeElement as HTMLElement | null)?.blur();
        break;
    }
  }
</script>

<div class="pane">
  <div class="head">
    <span>{title}</span>
    <span class="rule"></span>
    <span class="chord">⌘1</span>
  </div>

  <div
    class="places"
    bind:this={container}
    data-keys="local"
    role="listbox"
    aria-label="Places"
    tabindex="-1"
    onkeydown={onKeydown}
  >
    {#each rows as row (row.cluster.id)}
      <button
        type="button"
        class="place"
        class:active={mapState.clusterIndex === row.index}
        data-place={row.index}
        role="option"
        aria-selected={mapState.clusterIndex === row.index}
        tabindex={mapState.clusterIndex === row.index ? 0 : -1}
        onfocus={() => mapState.focusCluster(row.index)}
        onclick={() => mapState.focusCluster(row.index)}
        ondblclick={() => mapState.open(row.cluster.frames[0])}
      >
        <span class="name">{placeName(row.cluster)}</span>
        {#if layout === 1 && busiest > 0}
          <span class="meter"><span class="fill" style:width={`${(row.cluster.count / busiest) * 100}%`}></span></span>
        {/if}
        <span class="count">{row.cluster.count}</span>
      </button>
    {/each}

    {#if rows.length === 0}
      <p class="empty">
        {#if mapState.loading}
          reading positions…
        {:else if mapState.dir === ""}
          no folder open
        {:else}
          nothing here has a position
        {/if}
      </p>
    {/if}

    {#if mapState.withoutGPS !== ""}
      <div class="none">
        <span class="name">no position</span>
        <span class="count">{mapState.unpositioned + mapState.unreadable}</span>
      </div>
    {/if}
  </div>

  {#if layout === 2}
    <section class="foot">
      <div class="label">track</div>
      {#if span === null}
        <p class="note">nothing to draw a line through</p>
      {:else}
        <div class="gpx">
          <span class="swatch"></span>
          <span class="fname">{span.date}</span>
        </div>
        <p class="note">
          {span.from} → {span.to} · <span class="lit">{span.points} positioned</span>
        </p>
      {/if}

      <div class="label legend">legend</div>
      <div class="key"><span class="dot start"></span><span>first frame</span></div>
      <div class="key"><span class="dot end"></span><span>last frame</span></div>
      <div class="key"><span class="dot matched"></span><span>a frame on the line</span></div>
      <p class="note">
        the line joins the folder's own positions in capture order — no GPX file is read, and nothing is written
      </p>
    </section>
  {/if}
</div>

<style>
  .pane {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 10px;
    border-bottom: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .chord {
    letter-spacing: 0;
  }

  .places {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 6px 0;
    outline: none;
  }

  .place,
  .none {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    height: 26px;
    padding: 0 12px;
    border: none;
    border-left: 2px solid transparent;
    background: none;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .place:hover {
    background: var(--bg-row-zebra);
  }

  .place.active {
    background: var(--accent-wash-16);
    border-left-color: var(--accent);
  }

  .place:focus-visible {
    outline: none;
    box-shadow: var(--focus-inset-2);
  }

  .name {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .place.active .name {
    color: var(--text-hi);
  }

  .count {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-muted);
  }

  /* The density meter only appears in the heat layout, where the rail is
     ranked and the bar is what makes the ranking readable at a glance. */
  .meter {
    flex: 0 0 46px;
    height: 5px;
    border-radius: 3px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--cut);
  }

  .none {
    cursor: default;
  }

  .none .name,
  .none .count {
    color: var(--amber);
  }

  .empty {
    margin: 0;
    padding: 20px 12px;
    text-align: center;
    font-size: 11px;
    color: var(--text-ghost);
  }

  .foot {
    flex: 0 0 auto;
    padding: 10px;
    border-top: 1px solid var(--border);
  }

  .label {
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    padding-bottom: 7px;
  }

  .legend {
    padding-top: 14px;
  }

  .gpx {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 5px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
  }

  .swatch {
    flex: 0 0 auto;
    width: 14px;
    height: 2px;
    background: var(--violet);
  }

  .fname {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .key {
    display: flex;
    align-items: center;
    gap: 9px;
    height: 22px;
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
  }

  .dot {
    flex: 0 0 auto;
    width: 9px;
    height: 9px;
    border-radius: 50%;
  }

  .dot.start {
    border: 2px solid var(--violet);
  }

  .dot.end {
    background: var(--violet);
  }

  .dot.matched {
    background: var(--keep);
  }

  .note {
    margin: 6px 0 0;
    font-family: var(--font-mono);
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--text-dim);
    text-wrap: pretty;
  }

  .lit {
    color: var(--keep);
  }
</style>
