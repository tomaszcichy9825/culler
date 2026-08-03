<script module lang="ts">
  // Screen 10a: two near-identical frames side by side, one survives.
  //
  // Compare is the answer to the one question the contact sheet cannot settle
  // — which of these two is the keeper — so the screen is built around holding
  // both frames still while the eye moves between them. The panes share one
  // zoom and, until the lock is lifted, one pan: two photographs of the same
  // thing are only comparable while they are showing the same part of it.
  //
  // The screen decides nothing itself. Which frames are being compared comes
  // in as a prop, and every verdict goes back out through a callback, so the
  // store stays the single source of truth for what is pending — the same
  // arrangement the loupe screens use.

  import type { GroupDTO } from "../lib/bindings";
  import { groupKey } from "../lib/state.svelte";
  import { shotLabel } from "../lib/verdict";

  /** Which pane is being talked about. Always two, always in this order. */
  export type Side = "a" | "b";
  export const SIDES: Side[] = ["a", "b"];

  /** The title bar's right-hand text, per the drawn screen. */
  export interface CompareTitle {
    /** `1:1` or `fit`. */
    zoom: string;
    /** `panning locked` or `panning free`; the locked state is drawn in --brand. */
    lock: string;
    locked: boolean;
  }

  /**
   * The keyboard's end of the compare screen, registered by the instance the
   * way the table and the loupe register theirs. Only one compare view is ever
   * mounted; a second instance would take the registration over.
   */
  export interface CompareApi {
    /** ⇥ — hand the keys to the other pane. */
    switchSide(): void;
    /** w — the active side wins: it keeps, everything else being compared is cut. */
    wins(): void;
    /** k / x — a verdict on the active side alone. */
    verdict(v: "keep" | "cut"): void;
    /** l — pan the two panes together, or each on its own. */
    togglePanLock(): void;
    /** The zoom is shared, so it is one call for both panes. */
    toggleZoom(): void;
    /** Arrow-key panning, in screen pixels, honouring the lock. */
    pan(dx: number, dy: number): void;
    /** c / esc — leave compare. Runs the host's `onexit`. */
    exit(): void;
    /** Which pane holds the keys. */
    side(): Side;
    /** The two frames in the panes, A first. */
    frames(): GroupDTO[];
    /** What the title bar's right-hand side reads. */
    title(): CompareTitle;
    /** What the title bar's left-hand side reads. */
    summary(): string;
  }

  export function compareTitle(zoomed: boolean, locked: boolean): CompareTitle {
    return { zoom: zoomed ? "1:1" : "fit", lock: locked ? "panning locked" : "panning free", locked };
  }

  /** The drawn line: `comparing 2 of 4 selected`. */
  export function compareSummary(setSize: number): string {
    return `comparing ${Math.min(SIDES.length, setSize)} of ${setSize} selected`;
  }

  /** The drawn status-bar chip: `COMPARE · side A`. */
  export function compareChip(side: Side): string {
    return `COMPARE · side ${side.toUpperCase()}`;
  }

  /** The status bar's key hints. `l` reads as the state it would move to. */
  export function compareKeys(locked: boolean): { key: string; hint: string }[] {
    return [
      { key: "⇥", hint: "switch side" },
      { key: "w", hint: "this one wins, cut the rest" },
      { key: "k/x", hint: "verdict on this side" },
      { key: "l", hint: locked ? "unlock panning" : "lock panning" },
      { key: "c", hint: "back to grid" },
    ];
  }

  /**
   * compareSet is the entry rule: a selection of two or more is the comparison,
   * and with nothing selected it is the focused frame and the one after it.
   * An empty array means there is nothing to compare and the host should say so
   * rather than opening an empty screen.
   */
  export function compareSet(groups: GroupDTO[], selected: GroupDTO[], focusIndex: number): GroupDTO[] {
    if (selected.length >= SIDES.length) return selected;
    const first = groups[focusIndex];
    const next = groups[focusIndex + 1] ?? groups[focusIndex - 1];
    if (!first || !next) return [];
    return [first, next];
  }

  /**
   * railNote names what is being compared, from the capture times alone: a run
   * of frames inside a minute is a burst, anything wider is written as its
   * span, and frames with no readable timestamp get no note at all rather than
   * an invented one.
   */
  export function railNote(groups: GroupDTO[]): string {
    const times: number[] = [];
    for (const g of groups) {
      const t = Date.parse(g.shot);
      if (Number.isNaN(t)) return "";
      times.push(t);
    }
    if (times.length === 0) return "";

    const first = Math.min(...times);
    const last = Math.max(...times);
    const clock = (t: number) => shotLabel(new Date(t).toISOString()).slice(11);
    if (last - first <= 60_000) return `burst ${clock(first).slice(0, 5)}`;
    return `${clock(first)} → ${clock(last)}`;
  }

  /** The 1:1 crop inset, as the design sizes it. */
  const CROP = 150;

  /**
   * cropInset places the 1:1 detail. The middle of the inset holds whatever
   * natural pixel is in the middle of the pane: with the pan at rest that is
   * the centre of the frame, and a pan of one displayed pixel moves it by `fit`
   * natural pixels, `fit` being how far the preview was shrunk to fit the pane.
   */
  export function cropInset(
    size: { w: number; h: number },
    pan: { x: number; y: number },
    fit: number,
  ): { size: string; position: string } {
    const cx = size.w / 2 - pan.x * fit;
    const cy = size.h / 2 - pan.y * fit;
    return {
      size: `${size.w}px ${size.h}px`,
      position: `${CROP / 2 - cx}px ${CROP / 2 - cy}px`,
    };
  }

  const IDLE: CompareApi = {
    switchSide: () => {},
    wins: () => {},
    verdict: () => {},
    togglePanLock: () => {},
    toggleZoom: () => {},
    pan: () => {},
    exit: () => {},
    side: () => "a",
    frames: () => [],
    title: () => compareTitle(false, true),
    summary: () => compareSummary(0),
  };

  export const compare: CompareApi = { ...IDLE };
