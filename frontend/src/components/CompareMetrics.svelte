<script module lang="ts">
  // The metric rows down the bottom of a compare pane.
  //
  // The design draws five rows of key · value · meter · delta, and the screen
  // it was drawn for was comparing sharpness. Nothing in this application
  // measures sharpness, so the rows carry what the scan and the loaded preview
  // genuinely know — the rating, the pixel dimensions, what is on disk, which
  // halves exist, and when the shutter went — and the block says so in its own
  // label rather than dressing four real numbers up as a quality score.
  //
  // A metric that has no better and worse (a file size, a timestamp) says so:
  // its meter is the neutral bar and its delta is written in the dim tier. Only
  // a metric with a direction gets the keep and cut hues, because those two
  // colours mean "this one survives" everywhere else in the application.

  import type { GroupDTO } from "../lib/bindings";
  import type { Bytes } from "../lib/frame";
  import { formatBytes } from "../lib/preview";
  import { kindLabel, MAX_RATING, shotLabel } from "../lib/verdict";

  /** Natural size of a frame's preview, once the browser has decoded it. */
  export interface Dimensions {
    w: number;
    h: number;
  }

  /** One frame, plus whatever the screen has managed to learn about it. */
  export interface MetricInput {
    group: GroupDTO;
    /** Natural size of the preview. Absent until the image has loaded. */
    size?: Dimensions;
    /** Bytes on disk. Absent unless a caller supplied them: the DTO has none. */
    bytes?: Bytes;
  }

  /** A metric as one pane draws it. */
  export interface MetricCell {
    key: string;
    value: string;
    /** Meter fill, 0–1, or null when the value has no scale to fill. */
    ratio: number | null;
    /** How this pane did on this row. `none` for a metric with no direction. */
    tone: "win" | "lose" | "none";
    delta: string;
  }

  /** A value the app does not have. Never a zero, never a guess. */
  const NO_DATA = "—";

  /** Both frames have it and it is the same on each. */
  const LEVEL = "same";

  interface Measure {
    key: string;
    /** Whether a bigger number is a better frame, or neither is. */
    direction: "higher" | "none";
    /** The comparable number, or null when this frame has no such fact. */
    amount: (m: MetricInput) => number | null;
    /** What the row reads for this frame. */
    text: (m: MetricInput) => string;
    /** How a difference between the two is written. */
    delta: (diff: number) => string;
    /** False for a metric with no scale, whose meter stays empty. */
    meter?: false;
  }

  function signed(diff: number, format: (n: number) => string): string {
    return `${diff > 0 ? "+" : "−"}${format(Math.abs(diff))}`;
  }

  /** The clock the frame was shot at, or "" when none was recorded. */
  function clockOf(g: GroupDTO): string {
    const stamp = shotLabel(g.shot);
    return stamp === "unknown" ? "" : stamp.slice(11);
  }

  function halves(g: GroupDTO): number {
    return (g.hasRaw ? 1 : 0) + (g.hasJpeg ? 1 : 0);
  }

  function megapixels(size: Dimensions | undefined): number | null {
    if (!size || size.w === 0 || size.h === 0) return null;
    return (size.w * size.h) / 1_000_000;
  }

  function onDisk(bytes: Bytes | undefined): number | null {
    if (!bytes) return null;
    const total = (bytes.raw ?? 0) + (bytes.jpeg ?? 0);
    return total === 0 ? null : total;
  }

  /** A gap between two capture times, written the way a burst reads. */
  function duration(ms: number): string {
    const seconds = Math.round(ms / 1000);
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
    return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
  }

  function shotAt(g: GroupDTO): number | null {
    const t = Date.parse(g.shot);
    return Number.isNaN(t) ? null : t;
  }

  /**
   * The rows, in the order they are drawn. Only the first two have a direction:
   * a photographer's own rating and the number of pixels are the two facts here
   * where more is plainly better. A larger file and an earlier shutter are
   * differences, not merits, so they are shown without taking a side.
   */
  const MEASURES: Measure[] = [
    {
      key: "rating",
      direction: "higher",
      amount: (m) => m.group.rating,
      text: (m) => (m.group.rating > 0 ? `${m.group.rating} of ${MAX_RATING}` : "unrated"),
      delta: (diff) => signed(diff, (n) => String(n)),
    },
    {
      key: "pixels",
      direction: "higher",
      amount: (m) => megapixels(m.size),
      text: (m) => (m.size ? `${m.size.w} × ${m.size.h}` : NO_DATA),
      delta: (diff) => signed(diff, (n) => `${n.toFixed(1)} MP`),
    },
    {
      key: "on disk",
      direction: "none",
      amount: (m) => onDisk(m.bytes),
      text: (m) => {
        const total = onDisk(m.bytes);
        return total === null ? NO_DATA : formatBytes(total);
      },
      delta: (diff) => signed(diff, formatBytes),
    },
    {
      key: "files",
      direction: "none",
      amount: (m) => halves(m.group),
      text: (m) => kindLabel(m.group.kind),
      delta: (diff) => signed(diff, (n) => `${n} file`),
    },
    {
      key: "shot",
      direction: "none",
      meter: false,
      amount: (m) => shotAt(m.group),
      text: (m) => clockOf(m.group) || "not recorded",
      delta: (diff) => signed(diff, duration),
    },
  ];

  function cell(measure: Measure, mine: number | null, theirs: number | null, m: MetricInput): MetricCell {
    const known = mine !== null && theirs !== null;
    const diff = known ? mine - theirs : 0;
    const top = Math.max(mine ?? 0, theirs ?? 0);

    let tone: MetricCell["tone"] = "none";
    if (measure.direction === "higher" && known && diff !== 0) tone = diff > 0 ? "win" : "lose";

    let delta = NO_DATA;
    if (known) delta = diff === 0 ? LEVEL : measure.delta(diff);

    let ratio: number | null = null;
    if (measure.meter !== false && mine !== null) ratio = top > 0 ? mine / top : 0;

    return { key: measure.key, value: measure.text(m), ratio, tone, delta };
  }

  /**
   * metricRows is pure, and computes both panes at once because every row is a
   * comparison: neither side can say whether it won without the other's number.
   */
  export function metricRows(a: MetricInput, b: MetricInput): { a: MetricCell[]; b: MetricCell[] } {
    const rows: { a: MetricCell[]; b: MetricCell[] } = { a: [], b: [] };
    for (const measure of MEASURES) {
      const na = measure.amount(a);
      const nb = measure.amount(b);
      rows.a.push(cell(measure, na, nb, a));
      rows.b.push(cell(measure, nb, na, b));
    }
    return rows;
  }
