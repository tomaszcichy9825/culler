<script lang="ts">
  // The folder tree, flattened, over what the catalogue covers.
  //
  // Sources are the catalogue's watched roots: there is no second list of
  // folders kept beside it, so a folder opened from anywhere shows up here and
  // the counts are the ones the index measured. What it shows that a plain
  // directory listing cannot is what a folder holds — every frame at or under
  // it, and how many of those nobody has judged.
  //
  // Rendering the visible nodes as one flat list rather than nested components
  // is what makes the keyboard work: moving up and down is ±1 on an index, and
  // finding a node's parent is a scan backwards for a shallower depth. The
  // depth only drives the indent.

  import { formatCount, library, type CatalogTreeNode } from "../lib/library.svelte";
  import { app, tree } from "../lib/state.svelte";
  import NetworkChip from "./NetworkChip.svelte";

  interface Row {
    node: CatalogTreeNode;
    depth: number;
    expandable: boolean;
    expanded: boolean;
    loading: boolean;
  }

  let container = $state<HTMLDivElement | null>(null);

  // The sidebar fills itself the first time it is drawn, so launching into
  // CULL is enough to see the folders. Asking again is a refresh's business.
  $effect(() => {
    void library.ensureTree();
  });

  let rows = $derived.by(() => {
    const out: Row[] = [];
    const push = (node: CatalogTreeNode, depth: number) => {
      const known = library.childrenOf(node.path);
      out.push({
        node,
        depth,
        // Before a folder has been listed the backend's hasDirs is all there
        // is to go on; once it has, the answer is the list itself.
        expandable: known !== undefined ? known.length > 0 : node.hasDirs,
        expanded: library.expanded.has(node.path),
        loading: library.loadingNodes.has(node.path),
      });
      if (!library.expanded.has(node.path)) return;
      for (const child of known ?? []) push(child, depth + 1);
    };
    for (const root of library.treeRoots) push(root, 0);
    return out;
  });

  /** Keeps the keyboard position inside the list as it grows and shrinks. */
  $effect(() => {
    if (library.treeIndex > rows.length - 1) {
      library.treeIndex = Math.max(0, rows.length - 1);
    }
  });

  function rowEl(index: number): HTMLElement | null {
    return container?.querySelector(`[data-row="${index}"]`) ?? null;
  }

  function holdsFocus(): boolean {
    return container !== null && container.contains(document.activeElement);
  }

  function focusRow(index: number) {
    library.focusNode(Math.min(index, rows.length - 1));
    // Only chase focus if the tree already had it; otherwise moving the
    // position would steal the keyboard from the grid.
    if (holdsFocus()) queueMicrotask(() => rowEl(library.treeIndex)?.focus());
  }

  tree.focus = () => {
    queueMicrotask(() => rowEl(library.treeIndex)?.focus());
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
    const row = rows[library.treeIndex];
    if (!row) return;
    switch (e.key) {
      case "ArrowDown":
      case "j":
        e.preventDefault();
        focusRow(library.treeIndex + 1);
        break;
      case "ArrowUp":
      case "k":
        e.preventDefault();
        focusRow(library.treeIndex - 1);
        break;
      case "ArrowRight":
      case "l":
        e.preventDefault();
        // Right walks inward: open a closed folder, step into an open one, and
        // on a leaf that has no subfolders left to open, load its frames — the
        // rightmost thing there is to do with it.
        if (row.expandable && !row.expanded) void library.expandNode(row.node.path);
        else if (row.expanded) focusRow(library.treeIndex + 1);
        else library.openDir(row.node.path);
        break;
      case "ArrowLeft":
      case "h":
        e.preventDefault();
        if (row.expanded) library.collapseNode(row.node.path);
        else focusRow(parentOf(library.treeIndex));
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
        library.openDir(row.node.path);
        break;
      case "Delete":
      case "Backspace":
        if (!row.node.isRoot) break;
        e.preventDefault();
        void library.removeRoot(row.node.path);
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
  {#each rows as row, i (row.node.path)}
    <div
      class="row"
      class:active={app.folder?.dir === row.node.path}
      class:cursor={i === library.treeIndex}
      style:padding-left="{6 + row.depth * 13}px"
      role="treeitem"
      aria-level={row.depth + 1}
      aria-expanded={row.expandable ? row.expanded : undefined}
      aria-selected={app.folder?.dir === row.node.path}
    >
      <button
        class="twisty"
        class:hidden={!row.expandable}
        tabindex="-1"
        aria-hidden="true"
        title={row.expanded ? "Collapse" : "Expand"}
        onclick={() => void library.toggleNode(row.node.path)}
      >
        {row.loading ? "·" : row.expanded ? "▾" : "▸"}
      </button>

      <button
        class="name"
        data-row={i}
        data-path={row.node.path}
        tabindex={i === library.treeIndex ? 0 : -1}
        title={row.node.path}
        onfocus={() => library.focusNode(i)}
        onclick={() => {
          library.focusNode(i);
          library.openDir(row.node.path);
        }}
        ondblclick={() => void library.toggleNode(row.node.path)}
      >
        <svg class="folder" viewBox="0 0 14 12" aria-hidden="true">
          <path
            d="M1 2.5 Q1 1.5 2 1.5 L5.2 1.5 L6.4 3 L12 3 Q13 3 13 4 L13 9.5 Q13 10.5 12 10.5 L2 10.5 Q1 10.5 1 9.5 Z"
            fill="none"
            stroke="currentColor"
            stroke-width="1.1"
          />
        </svg>
        <span class="label">{row.node.name}</span>
        <!-- UNDECIDED_UNKNOWN is negative, so a folder that was not counted
             draws no badge rather than a zero that would read as "all judged". -->
        {#if row.node.undecided > 0}
          <span class="undecided" title="{row.node.undecided} still to judge">
            {formatCount(row.node.undecided)}
          </span>
        {/if}
        <span class="count" data-count={row.node.frames}>{formatCount(row.node.frames)}</span>
      </button>

      {#if row.node.isRoot && app.network[row.node.path]}
        <NetworkChip compact />
      {/if}

      {#if row.node.isRoot}
        <button
          class="remove"
          tabindex="-1"
          title="Stop watching {row.node.path}"
          aria-label="Stop watching {row.node.path}"
          onclick={() => void library.removeRoot(row.node.path)}
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

  /* The keyboard cursor. Drawn from the tracked index rather than from
     :focus-visible, which the webview drops the moment the mouse moves — the
     whole complaint. It persists so arrow navigation stays legible under the
     pointer, and sits under .active so the open folder still reads strongest. */
  .row.cursor {
    background: var(--bg-raised);
    box-shadow: inset 0 0 0 1px var(--border-strong);
  }

  .row.cursor.active {
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
    width: 16px;
    height: 22px;
    display: grid;
    place-items: center;
    color: var(--text-muted);
    font-size: 10px;
    line-height: 1;
    border-radius: 3px;
  }

  .twisty:hover {
    color: var(--text);
    background: var(--bg-field);
  }

  .twisty.hidden {
    visibility: hidden;
  }

  .folder {
    flex: 0 0 auto;
    width: 14px;
    height: 12px;
    margin-right: 5px;
    color: var(--text-dim);
  }

  .row.active .folder,
  .row:hover .folder {
    color: var(--text-muted);
  }

  .name {
    flex: 1;
    /* Without this a deep path stretches the row past the sidebar. */
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
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
    flex: 1;
    min-width: 0;
    font-size: 12.5px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 9.5px;
    color: var(--text-dim);
  }

  .undecided {
    flex: 0 0 auto;
    padding: 1px 4px;
    border-radius: 3px;
    background: var(--accent-wash-16);
    font-family: var(--font-mono);
    font-size: 9px;
    color: var(--accent);
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
