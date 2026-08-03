// The whole UI reads from one store. It holds the opened folder, where focus
// and selection are, and the transient chrome (toast, plan panel, overlay).
//
// Verdicts live on the group objects themselves rather than in a parallel map:
// the backend already returns the recorded verdict, mask and rating with every
// frame, so a group's own fields are the single source of truth for what is
// pending. Persisting them is a separate concern — see decisions.ts.

import type { DirEntryDTO, FolderDTO, GroupDTO, PlanDTO } from "./bindings";
import { verdictOf } from "./verdict";
import type { CutScope, Mask } from "./verdict";

export type View = "grid" | "loupe";
export type Tone = "info" | "error";

export interface Toast {
  id: number;
  message: string;
  tone: Tone;
}

/** How long a toast stays up. Long enough to read, short enough not to nag. */
const TOAST_MS = 2200;

/**
 * Used until the configuration has been read, and if it cannot be. Matches the
 * backend's own default so the two never disagree in practice.
 */
export const DEFAULT_SLOW_SCAN_SECONDS = 10;

/** How many tiles a row of the contact sheet holds, and its limits. */
export const DEFAULT_COLUMNS = 5;
const MIN_COLUMNS = 2;
const MAX_COLUMNS = 12;

/**
 * groupKey identifies a frame for selection and for Svelte's keyed each.
 * The content hash is the real identity, but a frame whose primary file could
 * not be read has none, and it still has to be selectable.
 */
export function groupKey(g: GroupDTO): string {
  return g.hash !== "" ? g.hash : `${g.dir}/${g.stem}`;
}

class CullerState {
  folder = $state<FolderDTO | null>(null);
  groups = $state<GroupDTO[]>([]);
  focusIndex = $state(0);
  selection = $state<Set<string>>(new Set());
  anchor = $state<number | null>(null);

  view = $state<View>("grid");
  /** Loupe 1:1 zoom, with the pan offset it is viewed at. */
  zoom = $state(false);
  panX = $state(0);
  panY = $state(0);

  /**
   * Columns the grid is currently laid out in. The grid owns it; the key layer
   * needs it so vertical focus moves by a row.
   */
  cols = $state(DEFAULT_COLUMNS);

  /**
   * Columns the contact sheet is asked for, which is what the − and + keycaps
   * in the pane header change. Tiles divide the width between them, so asking
   * for fewer columns is how the sheet asks for bigger tiles.
   */
  gridColumns = $state(DEFAULT_COLUMNS);

  /**
   * Culling semantics, read from the config at startup: the mask a fresh keep
   * starts with, and how far a cut reaches. Both change what the tile badges
   * mean, so the UI has to hold them rather than assume the defaults.
   */
  defaultKeepMask = $state<Mask>("rj");
  cutRemoves = $state<CutScope>("both");

  /** Set while a backend call is in flight, so the chrome can say so. */
  busy = $state(false);
  /**
   * The folder currently being scanned, or null. A scan of a card over SMB can
   * take a long time, so the grid says what it is waiting for rather than
   * going blank.
   */
  scanning = $state<string | null>(null);
  /** Set once a scan has run long enough to be worth reassuring the user about. */
  scanSlow = $state(false);
  /**
   * How far the running scan has got, once the backend has said. Null until
   * the first progress event, which is why the loader starts indeterminate.
   */
  scanProgress = $state<{ done: number; total: number } | null>(null);
  /**
   * The resolved directory the current scan's progress events carry. The path
   * the user typed may be relative or start with ~, so the first event of a
   * scan establishes what its later events will look like.
   */
  scanProgressDir = $state<string | null>(null);
  /** Path -> whether it lives on a network volume, one lookup per root. */
  network = $state<Record<string, boolean>>({});
  /** A folder that would not open. Cleared on the next successful open. */
  error = $state("");
  /** The plan awaiting confirmation. Non-null means the panel is up. */
  plan = $state<PlanDTO | null>(null);
  overlay = $state(false);
  toast = $state<Toast | null>(null);

  /** Left sidebar: open by default, collapsible to give the grid the width. */
  sidebar = $state(true);

