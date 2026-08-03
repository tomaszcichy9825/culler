<script lang="ts">
  // Marks a location that lives on a network volume. Worth flagging because
  // every operation on one is slower and the volume can vanish mid-batch.

  let { compact = false }: { compact?: boolean } = $props();

  const warning = "on a network volume — scanning and applying may be slow, and the volume can disconnect";
</script>

<span class="chip" class:compact title={warning}>
  <span class="dot" aria-hidden="true"></span>
  {#if !compact}<span class="text">network</span>{/if}
</span>

<style>
  /* The title-bar status chip: a dot in the text's own hue on a wash of it. */
  .chip {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--amber-wash-16);
    color: var(--amber);
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.02em;
    white-space: nowrap;
  }

  .chip.compact {
    padding: 2px;
    background: none;
  }

  .dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
