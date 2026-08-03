<script lang="ts" generics="T extends string">
  // A row of chips where exactly one is the answer.

  import ControlChip from "./ControlChip.svelte";

  interface Option {
    value: T;
    label: string;
  }

  interface Props {
    value: T;
    options: Option[];
    onchange: (value: T) => void;
    /** True when the choice is drawn but not yet connected to anything. */
    disabled?: boolean;
    /** What the group of chips is called, for a screen reader. */
    label: string;
    title?: string;
  }

  let { value, options, onchange, disabled = false, label, title = "" }: Props = $props();
</script>

<div class="chips" role="group" aria-label={label}>
  {#each options as option (option.value)}
    <ControlChip
      label={option.label}
      on={option.value === value}
      {disabled}
      {title}
      onclick={disabled ? undefined : () => onchange(option.value)}
    />
  {/each}
</div>

<style>
  .chips {
    display: flex;
    align-items: center;
    gap: 7px;
  }
</style>
