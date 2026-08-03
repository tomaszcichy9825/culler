<script lang="ts">
  // Whichever palette is up, if any. One mount point so the shell has a single
  // line to render rather than three it has to keep mutually exclusive.

  import { palette } from "../lib/palette.svelte";
  import CommandPalette from "./CommandPalette.svelte";
  import FilterPalette from "./FilterPalette.svelte";
  import MovePalette from "./MovePalette.svelte";
</script>

{#if palette.kind === "command"}
  <CommandPalette />
{:else if palette.kind === "move" || palette.kind === "copy"}
  <MovePalette />
{:else if palette.kind === "filter"}
  <FilterPalette />
{/if}
