// The three palettes share one store: which one is up, what has been typed
// into it, and where its cursor sits. Only one can be open at a time, so the
// state is a single nullable kind rather than three booleans that could
// disagree.
//
// The matcher is a subsequence scorer written here rather than pulled in: the
// lists a palette ranks are dozens of rows long, not thousands, and a scoring
// rule the design can be argued against is worth more than a dependency.
//
// The grid filter lives here too. It is a view over app.groups, never a write
// to it — nothing in this module changes a frame or a verdict.

import { DecisionService, LibraryIndexService } from "./bindings";
import type { DestinationDTO, GroupDTO, LibraryFolderDTO, LibraryFoldersDTO } from "./bindings";
import { gridSort } from "./sort.svelte";
import { app } from "./state.svelte";
import { verdictOf } from "./verdict";

/* ---- matcher ---- */

/** Characters after which the next one starts a new word, for the boundary bonus. */
const BOUNDARY = new Set([" ", "-", "_", ".", "/", ":", "·"]);

/** Returned by score() when the query is not a subsequence of the target. */
export const NO_MATCH = -1;

/** Awarded for a character that directly follows the previous match. */
const RUN_BONUS = 16;
/** Awarded for a character that starts a word. */
const WORD_BONUS = 10;
/** Awarded for any other matched character. */
const CHAR_BONUS = 4;
/** Charged per skipped character since the previous match, and its ceiling. */
const GAP_PENALTY = 1;
const MAX_GAP_PENALTY = 12;
/** Awarded when the match starts at the very beginning of the target. */
const HEAD_BONUS = 12;
/**
 * How many starting positions are tried. The rows a palette ranks are short
 * labels, so this is never reached in practice; it is here so a pathological
 * target cannot turn the matcher quadratic.
 */
const MAX_STARTS = 16;

function startsWord(target: string, i: number): boolean {
  return i === 0 || BOUNDARY.has(target[i - 1]);
}

/**
 * walk scores one greedy pass from a given starting position. boundaryFirst
 * picks the next word-start occurrence of a character in preference to the
 * nearest one, which is what makes "mtr" rank "mask-toggle-raw" above a target
 * that merely contains the three letters. Both passes only ever choose a
 * position at or after the cursor, so either produces a valid subsequence
 * match.
 */
function walk(query: string, target: string, boundaryFirst: boolean, from: number): number {
  let total = 0;
  let cursor = from;
  let previous = -2;

  for (const ch of query) {
    let at = -1;
    if (boundaryFirst) {
      for (let i = cursor; i < target.length; i++) {
        if (target[i] === ch && startsWord(target, i)) {
          at = i;
          break;
        }
      }
    }
    if (at === -1) at = target.indexOf(ch, cursor);
    if (at === -1) return NO_MATCH;

    if (at === previous + 1) total += RUN_BONUS;
    else if (startsWord(target, at)) total += WORD_BONUS;
    else total += CHAR_BONUS;

    if (previous >= 0) total -= Math.min(MAX_GAP_PENALTY, (at - previous - 1) * GAP_PENALTY);
    if (at === 0) total += HEAD_BONUS;

    previous = at;
    cursor = at + 1;
  }
  return total;
}

/**
 * score rates how well target answers query, higher being better, with
 * NO_MATCH for a query that is not a subsequence of it. An empty query matches
 * everything at zero, so a palette that has had nothing typed into it shows its
 * rows in their declared order.
 *
 * Matching is case-insensitive, and a shorter target breaks a tie: "cut" is a
 * better answer to "cut" than "cut the whole selection" is.
 */
export function score(query: string, target: string): number {
  const q = query.trim().toLowerCase();
  if (q === "") return 0;
  const t = target.toLowerCase();
  if (q.length > t.length) return NO_MATCH;

  // Every place the first character appears is tried, because a greedy walk
  // from the first one is often the worst match available: "sidebar" against
  // "show or hide the sidebar" has to skip the s of "show" to find the word
  // the row is actually about.
  let best = NO_MATCH;
  let starts = 0;
  for (let i = t.indexOf(q[0]); i !== -1 && starts < MAX_STARTS; i = t.indexOf(q[0], i + 1)) {
    starts++;
    const here = Math.max(walk(q, t, false, i), walk(q, t, true, i));
    if (here > best) best = here;
  }
  if (best === NO_MATCH) return NO_MATCH;
  return best - t.length / 8;
}

/** Charged per field for matching in a later one, in scoreAny. */
const FIELD_PENALTY = 6;

/**
 * scoreAny rates the best of several fields, the earlier ones counting for
 * more. A row is found by its name first and by the small print second: "add a
 * folder to the sidebar" should not beat "show or hide the sidebar" on the
 * query "sidebar" merely for being the shorter sentence.
 */
