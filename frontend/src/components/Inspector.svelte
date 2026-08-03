<script lang="ts">
  // The right pane: the focused frame at a readable size, what its verdict
  // does to each half, its exposure, and the facts the scan actually recorded.
  //
  // Every row here comes from the frame's own DTO. There is no EXIF pipeline
  // yet, so there are no camera rows — an inspector that guessed at a lens
  // would be worse than one that admits what it knows.

  import { queuedImage } from "../lib/imageQueue";
  import { previewURL } from "../lib/preview";
  import { app, groupKey } from "../lib/state.svelte";
  import {
    fileLabel,
    halfState,
    HALVES,
    kindLabel,
    MAX_RATING,
    maskOf,
    shotLabel,
    verdictOf,
    verdictWord,
  } from "../lib/verdict";
  import type { Half } from "../lib/verdict";

  /** Bars in the exposure histogram, per the design. */
  const BINS = 40;
  /** The sample is drawn small: 6k pixels is plenty for a 40-bin shape. */
  const SAMPLE_W = 96;
  const SAMPLE_H = 64;

  /**
   * Histograms are cached by frame identity. Arrowing along a burst and back
   * would otherwise redraw and re-read the same pixels every time.
   */
  const cache = new Map<string, number[]>();

  let bins = $state<number[] | null>(null);

  let frame = $derived(app.focused);
  let key = $derived(frame ? groupKey(frame) : "");
  let url = $derived(frame ? previewURL(frame) : "");
  let verdict = $derived(frame ? verdictOf(frame) : "");

  $effect(() => {
    bins = cache.get(key) ?? null;
  });

  function pathOf(half: Half): string {
    if (!frame) return "";
    return half === "r" ? frame.rawPath : frame.jpegPath;
  }

  /**
   * sample reads the loaded preview into a luma histogram. The preview is
   * served from the app's own origin, so the canvas is never tainted; a reader
   * that throws anyway leaves the block empty rather than breaking the pane.
   */
  function sample(img: HTMLImageElement, id: string) {
    if (cache.has(id)) {
      bins = cache.get(id) ?? null;
      return;
    }
    const canvas = document.createElement("canvas");
    canvas.width = SAMPLE_W;
    canvas.height = SAMPLE_H;
    const ctx = canvas.getContext("2d", { willReadFrequently: true });
    if (ctx === null) return;
    let pixels: Uint8ClampedArray;
    try {
      ctx.drawImage(img, 0, 0, SAMPLE_W, SAMPLE_H);
      pixels = ctx.getImageData(0, 0, SAMPLE_W, SAMPLE_H).data;
    } catch {
      return;
    }

    const counts = new Array<number>(BINS).fill(0);
    for (let i = 0; i < pixels.length; i += 4) {
      const luma = pixels[i] * 0.2126 + pixels[i + 1] * 0.7152 + pixels[i + 2] * 0.0722;
      counts[Math.min(BINS - 1, Math.floor((luma / 256) * BINS))]++;
    }
    const peak = Math.max(1, ...counts);
    const shape = counts.map((c) => Math.round((c / peak) * 100));
    cache.set(id, shape);
    // The focus may have moved on while the image was decoding.
    if (id === key) bins = shape;
  }
</script>

