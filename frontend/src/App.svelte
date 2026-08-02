<script lang="ts">
  // Composition plus the one keyboard listener in the app. Every binding is
  // resolved from the config keymap, so nothing here decides which key does
  // what — only what each action means.

  import { tick } from "svelte";
  import ApplyBar from "./components/ApplyBar.svelte";
  import Grid from "./components/Grid.svelte";
  import KeymapOverlay from "./components/KeymapOverlay.svelte";
  import Loader from "./components/Loader.svelte";
  import Loupe from "./components/Loupe.svelte";
  import NetworkChip from "./components/NetworkChip.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import Toast from "./components/Toast.svelte";
  import {
    cancelApply,
    confirmApply,
    lastFolder,
    loadKeymap,
    loadRoots,
    openFolder,
    pickRoot,
    requestApply,
    undo,
  } from "./lib/actions";
  import { flush, setDecision } from "./lib/decisions";
  import { buildLookup, eventSignature, isMac, ownsKeys } from "./lib/keymap";
  import { app, loupe, picker, tree } from "./lib/state.svelte";
  import type { Decision } from "./lib/state.svelte";

  /** How far one arrow press pans the zoomed loupe, in image pixels. */
  const PAN_STEP = 120;

  /** Bound but not built yet; they toast rather than doing nothing. */
  const laterActions: Record<string, string> = {
    "copy-palette": "copy destinations come in v0.2",
    "move-palette": "move destinations come in v0.2",
    "filter-palette": "filters come in v0.2",
    "command-palette": "the command palette comes in v0.2",
    redo: "nothing to redo",
  };

  const decisionActions: Record<string, Decision> = {
    "keep-all": "keep_all",
    "drop-raw": "drop_raw",
    "drop-jpeg": "drop_jpeg",
    "drop-both": "drop_all",
    "clear-decision": "none",
  };

  let path = $state(lastFolder());
  let lookup = $derived(buildLookup(app.keymap));

  void (async () => {
    loadRoots();
    await loadKeymap();
    if (path !== "") await openFolder(path);
    if (app.folder) path = app.folder.dir;
  })();

  function moveFocus(dx: number, dy: number) {
    if (app.view === "loupe" && app.zoom) {
      loupe.pan(-dx * PAN_STEP, -dy * PAN_STEP);
      return;
    }
    const rowStep = app.view === "loupe" ? 1 : app.cols;
    app.setFocus(app.focusIndex + dx + dy * rowStep);
  }

  function escape() {
    if (app.plan) {
      cancelApply();
      return;
    }
    if (app.overlay) {
      app.overlay = false;
      return;
    }
    if (app.view === "loupe") {
      if (app.zoom) {
        app.resetZoom();
        return;
      }
      app.view = "grid";
      return;
    }
    app.clearSelection();
  }

  /** The sidebar's controls only exist while it is open, so reveal it first. */
  async function revealSidebar() {
    if (!app.sidebar) {
      app.sidebar = true;
      await tick();
    }
  }

  async function focusPath() {
    await revealSidebar();
    picker.focus();
  }

  async function focusTree() {
    await revealSidebar();
    if (app.roots.length === 0) {
      app.notify("no folders yet — add one to start the tree");
      picker.focus();
      return;
    }
    tree.focus();
  }

  function run(action: string) {
    switch (action) {
      case "focus-left":
        moveFocus(-1, 0);
        break;
      case "focus-right":
        moveFocus(1, 0);
        break;
      case "focus-up":
        moveFocus(0, -1);
        break;
      case "focus-down":
        moveFocus(0, 1);
        break;
      case "toggle-loupe":
        app.view = app.view === "grid" ? "loupe" : "grid";
        if (app.view === "grid") app.resetZoom();
        break;
      case "zoom":
        if (app.view !== "loupe") {
          app.notify("zoom works in the loupe — Tab to open it");
          break;
        }
        if (app.zoom) app.resetZoom();
        else app.zoom = true;
        break;
      case "toggle-select":
        app.toggleSelect();
        break;
      case "select-all":
        app.selectAll();
        break;
      case "escape":
        escape();
        break;
      case "apply":
        if (app.plan) void confirmApply();
        else void requestApply();
        break;
      case "undo":
        void flush().then(undo);
        break;
      case "keymap-overlay":
        app.overlay = !app.overlay;
        break;
      case "toggle-sidebar":
        app.sidebar = !app.sidebar;
        break;
      case "focus-path":
        void focusPath();
        break;
      case "focus-tree":
        void focusTree();
        break;
      case "add-root":
        void pickRoot();
        break;
      default:
        if (action in decisionActions) {
          setDecision(decisionActions[action]);
        } else if (action in laterActions) {
          app.notify(laterActions[action]);
        }
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (ownsKeys(e.target)) {
      // The path box and the tree run their own keyboards; Esc is the way back
      // out of both, and Tab is left alone so they stay reachable by tabbing.
      if (e.key === "Escape") (e.target as HTMLElement).blur();
      return;
    }
    const action = lookup.get(eventSignature(e));
    if (action === undefined) return;
    e.preventDefault();

    // The plan panel and the overlay are modal in intent but not in focus:
    // they swallow everything except the keys that dismiss them.
    if (app.plan && action !== "apply" && action !== "escape") return;
    if (app.overlay && action !== "keymap-overlay" && action !== "escape") return;
    run(action);
  }
</script>

<svelte:window onkeydown={onKeydown} onblur={() => void flush()} onbeforeunload={() => void flush()} />

<div class="app" class:mac={isMac}>
  <header>
    <span class="brand">culler</span>
    {#if app.folder}
      <span class="where" title={app.folder.dir}>{app.folder.dir}</span>
      {#if app.folder.network}<NetworkChip />{/if}
    {/if}
    {#if app.busy}<span class="working">working…</span>{/if}
  </header>

  {#if app.error !== ""}
    <div class="error" role="alert" title={app.error}>{app.error}</div>
  {/if}

  <div class="body">
    <Sidebar bind:path />

    <main>
      {#if app.scanning !== null}
        <Loader />
      {:else if app.folder === null}
        <div class="empty">
          <p>Type a folder in the sidebar and press ↩.</p>
          <p class="hint">Absolute paths and ~ both work. Press ? for the keys.</p>
        </div>
      {:else if app.groups.length === 0}
        <div class="empty">
          <p class="where" title={app.folder.dir}>No photos in {app.folder.dir}</p>
        </div>
      {:else if app.view === "loupe"}
        <Loupe />
      {:else}
        <Grid />
      {/if}
    </main>
  </div>

  <ApplyBar />
</div>

{#if app.overlay}
  <KeymapOverlay />
{/if}
<Toast />

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
    --wails-draggable: no-drag;
  }

  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 14px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-chrome);
    flex: 0 0 auto;
    min-width: 0;
    /* The title bar is hidden inset, so the header doubles as the drag region. */
    --wails-draggable: drag;
  }

  /* Clear of the macOS traffic lights, which sit over the top-left corner. */
  .app.mac header {
    padding-left: 78px;
  }

  .brand {
    flex: 0 0 auto;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    color: var(--text-faint);
    text-transform: uppercase;
  }

  .where {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .working {
    flex: 0 0 auto;
    font-size: 12px;
    color: var(--text-faint);
  }

  .error {
    flex: 0 0 auto;
    padding: 6px 14px;
    background: var(--error-bg);
    border-bottom: 1px solid var(--error-border);
    color: var(--error-text);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .body {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  main {
    flex: 1;
    /* Lets the grid shrink with the sidebar rather than pushing it off screen. */
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .empty {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 0 20px;
    min-width: 0;
    color: var(--text-muted);
    font-size: 13px;
  }

  .empty p {
    margin: 0;
    max-width: 100%;
  }

  .empty .hint {
    color: var(--text-faint);
    font-size: 12px;
  }
</style>