export function scoreAny(query: string, fields: readonly string[]): number {
  let best = NO_MATCH;
  for (let i = 0; i < fields.length; i++) {
    const s = score(query, fields[i]);
    if (s === NO_MATCH) continue;
    const adjusted = s - i * FIELD_PENALTY;
    if (adjusted > best) best = adjusted;
  }
  return best;
}

/**
 * rank keeps the items query matches, best first. text returns either the one
 * string a row is found by or, in priority order, the several it can be found
 * by. Ties hold their original order, so a list authored in a deliberate order
 * keeps it until the query says otherwise.
 */
export function rank<T>(query: string, items: readonly T[], text: (item: T) => string | readonly string[]): T[] {
  const scored: { item: T; score: number }[] = [];
  for (const item of items) {
    const fields = text(item);
    const s = scoreAny(query, typeof fields === "string" ? [fields] : fields);
    if (s !== NO_MATCH) scored.push({ item, score: s });
  }
  scored.sort((a, b) => b.score - a.score);
  return scored.map((s) => s.item);
}

/* ---- grid filter ---- */

export type KindFilter = "all" | "raw" | "jpeg" | "pair";
export type VerdictFilter = "all" | "keep" | "cut" | "undecided";

export interface Filter {
  /** Which halves a frame has to hold. */
  kind: KindFilter;
  verdict: VerdictFilter;
  /** Stars a frame needs at least. Zero means the rating is not being filtered on. */
  minRating: number;
}

export const NO_FILTER: Filter = { kind: "all", verdict: "all", minRating: 0 };

export function filterIsSet(f: Filter): boolean {
  return f.kind !== "all" || f.verdict !== "all" || f.minRating > 0;
}

function kindMatches(g: GroupDTO, want: KindFilter): boolean {
  switch (want) {
    case "raw":
      return g.hasRaw && !g.hasJpeg;
    case "jpeg":
      return g.hasJpeg && !g.hasRaw;
    case "pair":
      return g.hasRaw && g.hasJpeg;
    default:
      return true;
  }
}

function verdictMatches(g: GroupDTO, want: VerdictFilter): boolean {
  const v = verdictOf(g);
  switch (want) {
    case "keep":
      return v === "keep";
    case "cut":
      return v === "cut";
    case "undecided":
      return v === "";
    default:
      return true;
  }
}

/** matchesFilter reports whether a frame survives f. */
export function matchesFilter(g: GroupDTO, f: Filter): boolean {
  return kindMatches(g, f.kind) && verdictMatches(g, f.verdict) && g.rating >= f.minRating;
}

/** filterSummary names the filter for a chip, and is empty when nothing is set. */
export function filterSummary(f: Filter): string {
  const parts: string[] = [];
  if (f.kind !== "all") parts.push(f.kind === "pair" ? "pairs" : `${f.kind} only`);
  if (f.verdict !== "all") parts.push(f.verdict);
  if (f.minRating > 0) parts.push(`${f.minRating}★ and up`);
  return parts.join(" · ");
}

/* ---- destinations ----
   Where frames get routed to, and the digits that reach them. The backend owns
   the list — it has to survive a relaunch and it is the same list the apply
   reads — so this is a cache of it, reloaded after anything that changes it. */

/** How many destinations the keyboard reaches by digit. Matches the backend. */
export const MAX_SLOTS = 9;

/**
 * The backend calls the destination store makes, in one object so the headless
 * bench can answer them without a running app. Same arrangement as the loupe
 * and the tree: the component asks the seam, not the binding.
 */
export const destinationPort = {
  list: (): Promise<DestinationDTO[] | null> => DecisionService.Destinations(),
  use: (path: string, label: string): Promise<void> => DecisionService.UseDestination(path, label),
  forget: (path: string): Promise<void> => DecisionService.ForgetDestination(path),
  pin: (path: string, pinned: boolean): Promise<void> => DecisionService.PinDestination(path, pinned),
  bind: (path: string, slot: number): Promise<void> => DecisionService.BindDestinationSlot(path, slot),
};

/**
 * The remembered destinations, in palette order. Loaded once and refreshed
 * after every change, because the digit each destination answers to is worked
 * out by the backend from recency and cannot be guessed here.
 */
class DestinationList {
  rows = $state<DestinationDTO[]>([]);
  /** Set once the first load has finished, so the palette can say "loading". */
  loaded = $state(false);
  /** Why the list is empty, when it is empty for a reason worth showing. */
  error = $state("");

  async load(): Promise<void> {
    try {
      this.rows = (await destinationPort.list()) ?? [];
      this.error = "";
    } catch (err) {
      this.rows = [];
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loaded = true;
    }
  }

