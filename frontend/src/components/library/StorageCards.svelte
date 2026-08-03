<script lang="ts">
  // The storage view (10b): one card per volume, each with a stacked bar and
  // the roots that sit on it.
  //
  // Every number here is of what the catalogue holds. The app does not stat the
  // filesystem, so there is no free space, no capacity and no "used of total" —
  // the design's card carries those, and they are left out rather than
  // estimated. A guessed capacity on a screen about reclaiming disk space would
  // be the worst possible place to be wrong.

  import StackedBar from "./StackedBar.svelte";
  import { basename, formatBytes, formatCount, library, percent } from "../../lib/library.svelte";

  let storage = $derived(library.storage);

  /** The roots on one volume, biggest first. */
  function rootsOn(volume: string) {
    return (storage?.roots ?? [])
      .filter((r) => r.volume === volume)
      .sort((a, b) => b.bytes - a.bytes);
  }
</script>

<div class="storage">
  {#if storage === null || storage.volumes.length === 0}
    <p class="empty">nothing is catalogued yet — add a folder on the left</p>
  {:else}
    {#each storage.volumes as volume (volume.volume)}
      <article class="card">
        <header>
          <span class="dot"></span>
          <span class="name" title={volume.volume}>{basename(volume.volume) || volume.volume}</span>
          <span class="pill">{volume.roots.length === 1 ? "root" : `${volume.roots.length} roots`}</span>
          <span class="spacer"></span>
          <span class="total">{formatBytes(volume.bytes)}</span>
          <span class="frames">{formatCount(volume.frames)} frames</span>
        </header>

        <StackedBar
          height={12}
          legend
          segments={[
            {
              label: "raw",
              value: volume.rawBytes,
              colour: "var(--accent)",
              note: formatBytes(volume.rawBytes),
            },
            {
              label: "jpeg",
              value: volume.jpegBytes,
              colour: "var(--brand)",
              note: formatBytes(volume.jpegBytes),
            },
          ]}
        />

        <div class="rows">
          {#each rootsOn(volume.volume) as root (root.root)}
            <button type="button" class="row" onclick={() => library.openDir(root.root)}>
              <span class="cell c-name" title={root.root}>{root.root}</span>
              <span class="cell c-frames">{formatCount(root.frames)}</span>
              <span class="cell c-size">{formatBytes(root.bytes)}</span>
              <span class="cell c-share">
                {root.frames === 0 ? "not indexed" : `${percent(root.bytes, volume.bytes).toFixed(0)}%`}
              </span>
            </button>
          {/each}
        </div>
      </article>
    {/each}
  {/if}
</div>

<style>
  .storage {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .empty {
    margin: 0;
    padding: 24px 0;
    text-align: center;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .card {
    border-radius: 7px;
    background: var(--bg-chrome);
    border: 1px solid var(--border);
    padding: 12px;
  }

  header {
    display: flex;
    align-items: baseline;
    gap: 8px;
    padding-bottom: 10px;
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    align-self: center;
  }

  .name {
    font-size: 13px;
    color: var(--text-hi);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pill {
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--accent-wash-14);
    color: var(--accent);
    font-size: 9.5px;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .spacer {
    flex: 1;
  }

  .total {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--text);
  }

  .frames {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .rows {
    padding-top: 10px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    height: 28px;
    padding: 0;
    border: none;
    border-top: 1px solid var(--border-hair);
    background: none;
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
  }

  .row:hover .c-name {
    color: var(--text);
  }

  .cell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .c-name {
    flex: 1 1 auto;
    /* The tail of a path is the part that identifies it, so a long one is cut
       at the front. */
    direction: rtl;
    text-align: left;
  }
  .c-frames {
    flex: 0 0 76px;
    text-align: right;
    font-family: var(--font-mono);
  }
  .c-size {
    flex: 0 0 82px;
    text-align: right;
    font-family: var(--font-mono);
    color: var(--text);
  }
  .c-share {
    flex: 0 0 92px;
    text-align: right;
    font-size: 10.5px;
    color: var(--text-dim);
  }
</style>
