<script lang="ts">
  // The folder tree, flattened.
  //
  // Rendering the visible nodes as one flat list rather than nested components
  // is what makes the keyboard work: moving up and down is ±1 on an index, and
  // finding a node's parent is a scan backwards for a shallower depth. The
  // depth only drives the indent.

  import { collapseNode, expandNode, openFolder, removeRoot, toggleNode } from "../lib/actions";
  import { app, tree } from "../lib/state.svelte";
  import NetworkChip from "./NetworkChip.svelte";

  interface Row {
    path: string;
    name: string;
    depth: number;
    isRoot: boolean;
    expandable: boolean;
    expanded: boolean;
    loading: boolean;
  }

  let container = $state<HTMLDivElement | null>(null);

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }

  let rows = $derived.by(() => {
    const out: Row[] = [];
    const push = (path: string, name: string, depth: number, isRoot: boolean, hasDirs: boolean | undefined) => {
      const known = app.children[path];
      out.push({
        path,
        name,
        depth,
        isRoot,
        // Before a folder has been listed the backend's hasDirs is all we
        // know; a root has not even got that, so assume it opens.
        expandable: known !== undefined ? known.length > 0 : (hasDirs ?? true),
        expanded: app.expanded.has(path),
        loading: app.loading.has(path),
      });
      if (app.expanded.has(path)) {
        for (const child of known ?? []) push(child.path, child.name, depth + 1, false, child.hasDirs);
      }
    };
    for (const root of app.roots) push(root, basename(root), 0, true, undefined);
    return out;
  });

  /** Keeps the keyboard position inside the list as it grows and shrinks. */
  $effect(() => {
    if (app.treeIndex > rows.length - 1) app.treeIndex = Math.max(0, rows.length - 1);
  });

  function rowEl(index: number): HTMLElement | null {
    return container?.querySelector(`[data-row="${index}"]`) ?? null;
  }

  function holdsFocus(): boolean {
    return container !== null && container.contains(document.activeElement);
  }

  function focusRow(index: number) {
    app.treeIndex = Math.max(0, Math.min(index, rows.length - 1));
    // Only chase focus if the tree already had it; otherwise moving the
    // position would steal the keyboard from the grid.
    if (holdsFocus()) queueMicrotask(() => rowEl(app.treeIndex)?.focus());
  }

  tree.focus = () => {
    queueMicrotask(() => rowEl(app.treeIndex)?.focus());
  };

  /** parentOf is the nearest row above index at a shallower depth. */
  function parentOf(index: number): number {
    const depth = rows[index]?.depth ?? 0;
    for (let i = index - 1; i >= 0; i--) {
      if (rows[i].depth < depth) return i;
    }
    return index;
  }

  function onKeydown(e: KeyboardEvent) {
    const row = rows[app.treeIndex];
    if (!row) return;
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        focusRow(app.treeIndex + 1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        focusRow(app.treeIndex - 1);
        break;
      case "ArrowRight":
      case "l":
        e.preventDefault();
        if (row.expandable && !row.expanded) void expandNode(row.path);
        else if (row.expanded) focusRow(app.treeIndex + 1);
        break;
      case "ArrowLeft":
      case "h":
        e.preventDefault();
        if (row.expanded) collapseNode(row.path);
        else focusRow(parentOf(app.treeIndex));
        break;
      case "Home":
        e.preventDefault();
        focusRow(0);
        break;
      case "End":
        e.preventDefault();
        focusRow(rows.length - 1);
        break;
      case "Enter":
      case " ":
        e.preventDefault();
        void openFolder(row.path);
        break;
      case "Delete":
      case "Backspace":
        if (!row.isRoot) break;
        e.preventDefault();
        removeRoot(row.path);
        break;
      case "Escape":
        e.preventDefault();
        // Hand the keyboard back to the grid rather than trapping it here.
        (document.activeElement as HTMLElement | null)?.blur();
        break;
    }
  }
</script>

<div
  class="tree"
  bind:this={container}
  data-keys="local"
  role="tree"
  aria-label="Folders"
  tabindex="-1"
  onkeydown={onKeydown}
>
  {#each rows as row, i (row.path)}
    <div
      class="row"
      class:active={app.folder?.dir === row.path}
      style:padding-left="{6 + row.depth * 13}px"
      role="treeitem"
      aria-level={row.depth + 1}
      aria-expanded={row.expandable ? row.expanded : undefined}
      aria-selected={app.folder?.dir === row.path}
    >
      <button
        class="twisty"
        class:hidden={!row.expandable}
        tabindex="-1"
        aria-hidden="true"
        title={row.expanded ? "Collapse" : "Expand"}
        onclick={() => void toggleNode(row.path)}
      >
        {row.loading ? "·" : row.expanded ? "▾" : "▸"}
      </button>

      <button
        class="name"
        data-row={i}
        tabindex={i === app.treeIndex ? 0 : -1}
        title={row.path}
        onfocus={() => (app.treeIndex = i)}
        onclick={() => {
          app.treeIndex = i;
          void openFolder(row.path);
        }}
        ondblclick={() => void toggleNode(row.path)}
      >
        <span class="label">{row.name}</span>
      </button>

      {#if row.isRoot && app.network[row.path]}
        <NetworkChip compact />
      {/if}

      {#if row.isRoot}
        <button
          class="remove"
          tabindex="-1"
          title="Remove {row.path} from the sidebar"
          aria-label="Remove {row.path} from the sidebar"
          onclick={() => removeRoot(row.path)}
        >
          ×
        </button>
      {/if}
    </div>
  {/each}

  {#if rows.length === 0}
    <p class="empty">No folders yet.</p>
  {/if}
</div>

<style>
  .tree {
    flex: 1;
    min-height: 0;
    min-width: 0;
    overflow-y: auto;
    overflow-x: hidden;
    padding: 2px 6px 8px;
    outline: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 1px;
    min-width: 0;
    border-radius: 5px;
    padding-right: 2px;
  }

  .row:hover {
    background: var(--bg-raised);
  }

  .row.active {
    background: var(--bg-row-active);
    box-shadow: inset 0 0 0 1px var(--border-selected);
  }

  button {
    font: inherit;
    border: none;
    background: none;
    color: inherit;
    cursor: pointer;
    padding: 0;
  }

  .twisty {
    flex: 0 0 auto;
    width: 14px;
    height: 22px;
    display: grid;
    place-items: center;
    color: var(--text-dim);
    font-size: 9px;
    line-height: 1;
  }

  .twisty.hidden {
    visibility: hidden;
  }

  .name {
    flex: 1;
    /* Without this a deep path stretches the row past the sidebar. */
    min-width: 0;
    display: flex;
    align-items: center;
    height: 22px;
    padding: 0 3px;
    border-radius: 4px;
    text-align: left;
    outline: none;
  }

  .name:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .label {
    min-width: 0;
    font-size: 12.5px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remove {
    flex: 0 0 auto;
    width: 16px;
    height: 16px;
    display: grid;
    place-items: center;
    border-radius: 4px;
    color: var(--text-dim);
    font-size: 13px;
    line-height: 1;
    opacity: 0;
  }

  .row:hover .remove,
  .remove:focus-visible {
    opacity: 1;
  }

  .remove:hover {
    background: var(--bg-field);
    color: var(--text);
  }

  .empty {
    margin: 6px 8px;
    font-size: 11.5px;
    color: var(--text-dim);
  }
</style>
