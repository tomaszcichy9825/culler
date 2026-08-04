// The things the keyboard layer and the buttons both call. Everything that
// talks to the backend goes through here so the busy flag, the error banner
// and the toast are handled in one place.
//
// The bottom of this file is the action registry: every action the user can
// take, declared once, with the code that runs it. The keymap dispatch and the
// command palette both read that one list, so an action cannot exist on a key
// without also being reachable from the palette.

import { tick } from "svelte";

import { Clipboard, Events } from "@wailsio/runtime";

import { ApplyService, ConfigService, LibraryService, XMPExportService } from "./bindings";
import { flush, message, setRating, setVerdict, toggleMask } from "./decisions";
import { exifState } from "./exif.svelte";
import { frameToGroup, library } from "./library.svelte";
import type { GroupDTO } from "./bindings";
import type { CatalogFrame } from "./library.svelte";
import { palette } from "./palette.svelte";
import { rejects } from "./rejects.svelte";
import { settings } from "./settings.svelte";
import { CONTACT_SHEET, LOUPE_FIRST, MODES, shell } from "./shell.svelte";
import type { Pane } from "./shell.svelte";
import { app, applyHashPatches, DEFAULT_SLOW_SCAN_SECONDS, loupe, picker, tree } from "./state.svelte";
import type { HashPatch } from "./state.svelte";
import { MAX_RATING } from "./verdict";
import type { Half } from "./verdict";

/** Where the last opened folder is remembered, so a relaunch lands back in it. */
const LAST_FOLDER = "culler.lastFolder";
/**
 * Where the tree's folders used to live. The catalogue holds them now; this is
 * read once, handed over, and cleared — see migrateRoots.
 */
const ROOTS = "culler.roots";

export function lastFolder(): string {
  try {
    return localStorage.getItem(LAST_FOLDER) ?? "";
  } catch {
    return "";
  }
}

/**
 * migrateRoots hands the folders a previous version kept in local storage to
 * the catalogue, once, and forgets them. From then on the catalogue is the
 * only place the sidebar's folders live: a root registered on one launch is
 * still there on the next without the frontend remembering anything.
 *
 * A migration that cannot write to storage still registers the roots. The
 * worst case is doing it again next launch, and registering a root the
 * catalogue already covers is a no-op.
 */
export async function migrateRoots() {
  let saved: string[] = [];
  try {
    const raw = localStorage.getItem(ROOTS);
    const parsed: unknown = raw === null ? [] : JSON.parse(raw);
    saved = Array.isArray(parsed) ? parsed.filter((p): p is string => typeof p === "string") : [];
  } catch {
    saved = [];
  }

  await library.loadRoots();
  if (saved.length > 0) await library.adopt(saved);
  try {
    localStorage.removeItem(ROOTS);
  } catch {
    // A webview with storage disabled still culls fine; it just repeats this.
  }
}

function rememberFolder(dir: string) {
  try {
    localStorage.setItem(LAST_FOLDER, dir);
  } catch {
    // See migrateRoots.
  }
}

/**
 * addRoot brings a folder into the catalogue so the sidebar can get back to
 * it. Opening a folder does this on its own, so this is the announcement of an
 * intent rather than the only route in: a folder already covered by a root is
 * a no-op, and nothing is indexed until a pass is asked for.
 */
export function addRoot(dir: string) {
  void library.registerIfNew(dir);
}

/** pickRoot opens the native chooser and adds what comes back. */
export async function pickRoot() {
  try {
    const chosen = await LibraryService.PickFolder();
    if (chosen === "") return;
    // addRoot registers and indexes: a folder the user deliberately added is
    // one they want counted, which is not true of one they merely opened.
    await library.addRoot(chosen);
    await openFolder(chosen);
  } catch (err) {
    app.notify(`could not open the folder chooser: ${message(err)}`, "error");
  }
}

/**
 * Bindings for actions the configuration does not know about yet. The Go
 * config owns the keymap, so anything added here is a fallback only: a chord
 * the user has already bound to something else keeps its configured meaning.
 */
