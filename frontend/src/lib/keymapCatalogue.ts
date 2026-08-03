// The catalogue behind the settings keymap page: every action the app can
// answer to, what it does, and the chords it ships with.
//
// The defaults below are copies of two tables that cannot be imported — Go's
// config.DefaultKeymap (the stock config file) and the FRONTEND_BINDINGS table
// in actions.ts (chords the shell adds for actions the config has never heard
// of). They are used for two things only: drawing the "changed from default"
// marker, and backing "reset to default". The keymap the app actually runs on
// is always the config's own, never this file. Keep them in step.

import { isMac } from "./keymap";

/** Where an action's stock chords come from. */
export type DefaultSource = "config" | "shell";

export interface ActionSpec {
  id: string;
  label: string;
  /** The one-line note drawn beside the chord cluster. */
  note: string;
  /** The right-aligned scope tag: where the action is answered. */
  scope: string;
  /**
   * "config" actions are written into a stock config file. "shell" actions are
   * not: actions.ts binds them at runtime only while the config stays silent,
   * so recording one here writes it into the config for the first time.
   */
  source: DefaultSource;
  chords: string[];
}

export interface ActionGroup {
  title: string;
  actions: ActionSpec[];
}

/**
 * The action catalogue, grouped as they are learned rather than alphabetically.
 * Labels follow the keymap overlay's wording so the two never disagree.
 */