  /** use records that frames have just been routed here, and reloads. */
  async use(path: string, label = ""): Promise<void> {
    const trimmed = path.trim();
    if (trimmed === "") return;
    await destinationPort.use(trimmed, label);
    await this.load();
  }

  async pin(path: string, pinned: boolean): Promise<void> {
    await destinationPort.pin(path, pinned);
    await this.load();
  }

  async bind(path: string, slot: number): Promise<void> {
    await destinationPort.bind(path, slot);
    await this.load();
  }

  async forget(path: string): Promise<void> {
    await destinationPort.forget(path);
    await this.load();
  }

  /** forDigit is what pressing a digit means, or null when nothing claims it. */
  forDigit(digit: number): DestinationDTO | null {
    return this.rows.find((d) => d.digit === digit) ?? null;
  }

  has(path: string): boolean {
    return this.rows.some((d) => d.path === path);
  }
}

export const destinations = new DestinationList();

/* ---- library folders ----
   The places the library already files photographs, which is what the palette
   suggests from once something has been typed. They are not destinations: the
   list is a read of the catalogue, nothing is recorded by showing one, and a
   folder only becomes a destination when the user picks it. */

/** How many folders the palette asks the catalogue for. */
export const FOLDER_LIMIT = 400;

/** The catalogue call the folder list makes, swapped out by the bench. */
export const libraryFolderPort = {
  list: (limit: number): Promise<LibraryFoldersDTO> => LibraryIndexService.Folders(limit),
};

/**
 * The catalogued folders and the library root a relative destination hangs
 * off. Loaded when the palette opens rather than held for the session: an index
 * pass, or an apply, changes what the library holds.
 */
class LibraryFolders {
  rows = $state<LibraryFolderDTO[]>([]);
  /** Where a destination that is not an absolute path lands. */
  root = $state("");
  loaded = $state(false);
  /**
   * Why there are no suggestions, when there is a reason worth showing. A
   * catalogue that cannot be read is not an error the palette refuses over:
   * typing a path still works, and that is the flow this feature sits beside.
   */
  error = $state("");

  async load(): Promise<void> {
    try {
      const out = await libraryFolderPort.list(FOLDER_LIMIT);
      this.rows = out.folders ?? [];
      this.root = out.root;
      this.error = "";
    } catch (err) {
      this.rows = [];
      this.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.loaded = true;
    }
  }

  /**
   * destinationFor is the folder written the way a destination is recorded:
   * library-relative when it lives under the library root, absolute otherwise.
   */
  destinationFor(f: LibraryFolderDTO): string {
    return f.rel !== "" ? f.rel : f.path;
  }
}

export const libraryFolders = new LibraryFolders();

/**
 * leafOf is the last part of a destination, which is what a chip on a tile has
 * room for. A template keeps its braces here: `{date:2006-01-02}` is what the
 * user typed and what they will recognise, and the expanded folder does not
 * exist until the apply.
 */
export function leafOf(path: string): string {
  const parts = path.split("/").filter((p) => p !== "");
  return parts.length === 0 ? path : parts[parts.length - 1];
}

/* ---- store ---- */

export type PaletteKind = "command" | "move" | "copy" | "filter";

class PaletteState {
  /** The palette on screen, or null. One at a time, by construction. */
  kind = $state<PaletteKind | null>(null);
  /** What has been typed into the open palette. */
  query = $state("");
  /** Which row the cursor is on, as an index into the palette's own row list. */
  index = $state(0);

  /** The grid filter. Survives the filter palette closing; that is the point. */
  filter = $state<Filter>({ ...NO_FILTER });

  get open(): boolean {
    return this.kind !== null;
  }

  /**
   * show opens a palette from scratch. A palette always opens empty: carrying
   * the last query over would mean the first keystroke edits something the user
   * cannot see the start of.
   */
  show(kind: PaletteKind) {
    this.kind = kind;
    this.query = "";
    this.index = 0;
  }

  /** toggle is what a binding calls, so the key that opens a palette closes it. */
  toggle(kind: PaletteKind) {
    if (this.kind === kind) this.close();
    else this.show(kind);
  }

  close() {
    this.kind = null;
    this.query = "";
    this.index = 0;
  }

  /** type appends one printable character and returns the cursor to the top. */
  type(ch: string) {
    this.query += ch;
    this.index = 0;
  }

  backspace() {
    this.query = this.query.slice(0, -1);
    this.index = 0;
  }

  clearQuery() {
    this.query = "";
    this.index = 0;
  }