const FRONTEND_BINDINGS: Record<string, string[]> = {
  // Modes come first deliberately. Off macOS "ctrl+n" and "mod+n" are the same
  // chord, and where the two collide the mode is the one that should answer —
  // panes keep their shift+mod alternative for exactly that case.
  "mode-cull": ["ctrl+1"],
  "mode-exif": ["ctrl+2"],
  "mode-map": ["ctrl+3"],
  "mode-import": ["ctrl+4"],
  "pane-left": ["mod+1", "shift+mod+1"],
  "pane-centre": ["mod+2", "shift+mod+2"],
  "pane-right": ["mod+3", "shift+mod+3"],
  "layout-1": ["alt+1"],
  "layout-2": ["alt+2"],
  "layout-3": ["alt+3"],
  "toggle-sidebar": ["s"],
  "focus-path": ["o"],
  "focus-tree": ["t"],
  "add-root": ["shift+o"],
  "copy-path": ["y"],
};

/**
 * loadSettings reads the configuration the UI depends on — the key bindings,
 * the slow-scan threshold and the culling semantics — in a single call.
 * Failure is survivable: the stock bindings and defaults apply and the user is
 * told.
 */
/**
 * The stock bindings, mirroring the backend's DefaultKeymap. They are the
 * floor: when the config cannot be read the app is still fully usable rather
 * than left with keys that only change modes.
 */
const DEFAULT_KEYMAP: Record<string, string[]> = {
  "focus-left": ["ArrowLeft"],
  "focus-right": ["ArrowRight"],
  "focus-up": ["ArrowUp"],
  "focus-down": ["ArrowDown"],
  "cycle-layout": ["Tab"],
  "toggle-loupe": ["space"],
  "toggle-select": ["s"],
  "select-all": ["mod+a"],
  escape: ["Escape"],
  "verdict-keep": ["k"],
  "verdict-cut": ["x"],
  "mask-toggle-raw": ["r"],
  "mask-toggle-jpeg": ["j"],
  "rate-1": ["1"],
  "rate-2": ["2"],
  "rate-3": ["3"],
  "rate-4": ["4"],
  "rate-5": ["5"],
  "rate-clear": ["0"],
  "copy-palette": ["c"],
  "move-palette": ["m"],
  "filter-palette": ["f"],
  zoom: ["z"],
  apply: ["Enter"],
  undo: ["mod+z"],
  redo: ["shift+mod+z"],
  "command-palette": ["mod+k"],
  search: ["/"],
  "keymap-overlay": ["?"],
  "open-settings": ["mod+,"],
  "enter-compare": ["shift+c"],
  "write-metadata": ["mod+s"],
};

export async function loadSettings() {
  const keymap: Record<string, string[]> = { ...DEFAULT_KEYMAP };
  let slowScanSeconds = DEFAULT_SLOW_SCAN_SECONDS;
  try {
    const cfg = await ConfigService.Get();
    for (const [action, chords] of Object.entries(cfg.keymap ?? {})) {
      keymap[action] = chords ?? [];
    }
    // The backend rejects anything below 1, so a smaller value here means a
    // payload that never came from a valid config.
    const configured = cfg.behaviour?.slowScanHintSeconds;
    if (typeof configured === "number" && configured >= 1) slowScanSeconds = configured;

    // These two decide what a verdict means, so the badges are wrong without
    // them. An unrecognised value keeps the default rather than being trusted.
    const mask = cfg.behaviour?.defaultKeepMask;
    if (mask === "rj" || mask === "r" || mask === "j") app.defaultKeepMask = mask;
    const cut = cfg.behaviour?.cutRemoves;
    if (cut === "both" || cut === "masked") app.cutRemoves = cut;
  } catch (err) {
    app.notify(`could not read settings: ${message(err)}`, "error");
  }
  app.slowScanSeconds = slowScanSeconds;

  const taken = new Set(Object.values(keymap).flat());
  for (const [action, chords] of Object.entries(FRONTEND_BINDINGS)) {
    if (action in keymap) continue;
    const free = chords.filter((c) => !taken.has(c));
    if (free.length > 0) keymap[action] = free;
  }
  app.keymap = keymap;
}

/**
 * Scans are serialised by this counter rather than by cancelling: a folder
 * switch while a slow one is still running must not have the older scan's
 * result land on top of the newer one's. The last request issued is the only
 * one allowed to touch the state, whichever order they come back in.
 */
let scanSeq = 0;

interface ScanOptions {
  /** Whether to record the folder as the one to reopen next launch. */
  remember: boolean;
  /** Whether to reset the view and say something about an empty folder. */
  announce: boolean;
}

