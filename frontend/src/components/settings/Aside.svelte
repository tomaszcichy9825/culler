<script lang="ts">
  // One block of the right-hand column: a section title, key/value rows, and
  // an optional paragraph.

  import type { Snippet } from "svelte";
  import SectionLabel from "./SectionLabel.svelte";

  export type Tone = "default" | "good" | "attention" | "accent" | "muted";

  export interface AsideRow {
    k: string;
    v: string;
    tone?: Tone;
  }

  interface Props {
    title: string;
    rows?: AsideRow[];
    note?: string;
    children?: Snippet;
  }

  let { title, rows = [], note = "", children }: Props = $props();
</script>

<div class="block">
  <SectionLabel {title} />

  {#each rows as row (row.k)}
    <div class="row">
      <span class="k">{row.k}</span>
      <span class="v {row.tone ?? 'default'}">{row.v}</span>
    </div>
  {/each}

  {#if children}{@render children()}{/if}

  {#if note !== ""}<p class="note">{note}</p>{/if}
</div>

<style>
  .block {
    padding: 11px 12px;
    border-bottom: 1px solid var(--border);
  }

  .row {
    display: flex;
    align-items: baseline;
    gap: 10px;
    min-height: 21px;
    padding: 1px 0;
  }

  .k {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-muted);
    line-height: 1.55;
    overflow-wrap: anywhere;
  }

  .v {
    flex: 0 0 auto;
    font-size: 11px;
    color: var(--text);
    white-space: nowrap;
  }

  .v.good {
    color: var(--keep);
  }

  .v.attention {
    color: var(--gold);
  }

  .v.accent {
    color: var(--accent);
  }

  .v.muted {
    color: var(--text-dim);
  }

  .note {
    margin: 8px 0 0;
    font-size: 10.5px;
    color: var(--text-muted);
    line-height: 1.55;
    text-wrap: pretty;
  }
</style>
