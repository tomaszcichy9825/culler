<script lang="ts">
  // The inset value box (§4.5) with something typed into it.
  //
  // Values are reported as they are typed rather than on blur, so the rules
  // are checked against what is on screen: a field the backend would refuse is
  // marked before the write is attempted, not after.

  interface Props {
    value: string | number;
    label: string;
    type?: "text" | "number";
    min?: number;
    /** Width of the box in characters. */
    size?: number;
    suffix?: string;
    invalid?: boolean;
    disabled?: boolean;
    placeholder?: string;
    oninput: (value: string) => void;
  }

  let {
    value,
    label,
    type = "text",
    min,
    size = 10,
    suffix = "",
    invalid = false,
    disabled = false,
    placeholder = "",
    oninput,
  }: Props = $props();
</script>

<div class="field" class:invalid class:disabled>
  <input
    {type}
    {min}
    {disabled}
    {placeholder}
    aria-label={label}
    aria-invalid={invalid}
    style:width="{size}ch"
    value={String(value)}
    oninput={(e) => oninput(e.currentTarget.value)}
  />
  {#if suffix !== ""}<span class="suffix">{suffix}</span>{/if}
</div>

<style>
  .field {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    border-radius: 5px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
  }

  .field.invalid {
    border-color: var(--cut);
  }

  .field.disabled {
    opacity: 0.55;
  }

  .field:focus-within {
    border-color: var(--border-focus);
  }

  input {
    min-width: 0;
    padding: 0;
    border: none;
    background: none;
    outline: none;
    font-family: inherit;
    font-size: 11px;
    color: var(--text);
  }

  /* The spinners crowd a 3ch box and the keyboard does the same job. */
  input[type="number"] {
    appearance: textfield;
    -moz-appearance: textfield;
  }

  input::-webkit-outer-spin-button,
  input::-webkit-inner-spin-button {
    appearance: none;
    margin: 0;
  }

  .suffix {
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }
</style>