async function scan(dir: string, { remember, announce }: ScanOptions) {
  const seq = ++scanSeq;
  const target = dir.trim();
  if (target === "") return;

  app.busy = true;
  app.scanning = target;
  app.scanSlow = false;
  app.scanProgress = null;
  app.scanProgressDir = null;
  const slow = setTimeout(() => {
    if (seq === scanSeq) app.scanSlow = true;
  }, app.slowScanSeconds * 1000);

  try {
    // Decisions from the folder being left have to land before it is replaced.
    await flush();
    const ticket = await LibraryService.OpenFolderStream(target);
    // Two guards, because two clocks disagree. scanSeq is this side's order of
    // intent; ticket.seq is the backend's order of begin(), which is what
    // actually decides whose stream it cancelled. A ticket the backend has
    // already superseded would install a token it will never emit against, so
    // it is dropped even if it is still the newest this side asked for.
    if (seq !== scanSeq || ticket.seq < installedSeq) {
      clearTimeout(slow);
      return;
    }
    installedSeq = ticket.seq;
    clearTimeout(watchdog);
    // A stream that goes silent — a scan the backend cancelled without a done,
    // or one that stalled — must not leave the UI busy forever.
    watchdog = setTimeout(() => {
      if (stream?.token === ticket.token && seq === scanSeq) {
        stream = null;
        app.busy = false;
        app.scanning = null;
        app.scanSlow = false;
        app.notify("the scan stopped responding — try opening the folder again", "error");
      }
    }, SCAN_WATCHDOG_MS);
    // The stream owns the state from here: frames and identities arrive as
    // events carrying this token, and the done event ends the scan. The slow
    // timer is handed over with the rest.
    stream = { token: ticket.token, seq, dir: ticket.dir, announce, slow, watchdog };
    app.beginStreamedFolder(ticket.dir, ticket.network);
    app.network[ticket.dir] = ticket.network;
    if (announce) {
      app.view = "grid";
      app.resetZoom();
    }
    if (remember) rememberFolder(ticket.dir);
  } catch (err) {
    clearTimeout(slow);
    if (seq !== scanSeq) return;
    app.error = message(err);
    app.busy = false;
    app.scanning = null;
    app.scanSlow = false;
    app.scanProgress = null;
    app.scanProgressDir = null;
  }
}

/** The highest backend begin-sequence installed, so a superseded one is dropped. */
let installedSeq = 0;
/** Cleared and re-armed on every open; fires if a stream goes silent. */
let watchdog: ReturnType<typeof setTimeout> | undefined;
const SCAN_WATCHDOG_MS = 60_000;

/**
 * The stream the state currently belongs to. Events carrying any other token
 * are a scan the user has already left and change nothing.
 */
let stream: {
  token: string;
  seq: number;
  dir: string;
  announce: boolean;
  slow: ReturnType<typeof setTimeout>;
  watchdog: ReturnType<typeof setTimeout>;
} | null = null;

/** A frame a caller wants focused once the stream that owns it identifies it. */
let pendingFocus: { token: string; hash: string } | null = null;

/** focusWhenHashed asks for hash to be focused, bound to the current stream. */
function focusWhenHashed(hash: string) {
  if (stream === null) return;
  pendingFocus = { token: stream.token, hash };
  tryPendingFocus();
}

/**
 * tryPendingFocus focuses the awaited frame once it has arrived and been
 * identified — but only while the stream that requested it is still the one
 * on screen, and searching the whole folder rather than a filtered view so a
 * live filter cannot hide the target.
 */
function tryPendingFocus() {
  if (pendingFocus === null || stream === null || pendingFocus.token !== stream.token) return;
  const i = app.allGroups.findIndex((g) => g.hash !== "" && g.hash === pendingFocus?.hash);
  if (i >= 0) {
    app.setFocus(i);
    pendingFocus = null;
  }
}

/**
 * streamFrames is the list the running scan fills. It is the grid's own list,
 * unless a search has taken the grid over — then the folder's frames are held
 * in the search buffer and the stream keeps filling that, so the frames it
 * finds are all there when the search closes.
 */
function streamFrames(frames: GroupDTO[]) {
  if (library.searchOpen) preSearchGroups = [...preSearchGroups, ...frames];
  else app.appendFrames(frames);
}

function streamHashes(patches: HashPatch[]) {
  if (library.searchOpen) applyHashPatches(preSearchGroups, patches);
  else app.patchHashes(patches);
}

/**
 * watchScanStream subscribes to the streamed open for the life of the app:
 * frames paint the grid as the walk finds them, identities and decisions land
 * on top, and the done event ends the scan. Call it once, at startup.
 */
