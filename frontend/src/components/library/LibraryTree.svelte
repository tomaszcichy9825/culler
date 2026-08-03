<script lang="ts">
  // LIBRARY's folder tree: the catalogued roots, and what is under them.
  //
  // The same shape as CULL's tree and for the same reason — the visible nodes
  // are one flat list, so moving up and down is ±1 on an index and a node's
  // parent is a scan backwards for a shallower depth. The depth only drives
  // the indent.
  //
  // What it shows that CULL's does not is what the folder holds: every frame
  // at or under it, and how many of those nobody has judged. A folder too big
  // to have counted draws the frame count alone rather than a wrong badge.

  import { formatCount, library, type CatalogTreeNode } from "../../lib/library.svelte";

  interface Row {
    node: CatalogTreeNode;
    depth: number;
    expandable: boolean;
    expanded: boolean;
    loading: boolean;
  }

  let container = $state<HTMLDivElement | null>(null);

  // The pane fills itself the first time it is drawn, so entering LIBRARY is
  // enough to see the folders. Asking again is a refresh's business.
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
    // Only chase focus if the tree already had it, or moving the position
    // would take the keyboard away from whatever the user was using.
    if (holdsFocus()) queueMicrotask(() => rowEl(library.treeIndex)?.focus());
  }

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
        if (row.expandable && !row.expanded) void library.expandNode(row.node.path);
        else if (row.expanded) focusRow(library.treeIndex + 1);
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
        // Hand the keyboard back rather than trapping it here.
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
  aria-label="Catalogued folders"
  tabindex="-1"
  onkeydown={onKeydown}
>
  {#each rows as row, i (row.node.path)}
    <div
      class="row"
      class:on={library.facets.root === row.node.path}
      style:padding-left="{4 + row.depth * 13}px"
      role="treeitem"
      aria-level={row.depth + 1}
      aria-expanded={row.expandable ? row.expanded : undefined}
      aria-selected={i === library.treeIndex}
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
        class:focused={i === library.treeIndex}
        data-row={i}
        data-path={row.node.path}
        tabindex={i === library.treeIndex ? 0 : -1}
        title="{row.node.path} — ⏎ opens it in cull"
        onfocus={() => library.focusNode(i)}
        onclick={() => {
          library.focusNode(i);
          // Clicking narrows the search to the folder, the way the facet lists
          // do; opening it in cull is ⏎ or a double click.
          library.setFacet("root", row.node.path);
        }}
        ondblclick={() => library.openDir(row.node.path)}
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
    </div>
  {/each}

  {#if rows.length === 0}
    <p class="empty">no folders yet</p>
  {/if}
</div>

<style>
  .tree {
    min-height: 0;
    min-width: 0;
    padding: 2px 4px 6px;
    outline: none;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 1px;
    min-width: 0;
    border-radius: 4px;
    padding-right: 2px;
  }

  .row:hover {
    background: var(--bg-raised);
  }

  .row.on {
    background: var(--accent-wash-16);
    box-shadow: inset 2px 0 0 var(--accent);
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
    height: 20px;
    display: grid;
    place-items: center;
    color: var(--text-muted);
    font-size: 9px;
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
    width: 13px;
    height: 11px;
    margin-right: 5px;
    color: var(--text-dim);
  }

  .row:hover .folder,
  .name.focused .folder {
    color: var(--text-muted);
  }

  .name {
    flex: 1;
    /* Without this a deep path stretches the row past the pane. */
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 20px;
    padding: 0 4px;
    border-radius: 4px;
    text-align: left;
    outline: none;
  }

  /* The focused row is marked whether or not the pane holds the keyboard, so
     that ⏎ never acts on a row the user cannot see it is standing on. */
  .name.focused {
    background: var(--bg-row-active);
    box-shadow: inset 0 0 0 1px var(--border-selected);
  }

  .name:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .label {
    flex: 1;
    min-width: 0;
    font-size: 11.5px;
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

  .empty {
    margin: 4px 8px;
    font-size: 10.5px;
    color: var(--text-ghost);
  }
</style>
