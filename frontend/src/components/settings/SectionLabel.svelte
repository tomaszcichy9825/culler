<script lang="ts">
  // The most repeated component in the design: an uppercase title, a hairline
  // filling the rest of the row, and an optional right-aligned hint.

  interface Props {
    title: string;
    /** Destructive groups carry their title in the cut colour. */
    danger?: boolean;
    hint?: string;
  }

  let { title, danger = false, hint = "" }: Props = $props();
</script>

<div class="label">
  <span class="title" class:danger>{title}</span>
  <span class="rule"></span>
  {#if hint !== ""}<span class="hint">{hint}</span>{/if}
</div>

<style>
  .label {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-bottom: 8px;
  }

  .title {
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .title.danger {
    color: var(--cut);
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .hint {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--text-ghost);
    white-space: nowrap;
  }
</style>
