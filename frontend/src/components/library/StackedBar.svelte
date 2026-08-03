<script module lang="ts">
  // The design's stacked bar (§4.10): a track with percentage-width segments
  // and, when asked for, a wrapped legend beneath. Plain divs — no canvas, no
  // chart library — because it is a row of coloured boxes.

  export interface Segment {
    /** What the segment counts, as the legend prints it. */
    label: string;
    value: number;
    /** Any CSS colour; the callers pass design tokens. */
    colour: string;
    /** The legend's right-hand value, already formatted. */
    note?: string;
  }
</script>

<script lang="ts">
  import { percent } from "../../lib/library.svelte";

  interface Props {
    segments: Segment[];
    /** Track height in pixels: 5 for a meter, 7 for a row, 12 for a volume. */
    height?: number;
    legend?: boolean;
    /**
     * The denominator. Defaults to the sum of the segments, which is right for
     * a share of the catalogue; pass a capacity to draw the empty remainder.
     */
    total?: number;
  }

  let { segments, height = 7, legend = false, total }: Props = $props();

  let sum = $derived(segments.reduce((n, s) => n + s.value, 0));
  let denominator = $derived(total !== undefined && total > 0 ? total : sum);
  /** A segment of nothing is not drawn: a zero-width sliver reads as a bug. */
  let drawn = $derived(segments.filter((s) => s.value > 0));
</script>

<div class="bar" style:height="{height}px" style:border-radius="{Math.min(3, height / 2)}px">
  {#each drawn as segment (segment.label)}
    <span
      class="seg"
      style:width="{percent(segment.value, denominator)}%"
      style:background={segment.colour}
      title="{segment.label}: {segment.note ?? segment.value}"
    ></span>
  {/each}
</div>

{#if legend}
  <div class="legend">
    {#each segments as segment (segment.label)}
      <span class="item">
        <span class="swatch" style:background={segment.colour}></span>
        <span class="key">{segment.label}</span>
        <span class="value">{segment.note ?? segment.value}</span>
      </span>
    {/each}
  </div>
{/if}

<style>
  .bar {
    display: flex;
    width: 100%;
    overflow: hidden;
    background: var(--bg-track);
  }

  .seg {
    display: block;
    height: 100%;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    padding-top: 7px;
  }

  .item {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 10.5px;
  }

  .swatch {
    width: 7px;
    height: 7px;
    border-radius: 2px;
  }

  .key {
    color: var(--text-muted);
  }

  .value {
    color: var(--text);
  }
</style>
