<script lang="ts">
  // A titled group of setting rows.
  //
  // Filtering removes rows from the page, and a group whose rows have all gone
  // must not leave its heading and hairline behind. That is a CSS question
  // rather than a state one: a group with no row inside it takes itself out of
  // the flow, so nothing has to count anything.

  import type { Snippet } from "svelte";
  import SectionLabel from "./SectionLabel.svelte";

  interface Props {
    title: string;
    danger?: boolean;
    hint?: string;
    children: Snippet;
  }

  let { title, danger = false, hint = "", children }: Props = $props();
</script>

<section class="group" aria-label={title}>
  <SectionLabel {title} {danger} {hint} />
  <div class="rows">
    {@render children()}
  </div>
</section>

<style>
  .group {
    margin-bottom: 20px;
  }

  /* The rows are SettingRow's own elements, hence the :global. */
  .group:not(:has(:global(.setting-row))) {
    display: none;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 11px;
  }
</style>