export const ACTION_GROUPS: ActionGroup[] = [
  {
    title: "Focus and selection",
    actions: [
      { id: "focus-left", label: "move focus left", note: "grid, filmstrip and table", scope: "cull", source: "config", chords: ["ArrowLeft"] },
      { id: "focus-right", label: "move focus right", note: "in the zoomed loupe, pans instead", scope: "cull", source: "config", chords: ["ArrowRight"] },
      { id: "focus-up", label: "move focus up", note: "one row in the grid", scope: "cull", source: "config", chords: ["ArrowUp"] },
      { id: "focus-down", label: "move focus down", note: "one row in the grid", scope: "cull", source: "config", chords: ["ArrowDown"] },
      { id: "toggle-select", label: "toggle selection", note: "adds the frame under the cursor", scope: "cull", source: "config", chords: ["space"] },
      { id: "select-all", label: "select all", note: "every frame in the folder", scope: "cull", source: "config", chords: ["mod+a"] },
      { id: "escape", label: "clear selection / leave loupe", note: "unwinds one step at a time", scope: "global", source: "config", chords: ["Escape"] },
    ],
  },
  {
    title: "Verdicts",
    actions: [
      { id: "verdict-keep", label: "keep the frame", note: "pressing it again clears the verdict", scope: "cull", source: "config", chords: ["k"] },
      { id: "verdict-cut", label: "cut the frame", note: "nothing leaves the disk until the plan is applied", scope: "cull", source: "config", chords: ["x"] },
      { id: "mask-toggle-raw", label: "keep or drop the RAW half", note: "only meaningful on a pair", scope: "cull", source: "config", chords: ["r"] },
      { id: "mask-toggle-jpeg", label: "keep or drop the JPEG half", note: "only meaningful on a pair", scope: "cull", source: "config", chords: ["j"] },
    ],
  },
  {
    title: "Ratings",
    actions: [
      { id: "rate-1", label: "one star", note: "", scope: "cull", source: "config", chords: ["1"] },
      { id: "rate-2", label: "two stars", note: "", scope: "cull", source: "config", chords: ["2"] },
      { id: "rate-3", label: "three stars", note: "", scope: "cull", source: "config", chords: ["3"] },
      { id: "rate-4", label: "four stars", note: "", scope: "cull", source: "config", chords: ["4"] },
      { id: "rate-5", label: "five stars", note: "", scope: "cull", source: "config", chords: ["5"] },
      { id: "rate-clear", label: "clear the rating", note: "", scope: "cull", source: "config", chords: ["0"] },
    ],
  },
  {
    title: "Layout",
    actions: [
      { id: "cycle-layout", label: "cycle layout", note: "walks the mode's sub-layouts", scope: "shell", source: "config", chords: ["Tab"] },
      { id: "zoom", label: "1:1 zoom", note: "in the loupe", scope: "cull", source: "config", chords: ["z"] },
      { id: "layout-1", label: "first sub-layout", note: "contact sheet, in CULL", scope: "shell", source: "shell", chords: ["alt+1"] },
      { id: "layout-2", label: "second sub-layout", note: "loupe-first, in CULL", scope: "shell", source: "shell", chords: ["alt+2"] },
      { id: "layout-3", label: "third sub-layout", note: "table, in CULL", scope: "shell", source: "shell", chords: ["alt+3"] },
      { id: "pane-left", label: "focus the left pane", note: "", scope: "shell", source: "shell", chords: ["mod+1", "shift+mod+1"] },
      { id: "pane-centre", label: "focus the centre pane", note: "", scope: "shell", source: "shell", chords: ["mod+2", "shift+mod+2"] },
      { id: "pane-right", label: "focus the right pane", note: "", scope: "shell", source: "shell", chords: ["mod+3", "shift+mod+3"] },
      { id: "toggle-sidebar", label: "show or hide the sidebar", note: "", scope: "shell", source: "shell", chords: ["s"] },
    ],
  },
  {
    title: "Modes",
    actions: [
      { id: "mode-cull", label: "CULL mode", note: "", scope: "shell", source: "shell", chords: ["ctrl+1"] },
      { id: "mode-exif", label: "EXIF mode", note: "", scope: "shell", source: "shell", chords: ["ctrl+2"] },
      { id: "mode-map", label: "MAP mode", note: "", scope: "shell", source: "shell", chords: ["ctrl+3"] },
      { id: "mode-import", label: "IMPORT mode", note: "", scope: "shell", source: "shell", chords: ["ctrl+4"] },
    ],
  },
  {
    title: "Folders",
    actions: [
      { id: "focus-path", label: "jump to the folder path box", note: "", scope: "library", source: "shell", chords: ["o"] },
      { id: "focus-tree", label: "jump to the folder tree", note: "arrows move, return opens", scope: "library", source: "shell", chords: ["t"] },
      { id: "add-root", label: "add a folder to the sidebar", note: "", scope: "library", source: "shell", chords: ["shift+o"] },
      { id: "copy-path", label: "copy the open folder's path", note: "", scope: "library", source: "shell", chords: ["y"] },
    ],
  },
  {
    title: "Writes",
    actions: [
      { id: "apply", label: "apply pending verdicts", note: "shows the plan first", scope: "global", source: "config", chords: ["Enter"] },
      { id: "undo", label: "undo last batch", note: "", scope: "global", source: "config", chords: ["mod+z"] },
      { id: "redo", label: "redo", note: "comes in v0.2", scope: "global", source: "config", chords: ["shift+mod+z"] },
    ],
  },
  {
    title: "Palettes and panels",
    actions: [
      { id: "keymap-overlay", label: "the keys overlay", note: "", scope: "global", source: "config", chords: ["?"] },
      { id: "open-settings", label: "settings", note: "this screen", scope: "global", source: "shell", chords: ["mod+,"] },
      { id: "command-palette", label: "command palette", note: "comes in v0.2", scope: "global", source: "config", chords: ["mod+k"] },
      { id: "search", label: "search the catalogue", note: "results replace the grid", scope: "global", source: "config", chords: ["/"] },
      { id: "copy-palette", label: "copy destinations", note: "comes in v0.2", scope: "global", source: "config", chords: ["c"] },
      { id: "move-palette", label: "move destinations", note: "comes in v0.2", scope: "global", source: "config", chords: ["m"] },
      { id: "filter-palette", label: "filters", note: "comes in v0.2", scope: "global", source: "config", chords: ["f"] },
    ],
  },
];

export const ACTIONS: ActionSpec[] = ACTION_GROUPS.flatMap((g) => g.actions);

