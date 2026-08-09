<script lang="ts">
  // Composition plus the one keyboard listener in the app. Every binding is
  // resolved from the config keymap, so nothing here decides which key does
  // what — only what each action means.
  //
  // The shell is the same on every screen: a title bar, three panes and a
  // status bar. A mode is a set of pane bodies, nothing more.

  import { tick, untrack } from "svelte";
  import ApplyBar from "./components/ApplyBar.svelte";
  import Grid from "./components/Grid.svelte";
  import Inspector from "./components/Inspector.svelte";
  import KeymapOverlay from "./components/KeymapOverlay.svelte";
  import Loader from "./components/Loader.svelte";
  import ColdStart from "./components/ColdStart.svelte";
  import CompareView, { compare as compareApi } from "./components/CompareView.svelte";
  import WritePlanDialog from "./components/exif/WritePlanDialog.svelte";
  import SearchBar from "./components/library/SearchBar.svelte";
  import StorageView from "./components/library/StorageView.svelte";
  import LoupeFirst from "./components/LoupeFirst.svelte";
  import LoupeOverlay from "./components/LoupeOverlay.svelte";
  import Palettes from "./components/Palettes.svelte";
  import SettingsView from "./components/SettingsView.svelte";
  import TableView from "./components/TableView.svelte";
  import { runAction, openFolder as openFolderAction } from "./lib/actions";
  import { ExifService, ImportService, LibraryIndexService, MapService, RejectsService } from "./lib/bindings";
  import { setVerdictFor } from "./lib/decisions";
  import { exifState } from "./lib/exif.svelte";
  import ImportCentre from "./components/import/ImportCentre.svelte";
  import ImportLeft from "./components/import/ImportLeft.svelte";
  import ImportRight from "./components/import/ImportRight.svelte";
  import { connectImport, importState, onOpenFolder as onImportOpen, watchImportProgress } from "./lib/import.svelte";
  import MapCentre from "./components/map/MapCentre.svelte";
  import MapLeft from "./components/map/MapLeft.svelte";
  import MapRight from "./components/map/MapRight.svelte";
  import GeotagDialog from "./components/map/GeotagDialog.svelte";
  import { geotag } from "./lib/geotag.svelte";
  import { connectMap, mapState, onOpenFrame, watchMapProgress } from "./lib/map.svelte";
  import RejectsDialog from "./components/RejectsDialog.svelte";
  import { connectRejects, rejects, watchRejectsProgress } from "./lib/rejects.svelte";
  import { connectCatalog, library, onOpenFolder, watchCatalogProgress } from "./lib/library.svelte";
  import { visibleGroups } from "./lib/palette.svelte";
  import { settings } from "./lib/settings.svelte";
  import { groupKey } from "./lib/state.svelte";
  import Sidebar from "./components/Sidebar.svelte";
  import VSplit from "./components/VSplit.svelte";
  import StatusBar from "./components/StatusBar.svelte";
  import TitleBar from "./components/TitleBar.svelte";
  import Toast from "./components/Toast.svelte";
  import {
    cancelApply,
    confirmApply,
    copyPath,
    lastFolder,
    loadSettings,
    markNetwork,
    migrateRoots,
    openFolder,
    pickRoot,
    requestApply,
    showSearchResults,
    undo,
    watchApplyProgress,
    watchScanProgress,
    watchScanStream,
  } from "./lib/actions";
  import { flush } from "./lib/decisions";
  import { buildLookup, eventSignature, inMapRegion, ownsKeys } from "./lib/keymap";
  import { CONTACT_SHEET, LOUPE_FIRST, shell } from "./lib/shell.svelte";
  import type { Pane } from "./lib/shell.svelte";
  import { app, picker, tree } from "./lib/state.svelte";

  /** How far one arrow press pans the zoomed loupe, in image pixels. */
  const PAN_STEP = 120;

  /** Bound but not built yet; they toast rather than doing nothing. */
  const laterActions: Record<string, string> = {
    redo: "nothing to redo",
  };


  const modeActions: Record<string, number> = {
    "mode-cull": 0,
    "mode-map": 1,
    "mode-import": 2,
  };

  const paneActions: Record<string, Pane> = {
    "pane-left": "left",
    "pane-centre": "centre",
    "pane-right": "right",
  };

  const layoutActions: Record<string, number> = {
    "layout-1": 0,
    "layout-2": 1,
    "layout-3": 2,
  };

  let path = $state(lastFolder());
  let lookup = $derived(buildLookup(app.keymap));

  // Reopen in the mode the last session was left in — but only when there is
  // a last folder to reopen. A cold start must land on PHOTOS, whose centre
  // pane is the ColdStart guidance; MAP and IMPORT draw their panes with or
  // without a folder, so restoring them over nothing would open the app on a
  // blank map with no way to see why. The per-mode sub-layouts are restored
  // unconditionally by the shell itself: they only say where each mode lands
  // when it is next entered.
  if (lastFolder() !== "") shell.restoreMode();

  /**
   * CULL's first two sub-layouts are the grid and one frame at a time, which
   * the app already has as its two views — so choosing a layout and the view
   * have to agree, or the segmented control starts lying.
   */
  function chooseLayout(index: number) {
    if (!shell.setLayout(index)) return;
    if (shell.mode !== "cull") return;
    if (index === LOUPE_FIRST && app.groups.length > 0) {
      app.view = "loupe";
      return;
    }
    app.view = "grid";
    app.resetZoom();
  }

  /**
   * Tab walks the current mode's sub-layouts in order, wrapping. The mode
   * decides how many there are, so nothing here knows that CULL has three.
   */
  function cycleLayout() {
    const next = shell.nextLayout();
    if (next === null) return;
    chooseLayout(next);
  }

  // Hand the generated services to the stores that were written against
  // injectable ports, so their components work in the harness and here alike.
  exifState.usePort({
    read: async (paths) => ((await ExifService.Read(paths)) ?? {}) as never,
    plan: async (edits) => (await ExifService.Plan(edits)) as never,
    apply: async (edits) => (await ExifService.Apply(edits)) as never,
  });
  connectCatalog({
    Roots: async () => ((await LibraryIndexService.Roots()) ?? []) as never,
    RegisterRoot: async (dir) => ((await LibraryIndexService.RegisterRoot(dir)) ?? []) as never,
    RemoveRoot: async (dir) => ((await LibraryIndexService.RemoveRoot(dir)) ?? []) as never,
    Reindex: (dir) => LibraryIndexService.Reindex(dir),
    Search: (q, f, limit, offset) => LibraryIndexService.Search(q, f as never, limit, offset) as never,
    Counts: (q, f) => LibraryIndexService.Counts(q, f as never) as never,
    Sessions: async (gap) => ((await LibraryIndexService.Sessions(gap)) ?? []) as never,
    Storage: () => LibraryIndexService.Storage() as never,
    TreeRoots: async () => ((await LibraryIndexService.TreeRoots()) ?? []) as never,
    TreeChildren: async (dir) => ((await LibraryIndexService.TreeChildren(dir)) ?? []) as never,
  });
  void watchCatalogProgress();
  onOpenFolder((dir, focusHash) => {
    // A folder chosen from the tree is a scope pick: it stays in whatever mode
    // is up, exactly as picking a session does, so the left-pane picker does
    // the same thing in PHOTOS and MAP. Only opening a specific frame — a
    // search result or a map pin, which carry a hash — jumps to cull to show it,
    // as does a pick made from a mode that has no view of a folder.
    const scoped = shell.mode === "cull" || shell.mode === "map";
    if (focusHash !== undefined || !scoped) shell.setMode("cull");
    // Opening a result is leaving the search, not searching from inside a
    // folder: the grid has to be the folder's again before it loads.
    library.closeSearch();
    void openFolderAction(dir, focusHash);
  });
  connectRejects({
    Survey: async (dirs) => (await RejectsService.Survey(dirs)) as never,
    Empty: async (dirs) => (await RejectsService.Empty(dirs)) as never,
  });
  void watchRejectsProgress();
  connectMap({
    Positions: async (dir) => (await MapService.Positions(dir)) as never,
    PositionsScope: async (refs) => (await MapService.PositionsScope(refs as never)) as never,
  });
  void watchMapProgress();
  onOpenFrame((dir, hash) => {
    shell.setMode("cull");
    library.closeSearch();
    void openFolderAction(dir, hash);
  });
  connectImport({
    DetectCards: async () => ((await ImportService.DetectCards()) ?? []) as never,
    CardSummary: async (path) => (await ImportService.CardSummary(path)) as never,
    ImportPlan: async (dir) => (await ImportService.ImportPlan(dir)) as never,
    Execute: async (dir, backupDest) => (await ImportService.Execute(dir, backupDest)) as never,
  });
  void watchImportProgress();
  onImportOpen((dir) => {
    shell.setMode("cull");
    library.closeSearch();
    void openFolderAction(dir);
  });

  // The catalogue is wired by here, so the startup sequence can hand it the
  // roots a previous version kept for itself before asking for a folder.
  void (async () => {
    watchScanProgress();
    watchScanStream();
    watchApplyProgress();
    await Promise.all([loadSettings(), migrateRoots()]);
    if (path !== "") await openFolder(path);
    if (app.folder) path = app.folder.dir;
  })();

  // The network badge is a property of a root, and the roots are the
  // catalogue's, so the lookup follows the catalogue rather than a list the
  // sidebar keeps. markNetwork answers once per path and caches.
  $effect(() => {
    const roots = library.roots.map((r) => r.path);
    untrack(() => {
      app.roots = roots;
      for (const root of roots) void markNetwork(root);
    });
  });

  // Search results reach the grid the way a folder's frames do, and the filter
  // effect below turns them into what is on screen. The swap itself lives in
  // actions.ts so that it is the same code the bench drives.
  $effect(() => {
    const open = library.searchOpen;
    const results = library.results;
    untrack(() => showSearchResults(open, results));
  });

  // Metadata drafts are scoped to the folder they were typed in. Only an
  // actual folder change — picked from the tree, a search result, a session,
  // a map pin, an import — prunes every draft; opening or closing the search
  // keeps them, except the drafts typed while the search itself was up, which
  // belong to the search and go when it closes. The rule is documented on
  // ExifState.setScope; this effect just reports both facts, and runs in
  // every mode, because drafts survive leaving PHOTOS and the folder can
  // change from any mode. Reopening the same folder keeps the drafts: the
  // files they belong to are still the files on screen.
  $effect(() => {
    const dir = app.folder?.dir ?? "";
    const searching = library.searchOpen;
    untrack(() => exifState.setScope(dir, searching));
  });

  // The inspector edits what the grid has selected (or focused), so the shell
  // owns the effect that keeps the editor fed — one path per frame, JPEG
  // preferred because that is the half a write can reach in place. The list
  // is compared against what was last loaded so a streamed grid reassigning
  // its array on every batch does not re-read the same frames again and
  // again; exifState.load serialises the reads itself, so a slow one cannot
  // land on top of a newer one. Leaving PHOTOS clears the loaded frames and
  // any plan on screen but keeps the drafts: they are committed-but-unwritten
  // work, and ⌘S is what lets go of them — unless the folder context changes
  // underneath them, which the effect above handles.
  let lastExifKey = "";
  $effect(() => {
    if (shell.mode !== "cull") {
      if (lastExifKey !== "") {
        lastExifKey = "";
        exifState.frames = [];
        exifState.plan = null;
      }
      return;
    }
    const paths = app.targets
      .map((g) => (g.hasJpeg && g.jpegPath !== "" ? g.jpegPath : g.rawPath))
      .filter((p) => p !== "");
    const wanted = paths.join("\n");
    if (wanted === lastExifKey) return;
    lastExifKey = wanted;
    if (wanted === "") {
      exifState.frames = [];
      return;
    }
    void exifState.load(paths);
  });

  // The filter narrows what the whole app sees: the grid, focus movement,
  // selection targets and the apply flow all read app.groups, so applying it
  // here keeps every one of them consistent. Focus follows the frame it was
  // on when that frame survives the filter.
  $effect(() => {
    const vis = visibleGroups();
    untrack(() => {
      const focused = app.groups[app.focusIndex];
      const key = focused === undefined ? null : groupKey(focused);
      app.groups = vis;
      if (key !== null) {
        const kept = vis.findIndex((g) => groupKey(g) === key);
        app.focusIndex = kept >= 0 ? kept : Math.max(0, Math.min(app.focusIndex, vis.length - 1));
      } else {
        app.focusIndex = 0;
      }
    });
  });

  function escape() {
    // Geotagging is the frontmost thing when it is up: back out of the confirm,
    // then out of armed placement, before anything else unwinds.
    if (geotag.plan !== null) {
      geotag.cancel();
      return;
    }
    if (geotag.armed) {
      geotag.disarm();
      return;
    }
    if (settings.open) {
      settings.open = false;
      return;
    }
    // The rejects dialog is the one permanent-deletion prompt, so Esc must
    // always close it even when the pointer has taken focus off its panel.
    if (rejects.open) {
      rejects.cancel();
      return;
    }
    if (exifState.plan !== null) {
      exifState.plan = null;
      return;
    }
    if (library.storageOpen) {
      library.storageOpen = false;
      return;
    }
    if (app.compare !== null) {
      app.compare = null;
      return;
    }
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
      if (shell.mode === "cull") shell.setLayout(CONTACT_SHEET);
      return;
    }
    // Search comes after the loupe: a result opened full-frame gives the loupe
    // back first, and the second Esc gives the folder back.
    if (library.searchOpen) {
      library.closeSearch();
      return;
    }
    if (shell.releasePane()) return;
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
    if (library.treeRoots.length === 0) {
      app.notify("no folders yet — add one to start the tree");
      picker.focus();
      return;
    }
    tree.focus();
  }

  /**
   * While the search is up the grid is holding index results, and ⏎ on one
   * means "take me to it" rather than "apply what I have decided" — there is
   * nothing to apply to a folder that is not open.
   */
  function openFocusedResult() {
    const focused = app.groups[app.focusIndex];
    if (focused === undefined) {
      app.notify("nothing to open");
      return;
    }
    library.openAt(focused.dir, focused.hash);
  }

  function run(action: string) {
    // The registry runs everything it knows; the switch below keeps the
    // shell-owned behaviours (escape unwinding, layout cycling, apply flow)
    // that need this component's own state.
    switch (action) {
      case "escape":
      case "cycle-layout":
      case "apply":
      case "zoom":
        break; // shell-owned, handled below
      default:
        if (runAction(action)) return;
    }
    switch (action) {
      case "cycle-layout":
        cycleLayout();
        break;
      case "zoom":
        // The loupe is cull's; from MAP or IMPORT the key must not zoom a
        // loupe the user cannot see (app.view survives a mode switch), and
        // the map zooms with its own − and + keys.
        if (shell.mode !== "cull") break;
        if (app.view !== "loupe") {
          app.notify("zoom works in the loupe — Tab to open it");
          break;
        }
        if (app.zoom) app.resetZoom();
        else app.zoom = true;
        break;
      case "escape":
        escape();
        break;
      case "apply":
        // A geotag confirm is the frontmost plan when it is up.
        if (geotag.plan !== null) {
          void geotag.confirm();
          break;
        }
        // Outside cull, ⏎ means what the pane on screen advertises — never the
        // apply flow, which would put the trash confirm over a grid the user
        // cannot see. The map opens the focused frame; IMPORT's screens each
        // draw their own ⏎ button, so the press does what that button says.
        if (shell.mode === "map") {
          mapState.open();
          break;
        }
        if (shell.mode === "import") {
          if (shell.layout === 2) {
            if (importState.ready) void importState.execute();
          } else if (shell.layout === 0 || importState.hasUnrouted) {
            importState.review();
          }
          break;
        }
        if (shell.mode !== "cull") break;
        // ⏎ confirms a plan that is up, then applies whatever has been decided
        // — a session or a search is a scope you cull like a folder, now that
        // apply spans folders. Only with nothing decided does ⏎ fall back to
        // opening the focused result, which is what browsing a search wants.
        if (app.plan) void confirmApply();
        else if (app.pending.length > 0) void requestApply();
        else if (library.searchOpen) openFocusedResult();
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
      case "copy-path":
        void copyPath();
        break;
      case "add-root":
        void pickRoot();
        break;
      default:
        // Verdict, mask, rating, selection and focus movement are NOT handled
        // here. They are registry actions guarded by `culling` (or hasFrames),
        // and runAction above is their one dispatcher — reaching this switch
        // for one means the guard declined (not in cull, no frames, or a
        // palette is open), so it must do nothing. Handling them here as a
        // fallback drove the hidden grid from MAP and IMPORT: verdicts landed
        // on frames the user could not see, and ⌘A in MAP selected the whole
        // folder invisibly and armed "place N selected" over all of it.
        if (action in modeActions) {
          shell.setModeByIndex(modeActions[action]);
        } else if (action in paneActions) {
          shell.focusPane(paneActions[action]);
          // Focusing the left pane hands the keyboard to the tree, so the
          // arrows drive folders — the pane treatment alone changed nothing.
          if (paneActions[action] === "left" && shell.mode === "cull") void focusTree();
        } else if (action in layoutActions) {
          chooseLayout(layoutActions[action]);
        } else if (action in laterActions) {
          app.notify(laterActions[action]);
        }
    }
  }

  /**
   * handleCompareKey drives the compare overlay's own keys. Returns true when
   * it claimed the event. The keys are the ones the design draws on the
   * compare screen: switch side, this-one-wins, verdicts, pan lock, zoom,
   * arrow panning, and two ways out.
   */
  function handleCompareKey(e: KeyboardEvent): boolean {
    if (e.metaKey || e.ctrlKey || e.altKey) return false;
    switch (e.key) {
      case "Tab":
        compareApi.switchSide();
        return true;
      case "w":
        compareApi.wins();
        return true;
      case "k":
        compareApi.verdict("keep");
        return true;
      case "x":
        compareApi.verdict("cut");
        return true;
      case "l":
        compareApi.togglePanLock();
        return true;
      case "z":
        compareApi.toggleZoom();
        return true;
      case "ArrowLeft":
        compareApi.pan(-PAN_STEP, 0);
        return true;
      case "ArrowRight":
        compareApi.pan(PAN_STEP, 0);
        return true;
      case "ArrowUp":
        compareApi.pan(0, -PAN_STEP);
        return true;
      case "ArrowDown":
        compareApi.pan(0, PAN_STEP);
        return true;
      case "c":
      case "Escape":
        compareApi.exit();
        return true;
    }
    // Everything else is swallowed rather than reaching the hidden grid.
    return true;
  }

  function onKeydown(e: KeyboardEvent) {
    if (ownsKeys(e.target)) {
      // The path box and the tree run their own keyboards; Esc is the way back
      // out of both, and Tab is left alone so they stay reachable by tabbing.
      if (e.key === "Escape") (e.target as HTMLElement).blur();
      return;
    }
    // The map wrap (data-keys="map") claims its own keys — f, −/+, ⏎ — and
    // stops their propagation, so anything arriving here from inside it is
    // unclaimed and the global chords must keep working: ⌘Z, ⌃1, /, ?, Esc in
    // one press, and ⏎ for the geotag confirm, which the wrap deliberately
    // lets bubble while the dialog is up. Two keys stay out of the keymap
    // there: Tab, because it is the only way the pins are reached at all, and
    // the arrows, which are the map's no-op rather than a licence to drive
    // the hidden grid's focus from a screen that does not show it.
    if (inMapRegion(e.target) && (e.key === "Tab" || e.key.startsWith("Arrow"))) return;
    // Compare owns the keyboard while it is up: its keys are its own, not the
    // grid's underneath. Anything it does not claim — modifier chords included,
    // which would open the palette under the overlay and strand its focus — is
    // swallowed rather than leaking through to a screen the user cannot see.
    // Esc still exits: it is one of the keys compare claims.
    if (app.compare !== null) {
      handleCompareKey(e);
      e.preventDefault();
      return;
    }
    const action = lookup.get(eventSignature(e));
    if (action === undefined) return;
    e.preventDefault();

    // The plan panels and the overlay are modal in intent but not in focus:
    // they swallow everything except the keys that dismiss them. Without this
    // a verdict key would fall through to the grid behind the dialog.
    if (geotag.plan !== null && action !== "apply" && action !== "escape") return;
    if (app.plan && action !== "apply" && action !== "escape") return;
    if (exifState.plan !== null && action !== "escape") return;
    // Emptying rejects and the storage view are modal but take no DOM focus of
    // their own once the pointer leaves them, so without this every key drives
    // the grid behind them — and Enter behind the rejects dialog would run an
    // apply behind the one permanent-deletion prompt in the app.
    if (rejects.open && action !== "escape") return;
    if (library.storageOpen && action !== "escape") return;
    if (app.overlay && action !== "keymap-overlay" && action !== "escape") return;
    run(action);
  }

  function dimmed(pane: Pane): boolean {
    return shell.focusedPane !== null && shell.focusedPane !== pane;
  }
