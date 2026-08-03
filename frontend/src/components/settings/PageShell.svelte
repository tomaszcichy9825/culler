<script lang="ts">
  // The two panes every settings page is made of: the scrolling column of
  // groups, and the 280px aside beside it. The nav is the screen's, not the
  // page's, so it is not here.

  import type { Snippet } from "svelte";

  interface Props {
    content: Snippet;
    aside: Snippet;
  }

  let { content, aside }: Props = $props();
</script>

<div class="content">
  <div class="scroll">
    {@render content()}
  </div>
</div>

<aside class="aside">
  {@render aside()}
</aside>

<style>
  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-window);
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 18px 22px;
  }

  .aside {
    flex: 0 0 280px;
    display: flex;
    flex-direction: column;
    background: var(--bg-pane);
    border-left: 1px solid var(--border);
    overflow-y: auto;
  }
</style>
