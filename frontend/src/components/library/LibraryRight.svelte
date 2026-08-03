<script lang="ts">
  // LIBRARY's right pane. Search shows the faceted lists (5a); sessions shows
  // the volumes and the selected session (5b); storage shows what each volume
  // is holding (10b).

  import FacetList from "./FacetList.svelte";
  import StackedBar from "./StackedBar.svelte";
  import {
    basename,
    formatBytes,
    formatClock,
    formatCount,
    formatDate,
    formatSpan,
    library,
  } from "../../lib/library.svelte";

  interface Props {
    /** The sub-layout index: 0 search, 1 sessions, 2 storage. */
    layout: number;
  }

  let { layout }: Props = $props();

  let counts = $derived(library.counts);
  let session = $derived(library.session);
  let volumes = $derived(library.storage?.volumes ?? []);
</script>

<aside class="right">
  {#if layout === 0}
    {#if counts === null}
      <p class="empty">facets appear once the catalogue has been searched</p>
    {:else}
      <FacetList
        title="kind"
        rows={counts.kinds}
        selected={library.facets.kind}
        onpick={(value) => library.setFacet("kind", value)}
      />
      <FacetList
        title="verdict"
        rows={counts.verdicts}
        selected={library.facets.verdict}
        colour="var(--keep)"
        onpick={(value) => library.setFacet("verdict", value)}
      />
      <FacetList
        title="rating"
        rows={counts.ratings}
        selected={library.facets.minRating === 0 ? "" : String(library.facets.minRating)}
        colour="var(--gold)"
        onpick={(value) => library.setFacet("minRating", Number(value))}
      />
      {#if library.facets.root !== ""}
        <section>
          <div class="label"><span>folder</span><span class="rule"></span></div>
          <div class="kv">
            <span class="v" title={library.facets.root}>{basename(library.facets.root)}</span>
            <button type="button" class="clear" onclick={() => library.setFacet("root", "")}>
              clear
            </button>
          </div>
        </section>
      {/if}
    {/if}
  {:else if layout === 1}
    <section>
      <div class="label"><span>volumes</span><span class="rule"></span></div>
      {#each volumes as volume (volume.volume)}
        <div class="volume">
          <div class="line">
            <span class="dot"></span>
            <span class="name" title={volume.volume}>{basename(volume.volume) || volume.volume}</span>
            <span class="spacer"></span>
            <span class="value">{formatBytes(volume.bytes)}</span>
          </div>
          <StackedBar
            height={7}
            segments={[
              { label: "raw", value: volume.rawBytes, colour: "var(--accent)" },
              { label: "jpeg", value: volume.jpegBytes, colour: "var(--brand)" },
            ]}
          />
        </div>
      {:else}
        <p class="empty">nothing catalogued</p>
      {/each}
    </section>

    <section>
      <div class="label"><span>selected session</span><span class="rule"></span></div>
      {#if session === null}
        <p class="empty">pick a session</p>
      {:else}
        <div class="kv"><span class="k">date</span><span class="v">{formatDate(session.start)}</span></div>
        <div class="kv">
          <span class="k">ran</span>
          <span class="v">
            {formatClock(session.start)}–{formatClock(session.end)} · {formatSpan(session.spanMinutes)}
          </span>
        </div>
        <div class="kv">
          <span class="k">frames</span>
          <span class="v">{formatCount(session.frames)}</span>
        </div>
        <div class="kv">
          <span class="k">kept / cut</span>
          <span class="v">{formatCount(session.kept)} / {formatCount(session.cut)}</span>
        </div>
        <div class="kv">
          <span class="k">to judge</span>
          <span class="v" class:absent={session.undecided === 0}>{formatCount(session.undecided)}</span>
        </div>
        <div class="kv"><span class="k">on disk</span><span class="v">{formatBytes(session.bytes)}</span></div>
        <div class="kv">
          <span class="k">folder</span>
          <span class="v" title={session.dir}>{session.source}</span>
        </div>
        <button type="button" class="primary" onclick={() => library.openDir(session.dir)}>
          open in CULL
        </button>
      {/if}
    </section>
  {:else}
    <section>
      <div class="label"><span>catalogued</span><span class="rule"></span></div>
      <div class="kv">
        <span class="k">frames</span>
        <span class="v">{formatCount(library.storage?.frames ?? 0)}</span>
      </div>
      <div class="kv">
        <span class="k">raw</span>
        <span class="v">{formatBytes(library.storage?.rawBytes ?? 0)}</span>
      </div>
      <div class="kv">
        <span class="k">jpeg</span>
        <span class="v">{formatBytes(library.storage?.jpegBytes ?? 0)}</span>
      </div>
      <div class="kv">
        <span class="k">total</span>
        <span class="v">{formatBytes(library.storage?.bytes ?? 0)}</span>
      </div>
    </section>

    <section>
      <div class="label"><span>notes</span><span class="rule"></span></div>
      <p class="note">
        These are the bytes the catalogue recorded when it last walked each folder. Nothing here
        deletes anything, and nothing here reads the disk's free space — reclaiming goes through
        the same plan as a cull, and every line says which volume it touches.
      </p>
    </section>
  {/if}
</aside>

<style>
  .right {
    flex: 0 0 auto;
    min-height: 0;
    overflow-y: auto;
    background: var(--bg-pane);
  }

  section {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
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

  .empty {
    margin: 0;
    padding: 8px 0;
    font-size: 10.5px;
    color: var(--text-ghost);
  }

  .volume {
    padding: 4px 0 8px;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-bottom: 5px;
    font-size: 11px;
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent);
  }

  .name {
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .value {
    font-family: var(--font-mono);
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .kv {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-height: 20px;
    font-size: 11px;
  }

  .k {
    flex: 0 0 72px;
    color: var(--text-dim);
  }

  .v {
    flex: 1;
    min-width: 0;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .v.absent {
    color: var(--text-dead);
  }

  .clear {
    flex: 0 0 auto;
    padding: 0;
    border: none;
    background: none;
    font: inherit;
    font-size: 10.5px;
    color: var(--text-dim);
    cursor: pointer;
  }

  .clear:hover {
    color: var(--text);
  }

  .primary {
    width: 100%;
    height: 26px;
    margin-top: 10px;
    border-radius: 4px;
    border: none;
    background: var(--accent);
    color: var(--on-accent);
    font: inherit;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
  }

  .note {
    margin: 0;
    font-size: 10.5px;
    line-height: 1.5;
    color: var(--text-muted);
  }
</style>
