<script lang="ts">
  // The settings control chip (§4.3). The selected member of a group is the
  // one filled in; a destructive chip is outlined in the cut colour; a chip
  // with nothing behind it is drawn back and cannot be pressed.

  interface Props {
    label: string;
    /** The selected member of its group. */
    on?: boolean;
    danger?: boolean;
    disabled?: boolean;
    /** Renders as a value rather than a control when there is nothing to press. */
    onclick?: () => void;
    title?: string;
  }

  let { label, on = false, danger = false, disabled = false, onclick, title = "" }: Props = $props();
</script>

{#if onclick !== undefined && !disabled}
  <button type="button" class="ctl" class:on class:danger aria-pressed={on} {title} {onclick}>{label}</button>
{:else}
  <span class="ctl" class:on class:danger class:disabled title={title || undefined} aria-disabled={disabled}>
    {label}
  </span>
{/if}

<style>
  .ctl {
    display: inline-block;
    padding: 5px 10px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-family: inherit;
    font-size: 11px;
    font-weight: 400;
    color: var(--text-muted);
    white-space: nowrap;
    text-align: center;
  }

  button.ctl {
    cursor: pointer;
  }

  button.ctl:hover {
    border-color: var(--text-dim);
    color: var(--text);
  }

  button.ctl:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }

  .ctl.on {
    background: var(--keep);
    border-color: var(--keep);
    color: var(--on-accent);
    font-weight: 700;
  }

  .ctl.danger {
    background: var(--cut-wash-14);
    border-color: var(--cut);
    color: var(--cut);
    font-weight: 400;
  }

  .ctl.disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }
</style>
