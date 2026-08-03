<script lang="ts">
  // A chord drawn as one cap per key (§4.4), which is how the keymap page and
  // the overlay render bindings.

  import { chordParts } from "../../lib/keymapCatalogue";

  interface Props {
    chord: string;
    /** The large form used inside the recording box. */
    big?: boolean;
    /** Draws the caps back, for a chord that is a fallback rather than bound. */
    faint?: boolean;
  }

  let { chord, big = false, faint = false }: Props = $props();

  let parts = $derived(chordParts(chord));
</script>

<span class="chord" class:big class:faint>
  {#each parts as part, i (i)}
    <kbd>{part}</kbd>
  {/each}
</span>

<style>
  .chord {
    display: inline-flex;
    align-items: center;
    gap: 3px;
  }

  kbd {
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-family: inherit;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--text-2);
    white-space: nowrap;
  }

  .big kbd {
    padding: 3px 8px;
    background: transparent;
    border: none;
    font-size: 14px;
    font-weight: 700;
    color: var(--accent);
  }

  .faint kbd {
    background: transparent;
    border-color: var(--border);
    color: var(--text-dim);
  }
</style>
