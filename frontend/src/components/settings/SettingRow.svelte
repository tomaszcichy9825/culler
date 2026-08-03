<script lang="ts">
  // One setting: what it is called and what it does on the left, the controls
  // that change it on the right.
  //
  // A row that the filter excludes takes itself out of the page and tells its
  // group, so a group filtered down to nothing disappears with it. A row whose
  // control has no backend behind it says so in words rather than looking live.

  import type { Snippet } from "svelte";
  import { settings } from "../../lib/settings.svelte";
  import { matchesFilter } from "./context";

  interface Props {
    name: string;
    desc: string;
    /** The config field this row edits, so a rejection can land on it. */
    field?: string;
    /** False when nothing behind the control is built yet. */
    wired?: boolean;
    /** Extra words the filter should match, beyond the name and description. */
    terms?: string;
    children: Snippet;
  }

  let { name, desc, field = "", wired = true, terms = "", children }: Props = $props();

  let shown = $derived(matchesFilter(settings.filter, name, desc, terms));
  let error = $derived(field === "" ? "" : settings.errorFor(field));
</script>

{#if shown}
  <div class="setting-row" class:invalid={error !== ""} data-field={field}>
    <div class="what">
      <span class="name">
        {name}
        {#if !wired}<span class="tag" title="No backend behind this control yet">not wired yet</span>{/if}
      </span>
      <span class="desc">{desc}</span>
      {#if error !== ""}<span class="error" role="alert">{error}</span>{/if}
    </div>
    <div class="controls">
      {@render children()}
    </div>
  </div>
{/if}

<style>
  .setting-row {
    display: flex;
    align-items: flex-start;
    gap: 14px;
  }

  .what {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }

  .name {
    font-size: 12px;
    color: var(--text);
    overflow-wrap: anywhere;
  }

  .tag {
    margin-left: 7px;
    padding: 1px 5px;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 9.5px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .desc {
    font-size: 10.5px;
    color: var(--text-muted);
    line-height: 1.55;
    text-wrap: pretty;
  }

  .error {
    font-size: 10.5px;
    color: var(--cut);
    line-height: 1.55;
    text-wrap: pretty;
  }

  .controls {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 7px;
  }
</style>
