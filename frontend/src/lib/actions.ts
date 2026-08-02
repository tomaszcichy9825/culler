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

export async function openFolder(dir: string) {
  if (dir.trim() === "") return;
  // Decisions from the folder being left have to land before it is replaced.
  await flush();
  app.busy = true;
  try {
    const folder = await LibraryService.OpenFolder(dir.trim());
    app.setFolder(folder);
    app.view = "grid";
    app.resetZoom();
    rememberFolder(folder.dir);
    if ((folder.groups ?? []).length === 0) app.notify("no photos in that folder");
  } catch (err) {
    app.error = message(err);
  } finally {
    app.busy = false;
  }
}

/** reload rescans the open folder, picking up whatever an apply changed. */
export async function reload() {
  const dir = app.folder?.dir;
  if (!dir) return;
  try {
    const folder = await LibraryService.OpenFolder(dir);
    app.setFolder(folder);
  } catch (err) {
    app.error = message(err);
  }
}

/**
 * requestApply plans the pending decisions and puts the summary up for
 * confirmation. Nothing has touched the disk when this resolves.
 */
export async function requestApply() {
  const dir = app.folder?.dir;
  if (!dir) return;
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
    await reload();
    app.clearSelection();
    if (failed > 0) app.notify(`${failed} action(s) failed; their frames kept their decision`, "error");
    else app.notify(batch.description || "applied");
  } catch (err) {
    app.plan = null;
    await reload();
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
