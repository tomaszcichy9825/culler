<script lang="ts">
  // A volume's capacity as one bar: what is on it, and what this import is
  // about to add.
  //
  // The added segment is drawn beyond the used one rather than inside it, so
  // the bar answers the only question worth asking on this screen — will it
  // fit — by whether it reaches the end of the track. A volume that would not
  // answer its capacity gets no bar at all: a guessed one on a screen about
  // filling a disk would be the worst possible place to be wrong.

  import { formatBytes, percent } from "../../lib/import.svelte";

  interface Props {
    total: number;
    free: number;
    /** Bytes this import will add, zero when nothing lands here. */
    landing?: number;
    height?: number;
    /** Whether the landing bytes still fit, which colours that segment. */
    fits?: boolean;
    legend?: boolean;
  }

  let { total, free, landing = 0, height = 8, fits = true, legend = false }: Props = $props();

  let known = $derived(total > 0);
  let used = $derived(Math.max(0, total - free));
  let usedShare = $derived(percent(used, total));
  // The added segment is clamped against what is left, so an import too big
  // for the volume fills the remainder rather than overflowing the track — the
  // amber or red of it is the warning, not a bar drawn outside its own box.
  let landingShare = $derived(percent(Math.min(landing, Math.max(free, 0)), total));
</script>

{#if known}
  <div class="wrap">
    <div
      class="track"
      style:height="{height}px"
      role="img"
      aria-label="{formatBytes(used)} used of {formatBytes(total)}"
    >
      <span class="seg used" style:width="{usedShare}%"></span>
      {#if landing > 0}
        <span class="seg landing" class:tight={!fits} style:width="{landingShare}%"></span>
      {/if}
    </div>
    {#if legend}
      <div class="legend">
        <span class="used-text">{formatBytes(used)} used</span>
        <span class="dot">·</span>
        <span>{formatBytes(free)} free</span>
        {#if landing > 0}
          <span class="spacer"></span>
          <span class="landing-text" class:tight={!fits}>+{formatBytes(landing)}</span>
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .wrap {
    min-width: 0;
  }

  .track {
    display: flex;
    width: 100%;
    border-radius: 3px;
    background: var(--bg-track);
    overflow: hidden;
  }

  .seg {
    display: block;
    height: 100%;
  }

  .used {
    background: var(--neutral-bar);
  }

  .landing {
    background: var(--accent);
  }

  .landing.tight {
    background: var(--cut);
  }

  .legend {
    display: flex;
    align-items: baseline;
    gap: 5px;
    padding-top: 4px;
    font-size: 10px;
    color: var(--text-dim);
  }

  .used-text {
    font-family: var(--font-mono);
  }

  .dot {
    color: var(--text-ghost);
  }

  .spacer {
    flex: 1;
  }

  .landing-text {
    font-family: var(--font-mono);
    color: var(--accent);
  }

  .landing-text.tight {
    color: var(--cut);
  }
</style>
