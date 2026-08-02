// The things the keyboard layer and the buttons both call. Everything that
// talks to the backend goes through here so the busy flag, the error banner
// and the toast are handled in one place.

import { ApplyService, ConfigService, LibraryService } from "./bindings";
import { flush, message } from "./decisions";
import { app } from "./state.svelte";

/** Where the last opened folder is remembered, so a relaunch lands back in it. */
const LAST_FOLDER = "culler.lastFolder";
/** The tree's top-level folders, restored on launch. */
const ROOTS = "culler.roots";

export function lastFolder(): string {
  try {
    return localStorage.getItem(LAST_FOLDER) ?? "";
  } catch {
    return "";
  }
}

/** loadRoots restores the tree's top-level folders from the last session. */
export function loadRoots() {
  try {
    const raw = localStorage.getItem(ROOTS);
    const parsed: unknown = raw === null ? [] : JSON.parse(raw);
    app.roots = Array.isArray(parsed) ? parsed.filter((p): p is string => typeof p === "string") : [];
  } catch {
    app.roots = [];
  }
  for (const root of app.roots) void markNetwork(root);
}

function saveRoots() {
  try {
    localStorage.setItem(ROOTS, JSON.stringify(app.roots));
  } catch {
    // A webview with storage disabled still culls fine; it just forgets.
  }
}

function rememberFolder(dir: string) {
  try {
    localStorage.setItem(LAST_FOLDER, dir);
  } catch {
    // See saveRoots.
  }
}

/** trimTrailing drops trailing separators so paths compare consistently. */
function trimTrailing(p: string): string {
  return p.length > 1 ? p.replace(/\/+$/, "") : p;
}

/**
 * under reports whether child sits inside parent. The comparison is on whole
 * path segments, so /Volumes/CardTwo is not treated as living inside
 * /Volumes/Card.
 */
export function under(child: string, parent: string): boolean {
  const c = trimTrailing(child);
  const p = trimTrailing(parent);
  return c === p || c.startsWith(p === "/" ? "/" : `${p}/`);
}

/** addRoot adds dir as a top-level folder unless a root already covers it. */
export function addRoot(dir: string): boolean {
  const path = trimTrailing(dir);
  if (path === "" || app.roots.some((r) => under(path, r))) return false;
  // A new root that contains existing ones replaces them, so the tree does
  // not show the same folder at two levels.
  app.roots = [...app.roots.filter((r) => !under(r, path)), path];
  saveRoots();
  void markNetwork(path);
  return true;
}

export function removeRoot(dir: string) {
  app.roots = app.roots.filter((r) => r !== dir);
  // Drop everything cached beneath it: keeping it would quietly go stale.
  const expanded = new Set([...app.expanded].filter((p) => !under(p, dir)));
  app.expanded = expanded;
  for (const path of Object.keys(app.children)) {
    if (under(path, dir)) delete app.children[path];
  }
  delete app.network[dir];
  saveRoots();
}

/** pickRoot opens the native chooser and adds what comes back. */
export async function pickRoot() {
  try {
    const chosen = await LibraryService.PickFolder();
    if (chosen === "") return;
    addRoot(chosen);
    await openFolder(chosen);
  } catch (err) {
    app.notify(`could not open the folder chooser: ${message(err)}`, "error");
  }
}

/** listChildren fills a node's subdirectories, once. */
export async function listChildren(dir: string) {
  if (dir in app.children || app.loading.has(dir)) return;
  app.loading = new Set(app.loading).add(dir);
  try {
    app.children[dir] = (await LibraryService.ListDirs(dir)) ?? [];
  } catch (err) {
    // An unreadable folder is a leaf rather than a failure: the tree carries
    // on and the user still sees why.
    app.children[dir] = [];
    app.notify(`could not list ${dir}: ${message(err)}`, "error");
  } finally {
    const loading = new Set(app.loading);
    loading.delete(dir);
    app.loading = loading;
  }
}

export async function expandNode(dir: string) {
  if (app.expanded.has(dir)) return;
  app.expanded = new Set(app.expanded).add(dir);
  await listChildren(dir);
}

export function collapseNode(dir: string) {
  const expanded = new Set(app.expanded);
  expanded.delete(dir);
  app.expanded = expanded;
}

export async function toggleNode(dir: string) {
  if (app.expanded.has(dir)) collapseNode(dir);
  else await expandNode(dir);
}

/**
 * Bindings for actions the configuration does not know about yet. The Go
 * config owns the keymap, so anything added here is a fallback only: a chord
 * the user has already bound to something else keeps its configured meaning.
 */
const FRONTEND_BINDINGS: Record<string, string[]> = {
  "toggle-sidebar": ["s"],
  "focus-path": ["o"],
  "focus-tree": ["t"],
  "add-root": ["shift+o"],
};

/** loadKeymap reads the configured bindings, falling back to none on failure. */
export async function loadKeymap() {
  const keymap: Record<string, string[]> = {};
  try {
    const cfg = await ConfigService.Get();
    for (const [action, chords] of Object.entries(cfg.keymap ?? {})) {
      keymap[action] = chords ?? [];
    }
  } catch (err) {
    app.notify(`could not read settings: ${message(err)}`, "error");
  }

  const taken = new Set(Object.values(keymap).flat());
  for (const [action, chords] of Object.entries(FRONTEND_BINDINGS)) {
    if (action in keymap) continue;
    const free = chords.filter((c) => !taken.has(c));
    if (free.length > 0) keymap[action] = free;
  }
  app.keymap = keymap;
}

/** How long a scan runs before the UI says it is a slow one. */
const SLOW_SCAN_MS = 10_000;

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
  const slow = setTimeout(() => {
    if (seq === scanSeq) app.scanSlow = true;
  }, SLOW_SCAN_MS);

  try {
    // Decisions from the folder being left have to land before it is replaced.
    await flush();
    const folder = await LibraryService.OpenFolder(target);
    if (seq !== scanSeq) return;
    app.setFolder(folder);
    if (announce) {
      app.view = "grid";
      app.resetZoom();
    }
    if (remember) rememberFolder(folder.dir);
    if (announce && (folder.groups ?? []).length === 0) app.notify("no photos in that folder");
  } catch (err) {
    if (seq !== scanSeq) return;
    app.error = message(err);
  } finally {
    clearTimeout(slow);
    // Only the newest scan clears the indicator; a superseded one leaving
    // would blank the state the live scan is still using.
    if (seq === scanSeq) {
      app.busy = false;
      app.scanning = null;
      app.scanSlow = false;
    }
  }
}

export async function openFolder(dir: string) {
  await scan(dir, { remember: true, announce: true });
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