export function watchScanStream() {
  Events.On("scan:frames", (event) => {
    const batch = event.data;
    if (!batch || stream === null || batch.token !== stream.token) return;
    streamFrames(batch.frames ?? []);
  });
  Events.On("scan:hashed", (event) => {
    const batch = event.data;
    if (!batch || stream === null || batch.token !== stream.token) return;
    streamHashes(batch.frames ?? []);
    tryPendingFocus();
  });
  Events.On("scan:done", (event) => {
    const done = event.data;
    if (!done || stream === null || done.token !== stream.token) return;
    const s = stream;
    stream = null;
    clearTimeout(s.slow);
    clearTimeout(s.watchdog);
    if (pendingFocus?.token === s.token) pendingFocus = null;
    // Only the newest scan clears the indicator; a superseded one leaving
    // would blank the state the live scan is still using.
    if (s.seq === scanSeq) {
      app.busy = false;
      app.scanning = null;
      app.scanSlow = false;
      app.scanProgress = null;
      app.scanProgressDir = null;
      const count = library.searchOpen ? preSearchGroups.length : app.allGroups.length;
      if (done.error !== "") app.error = done.error;
      else if (s.announce && count === 0) app.notify("no photos in that folder");
    }
  });
}

export async function openFolder(dir: string, focusHash?: string) {
  await scan(dir, { remember: true, announce: true });
  // A caller that names a frame — a search result jumping into the grid — gets
  // it focused as soon as the stream identifies it. A stale request (the user
  // switched folders mid-flight) is dropped with its stream.
  if (focusHash !== undefined) focusWhenHashed(focusHash);
  // A folder the user has opened is one the sidebar should be able to show
  // them again, so it joins the catalogue unless a root already covers it.
  // Quiet and cheap: it registers the root and redraws the top of the tree,
  // and leaves indexing to the pass the user asks for.
  const opened = app.folder?.dir;
  if (opened !== undefined && opened !== "") void library.registerIfNew(opened);
}

/**
 * reload rescans a folder, picking up whatever an apply changed. It defaults
 * to the open one, and does nothing if that folder has since been left.
 */
export async function reload(dir?: string) {
  const target = dir ?? app.folder?.dir;
  if (!target || app.folder?.dir !== target) return;
  await scan(target, { remember: false, announce: false });
}

/**
 * markNetwork looks up whether a path is on a network volume, once. The
 * answer is cached because it costs a statfs and a root does not move.
 */
export async function markNetwork(path: string) {
  if (path in app.network) return;
  try {
    app.network[path] = await LibraryService.IsNetwork(path);
  } catch {
    // Not knowing is the same as no badge; it is a hint, not a guarantee.
    app.network[path] = false;
  }
}

/**
 * requestApply plans the pending decisions and puts the summary up for
 * confirmation. Nothing has touched the disk when this resolves.
 */
export async function requestApply() {
  const dir = app.folder?.dir;
  if (!dir) return;
  // A plan describes the folder as it is now; planning against one that is
  // mid-rescan would describe a folder about to be replaced.
  if (app.scanning !== null) {
    app.notify("still scanning — hold on");
    return;
  }
  const hashes = app.pending.map((g) => g.hash).filter((h) => h !== "");
  if (hashes.length === 0) {
    app.notify("nothing to apply");
    return;
  }
  await flush();
  app.busy = true;
  try {
    app.plan = await ApplyService.Plan(dir, hashes);
  } catch (err) {
    app.notify(`could not plan: ${message(err)}`, "error");
  } finally {
    app.busy = false;
  }
}

/** confirmApply executes the plan on screen and reloads the folder. */
export async function confirmApply() {
  const dir = app.folder?.dir;
  if (!dir || !app.plan) return;
  const hashes = app.pending.map((g) => g.hash).filter((h) => h !== "");
  app.busy = true;
  try {
    const batch = await ApplyService.Apply(dir, hashes);
    const failed = (batch.actions ?? []).filter((a) => a.outcome !== "ok").length;
    app.plan = null;
    // Refresh the folder that was applied to, not whatever is open by now:
    // a folder switch during the apply must not pull the user back.
    await reload(dir);
    app.clearSelection();
    if (failed > 0) app.notify(`${failed} action(s) failed; their frames kept their decision`, "error");
    else app.notify(batch.description || "applied");
  } catch (err) {
    app.plan = null;
    await reload(dir);
    app.notify(`apply failed: ${message(err)}`, "error");
  } finally {
    app.busy = false;
  }
}

