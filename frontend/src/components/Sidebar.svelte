<script lang="ts">
  // The left rail: the folder tree, an Add button that opens the native
  // chooser, and the typed-path box that has to stay for network volumes and
  // anything the chooser will not reach.

  import { pickRoot } from "../lib/actions";
  import { app } from "../lib/state.svelte";
  import FolderPicker from "./FolderPicker.svelte";
  import Tree from "./Tree.svelte";

  let { path = $bindable("") }: { path?: string } = $props();

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }
</script>

{#if app.sidebar}
  <aside class="sidebar">
    <div class="head">
      <span class="title">Folders</span>
      <button class="add" onclick={() => void pickRoot()} disabled={app.busy} title="Add a folder to the sidebar">
        + Add
      </button>
      <button class="icon" onclick={() => (app.sidebar = false)} title="Collapse sidebar" aria-label="Collapse sidebar">
        ‹
      </button>
    </div>

    <Tree />

    <div class="foot">
      {#if app.folder}
        <div class="current" title={app.folder.dir}>
          <span class="leaf">{basename(app.folder.dir)}</span>
          <span class="count">{app.groups.length} frames</span>
        </div>
      {/if}
      <FolderPicker bind:value={path} />
    </div>
  </aside>
{:else}
  <aside class="rail">
    <button class="icon" onclick={() => (app.sidebar = true)} title="Expand sidebar" aria-label="Expand sidebar">›</button>
  </aside>
{/if}

<style>
  .sidebar,
  .rail {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--bg-chrome);
    border-right: 1px solid var(--border);
    overflow: hidden;
  }

  .sidebar {
    width: 232px;
  }

  .rail {
    width: 30px;
    align-items: center;
    padding-top: 8px;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 8px 8px 6px 12px;
    min-width: 0;
    flex: 0 0 auto;
  }

  .title {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .add {
    flex: 0 0 auto;
    font: inherit;
    font-size: 11px;
    padding: 3px 8px;
    border-radius: 5px;
    border: 1px solid var(--border-strong);
    background: var(--bg-chip);
    color: var(--text);
    cursor: pointer;
    white-space: nowrap;
  }

  .add:hover:not(:disabled) {
    border-color: var(--accent);
  }

  .add:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .icon {
    flex: 0 0 auto;
    width: 20px;
    height: 20px;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    background: none;
    border-radius: 4px;
    color: var(--text-faint);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
  }

  .icon:hover {
    background: var(--bg-raised);
    color: var(--text);
  }

  .foot {
    flex: 0 0 auto;
    padding: 8px 10px;
    border-top: 1px solid var(--border);
    min-width: 0;
  }

  .current {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
    margin-bottom: 6px;
  }

  .leaf {
    flex: 0 1 auto;
    min-width: 0;
    font-size: 12px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    flex: 0 0 auto;
    font-size: 10.5px;
    color: var(--text-faint);
    white-space: nowrap;
  }
</style>