const BY_ID = new Map(ACTIONS.map((a) => [a.id, a]));

export function actionSpec(id: string): ActionSpec | undefined {
  return BY_ID.get(id);
}

/**
 * defaultChords returns the chords an action ships with, or an empty list for
 * an action this catalogue has never heard of — a config may name anything.
 */
export function defaultChords(id: string): string[] {
  return BY_ID.get(id)?.chords ?? [];
}

/** Only "config" actions belong in a stock config file. */
export function isConfigDefault(id: string): boolean {
  return BY_ID.get(id)?.source === "config";
}

/** The stock config keymap, mirroring Go's config.DefaultKeymap. */
export const DEFAULT_KEYMAP: Record<string, string[]> = Object.fromEntries(
  ACTIONS.filter((a) => a.source === "config").map((a) => [a.id, [...a.chords]]),
);

/**
 * shiftMatters mirrors the rule in keymap.ts: Shift is already baked into a
 * punctuation character, so recording "?" must not produce "shift+?".
 */
function shiftMatters(key: string): boolean {
  return key.length > 1 || /^[a-z0-9]$/.test(key);
}

/**
 * chordFromEvent writes a keypress in the config's own chord notation, so what
 * the recorder stores is what a person would have typed into the file. It is
 * the inverse of parseChord, and returns null for a press that is only
 * modifiers — holding Cmd is not a chord yet.
 *
 * Modifiers are emitted in the order parseChord canonicalises them (mod, ctrl,
 * shift, alt); the parser ignores order, so "shift+mod+z" from the stock file
 * and "mod+shift+z" from here are the same binding.
 */
export function chordFromEvent(e: KeyboardEvent): string | null {
  if (e.key === "Shift" || e.key === "Control" || e.key === "Alt" || e.key === "Meta") return null;

  // Option on macOS rewrites the digit row, so digits come from the physical
  // key exactly as eventSignature reads them.
  const digit = /^Digit([0-9])$/.exec(e.code);
  let key: string;
  if (digit !== null) key = digit[1];
  else if (e.key === " ") key = "space";
  else if (e.key.length === 1) key = e.key.toLowerCase();
  else key = e.key; // ArrowLeft, Escape, Enter, Tab — written as the DOM names them

  const parts: string[] = [];
  if (isMac ? e.metaKey : e.ctrlKey) parts.push("mod");
  if (isMac && e.ctrlKey) parts.push("ctrl");
  if (e.shiftKey && shiftMatters(key)) parts.push("shift");
  if (e.altKey) parts.push("alt");
  parts.push(key);
  return parts.join("+");
}

const KEY_GLYPHS: Record<string, string> = {
  arrowleft: "←",
  arrowright: "→",
  arrowup: "↑",
  arrowdown: "↓",
  space: "Space",
  escape: "Esc",
  enter: "↩",
  tab: "Tab",
  backspace: "⌫",
  delete: "Del",
  home: "Home",
  end: "End",
};

/**
 * chordParts renders a chord as one string per keycap, which is what §4.4 draws
 * — formatChord in keymap.ts joins them into a single label instead.
 */
export function chordParts(chord: string): string[] {
  const raw = chord.split("+");
  const key = raw.pop() ?? "";
  const caps = raw.map((p) => {
    switch (p.toLowerCase()) {
      case "mod":
      case "cmd":
      case "meta":
      case "super":
        return isMac ? "⌘" : "Ctrl";
      case "ctrl":
      case "control":
        return isMac ? "⌃" : "Ctrl";
      case "shift":
        return isMac ? "⇧" : "Shift";
      case "alt":
      case "option":
        return isMac ? "⌥" : "Alt";
      default:
        return p;
    }
  });
  caps.push(KEY_GLYPHS[key.toLowerCase()] ?? (key.length === 1 ? key.toUpperCase() : key));
  return caps;
}

/** sameChords compares two chord lists as sets written in order. */
export function sameChords(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((c, i) => c === b[i]);
}