export function cancelApply() {
  app.plan = null;
}

export async function undo() {
  app.busy = true;
  try {
    await ApplyService.Undo();
    await reload();
    app.notify("undone");
  } catch (err) {
    app.notify(message(err), "error");
  } finally {
    app.busy = false;
  }
}

/**
 * belongsToScan decides whether a progress event is from the open currently
 * being waited on. Events carry the backend's resolved path while the user may
 * have typed a relative one or a ~, so an absolute target is matched exactly
 * and anything else locks on to the first path it sees. Either way, events
 * from an abandoned open are ignored rather than driving the bar backwards.
 */
function belongsToScan(eventDir: string, target: string): boolean {
  if (app.scanProgressDir !== null) return eventDir === app.scanProgressDir;
  return target.startsWith("/") ? eventDir === target : true;
}

/**
 * watchScanProgress subscribes to the backend's scan progress for the life of
 * the app. Call it once, at startup.
 */
export function watchScanProgress() {
  Events.On("scan:progress", (event) => {
    const progress = event.data;
    const target = app.scanning;
    if (!progress || target === null) return;
    if (!belongsToScan(progress.dir, target)) return;
    app.scanProgressDir = progress.dir;
    app.scanProgress = { done: progress.done, total: progress.total };
  });
}

/** copyPath puts the open folder's path on the clipboard. */
export async function copyPath() {
  const dir = app.folder?.dir;
  if (!dir) return;
  try {
    await navigator.clipboard.writeText(dir);
    app.notify("copied");
    return;
  } catch {
    // A webview that withholds the async clipboard still has the native one.
  }
  try {
    await Clipboard.SetText(dir);
    app.notify("copied");
  } catch (err) {
    app.notify(`could not copy: ${message(err)}`, "error");
  }
}

/**
 * Whether the previous call to showSearchResults had the search up, so that
 * closing it restores the folder exactly once rather than on every pass.
 */
let searchWasOpen = false;
/** The folder's frames set aside while search results occupy the grid. */
let preSearchGroups: GroupDTO[] = [];

/**
 * showSearchResults puts what the index answered onto the grid, and takes it
 * off again when the search closes.
 *
 * Results arrive as app.allGroups, which is where a folder's frames arrive, so
 * the filter, focus movement, the loupe and the table work on them without
 * knowing they came from the catalogue rather than a directory. Closing the
 * search hands the open folder back untouched: searching never loaded, left or
 * changed a folder, so there is nothing to restore but the list.
 */
export function showSearchResults(open: boolean, results: CatalogFrame[]) {
  if (open) {
    // The folder's frames are set aside while the results take the grid.
    // A streamed folder never keeps a second copy in folder.groups, so the
    // list itself is what has to be restored — there is nowhere else to read
    // it back from.
    if (!searchWasOpen) preSearchGroups = app.allGroups;
    app.allGroups = results.map(frameToGroup);
    app.focusIndex = Math.max(0, Math.min(app.focusIndex, results.length - 1));
  } else if (searchWasOpen) {
    app.allGroups = preSearchGroups;
    preSearchGroups = [];
    app.focusIndex = 0;
  }
  searchWasOpen = open;
}

/* ---- the shell's own actions ---- */

/** How far one arrow press pans the zoomed loupe, in image pixels. */
const PAN_STEP = 120;

function moveFocus(dx: number, dy: number) {
  if (app.view === "loupe" && app.zoom) {
    loupe.pan(-dx * PAN_STEP, -dy * PAN_STEP);
    return;
  }
  const rowStep = app.view === "loupe" ? 1 : app.cols;
  app.setFocus(app.focusIndex + dx + dy * rowStep);
}

/**
 * CULL's first two sub-layouts are the grid and one frame at a time, which the
 * app already has as its two views — so choosing a layout and the view have to
 * agree, or the segmented control starts lying.
 */