  /** Top-level folders of the tree. The user adds and removes these. */
  roots = $state<string[]>([]);
  /** Subdirectories per path, filled in lazily as nodes are expanded. */
  children = $state<Record<string, DirEntryDTO[]>>({});
  /** Paths whose children are showing. */
  expanded = $state<Set<string>>(new Set());
  /** Paths with a listing in flight, so a slow volume shows it is working. */
  loading = $state<Set<string>>(new Set());
  /** Which tree row holds the keyboard position. */
  treeIndex = $state(0);

  keymap = $state<Record<string, string[]>>({});
  /** How long a scan runs before the "still scanning" line appears, in seconds. */
  slowScanSeconds = $state(DEFAULT_SLOW_SCAN_SECONDS);

  #toastSeq = 0;
  #toastTimer: ReturnType<typeof setTimeout> | null = null;

  get focused(): GroupDTO | null {
    return this.groups[this.focusIndex] ?? null;
  }

  get selected(): GroupDTO[] {
    if (this.selection.size === 0) return [];
    return this.groups.filter((g) => this.selection.has(groupKey(g)));
  }

  /**
   * targets resolves what an action applies to, per the selection rules: a
   * selection wins, otherwise it is the focused frame alone.
   */
  get targets(): GroupDTO[] {
    const chosen = this.selected;
    if (chosen.length > 0) return chosen;
    const f = this.focused;
    return f ? [f] : [];
  }

  /** Every frame carrying a verdict, which is what an apply would act on. */
  get pending(): GroupDTO[] {
    return this.groups.filter((g) => verdictOf(g) !== "");
  }

  /** The three numbers the contact sheet's verdict legend shows. */
  get verdictCounts(): { keep: number; cut: number; undecided: number } {
    const counts = { keep: 0, cut: 0, undecided: 0 };
    for (const g of this.groups) {
      const v = verdictOf(g);
      if (v === "keep") counts.keep++;
      else if (v === "cut") counts.cut++;
      else counts.undecided++;
    }
    return counts;
  }

  /**
   * nudgeColumns resizes the tiles. It clamps rather than wrapping, so holding
   * − at the widest tile does nothing instead of jumping to the narrowest.
   */
  nudgeColumns(delta: number) {
    this.gridColumns = Math.max(MIN_COLUMNS, Math.min(MAX_COLUMNS, this.gridColumns + delta));
  }

  setFolder(folder: FolderDTO) {
    const groups = folder.groups ?? [];
    this.folder = folder;
    this.groups = groups;
    this.focusIndex = groups.length === 0 ? 0 : Math.min(this.focusIndex, groups.length - 1);
    this.selection = new Set();
    this.anchor = null;
    this.error = "";
  }

  setFocus(index: number) {
    if (this.groups.length === 0) return;
    this.focusIndex = Math.max(0, Math.min(index, this.groups.length - 1));
  }

  moveFocus(delta: number) {
    this.setFocus(this.focusIndex + delta);
  }

  toggleSelect() {
    const g = this.focused;
    if (!g) return;
    const key = groupKey(g);
    const next = new Set(this.selection);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    this.selection = next;
    this.anchor = this.focusIndex;
  }

  selectAll() {
    this.selection = new Set(this.groups.map(groupKey));
    this.anchor = this.focusIndex;
  }

  clearSelection() {
    this.selection = new Set();
    this.anchor = null;
  }

  isSelected(g: GroupDTO): boolean {
    return this.selection.has(groupKey(g));
  }

  resetZoom() {
    this.zoom = false;
    this.panX = 0;
    this.panY = 0;
  }

  notify(message: string, tone: Tone = "info") {
    this.toast = { id: ++this.#toastSeq, message, tone };
    if (this.#toastTimer !== null) clearTimeout(this.#toastTimer);
    this.#toastTimer = setTimeout(() => {
      this.toast = null;
      this.#toastTimer = null;
    }, TOAST_MS);
  }
}

export const app = new CullerState();

/**
 * Panning needs the loupe's own scale and stage size to clamp against, so the
 * loupe registers the handler and the key layer just asks for a nudge.
 */
export const loupe: { pan: (dx: number, dy: number) => void } = { pan: () => {} };

/** Same arrangement for the path box, which the key layer has to be able to reach. */
export const picker: { focus: () => void } = { focus: () => {} };

/** And for the tree, which the key layer hands the keyboard over to. */
export const tree: { focus: () => void } = { focus: () => {} };
