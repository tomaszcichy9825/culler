<script lang="ts">
  // A self-contained histogram: give it a preview URL and a frame identity
  // and it loads, samples and draws. The image comes from the browser cache
  // when the grid has already shown the frame, so the cost is usually nil.

  import { cachedHistogram, sampleHistogram } from "../lib/histogram";

  interface Props {
    url: string;
    id: string;
    /** Bar area height in px. The design draws the inspector's at 56. */
    height?: number;
  }

  let { url, id, height = 44 }: Props = $props();

  let bins = $state<number[] | null>(null);

  $effect(() => {
    const want = id;
    const hit = cachedHistogram(want);
    if (hit !== undefined) {
      bins = hit;
      return;
    }
    bins = null;
    if (url === "") return;
    const img = new Image();
    img.decoding = "async";
    img.onload = () => {
      const shape = sampleHistogram(img, want);
      // The focus may have moved on while the image was decoding.
      if (want === id) bins = shape;
    };
    img.src = url;
    return () => {
      img.onload = null;
    };
  });
</script>

<div class="histogram" style:height="{height}px" data-bins={bins === null ? 0 : bins.length}>
  {#if bins === null}
    <span class="hempty">waiting for the preview</span>
  {:else}
    {#each bins as h, i (i)}
      <span class="hbar" style:height="{Math.max(h, 1)}%"></span>
    {/each}
  {/if}
</div>

<style>
  .histogram {
    display: flex;
    align-items: flex-end;
    gap: 1px;
  }

  .hbar {
    flex: 1;
    min-width: 0;
    background: var(--histogram-bar, var(--text-dim));
    border-radius: 1px 1px 0 0;
  }

  .hempty {
    align-self: center;
    width: 100%;
    text-align: center;
    font-size: 10.5px;
    color: var(--text-ghost, var(--text-dim));
  }
</style>
