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

import type { GroupDTO } from "./bindings";
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
   Transitional. There is no destinations service yet, so the move and copy
   palettes offer what the user has typed before, kept in this webview's own
   storage. When the backend grows one, this block is what it replaces: nothing
   else reads the key. */

const DESTINATIONS = "culler.destinations";
/** How many destinations are remembered. Long enough to cover a shoot's worth. */
const MAX_DESTINATIONS = 12;

export function destinations(): string[] {
  try {
    const raw = localStorage.getItem(DESTINATIONS);
    const parsed: unknown = raw === null ? [] : JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((p): p is string => typeof p === "string");
  } catch {
    return [];
  }
}

/** rememberDestination moves path to the head of the recent list. */
export function rememberDestination(path: string) {
  const trimmed = path.trim();
  if (trimmed === "") return;
  const next = [trimmed, ...destinations().filter((d) => d !== trimmed)].slice(0, MAX_DESTINATIONS);
  try {
    localStorage.setItem(DESTINATIONS, JSON.stringify(next));
  } catch {
    // A webview with storage disabled still moves files; it just forgets where to.
  }
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
 * What a press turned out to mean. Everything but "ignore" is a press the
 * palette has consumed, so it is also what tells the frame when to call
 * preventDefault.
 */
export type KeyOutcome = "close" | "move" | "edge" | "run" | "type" | "erase" | "reserved" | "ignore";

/** printable reports whether a press should be treated as typing. */
function printable(e: KeyPress): boolean {
  return e.key.length === 1 && e.metaKey !== true && e.ctrlKey !== true;
}

/**
 * routeKey is every palette's keyboard, in one place so the three cannot drift
 * apart. count is how many rows the palette is showing, so the cursor wraps at
 * the right place, and run is what ⏎ does with alt held or not.
 *
 * Nothing here traps anything: Esc closes, and the frame gives the keyboard
 * back to the grid when it goes.
 */
export function routeKey(e: KeyPress, count: number, run: (alt: boolean) => void): KeyOutcome {
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
      palette.index = 0;
      return "edge";
    case "End":
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
      if (e.metaKey === true || e.altKey === true) palette.clearQuery();
      else palette.backspace();
      return "erase";
  }

  if (printable(e)) {
    palette.type(e.key);
    return "type";
  }
  return "ignore";
}

/**
 * visibleGroups is the grid's contents once the filter has had its say. It is
 * a plain function rather than a $derived export so that reading it inside a
 * component's own $derived tracks both app.groups and the filter.
 *
 * Nothing consumes it yet: app.groups is the grid's source, and swapping it is
 * a one-line change in App.svelte and Grid.svelte that belongs to whoever owns
 * those files. Until then the filter narrows what the palette reports and
 * nothing else.
 */
export function visibleGroups(): GroupDTO[] {
  const f = palette.filter;
  if (!filterIsSet(f)) return app.allGroups;
  return app.allGroups.filter((g) => matchesFilter(g, f));
}

/** How many frames a filter would leave, without applying it. */
export function countMatching(f: Filter): number {
  let n = 0;
  for (const g of app.allGroups) if (matchesFilter(g, f)) n++;
  return n;
}
