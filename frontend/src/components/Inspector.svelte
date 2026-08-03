<script lang="ts">
  // The right pane: the focused frame at a readable size, what its verdict
  // does to each half, its histogram, and the facts behind it.
  //
  // Two sources, and the pane is careful about which is which. The frame's own
  // DTO carries what the scan recorded — the files, the pairing, the rating.
  // The camera rows come from the shared EXIF cache, read lazily for whichever
  // frame is focused, and a tag the file does not carry gets no row at all: an
  // inspector that guesses at a lens is worse than one that admits what it
  // knows.
  //
  // Every value in here is a button, because the reason to look at a path or a
  // serial number in a culling tool is almost always to paste it somewhere.

  import type { Snippet } from "svelte";
  import { Clipboard } from "@wailsio/runtime";
  import type { GroupDTO } from "../lib/bindings";
  import { message } from "../lib/decisions";
  import { exifCache, valueOf } from "../lib/exifcache.svelte";
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

  /** A value the pane does not have. Never a zero, never a guess. */
  const NO_DATA = "—";

  /** Where a section's collapsed state is kept, one key per section. */
  const STORE = "culler.inspector.section.";

  /** The collapsible sections, in the order they are drawn. */
  const SECTIONS = ["histogram", "metadata", "files", "warnings"] as const;

  function stored(id: string): boolean {
    try {
      return localStorage.getItem(STORE + id) === "collapsed";
    } catch {
      // A webview with storage denied still draws; it just forgets.
      return false;
    }
  }

  let shut = $state<Record<string, boolean>>(
    Object.fromEntries(SECTIONS.map((id) => [id, stored(id)])),
  );

  function toggle(id: string) {
    const next = shut[id] !== true;
    shut = { ...shut, [id]: next };
    try {
      localStorage.setItem(STORE + id, next ? "collapsed" : "open");
    } catch {
      // As above: the pane works, the preference does not survive a restart.
    }
  }

  let bins = $state<number[] | null>(null);

  let frame = $derived(app.focused);
  let key = $derived(frame ? groupKey(frame) : "");
  let url = $derived(frame ? previewURL(frame) : "");
  let verdict = $derived(frame ? verdictOf(frame) : "");

  // One frame at a time: the inspector draws one, so it asks for one. The
  // cache does the batching, and a frame already read costs nothing to re-ask.
  $effect(() => {
    if (frame) exifCache.request([frame]);
  });

  let meta = $derived(exifCache.get(key));

  /** The camera rows, in the order the design stacks them. Absent tags drop. */
  let cameraRows = $derived(
    (
      [
        ["make", "Make"],
        ["model", "Model"],
        ["lens", "LensModel"],
        ["shutter", "ExposureTime"],
        ["ƒ", "FNumber"],
        ["iso", "ISO"],
        ["focal", "FocalLength"],
      ] as const
    )
      .map(([label, tag]) => ({ label, value: valueOf(meta, tag) }))
      .filter((row) => row.value !== ""),
  );

  /**
   * The capture time the camera wrote beats the one the scan inferred: the
   * scan falls back to the file's modification time, which a card reader can
   * move. Until the read lands, the scan's is what there is.
   */
  let shot = $derived(shotLabel(valueOf(meta, "DateTimeOriginal") || (frame?.shot ?? "")));

  function pathOf(half: Half): string {
    if (!frame) return "";
    return half === "r" ? frame.rawPath : frame.jpegPath;
  }

  /**
   * copy is the title bar's clipboard behaviour, on every value in the pane:
   * the async clipboard first, the native one for a webview that withholds it.
   */
  async function copy(value: string) {
    if (value === "") return;
    try {
      await navigator.clipboard.writeText(value);
      app.notify("copied");
      return;
    } catch {
      // Fall through to the runtime's own clipboard.
    }
    try {
      await Clipboard.SetText(value);
      app.notify("copied");
    } catch (err) {
      app.notify(`could not copy: ${message(err)}`, "error");
    }
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

{#snippet block(id: string, title: string, body: Snippet<[GroupDTO]>, f: GroupDTO)}
  <section class="block">
    <!-- A button, not a div with a handler: ⏎ and space toggle it for free,
         and it lands in the tab order where a control belongs. -->
    <button
      type="button"
      class="section-label"
      data-section={id}
      aria-expanded={shut[id] !== true}
      onclick={() => toggle(id)}
    >
      <span class="chev" class:shut={shut[id]} aria-hidden="true"></span>
      <span>{title}</span>
      <span class="hair"></span>
    </button>
    {#if shut[id] !== true}
      {@render body(f)}
    {/if}
  </section>
{/snippet}

{#snippet row(k: string, v: string, tone = "")}
  <div class="row">
    <span class="rkey">{k}</span>
    <!-- A path row is laid out right-to-left so it truncates at the front. The
         leading mark keeps the path itself left-to-right inside that box:
         without it the "/" a path starts with is a neutral character and gets
         placed at the far end, turning /Volumes/Card into Volumes/Card/. -->
    {#if v === "" || v === NO_DATA}
      <!-- Nothing to put on the clipboard, so nothing that looks like it would. -->
      <span class="rvalue absent">{NO_DATA}</span>
    {:else}
      <button type="button" class="rvalue {tone}" title="{v}&#10;Click to copy" onclick={() => void copy(v)}>
        {tone === "path" ? `‎${v}` : v}
      </button>
    {/if}
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

    {@render block("histogram", "Histogram", histogram, frame)}
    {@render block("metadata", "Metadata", metadata, frame)}
    {@render block("files", "Files", files, frame)}
    {#if (frame.warnings ?? []).length > 0}
      {@render block("warnings", "Warnings", warnings, frame)}
    {/if}
  </div>

  {#snippet histogram()}
    <div class="histogram" data-bins={bins === null ? 0 : bins.length}>
      {#if bins === null}
        <span class="hempty">waiting for the preview</span>
      {:else}
        {#each bins as h, i (i)}
          <span class="hbar" style:height="{Math.max(h, 1)}%"></span>
        {/each}
      {/if}
    </div>
  {/snippet}

  {#snippet metadata(f: GroupDTO)}
    {@render row("shot", shot)}
    {#each cameraRows as camera (camera.label)}
      {@render row(camera.label, camera.value)}
    {/each}
    {@render row("files", kindLabel(f.kind))}
    <!-- A mask says which halves a verdict holds on to, so it means nothing
         until there is one to hold them. -->
    {#if verdict !== ""}
      {@render row("kept", maskOf(f) === "rj" ? "both halves" : maskOf(f) === "r" ? "RAW only" : "JPEG only")}
    {/if}
    {@render row("rating", f.rating === 0 ? "unrated" : `${f.rating} of ${MAX_RATING}`)}
    {#if f.sidecars > 0}
      {@render row("sidecars", String(f.sidecars))}
    {/if}
  {/snippet}

  {#snippet files(f: GroupDTO)}
    {@render row("folder", f.dir, "path")}
    {@render row("raw", f.rawPath, "path")}
    {@render row("jpeg", f.jpegPath, "path")}
    {@render row("identity", f.hash === "" ? "unreadable" : f.hash, f.hash === "" ? "warn" : "path")}
  {/snippet}

  {#snippet warnings(f: GroupDTO)}
    {#each f.warnings ?? [] as warning (warning)}
      <button type="button" class="warning" title="{warning}&#10;Click to copy" onclick={() => void copy(warning)}>
        {warning}
      </button>
    {/each}
  {/snippet}
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

  /* §4.8's section label, with the whole row made the hit target rather than a
     chevron the size of a full stop. */
  .section-label {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 0 7px;
    border: none;
    background: none;
    font: inherit;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    text-align: left;
    color: var(--text-faint);
    cursor: pointer;
  }

  .section-label:hover {
    color: var(--text-dim);
  }

  .chev {
    flex: 0 0 auto;
    width: 0;
    height: 0;
    border-left: 4px solid currentColor;
    border-top: 3px solid transparent;
    border-bottom: 3px solid transparent;
    transform: rotate(90deg);
  }

  .chev.shut {
    transform: none;
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
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    font: inherit;
    font-size: 11px;
    text-align: left;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: pointer;
  }

  /* The wash is the only thing that says a value is a control. Nothing here is
     a button in the design's sense, and drawing eleven of them as buttons
     would turn a fact sheet into a toolbar. */
  button.rvalue:hover {
    background: var(--bg-raised);
  }

  .rvalue.absent {
    color: var(--text-dead);
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
    display: block;
    width: 100%;
    margin: 0 0 4px;
    padding: 0;
    border: none;
    border-radius: 3px;
    background: none;
    font: inherit;
    font-size: 11px;
    line-height: 1.5;
    text-align: left;
    color: var(--amber);
    overflow-wrap: anywhere;
    cursor: pointer;
  }

  .warning:hover {
    background: var(--bg-raised);
  }

  .inspector button:focus-visible {
    outline: 2px solid var(--border-focus);
    outline-offset: -2px;
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
