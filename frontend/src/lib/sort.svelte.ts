// The grid's sort: one field and one direction, applied at the seam where
// app.groups is derived from app.allGroups. The contact sheet, the loupe
// order, the filmstrip, auto-advance and compare all read app.groups, so
// sorting there keeps every one of them walking the same order — and a
// streamed scan keeps appending to allGroups in arrival order, which is why
// the source list is never sorted in place.
//
// The choice is remembered by the app itself, the way the theme is: the
// config file has no field for it, and a sort is a viewing preference rather
// than a decision about anyone's photographs.

import type { GroupDTO } from "./bindings";

/** Where the chosen sort is remembered across launches. */
const SORT_KEY = "culler.gridSort";

export type SortField = "shot" | "name";

/** DSCF1201 before DSCF1210, which a plain string sort gets wrong. */
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" });

/** A frame's shot time as a number, NaN when the scan read none. */
function shotTime(g: GroupDTO): number {
  return Date.parse(g.shot);
}

/**
 * Name order, with the folder as a second key: stems are unique within a
 * folder but search results span folders, and two frames must never compare
 * equal or their order would depend on how the list happened to arrive.
 */
function byName(a: GroupDTO, b: GroupDTO): number {
  return collator.compare(a.stem, b.stem) || collator.compare(a.dir, b.dir);
}

/**
 * sortGroups is pure: a new array, the input untouched. Under a shot sort a
 * frame with no readable timestamp goes to the end whichever way the sort
 * runs — an unknown time is missing data, not the newest or oldest frame —
 * and name order keeps such frames together. Bursts share a second, so name
 * order is also the tiebreak within one; ascending deliberately, whatever the
 * direction, so a burst always reads in capture order.
 */
export function sortGroups(groups: GroupDTO[], field: SortField, descending: boolean): GroupDTO[] {
  const dir = descending ? -1 : 1;
  const sorted = [...groups];
  if (field === "name") {
    sorted.sort((a, b) => dir * byName(a, b));
    return sorted;
  }
  sorted.sort((a, b) => {
    const ta = shotTime(a);
    const tb = shotTime(b);
    const aMissing = Number.isNaN(ta);
    const bMissing = Number.isNaN(tb);
    if (aMissing || bMissing) return aMissing && bMissing ? byName(a, b) : aMissing ? 1 : -1;
    return dir * (ta - tb) || byName(a, b);
  });
  return sorted;
}

function isField(v: string): v is SortField {
  return v === "shot" || v === "name";
}

/** What a relaunch finds in storage, or null when nothing valid was kept. */
function readStored(): { field: SortField; descending: boolean } | null {
  try {
    const raw = localStorage.getItem(SORT_KEY);
    if (raw === null) return null;
    const [field, direction] = raw.split(":");
    if (!isField(field) || (direction !== "asc" && direction !== "desc")) return null;
    return { field, descending: direction === "desc" };
  } catch {
    // A webview with storage disabled still sorts; it just forgets.
    return null;
  }
}

class GridSortState {
  field = $state<SortField>("shot");
  /** Newest first by default: the frames just shot are the ones being culled. */
  descending = $state(true);

  constructor() {
    const stored = readStored();
    if (stored !== null) {
      this.field = stored.field;
      this.descending = stored.descending;
    }
  }

  /** The header's own words for the current sort, e.g. "shot ↓". */
  get label(): string {
    return `${this.field} ${this.descending ? "↓" : "↑"}`;
  }

  /**
   * setField chooses a field with its natural direction — shot newest first,
   * name A to Z — rather than keeping the old field's arrow, which would make
   * "sort by name" mean something different depending on where you came from.
   */
  setField(field: SortField) {
    this.field = field;
    this.descending = field === "shot";
    this.persist();
  }

  /** cycleField is what the header chip does: the other field, its natural way up. */
  cycleField() {
    this.setField(this.field === "shot" ? "name" : "shot");
  }

  reverse() {
    this.descending = !this.descending;
    this.persist();
  }

  /** apply sorts a list by the current choice, never in place. */
  apply(groups: GroupDTO[]): GroupDTO[] {
    return sortGroups(groups, this.field, this.descending);
  }

  private persist() {
    try {
      localStorage.setItem(SORT_KEY, `${this.field}:${this.descending ? "desc" : "asc"}`);
    } catch {
      // See readStored.
    }
  }
}

export const gridSort = new GridSortState();
