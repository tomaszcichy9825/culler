<script lang="ts">
  // The left rail: the catalogue's folder tree, an Add button that opens the
  // native chooser, the typed-path box that has to stay for network volumes
  // and anything the chooser will not reach, and the shoots the catalogue has
  // grouped those folders into.
  //
  // Sources and Sessions are two views of one index rather than two lists: a
  // folder opened from anywhere joins the catalogue, so it appears in the tree
  // with its counts and its frames fall into a session, without the user
  // having registered anything.

  import { pickRoot } from "../lib/actions";
  import { formatCount, library } from "../lib/library.svelte";
  import { app } from "../lib/state.svelte";
  import FolderPicker from "./FolderPicker.svelte";
  import Sessions from "./library/Sessions.svelte";
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
      <span class="title">Sources</span>
      <span class="hair"></span>
      <!-- The listing phase counts frames as it finds them; the hashing phase
           knows its total and says how far through it is. -->
      {#if library.indexing !== null}
        <span class="indexing" title={library.indexing.dir}>
          <span class="dot" aria-hidden="true"></span>
          {library.indexing.phase === "hashing" && library.indexing.pending > 0
            ? `${formatCount(library.indexing.hashed)}/${formatCount(library.indexing.pending)}`
            : formatCount(library.indexing.frames)}
        </span>
      {/if}
      <button class="add" onclick={() => void pickRoot()} title="Add a folder to the sidebar"> + add </button>
      <button class="icon" onclick={() => (app.sidebar = false)} title="Collapse sidebar" aria-label="Collapse sidebar">
        ‹
      </button>
    </div>

    <!-- The typed path lives with the Add button it complements — both are
         ways in, so burying the input at the bottom read as unrelated. -->
    <div class="path-entry">
      <FolderPicker bind:value={path} />
    </div>

    <Tree />

    <div class="head sessions-head">
      <span class="title">Sessions</span>
      <span class="hair"></span>
      <span class="hint">{formatCount(library.sessions.length)}</span>
    </div>

    <Sessions />

    {#if app.folder}
      <div class="foot">
        <div class="current" title={app.folder.dir}>
          <span class="leaf">{basename(app.folder.dir)}</span>
          <span class="count">{app.groups.length} frames</span>
        </div>
      </div>
    {/if}
  </aside>
{:else}
  <aside class="rail">
    <button class="icon" onclick={() => (app.sidebar = true)} title="Expand sidebar" aria-label="Expand sidebar">›</button>
  </aside>
{/if}

<style>
  /* Width, background and the rule to its right belong to the pane that holds
     this — the rail has to be able to take on the focused-pane treatment. */
  .sidebar,
  .rail {
    flex: 1;
    width: 100%;
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }

  .rail {
    align-items: center;
    padding-top: 8px;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 8px 7px 12px;
    min-width: 0;
    flex: 0 0 auto;
  }

  .title {
    flex: 0 0 auto;
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .hair {
    flex: 1;
    min-width: 0;
    height: 1px;
    background: var(--border);
  }

  /* Sessions is a second group in the same rail, so it gets a rule above it
     and a tighter top than the one at the head of the pane. */
  .sessions-head {
    padding-top: 8px;
    border-top: 1px solid var(--border);
  }

  .hint {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 10px;
    letter-spacing: 0;
    color: var(--text-dim);
  }

  .indexing {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-mono);
    font-size: 9.5px;
    letter-spacing: 0;
    color: var(--accent);
  }

  .indexing .dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: currentColor;
  }

  .add {
    flex: 0 0 auto;
    font: inherit;
    font-size: 10.5px;
    padding: 2px 7px;
    border-radius: 4px;
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text-muted);
    cursor: pointer;
    white-space: nowrap;
  }

  .add:hover {
    border-color: var(--accent);
    color: var(--text-hi);
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
    color: var(--text-dim);
    font-size: 14px;
    line-height: 1;
    cursor: pointer;
  }

  .icon:hover {
    background: var(--bg-raised);
    color: var(--text);
  }

  .path-entry {
    flex: 0 0 auto;
    padding: 0 10px 8px;
    border-bottom: 1px solid var(--border);
    min-width: 0;
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
    color: var(--text-dim);
    white-space: nowrap;
  }
</style>
