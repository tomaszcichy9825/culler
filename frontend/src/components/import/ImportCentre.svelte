<script lang="ts">
  // IMPORT's centre pane: three screens over one card, chosen by the mode's
  // sub-layout.
  //
  // review (⌥1) is what is on the card, route (⌥2) is where the review in CULL
  // sent it, verify (⌥3) is the execution. They are the same three questions an
  // import always asks, in the order it asks them, and Tab walks between them.

  import ReviewPanel from "./ReviewPanel.svelte";
  import RoutePanel from "./RoutePanel.svelte";
  import VerifyPanel from "./VerifyPanel.svelte";

  interface Props {
    /** The mode's sub-layout: 0 review, 1 route, 2 verify. */
    layout: number;
    /** What "review in cull" does. The shell passes its own openFolder. */
    onreview?: (dir: string) => void;
  }

  let { layout, onreview }: Props = $props();
</script>

<div class="centre" data-layout={layout}>
  {#if layout === 1}
    <RoutePanel {onreview} />
  {:else if layout === 2}
    <VerifyPanel />
  {:else}
    <ReviewPanel {onreview} />
  {/if}
</div>

<style>
  .centre {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
</style>