</script>

<script lang="ts">
  interface Props {
    /** One pane's rows, from `metricRows`. */
    rows: MetricCell[];
    /** The block's own label, which says where these numbers came from. */
    label?: string;
    note?: string;
  }

  let { rows, label = "Measured", note = "what the scan knows" }: Props = $props();
</script>

<div class="metrics">
  <div class="head">
    <span class="label">{label}</span>
    <span class="rule"></span>
    <span class="note">{note}</span>
  </div>

  {#each rows as row (row.key)}
    <div class="metric {row.tone}">
      <span class="mkey">{row.key}</span>
      <span class="mval">{row.value}</span>
      <span class="meter">
        {#if row.ratio !== null}
          <span class="fill" style:width="{Math.round(row.ratio * 100)}%"></span>
        {/if}
      </span>
      <span class="delta">{row.delta}</span>
    </div>
  {/each}
</div>

<style>
  .metrics {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    background: var(--bg-window);
    border-top: 1px solid var(--border);
  }

  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 26px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border-hair);
  }

  .label {
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

  .note {
    font-size: 10px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .metric {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 26px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border-hair);
  }

  .mkey {
    flex: 0 0 96px;
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .mval {
    flex: 0 0 auto;
    font-size: 11.5px;
    color: var(--text);
    white-space: nowrap;
  }

  .metric.win .mval {
    color: var(--keep);
  }

  .meter {
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
    /* Neutral by default: a bar is only a verdict hue where one frame is
       genuinely better than the other on that row. */
    background: var(--neutral-bar);
  }

  .metric.win .fill {
    background: var(--keep);
  }

  .delta {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .metric.win .delta {
    color: var(--keep);
  }

  .metric.lose .delta {
    color: var(--cut);
  }
</style>