</script>

<script lang="ts">
  import CompareMetrics from "./CompareMetrics.svelte";
  import type { Dimensions, MetricInput } from "./CompareMetrics.svelte";
  import { metricRows } from "./CompareMetrics.svelte";
  import type { Bytes } from "../lib/frame";
  import { shotLine, verdictBadge } from "../lib/frame";
  import { queuedImage } from "../lib/imageQueue";
  import { previewURL } from "../lib/preview";
  import { app } from "../lib/state.svelte";
  import type { CutScope } from "../lib/verdict";

  interface Props {
    /** The frames being compared, in rail order. Two or more; see `compareSet`. */
    groups: GroupDTO[];
    /**
     * Asks for a verdict on named frames. Compare never writes: `w` arrives
     * here as a keep on the winner followed by a cut on the rest.
     */
    onverdict: (frames: GroupDTO[], verdict: "keep" | "cut") => void;
    /** Asks to leave compare. Bound to `c` and to esc by the host. */
    onexit: () => void;
    /** A frame was loaded into a pane, as an index into `groups`. */
    onfocus?: (index: number) => void;
    /** Sizes on disk by group key, if the caller has them. The DTO carries none. */
    bytes?: Record<string, Bytes>;
    /** How far a cut reaches. Defaults to the configured behaviour. */
    cutRemoves?: CutScope;
  }

  let { groups, onverdict, onexit, onfocus, bytes, cutRemoves }: Props = $props();

  let cut = $derived(cutRemoves ?? app.cutRemoves);

  /** Which member of the set each pane is showing. */
  let pair = $state<Record<Side, number>>({ a: 0, b: 1 });
  let active = $state<Side>("a");
  let locked = $state(true);
  let zoom = $state(false);

  /**
   * One pan per pane. While the lock is on, both are written together — which
   * is what makes the two views comparable — and lifting it lets each pane be
   * moved to the part of its own frame worth looking at.
   */
  let pans = $state<Record<Side, { x: number; y: number }>>({ a: { x: 0, y: 0 }, b: { x: 0, y: 0 } });

  /** Natural preview size by group key, learnt when each image decodes. */
  let natural = $state<Record<string, Dimensions>>({});
  /** Natural pixels per displayed pixel, per pane: what 1:1 would cost. */
  let fits = $state<Record<Side, number>>({ a: 1, b: 1 });
  /** What the image is actually drawn at, which is 1 until the zoom is on. */
  let scales = $state<Record<Side, number>>({ a: 1, b: 1 });

  let imgs = $state<Record<Side, HTMLImageElement | null>>({ a: null, b: null });
  let stages = $state<Record<Side, HTMLDivElement | null>>({ a: null, b: null });
  let panes = $state<HTMLDivElement | null>(null);
  let dragging = $state<Side | null>(null);
  let dragX = 0;
  let dragY = 0;

  // A set can arrive shorter than the panes it was filling — the host may hand
  // over a new selection without remounting — so the pair is kept inside it.
  $effect(() => {
    const n = groups.length;
    if (n === 0) return;
    if (pair.a >= n || pair.b >= n || pair.a === pair.b) {
      pair = { a: 0, b: Math.min(1, n - 1) };
    }
  });

  function frameOf(side: Side): GroupDTO | null {
    return groups[pair[side]] ?? null;
  }

  let a = $derived(frameOf("a"));
  let b = $derived(frameOf("b"));

  function input(g: GroupDTO): MetricInput {
    const key = groupKey(g);
    return { group: g, size: natural[key], bytes: bytes?.[key] };
  }

  let metrics = $derived(a && b ? metricRows(input(a), input(b)) : { a: [], b: [] });

  /**
   * measure works out what 1:1 would cost each pane. The two frames may fit
   * their panes at different scales — a portrait beside a landscape — so the
   * zoom is one decision but the factor behind it is per pane.
   */
  function measure() {
    for (const side of SIDES) {
      const el = imgs[side];
      if (!el || el.clientWidth === 0 || el.naturalWidth === 0) {
        fits[side] = 1;
        scales[side] = 1;
        continue;
      }
      fits[side] = el.naturalWidth / el.clientWidth;
      scales[side] = zoom ? Math.max(1, fits[side]) : 1;
    }
  }

  function remember(side: Side) {
    const el = imgs[side];
    const g = frameOf(side);
    if (el && g && el.naturalWidth > 0) {
      natural = { ...natural, [groupKey(g)]: { w: el.naturalWidth, h: el.naturalHeight } };
    }
    measure();
  }

  $effect(() => {
    // Re-measure whenever the zoom is toggled or either frame changes.
    zoom;
    pair.a;
    pair.b;
    measure();
  });

  // The panes resize with the window, and a stale fit would put the crop inset
  // on the wrong part of the photograph.
  $effect(() => {
    const el = panes;
    if (!el || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      measure();
      clamp();
    });
    observer.observe(el);
    return () => observer.disconnect();
  });

  /** clamp keeps each pan inside the part of its image that is off stage. */
  function clamp() {
    for (const side of SIDES) {
      const el = imgs[side];
      const stage = stages[side];
      const scale = scales[side];
      if (!el || !stage || scale <= 1) {
        pans[side] = { x: 0, y: 0 };
        continue;
      }
      const limitX = Math.max(0, (el.clientWidth * scale - stage.clientWidth) / 2 / scale);
      const limitY = Math.max(0, (el.clientHeight * scale - stage.clientHeight) / 2 / scale);
      const pan = pans[side];
      pans[side] = {
        x: Math.max(-limitX, Math.min(limitX, pan.x)),
        y: Math.max(-limitY, Math.min(limitY, pan.y)),
      };
    }
  }

  /**
   * nudge moves the pan. With the lock on it moves both panes by the same
   * amount in image pixels rather than the same number of screen pixels, so
   * two frames at different scales stay over the same detail.
   */
  function nudge(side: Side, dx: number, dy: number) {
    const sides = locked ? SIDES : [side];
    for (const s of sides) {
      const scale = scales[s] || 1;
      pans[s] = { x: pans[s].x + dx / scale, y: pans[s].y + dy / scale };
    }
    clamp();
  }

  function onPointerDown(side: Side, e: PointerEvent) {
    if (!zoom) return;
    dragging = side;
    dragX = e.clientX;
    dragY = e.clientY;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }

  function onPointerMove(side: Side, e: PointerEvent) {
    if (dragging !== side) return;
    nudge(side, e.clientX - dragX, e.clientY - dragY);
    dragX = e.clientX;
    dragY = e.clientY;
  }

  function onPointerUp(side: Side, e: PointerEvent) {
    if (dragging !== side) return;
    dragging = null;
    (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
  }

  /**
   * load puts a frame from the rail into the pane holding the keys. A frame
   * already in the other pane swaps rather than appearing twice, because two
   * identical panes are the one arrangement compare has nothing to say about.
   */
  function load(index: number) {
    const other: Side = active === "a" ? "b" : "a";
    if (pair[other] === index) pair = { ...pair, [other]: pair[active], [active]: index };
    else pair = { ...pair, [active]: index };
    onfocus?.(index);
  }

  function inPane(index: number): Side | null {
    if (pair.a === index) return "a";
    if (pair.b === index) return "b";
    return null;
  }

  /** The 1:1 inset, centred on whatever the middle of the pane is showing. */
  function cropStyle(side: Side): string {
    const g = frameOf(side);
    if (!g) return "";
    const size = natural[groupKey(g)];
    const url = previewURL(g);
    if (!size || url === "") return "";
    const inset = cropInset(size, pans[side], fits[side] || 1);
    return (
      `background-image:url("${url}");` +
      `background-size:${inset.size};` +
      `background-position:${inset.position}`
    );
  }

  function hasCrop(side: Side): boolean {
    const g = frameOf(side);
    return g !== null && natural[groupKey(g)] !== undefined && previewURL(g) !== "";
  }

  function cropNote(side: Side): string {
    const moved = pans[side].x !== 0 || pans[side].y !== 0;
    return `preview 1:1 · ${moved ? "panned" : "centre"}`;
  }

  // The keyboard's end of the screen. Same registration as TableView's `table`:
  // the host imports `compare` and calls these from its own key layer, so the
  // bindings stay in the keymap and nothing here decides which key does what.
  compare.switchSide = () => {
    active = active === "a" ? "b" : "a";
  };

  compare.wins = () => {
    const winner = frameOf(active);
    if (!winner) return;
    const index = pair[active];
    const losers = groups.filter((_, i) => i !== index);
    onverdict([winner], "keep");
    if (losers.length > 0) onverdict(losers, "cut");
  };

  compare.verdict = (v: "keep" | "cut") => {
    const g = frameOf(active);
    if (g) onverdict([g], v);
  };

  compare.togglePanLock = () => {
    locked = !locked;
    // Locking again brings the panes back together rather than leaving them
    // half a frame apart with a title bar claiming they are in step.
    if (locked) pans = { a: pans[active], b: pans[active] };
  };

  compare.toggleZoom = () => {
    zoom = !zoom;
    if (!zoom) pans = { a: { x: 0, y: 0 }, b: { x: 0, y: 0 } };
    measure();
    clamp();
  };

  compare.pan = (dx: number, dy: number) => {
    if (!zoom) return;
    nudge(active, dx, dy);
  };

  compare.exit = () => onexit();
  compare.side = () => active;
  compare.frames = () => SIDES.map(frameOf).filter((g): g is GroupDTO => g !== null);
  compare.title = () => compareTitle(zoom, locked);
  compare.summary = () => compareSummary(groups.length);

  let note = $derived(railNote(groups));
</script>

<div class="compare">
  <div class="panes" bind:this={panes}>
    {#each SIDES as side (side)}
      {@const g = side === "a" ? a : b}
      {@const tag = side.toUpperCase()}
      <section class="pane" class:active={side === active} aria-label="side {tag}">
        <header class="phead">
          <span class="tag">{tag}</span>
          {#if g}
            {@const badge = verdictBadge(g, cut)}
            <span class="stem" title={g.stem}>{g.stem}</span>
            <span class="when">{shotLine(g)}</span>
            <span class="pill {badge.tone}">{badge.label}</span>
          {:else}
            <span class="stem empty">no frame</span>
            <span class="when"></span>
          {/if}
        </header>

        <div
          class="stage"
          class:zoomed={zoom}
          class:dragging={dragging === side}
          bind:this={stages[side]}
          role="img"
          aria-label={g ? g.stem : "no frame"}
          onpointerdown={(e) => onPointerDown(side, e)}
          onpointermove={(e) => onPointerMove(side, e)}
          onpointerup={(e) => onPointerUp(side, e)}
          onpointercancel={(e) => onPointerUp(side, e)}
        >
          {#if g}
            {@const url = previewURL(g)}
            {#if url !== ""}
              <img
                class="shot"
                class:ringed={side === active}
                bind:this={imgs[side]}
                src={url}
                alt={g.stem}
                onload={() => remember(side)}
                style:transform="scale({scales[side]}) translate({pans[side].x}px, {pans[side].y}px)"
              />
            {:else}
              <span class="missing">no preview for {g.stem}</span>
            {/if}

            {#if hasCrop(side)}
              <div class="inset" style={cropStyle(side)} aria-hidden="true"></div>
              <span class="chip">{cropNote(side)}</span>
            {/if}
          {:else}
            <span class="missing">nothing on this side</span>
          {/if}
        </div>

        <CompareMetrics rows={side === "a" ? metrics.a : metrics.b} />
      </section>
    {/each}
  </div>

  <div class="rail">
    <div class="rlabel">
      <span class="rtitle">Comparing</span>
      <span class="rnote">{groups.length} frames{note === "" ? "" : ` · ${note}`}</span>
    </div>
    <div class="rframes" role="listbox" aria-label="Frames being compared">
      {#each groups as g, i (groupKey(g))}
        {@const url = previewURL(g)}
        {@const held = inPane(i)}
        <button
          type="button"
          class="rframe"
          role="option"
          aria-selected={held !== null}
          tabindex="-1"
          onclick={() => load(i)}
        >
          <span class="rthumb" class:held={held !== null}>
            {#if url !== ""}
              <img use:queuedImage={url} alt={g.stem} decoding="async" />
            {/if}
            {#if held !== null}
              <span class="rtag" class:on={held === active}>{held.toUpperCase()}</span>
            {/if}
          </span>
          <span class="rstem" class:held={held !== null} title={g.stem}>{g.stem}</span>
        </button>
      {/each}
    </div>
  </div>
</div>

<style>
  .compare {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-app);
  }

  .panes {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  .pane {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .pane + .pane {
    border-left: 1px solid var(--border);
  }

  .phead {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 34px;
    padding: 0 14px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
  }

  .pane.active .phead {
    background: var(--accent-wash-10);
  }

  .tag {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 19px;
    height: 19px;
    border-radius: 4px;
    background: var(--bg-field);
    font-size: 11px;
    font-weight: 700;
    color: var(--text-muted);
  }

  /* The pane holding the keys is the one whose tag is filled: everything the
     status bar says about "this side" is about that pane. */
  .pane.active .tag {
    background: var(--accent);
    color: var(--on-accent);
  }

  .stem {
    flex: 0 0 auto;
    font-size: 12.5px;
    color: var(--text);
    white-space: nowrap;
  }

  .stem.empty {
    color: var(--text-dim);
  }

  .when {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .pill {
    flex: 0 0 auto;
    padding: 2px 7px;
    border-radius: 4px;
    background: var(--bg-field);
    font-size: 10.5px;
    font-weight: 700;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .pill.keep {
    background: var(--keep-wash-16);
    color: var(--keep-text);
  }

  .pill.cut {
    background: var(--cut-wash-16);
    color: var(--cut-text);
  }

  .stage {
    flex: 1;
    min-height: 0;
    position: relative;
    display: grid;
    place-items: center;
    padding: 16px;
    overflow: hidden;
    background: var(--bg-app);
  }

  .stage.zoomed {
    cursor: grab;
  }

  .stage.dragging {
    cursor: grabbing;
  }

  .shot {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    display: block;
    border-radius: 3px;
    transform-origin: center center;
  }

  .shot.ringed {
    box-shadow: var(--focus-ring);
  }

  .missing {
    font-size: 13px;
    color: var(--text-dim);
    max-width: 90%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* One image pixel per screen pixel, on the middle of whatever the pane is
     showing. The same preview URL as the pane itself, so it costs a cache hit
     rather than a second read of the card. */
  .inset {
    position: absolute;
    bottom: 26px;
    right: 26px;
    width: 150px;
    height: 150px;
    border-radius: 3px;
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow-float);
    background-color: var(--bg-thumb);
    background-repeat: no-repeat;
    pointer-events: none;
  }

  .chip {
    position: absolute;
    bottom: 26px;
    left: 26px;
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--glass);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
    pointer-events: none;
  }

  .rail {
    flex: 0 0 78px;
    display: flex;
    align-items: center;
    background: var(--bg-window);
    border-top: 1px solid var(--border);
    overflow: hidden;
  }

  .rlabel {
    flex: 0 0 auto;
    height: 100%;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 2px;
    padding: 0 12px;
    background: var(--bg-chrome);
    border-right: 1px solid var(--border);
    white-space: nowrap;
  }

  .rtitle {
    font-size: 10px;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .rnote {
    font-size: 11px;
    color: var(--accent);
  }

  .rframes {
    flex: 1;
    min-width: 0;
    height: 100%;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 0 12px;
    overflow-x: auto;
    overflow-y: hidden;
  }

  .rframe {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 0;
    margin: 0;
    border: 0;
    background: none;
    font: inherit;
    color: inherit;
    text-align: left;
    cursor: pointer;
    appearance: none;
    outline: none;
  }

  .rthumb {
    position: relative;
    display: block;
    width: 82px;
    height: 54px;
    border-radius: 3px;
    overflow: hidden;
    background: var(--bg-thumb);
    border: 1px solid var(--border-strong);
  }

  .rthumb.held {
    border-color: var(--border-focus);
    box-shadow: var(--focus-ring);
  }

  .rthumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .rtag {
    position: absolute;
    top: 2px;
    left: 3px;
    padding: 1px 4px;
    border-radius: 2px;
    background: var(--bg-field);
    font-size: 9px;
    font-weight: 700;
    line-height: 1.3;
    color: var(--text-muted);
  }

  .rtag.on {
    background: var(--accent);
    color: var(--on-accent);
  }

  .rstem {
    max-width: 82px;
    font-size: 9.5px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .rstem.held {
    color: var(--text-2);
  }
</style>