export function chooseLayout(index: number) {
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
 * Tab walks the current mode's sub-layouts in order, wrapping. The mode decides
 * how many there are, so nothing here knows that CULL has three.
 */
function cycleLayout() {
  const next = shell.nextLayout();
  if (next === null) return;
  chooseLayout(next);
}

/**
 * escape unwinds one level of whatever is up, innermost first. A palette is the
 * outermost thing on screen and handles its own Esc, so this only reaches it
 * when the keyboard has drifted off the dialog.
 */
function escape() {
  if (palette.open) {
    palette.close();
    return;
  }
  if (library.storageOpen) {
    library.storageOpen = false;
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

function toggleZoom() {
  if (app.view !== "loupe") {
    app.notify("zoom works in the loupe — Tab to open it");
    return;
  }
  if (app.zoom) app.resetZoom();
  else app.zoom = true;
}

/* ---- the action registry ---- */

/**
 * Action is one thing the user can do, declared once. The keymap binds ids to
 * chords and this list says what an id means, which is what makes every
 * binding reachable from the command palette without a second list to keep in
 * step.
 */
export interface Action {
  /** The name the config keymap binds a chord to. */
  id: string;
  /** As the palette names it. */
  label: string;
  /** The palette's section title. Sections appear in first-declared order. */
  group: string;
  /**
   * The dimmed note beside the name. A function for anything that depends on
   * the current mode or the open folder, so the row is right when it is drawn
   * rather than when it was declared.
   */
  note?: string | (() => string);
  /** One glyph for the palette's icon column. */
  icon?: string;
  run: () => void;
  /**
   * Whether the action can run at all right now. An action whose when() is
   * false is hidden from the palette and does nothing on its key: it is for
   * things that do not exist in the current context, not for things that would
   * fail and want to say why.
   */
  when?: () => boolean;
}

const NAVIGATE = "navigate";
const VERDICT = "verdict";
const RATING = "rating";
const SELECT = "select";
const VIEW = "view";
const MODE = "mode";
const PANES = "panes";
const FILES = "files";
const FOLDERS = "folders";
const APP = "app";

/** Whether there is anything on screen for a frame action to act on. */
function hasFrames(): boolean {
  return app.groups.length > 0;
}

/**
 * culling is true only when the grid is on screen with frames in it. The
 * decision keys, the loupe and compare all act on that grid, so running them
 * from EXIF or MAP or IMPORT — where the same frames are not shown — would
 * change state the user cannot see.
 */
function culling(): boolean {
  return shell.mode === "cull" && app.groups.length > 0;
}

function ratingActions(): Action[] {
  const rows: Action[] = [];
  for (let n = 1; n <= MAX_RATING; n++) {
    rows.push({
      id: `rate-${n}`,
      label: `${n} star${n === 1 ? "" : "s"}`,
      group: RATING,
      icon: "★",
      note: "again clears it",
      when: culling,
      run: () => setRating(n),
    });
  }
  return rows;
}

function maskAction(half: Half): Action {
  const name = half === "r" ? "RAW" : "JPEG";
  return {
    id: half === "r" ? "mask-toggle-raw" : "mask-toggle-jpeg",
    label: `keep or drop the ${name} half`,
    group: VERDICT,
    icon: half === "r" ? "R" : "J",
    note: "implies a keep",
    when: culling,
    run: () => toggleMask(half),
  };
}

function layoutAction(index: number): Action {
  return {
    id: `layout-${index + 1}`,
    label: `layout ${index + 1}`,
    group: VIEW,
    icon: "▤",
    note: () => shell.spec.layouts[index] ?? "",
    when: () => shell.spec.layouts.length > index,
    run: () => chooseLayout(index),
  };
}

function modeAction(index: number): Action {
  const spec = MODES[index];
  return {
    id: `mode-${spec.id}`,
    label: `${spec.label} mode`,
    group: MODE,
    icon: "◧",
    note: () => Object.values(spec.panes).join(" · "),
    run: () => shell.setModeByIndex(index),
  };
}

function paneAction(pane: Pane): Action {
  return {
    id: `pane-${pane}`,
    label: `focus the ${pane} pane`,
    group: PANES,
    icon: "▥",
    note: () => shell.spec.panes[pane],
    run: () => {
      shell.focusPane(pane);
      // The pane treatment alone changes nothing for the keyboard: focusing
      // the left pane hands the arrows to the folder tree.
      if (pane === "left" && shell.mode === "cull") void focusTree();
    },
  };
}

/**
 * ACTIONS is the whole vocabulary of the application, in the order the palette
 * shows it. Adding a user action means adding a row here; there is nowhere else
 * to add one.
 */
export const ACTIONS: Action[] = [
  { id: "focus-left", label: "move focus left", group: NAVIGATE, icon: "←", when: hasFrames, run: () => moveFocus(-1, 0) },
  { id: "focus-right", label: "move focus right", group: NAVIGATE, icon: "→", when: hasFrames, run: () => moveFocus(1, 0) },
  { id: "focus-up", label: "move focus up", group: NAVIGATE, icon: "↑", when: hasFrames, run: () => moveFocus(0, -1) },
  { id: "focus-down", label: "move focus down", group: NAVIGATE, icon: "↓", when: hasFrames, run: () => moveFocus(0, 1) },
  {
    id: "focus-path",
    label: "jump to the folder path box",
    group: NAVIGATE,
    icon: "›",
    note: "type a path, ↩ opens it",
    run: () => void focusPath(),
  },
  {
    id: "focus-tree",
    label: "jump to the folder tree",
    group: NAVIGATE,
    icon: "›",
    note: "arrows move, ↩ opens",
    run: () => void focusTree(),
  },

  {
    id: "verdict-keep",
    label: "keep the frame",
    group: VERDICT,
    icon: "✓",
    note: "again clears it",
    when: culling,
    run: () => setVerdict("keep"),
  },
  {
    id: "verdict-cut",
    label: "cut the frame",
    group: VERDICT,
    icon: "✕",
    note: "again clears it",
    when: culling,
    run: () => setVerdict("cut"),
  },
  maskAction("r"),
  maskAction("j"),

  ...ratingActions(),
  { id: "rate-clear", label: "clear the rating", group: RATING, icon: "☆", when: culling, run: () => setRating(0) },

  { id: "toggle-select", label: "toggle selection", group: SELECT, icon: "▣", when: culling, run: () => app.toggleSelect() },
  {
    id: "toggle-loupe",
    label: "loupe the focused frame",
    group: VIEW,
    icon: "◎",
    note: "space closes it again",
    when: culling,
    run: () => (app.view = app.view === "loupe" ? "grid" : "loupe"),
  },
  { id: "select-all", label: "select every frame", group: SELECT, icon: "▦", when: culling, run: () => app.selectAll() },
  {
    id: "escape",
    label: "back out",
    group: SELECT,
    icon: "⎋",
    note: "clears the selection, leaves the loupe",
    run: escape,
  },

  { id: "cycle-layout", label: "cycle layout", group: VIEW, icon: "▤", note: () => shell.layoutLabel, run: cycleLayout },
  { id: "zoom", label: "1:1 zoom", group: VIEW, icon: "⊕", note: "in the loupe", run: toggleZoom },
  layoutAction(0),
  layoutAction(1),
  layoutAction(2),
  { id: "toggle-sidebar", label: "show or hide the sidebar", group: VIEW, icon: "▯", run: () => (app.sidebar = !app.sidebar) },

  modeAction(0),
  modeAction(1),
  modeAction(2),
  modeAction(3),

  paneAction("left"),
  paneAction("centre"),
  paneAction("right"),

  {
    id: "apply",
    label: "apply pending verdicts",
    group: FILES,
    icon: "▶",
    note: () => (app.plan ? "confirm the plan on screen" : `${app.pending.length} frame(s) pending`),
    // Not while search results hold the grid: their frames belong to other
    // folders, so an apply here would plan foreign hashes against this one.
    // The keyboard's Enter opens the focused result instead (handled in App).
    when: () => app.folder !== null && !library.searchOpen,
    run: () => {
      if (app.plan) void confirmApply();
      else void requestApply();
    },
  },
  {
    id: "undo",
    label: "undo the last batch",
    group: FILES,
    icon: "↺",
    run: () => void flush().then(undo),
  },
  {
    id: "redo",
    label: "redo",
    group: FILES,
    icon: "↻",
    note: "there is no redo stack yet",
    run: () => app.notify("nothing to redo"),
  },
  {
    id: "move-palette",
    label: "move frames to a folder",
    group: FILES,
    icon: "⇢",
    note: "pick a destination",
    when: () => app.targets.length > 0,
    run: () => palette.toggle("move"),
  },
  {
    id: "copy-palette",
    label: "copy frames to a folder",
    group: FILES,
    icon: "⇉",
    note: "pick a destination",
    when: () => app.targets.length > 0,
    run: () => palette.toggle("copy"),
  },
  {
    id: "copy-path",
    label: "copy the open folder's path",
    group: FILES,
    icon: "⧉",
    when: () => app.folder !== null,
    run: () => void copyPath(),
  },

  { id: "add-root", label: "add a folder to the sidebar", group: FOLDERS, icon: "+", run: () => void pickRoot() },

  {
    id: "command-palette",
    label: "command palette",
    group: APP,
    icon: "›",
    note: "everything, searchable",
    run: () => palette.toggle("command"),
  },
  {
    id: "search",
    label: "search the catalogue",
    group: APP,
    icon: "⌕",
    note: "the index, on the grid — esc puts the folder back",
    run: () => library.toggleSearch(),
  },
  {
    id: "storage",
    label: "storage",
    group: APP,
    icon: "▦",
    note: "what every volume is holding",
    run: () => (library.storageOpen = true),
  },
  {
    id: "filter-palette",
    label: "filter the grid",
    group: APP,
    icon: "⛭",
    note: "by kind, verdict or rating",
    when: hasFrames,
    run: () => palette.toggle("filter"),
  },
  { id: "keymap-overlay", label: "keyboard shortcuts", group: APP, icon: "?", run: () => (app.overlay = !app.overlay) },
  {
    id: "export-xmp",
    label: "export XMP sidecars",
    group: FILES,
    icon: "✧",
    note: "verdicts and ratings, for Lightroom and Bridge",
    when: () => app.folder !== null,
    run: () =>
      void (async () => {
        try {
          const r = await XMPExportService.ExportFolder(app.folder?.dir ?? "");
          const cleared = r.cleared > 0 ? `, cleared ${r.cleared}` : "";
          const failed = r.failed > 0 ? ` — ${r.failed} failed` : "";
          app.notify(`wrote ${r.written} sidecar${r.written === 1 ? "" : "s"}${cleared}${failed}`, r.failed > 0 ? "error" : undefined);
        } catch (err) {
          app.notify(message(err), "error");
        }
      })(),
  },
  {
    id: "empty-rejects",
    label: "empty rejects…",
    group: FILES,
    icon: "⌫",
    note: "the only permanent deletion in the app",
    run: () => void rejects.request(),
  },
  {
    id: "open-settings",
    label: "settings",
    group: APP,
    icon: "⚙",
    note: "behaviours and shortcuts",
    run: () => (settings.open = true),
  },
  {
    id: "write-metadata",
    label: "write metadata changes",
    group: FILES,
    icon: "✎",
    note: "shows the write plan first",
    when: () => shell.mode === "exif" && exifState.dirtyFrames.length > 0,
    run: () => void exifState.requestWrite(),
  },
  {
    id: "enter-compare",
    label: "compare frames",
    group: VIEW,
    icon: "⇄",
    note: "the selection, or this frame and the next",
    when: () => shell.mode === "cull" && app.groups.length >= 2,
    run: enterCompare,
  },
];

/**
 * enterCompare pits the selection against each other, or, with nothing
 * selected, the focused frame against its neighbour — the burst-culling case.
 */
function enterCompare() {
  const selected = app.selected;
  if (selected.length >= 2) {
    app.compare = selected;
    return;
  }
  const i = app.focusIndex;
  const pair = app.groups.slice(i, i + 2);
  if (pair.length < 2) {
    app.notify("nothing to compare this frame with");
    return;
  }
  app.compare = pair;
}

const BY_ID = new Map(ACTIONS.map((a) => [a.id, a]));

export function actionOf(id: string): Action | undefined {
  return BY_ID.get(id);
}

/** noteOf resolves a note that may depend on the current state. */
export function noteOf(action: Action): string {
  return typeof action.note === "function" ? action.note() : (action.note ?? "");
}

/**
 * While a palette is up it is the only thing the keyboard drives. The palettes
 * mark themselves data-keys="local" so the global listener stays out of the
 * way, but a click that moves focus off the dialog would otherwise put the grid
 * back in charge behind a dialog the user can still see.
 */
const WHILE_PALETTE_OPEN = new Set(["escape", "command-palette", "copy-palette", "move-palette", "filter-palette"]);

/**
 * runAction is the single dispatch. It answers whether the action ran, so a
 * caller can tell an unknown id from one that declined.
 */
export function runAction(id: string): boolean {
  const action = BY_ID.get(id);
  if (action === undefined) return false;
  if (palette.open && !WHILE_PALETTE_OPEN.has(id)) return false;
  if (action.when !== undefined && !action.when()) return false;
  action.run();
  return true;
}

/** available lists the actions that could run right now, in declared order. */
export function available(): Action[] {
  return ACTIONS.filter((a) => a.when === undefined || a.when());
}
