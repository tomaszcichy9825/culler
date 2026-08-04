// The shell's own state: which mode is showing, which sub-layout within it,
// and which pane holds the keyboard.
//
// Every screen in the application is the same shell — a title bar, three panes
// and a status bar — and a mode is nothing more than a set of pane bodies. The
// mode axis is orthogonal to everything in state.svelte.ts, which is why it
// lives apart: the folder, the selection and the pending decisions survive a
// mode switch untouched.

export type Mode = "cull" | "exif" | "map" | "import";
export type Pane = "left" | "centre" | "right";

export interface ModeSpec {
  id: Mode;
  /** As drawn in the mode bar. */
  label: string;
  /** Sub-layouts, selected with ⌥1–3. */
  layouts: string[];
  /** What each pane is called when it is focused. */
  panes: Record<Pane, string>;
}

/**
 * Always four, always in this order. The EXIF sub-layout labels are the names
 * of the two screens that exist for that mode; the authored labels for its
 * segmented control did not survive the design export.
 *
 * The fourth slot was LIBRARY until the catalogue stopped being a room of its
 * own: the tree, the search and the sessions are all in CULL now, and the slot
 * belongs to the flow that puts photographs into the library rather than the
 * one that looks around it.
 */
export const MODES: ModeSpec[] = [
  {
    id: "cull",
    label: "PHOTOS",
    layouts: ["contact sheet", "loupe-first", "table"],
    panes: { left: "SOURCES", centre: "GRID", right: "INSPECTOR" },
  },
  {
    id: "exif",
    label: "EXIF",
    layouts: ["single frame", "batch"],
    panes: { left: "FRAMES", centre: "EDITOR", right: "TARGETS" },
  },
  {
    id: "map",
    label: "MAP",
    layouts: ["pins", "heat", "track"],
    panes: { left: "PLACES", centre: "MAP", right: "INSPECTOR" },
  },
  {
    id: "import",
    label: "IMPORT",
    layouts: ["review", "route", "verify"],
    panes: { left: "CARDS", centre: "IMPORT", right: "ROUTING" },
  },
];

export const PANES: Pane[] = ["left", "centre", "right"];

/** Which sub-layout of CULL shows the grid, and which shows one frame at a time. */
export const CONTACT_SHEET = 0;
export const LOUPE_FIRST = 1;

function specOf(mode: Mode): ModeSpec {
  return MODES.find((m) => m.id === mode) ?? MODES[0];
}

class ShellState {
  mode = $state<Mode>("cull");

  /**
   * The chosen sub-layout of every mode, not just the current one: switching
   * away and back returns to the layout that mode was left in.
   */
  layouts = $state<Record<Mode, number>>({ cull: 0, exif: 0, map: 0, import: 0 });

  /** The pane holding the keyboard, or null when the grid has it. */
  focusedPane = $state<Pane | null>(null);

  get spec(): ModeSpec {
    return specOf(this.mode);
  }

  /** The current mode's sub-layout index. */
  get layout(): number {
    return this.layouts[this.mode];
  }

  get layoutLabel(): string {
    return this.spec.layouts[this.layout] ?? "";
  }

  /** What the focused pane calls itself, for its header strip. */
  get focusedPaneName(): string {
    return this.focusedPane === null ? "" : this.spec.panes[this.focusedPane];
  }

  setMode(mode: Mode) {
    if (this.mode === mode) return;
    this.mode = mode;
    // A pane focus belongs to the mode it was taken in — the left pane of MAP
    // is a different pane from the left pane of CULL.
    this.focusedPane = null;
  }

  /** setModeByIndex backs ⌃1–4, which are positional rather than named. */
  setModeByIndex(index: number) {
    const spec = MODES[index];
    if (spec) this.setMode(spec.id);
  }

  /** setLayout backs ⌥1–3. Out-of-range indices are a no-op, not a clamp:
      a mode with two sub-layouts has nothing to show for ⌥3. */
  setLayout(index: number): boolean {
    if (index < 0 || index >= this.spec.layouts.length) return false;
    this.layouts[this.mode] = index;
    return true;
  }

  /**
   * nextLayout is where Tab lands: the mode's next sub-layout, wrapping round.
   * It reads the active mode's own list rather than knowing about CULL, so a
   * mode gaining sub-layouts inherits the key with nothing else to change. A
   * mode with one layout has nowhere to go and says so.
   */
  nextLayout(): number | null {
    const count = this.spec.layouts.length;
    if (count < 2) return null;
    return (this.layout + 1) % count;
  }

  focusPane(pane: Pane) {
    this.focusedPane = this.focusedPane === pane ? null : pane;
  }

  /** focusPaneByIndex backs ⌘1–3. */
  focusPaneByIndex(index: number) {
    const pane = PANES[index];
    if (pane) this.focusPane(pane);
  }

  /** Returns whether there was a focus to give back, so esc can keep unwinding. */
  releasePane(): boolean {
    if (this.focusedPane === null) return false;
    this.focusedPane = null;
    return true;
  }
}

export const shell = new ShellState();