{#snippet label(title: string)}
  <div class="section-label">
    <span>{title}</span>
    <span class="hair"></span>
  </div>
{/snippet}

{#snippet row(k: string, v: string, tone = "")}
  <div class="row">
    <span class="rkey">{k}</span>
    <!-- A path row is laid out right-to-left so it truncates at the front. The
         leading mark keeps the path itself left-to-right inside that box:
         without it the "/" a path starts with is a neutral character and gets
         placed at the far end, turning /Volumes/Card into Volumes/Card/. -->
    <span class="rvalue {tone}" title={v}>{tone === "path" ? `‎${v}` : v}</span>
  </div>
{/snippet}

{#if frame}
  <div class="inspector">
    <div class="well">
      {#if url !== ""}
        <img
          use:queuedImage={url}
          alt={frame.stem}
          decoding="async"
          onload={(e) => sample(e.currentTarget as HTMLImageElement, key)}
        />
      {:else}
        <span class="missing">no preview</span>
      {/if}
    </div>

    <section class="block">
      <div class="heading">
        <span class="iname" title={frame.stem}>{frame.stem}</span>
        <span class="icount">{app.focusIndex + 1} of {app.groups.length}</span>
      </div>

      <div class="pair">
        {#each HALVES as half (half)}
          {@const state = halfState(frame, half, app.cutRemoves)}
          {@const path = pathOf(half)}
          <div class="card {state}" data-half={half} data-state={state}>
            <span class="ext">{path === "" ? (half === "r" ? "RAW" : "JPEG") : fileLabel(path, half.toUpperCase())}</span>
            <span class="note">{state === "absent" ? "none" : state}</span>
          </div>
        {/each}
      </div>

      <div class="verdict-line">
        <span class="vword {verdict}">{verdict === "" ? "undecided" : verdictWord(verdict)}</span>
        <span class="stars" aria-label="{frame.rating} of {MAX_RATING}">
          {#each { length: MAX_RATING } as _, i (i)}
            <span class="star" class:lit={i < frame.rating}></span>
          {/each}
        </span>
        <span class="rhint">1–5 to rate</span>
      </div>
    </section>

    <section class="block">
      {@render label("Exposure")}
      <div class="histogram" data-bins={bins === null ? 0 : bins.length}>
        {#if bins === null}
          <span class="hempty">waiting for the preview</span>
        {:else}
          {#each bins as h, i (i)}
            <span class="hbar" style:height="{Math.max(h, 1)}%"></span>
          {/each}
        {/if}
      </div>
    </section>

    <section class="block">
      {@render label("Frame")}
      {@render row("shot", shotLabel(frame.shot))}
      {@render row("files", kindLabel(frame.kind))}
      <!-- A mask says which halves a verdict holds on to, so it means nothing
           until there is one to hold them. -->
      {#if verdict !== ""}
        {@render row("kept", maskOf(frame) === "rj" ? "both halves" : maskOf(frame) === "r" ? "RAW only" : "JPEG only")}
      {/if}
      {@render row("rating", frame.rating === 0 ? "unrated" : `${frame.rating} of ${MAX_RATING}`)}
      {#if frame.sidecars > 0}
        {@render row("sidecars", String(frame.sidecars))}
      {/if}
    </section>

    <section class="block">
      {@render label("Files")}
      {@render row("folder", frame.dir, "path")}
      {@render row("raw", frame.rawPath === "" ? "—" : frame.rawPath, "path")}
      {@render row("jpeg", frame.jpegPath === "" ? "—" : frame.jpegPath, "path")}
      {@render row("identity", frame.hash === "" ? "unreadable" : frame.hash, frame.hash === "" ? "warn" : "path")}
    </section>

    {#if (frame.warnings ?? []).length > 0}
      <section class="block">
        {@render label("Warnings")}
        {#each frame.warnings ?? [] as warning (warning)}
          <p class="warning">{warning}</p>
        {/each}
      </section>
    {/if}
  </div>
{:else}
  <div class="ghost">
    <p>no frame selected</p>
    <p>the inspector fills in</p>
    <p>once you pick one</p>
  </div>
{/if}

<style>
  .inspector {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
  }

  .well {
    position: relative;
    aspect-ratio: 3 / 2;
    display: grid;
    place-items: center;
    background: var(--bg-thumb);
    border-bottom: 1px solid var(--border);
    overflow: hidden;
  }

  .well img {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .missing {
    font-size: 10.5px;
    color: var(--text-ghost);
  }

  .block {
    padding: 10px 12px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }

  .section-label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 7px;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .hair {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .heading {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
  }

  .iname {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .icount {
    flex: 0 0 auto;
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .pair {
    display: flex;
    gap: 6px;
    margin-top: 8px;
  }

  .card {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 6px;
    padding: 5px 7px;
    border-radius: 4px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
  }

  .card .ext {
    font-size: 11px;
    font-weight: 700;
    color: var(--text-muted);
  }

  .card .note {
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .card.kept {
    border-color: var(--keep);
  }

  .card.kept .ext {
    color: var(--keep);
  }

  .card.cut {
    border-color: var(--cut);
  }

  .card.cut .ext {
    color: var(--cut);
    text-decoration: line-through;
  }

  .card.cut .note {
    color: var(--cut);
  }

  .card.absent .ext {
    color: var(--text-ghost);
  }

  .verdict-line {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 9px;
    min-width: 0;
  }

  .vword {
    font-size: 11px;
    font-weight: 700;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .vword.keep {
    color: var(--keep);
  }

  .vword.cut {
    color: var(--cut);
  }

  .stars {
    display: flex;
    gap: 3px;
  }

  .star {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--bg-track);
  }

  .star.lit {
    background: var(--gold);
  }

  .rhint {
    flex: 1;
    text-align: right;
    font-size: 10px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .histogram {
    height: 56px;
    display: flex;
    align-items: flex-end;
    gap: 1px;
  }

  .hbar {
    flex: 1;
    min-width: 0;
    background: var(--text-dead);
  }

  .hempty {
    flex: 1;
    align-self: center;
    text-align: center;
    font-size: 10px;
    color: var(--text-ghost);
  }

  .row {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-height: 20px;
    min-width: 0;
  }

  .rkey {
    flex: 0 0 72px;
    font-size: 11px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rvalue {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* A path that does not fit is truncated at the front: the leaf is the part
     that identifies it, and the folder it sits in is already a row above. */
  .rvalue.path {
    direction: rtl;
    text-align: left;
  }

  .rvalue.warn {
    color: var(--amber);
  }

  .warning {
    margin: 0 0 4px;
    font-size: 11px;
    line-height: 1.5;
    color: var(--amber);
    overflow-wrap: anywhere;
  }

  .ghost {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    padding: 0 16px;
    text-align: center;
    color: var(--text-ghost);
    font-size: 11px;
    line-height: 1.7;
  }

  .ghost p {
    margin: 0;
    max-width: 100%;
  }
</style>