</script>

<svelte:window onkeydown={onKeydown} onblur={() => void flush()} onbeforeunload={() => void flush()} />

{#snippet focusHead(pane: Pane)}
  <div class="focus-head">
    <span class="fdot" aria-hidden="true"></span>
    <span class="fname">{shell.spec.panes[pane]} · focused</span>
    <span class="fesc">esc → grid</span>
  </div>
{/snippet}

{#snippet ghost(title: string, line: string, key: string, hint: string)}
  <div class="ghost">
    <p class="gtitle">{title}</p>
    <p class="gline">{line}</p>
    <p class="ghint"><span class="gkey">{key}</span> {hint}</p>
  </div>
{/snippet}

<div class="app" data-mode={shell.mode} data-layout={shell.layout}>
  <TitleBar onlayout={chooseLayout} oncommand={() => run("command-palette")} onpath={() => void copyPath()} />

  {#if library.searchOpen}
    <SearchBar />
  {/if}

  {#if app.error !== ""}
    <div class="error" role="alert" title={app.error}>{app.error}</div>
  {/if}

  <div class="body">
    <section
      class="pane left"
      class:collapsed={!app.sidebar}
      class:focused={shell.focusedPane === "left"}
      class:dim={dimmed("left")}
    >
      {#if shell.focusedPane === "left"}{@render focusHead("left")}{/if}
      <div class="pane-body">
        <!-- The scope picker is the same in PHOTOS and MAP: one folder tree
             and session list that sets what every mode shows. MAP keeps its
             own left-pane content below it — the places on the map — under a
             divider the user can drag and that is remembered; PHOTOS is the
             picker alone. -->
        {#if shell.mode === "import"}
          <ImportLeft />
        {:else if shell.mode === "map"}
          <VSplit storageKey="culler.split.map-left">
            {#snippet top()}<Sidebar bind:path />{/snippet}
            {#snippet bottom()}<MapLeft layout={shell.layout} />{/snippet}
          </VSplit>
        {:else}
          <Sidebar bind:path />
        {/if}
      </div>
    </section>

    <section class="pane centre" class:focused={shell.focusedPane === "centre"} class:dim={dimmed("centre")}>
      {#if shell.focusedPane === "centre"}{@render focusHead("centre")}{/if}
      <div class="pane-body">
        {#if shell.mode === "import"}
          <ImportCentre
            layout={shell.layout}
            onreview={(dir) => {
              shell.setMode("cull");
              void openFolderAction(dir);
            }}
          />
        {:else if shell.mode === "map"}
          <MapCentre layout={shell.layout} />
        {:else if shell.mode !== "cull"}
          {@render ghost(shell.spec.label, `${shell.layoutLabel} comes later`, "⌃1", "back to cull")}
        {:else if app.folder === null && app.scanning === null}
          <ColdStart />
        {:else if app.allGroups.length === 0 && app.scanning !== null}
          <!-- Scanning, nothing painted yet: the full loader until the first
               batch of frames arrives. -->
          <Loader />
        {:else if app.allGroups.length === 0}
          <div class="empty">
            <p class="where" title={app.folder?.dir}>No photos in {app.folder?.dir}</p>
          </div>
        {:else}
          <!-- Frames exist (or are arriving). The strip shows above whichever
               layout is up while the walk is still running. -->
          {#if app.scanning !== null}
            <div class="scan-strip" role="status">
              <span class="scan-dot" aria-hidden="true"></span>
              still scanning — {app.allGroups.length} frame{app.allGroups.length === 1 ? "" : "s"} so far
            </div>
          {/if}
          {#if app.groups.length === 0}
            <div class="empty">
              <p class="hint">No frames match the filter. Press F to change it.</p>
            </div>
          {:else if shell.layout === 1}
            <LoupeFirst />
          {:else if shell.layout === 2}
            <TableView
              groups={app.groups}
              focusIndex={app.focusIndex}
              onFocus={(i) => (app.focusIndex = i)}
              onActivate={() => (app.view = "loupe")}
              isSelected={(g) => app.selection.has(groupKey(g))}
              preview={false}
              cutRemoves={app.cutRemoves}
            />
          {:else}
            <Grid />
          {/if}
        {/if}
      </div>
    </section>

    <section class="pane right" class:focused={shell.focusedPane === "right"} class:dim={dimmed("right")}>
      {#if shell.focusedPane === "right"}{@render focusHead("right")}{/if}
      <div class="pane-body">
        {#if shell.mode === "cull"}
          <Inspector />
        {:else if shell.mode === "import"}
          <ImportRight />
        {:else if shell.mode === "map"}
          <MapRight />
        {:else}
          {@render ghost(shell.spec.panes.right, "this pane comes later", "⌃1", "back to cull")}
        {/if}
      </div>
    </section>

    <!-- Inside .body deliberately: the overlay covers the three panes and no
         more, so the title bar and the status bar stay visible and clickable
         — which is where the mode chip and the verdict keys are named, and
         where the window is dragged from. -->
    {#if app.view === "loupe" && shell.mode === "cull" && shell.layout !== 1 && app.folder !== null && app.groups.length > 0}
      <LoupeOverlay />
    {/if}
  </div>

  {#if app.compare !== null}
    <div class="overlay-fill">
      <CompareView
        groups={app.compare}
        onverdict={(frames, v) => setVerdictFor(frames, v)}
        onexit={() => (app.compare = null)}
        cutRemoves={app.cutRemoves}
      />
    </div>
  {/if}

  {#if settings.open}
    <SettingsView />
  {/if}

  {#if library.storageOpen}
    <StorageView />
  {/if}

  <Palettes />
  <WritePlanDialog />
  <RejectsDialog />
  <GeotagDialog />

  <ApplyBar />
  <StatusBar />
</div>

{#if app.overlay}
  <KeymapOverlay />
{/if}
<Toast />

<style>
  .app {
    /* The containing block for every absolute overlay — the loupe, compare,
       settings — so `inset: 0` covers the window and not the viewport. */
    position: relative;
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
    background: var(--bg-window);
    --wails-draggable: no-drag;
  }

  /* Compare's body is written as a pane; this makes it cover the shell like
     every other overlay rather than splitting the window in half. */
  .overlay-fill {
    position: absolute;
    inset: 0;
    z-index: 45;
    display: flex;
  }

  .error {
    flex: 0 0 auto;
    padding: 6px 14px;
    background: var(--cut-wash-14);
    border-bottom: 1px solid var(--cut);
    color: var(--cut);
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .body {
    flex: 1;
    min-height: 0;
    display: flex;
    /* The containing block for the loupe overlay, which covers the panes and
       leaves the title bar and status bar showing. */
    position: relative;
  }

  .pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
    /* The panes are separated by one rule each, not two. */
    border: 0 solid var(--border);
    border-right-width: 1px;
  }

  .pane.left {
    flex: 0 0 auto;
    width: 208px;
    background: var(--bg-pane);
  }

  .pane.left.collapsed {
    width: 30px;
  }

  .pane.centre {
    flex: 1;
    background: var(--bg-window);
  }

  .pane.right {
    flex: 0 0 auto;
    width: 296px;
    background: var(--bg-pane);
    border-right-width: 0;
    border-left-width: 1px;
  }

  .pane-body {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }


  /* Focus at pane scale is the one place the shell shows it: the surface
     lifts, the border warms, and a strip says which pane has the keyboard. */
  .pane.focused {
    background: var(--bg-raised);
    border-color: var(--border-pane-focus);
    box-shadow: var(--focus-inset-2);
  }

  .pane.dim {
    opacity: 0.72;
  }

  .focus-head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 8px;
    background: var(--accent-wash-14);
    font-size: 10px;
    white-space: nowrap;
    overflow: hidden;
  }

  .fdot {
    flex: 0 0 auto;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent);
  }

  .fname {
    flex: 1;
    min-width: 0;
    color: var(--accent);
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .fesc {
    flex: 0 0 auto;
    color: var(--text-on-focus-hint);
  }

  .ghost {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 2px;
    padding: 0 16px;
    text-align: center;
    color: var(--text-ghost);
    font-size: 11px;
    line-height: 1.7;
  }

  .ghost p {
    margin: 0;
    max-width: 100%;
  }

  .gtitle {
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .ghint {
    margin-top: 10px;
    font-size: 10px;
  }

  .gkey {
    color: var(--text-dim);
  }

  .scan-strip {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 4px 12px;
    font-size: 11px;
    color: var(--text-muted);
    border-bottom: 1px solid var(--border);
    background: var(--bg-chrome);
  }

  .scan-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    animation: scan-pulse 1.2s ease-in-out infinite;
  }

  @keyframes scan-pulse {
    50% {
      opacity: 0.25;
    }
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
    font-size: 12px;
  }

  .empty p {
    margin: 0;
    max-width: 100%;
  }

  .empty .where {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