  /**
   * move walks the rows, wrapping. An empty list leaves the cursor at zero
   * rather than at -1, so a palette showing nothing still has a valid index.
   */
  move(delta: number, count: number) {
    if (count <= 0) {
      this.index = 0;
      return;
    }
    this.index = (((this.index + delta) % count) + count) % count;
  }

  /** clamp keeps the cursor inside a list that shrank under it as the user typed. */
  clamp(count: number) {
    if (count <= 0) this.index = 0;
    else if (this.index > count - 1) this.index = count - 1;
  }

  setFilter(f: Filter) {
    this.filter = f;
  }

  clearFilter() {
    this.filter = { ...NO_FILTER };
  }
}

export const palette = new PaletteState();

/* ---- keyboard ---- */

/** The parts of a keyboard event the palettes route on. */
export interface KeyPress {
  key: string;
  metaKey?: boolean;
  ctrlKey?: boolean;
  altKey?: boolean;
}

/**
 * What a press turned out to mean. Everything but "ignore" and "caret" is a
 * press the palette has consumed, so it is also what tells the frame when to
 * call preventDefault — a "caret" press belongs to the text field the event
 * came from, and preventing it is exactly the bug that made editing a typed
 * destination mean deleting back to the mistake.
 */
export type KeyOutcome =
  | "close"
  | "move"
  | "edge"
  | "run"
  | "type"
  | "erase"
  | "reserved"
  | "caret"
  | "ignore";

/** printable reports whether a press should be treated as typing. */
function printable(e: KeyPress): boolean {
  return e.key.length === 1 && e.metaKey !== true && e.ctrlKey !== true;
}

/**
 * The keys that move a text caret rather than a row cursor: ← and → with any
 * modifier on them, since ⌥← is a word and ⌘← is the start of the line and
 * both are the field's business, not the list's.
 */
function movesTheCaret(e: KeyPress): boolean {
  return e.key === "ArrowLeft" || e.key === "ArrowRight";
}

/**
 * routeKey is every palette's keyboard, in one place so they cannot drift
 * apart. count is how many rows the palette is showing, so the cursor wraps at
 * the right place, and run is what ⏎ does with alt held or not.
 *
 * editing says the press came from a real text field — the destination
 * palette's, which holds its own text so that a caret, a selection and a paste
 * all behave the way they do everywhere else. Then this function claims only
 * the keys that are unambiguously the list's (↑↓, ⏎, Esc, Tab) and hands the
 * rest back: the field types, erases and moves its own caret, and Home/End go
 * to the ends of the text rather than the ends of the list.
 *
 * Nothing here traps anything: Esc closes, and the frame gives the keyboard
 * back to the grid when it goes.
 */
export function routeKey(e: KeyPress, count: number, run: (alt: boolean) => void, editing = false): KeyOutcome {
  switch (e.key) {
    case "Escape":
      palette.close();
      return "close";
    case "ArrowDown":
      palette.move(1, count);
      return "move";
    case "ArrowUp":
      palette.move(-1, count);
      return "move";
    case "Home":
      if (editing) return "caret";
      palette.index = 0;
      return "edge";
    case "End":
      if (editing) return "caret";
      palette.index = Math.max(0, count - 1);
      return "edge";
    case "Enter":
      run(e.altKey === true);
      return "run";
    case "Tab":
      // Reserved for "run with arguments". Consumed rather than passed on, so
      // focus cannot walk out of the dialog while it means nothing.
      return "reserved";
    case "Backspace":
      if (editing) return "caret";
      if (e.metaKey === true || e.altKey === true) palette.clearQuery();
      else palette.backspace();
      return "erase";
  }

  if (movesTheCaret(e)) return editing ? "caret" : "ignore";
  if (editing) return "caret";

  if (printable(e)) {
    palette.type(e.key);
    return "type";
  }
  return "ignore";
}

/**
 * visibleGroups is the grid's contents once the filter and the sort have had
 * their say. It is a plain function rather than a $derived export so that
 * reading it inside an effect or a component's own $derived tracks the source
 * list, the filter and the sort together — App.svelte derives app.groups from
 * it, which is how the contact sheet, the loupe, the filmstrip and
 * auto-advance all see one order. allGroups itself is never reordered: a
 * streamed scan keeps appending to it, and search results replace it, so the
 * sort lives here where both flows pass through.
 */
export function visibleGroups(): GroupDTO[] {
  const f = palette.filter;
  const shown = filterIsSet(f) ? app.allGroups.filter((g) => matchesFilter(g, f)) : app.allGroups;
  return gridSort.apply(shown);
}

/** How many frames a filter would leave, without applying it. */
export function countMatching(f: Filter): number {
  let n = 0;
  for (const g of app.allGroups) if (matchesFilter(g, f)) n++;
  return n;
}
