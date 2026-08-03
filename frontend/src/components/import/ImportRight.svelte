<script lang="ts">
  // IMPORT's right pane: the volumes the routed frames are landing on.
  //
  // The storage view in LIBRARY answers "what have I got"; this one answers
  // "will it fit". One card per volume, with what is already on it, what this
  // import adds, and the destinations that land there. A volume the lister
  // could not measure gets its destinations listed and no bar — see
  // CapacityBar.

  import CapacityBar from "./CapacityBar.svelte";
  import { basename, formatBytes, formatCount, importState } from "../../lib/import.svelte";

  interface VolumeGroup {
    volume: string;
    name: string;
    free: number;
    total: number;
    network: boolean;
    removable: boolean;
    bytes: number;
    frames: number;
    fits: boolean;
    destinations: { destination: string; path: string; frames: number; bytes: number }[];
  }

  /**
   * The routes gathered by the volume they land on. Two destinations on one
   * disk share its free space, so they have to be weighed together — which is
   * also how the backend decides whether they fit.
   */
  let groups = $derived.by((): VolumeGroup[] => {
    const byVolume = new Map<string, VolumeGroup>();
    for (const space of importState.space) {
      const key = space.volume === "" ? space.path : space.volume;
      let group = byVolume.get(key);
      if (group === undefined) {
        group = {
          volume: key,
          name: space.volumeName || basename(key) || key,
          free: space.free,
          total: space.total,
          network: space.network,
          removable: space.removable,
          bytes: 0,
          frames: 0,
          fits: true,
          destinations: [],
        };
        byVolume.set(key, group);
      }
      group.bytes += space.bytes;
      group.frames += space.frames;
      group.fits = group.fits && space.fits;
      group.destinations.push({
        destination: space.destination,
        path: space.path,
        frames: space.frames,
        bytes: space.bytes,
      });
    }
    return [...byVolume.values()].sort((a, b) => b.bytes - a.bytes);
  });
</script>

<div class="pane">
  <div class="label">
    <span>landing</span>
    <span class="rule"></span>
    <span class="hint">{formatCount(groups.length)}</span>
  </div>

  <div class="cards">
    {#each groups as group (group.volume)}
      <article class="card" class:tight={!group.fits}>
        <header>
          <span class="dot" class:net={group.network} class:removable={group.removable}></span>
          <span class="name" title={group.volume}>{group.name}</span>
          {#if group.network}<span class="pill">network</span>{/if}
          {#if group.removable}<span class="pill amber">removable</span>{/if}
          <span class="spacer"></span>
          <span class="total">{formatBytes(group.bytes)}</span>
        </header>

        <CapacityBar
          total={group.total}
          free={group.free}
          landing={group.bytes}
          fits={group.fits}
          height={12}
          legend
        />

        <div class="rows">
          {#each group.destinations as dest (dest.destination)}
            <div class="row" title={dest.path}>
              <span class="cell c-name">{dest.destination}</span>
              <span class="cell c-frames">{formatCount(dest.frames)}</span>
              <span class="cell c-size">{formatBytes(dest.bytes)}</span>
            </div>
          {/each}
        </div>

        {#if !group.fits}
          <p class="warn">not enough room for what is routed here</p>
        {:else if group.removable}
          <p class="warn">this destination is on removable media</p>
        {/if}
      </article>
    {/each}

    {#if groups.length === 0}
      <p class="empty">
        nothing is routed yet — the volumes appear as frames are given destinations in cull
      </p>
    {/if}
  </div>
</div>

<style>
  .pane {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    padding: 10px 12px;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 8px;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .hint {
    font-family: var(--font-mono);
    letter-spacing: 0;
    color: var(--text-dim);
  }

  .cards {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .card {
    border-radius: 7px;
    background: var(--bg-chrome);
    border: 1px solid var(--border);
    padding: 12px;
    min-width: 0;
  }

  .card.tight {
    border-color: var(--cut-wash-22);
  }

  header {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding-bottom: 10px;
    min-width: 0;
  }

  .dot {
    flex: 0 0 auto;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    align-self: center;
  }

  .dot.net {
    background: var(--violet);
  }

  .dot.removable {
    background: var(--amber);
  }

  .name {
    flex: 1 1 auto;
    min-width: 0;
    font-size: 13px;
    color: var(--text-hi);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pill {
    flex: 0 0 auto;
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--violet-wash-16);
    color: var(--violet);
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .pill.amber {
    background: var(--amber-wash-16);
    color: var(--amber);
  }

  .spacer {
    flex: 1;
  }

  .total {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--accent);
  }

  .rows {
    padding-top: 10px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 26px;
    border-top: 1px solid var(--border-hair);
    font-size: 11px;
    color: var(--text-muted);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .c-name {
    flex: 1 1 auto;
    font-family: var(--font-mono);
    color: var(--text);
  }

  .c-frames {
    flex: 0 0 60px;
    text-align: right;
    font-family: var(--font-mono);
  }

  .c-size {
    flex: 0 0 74px;
    text-align: right;
    font-family: var(--font-mono);
    color: var(--text);
  }

  .warn {
    margin: 8px 0 0;
    font-size: 10.5px;
    color: var(--amber);
  }

  .card.tight .warn {
    color: var(--cut-text);
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    line-height: 1.5;
    color: var(--text-ghost);
  }
</style>
