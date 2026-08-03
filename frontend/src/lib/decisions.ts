// Recording a verdict and getting it to disk are deliberately separate.
//
// A keystroke updates the group in memory and returns; the badge renders on
// the next frame with nothing waiting on the database. The changed frames
// collect in a buffer that flushes on a ticker, and again before anything that
// depends on the database being current (an apply, leaving the folder).
//
// Verdicts and ratings are stored independently by the backend and buffered
// independently here, because rating a frame nobody has judged yet is normal.

import { DecisionService } from "./bindings";
import type { DestinationItem, GroupDTO, RatingItem, VerdictItem } from "./bindings";
import { app } from "./state.svelte";
import { clampMask, hasHalf, MAX_RATING, maskOf, toggled, verdictOf } from "./verdict";
import type { Half, Mask, Verdict } from "./verdict";

/** How often buffered changes are written out. */
const FLUSH_MS = 500;

const verdictBuffer = new Map<string, VerdictItem>();
const ratingBuffer = new Map<string, RatingItem>();
const destinationBuffer = new Map<string, DestinationItem>();
let timer: ReturnType<typeof setTimeout> | null = null;
let inFlight: Promise<void> = Promise.resolve();

function schedule() {
  if (timer !== null) return;
  timer = setTimeout(() => {
    timer = null;
    void flush();
  }, FLUSH_MS);
}

function queueVerdict(g: GroupDTO) {
  verdictBuffer.set(g.hash, { hash: g.hash, dir: g.dir, stem: g.stem, verdict: g.verdict, mask: g.mask });
  schedule();
}

function queueRating(g: GroupDTO) {
  ratingBuffer.set(g.hash, { hash: g.hash, dir: g.dir, stem: g.stem, rating: g.rating });
  schedule();
}

function queueDestination(g: GroupDTO) {
  destinationBuffer.set(g.hash, {
    hash: g.hash,
    dir: g.dir,
    stem: g.stem,
    destination: g.destination,
  });
  schedule();
}

/**
 * flush writes every buffered change, each kind in its own transaction.
 * Flushes are chained rather than overlapped so two batches can never land out
 * of order and leave the database holding the older of two verdicts on a
 * frame.
 *
 * Destinations go last. A destination implies a keep, so writing it after the
 * verdicts means the implication lands on top of whatever the verdict batch
 * said rather than being overwritten by it.
 */
export function flush(): Promise<void> {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  if (verdictBuffer.size === 0 && ratingBuffer.size === 0 && destinationBuffer.size === 0) return inFlight;

  const verdicts = [...verdictBuffer.values()];
  const ratings = [...ratingBuffer.values()];
  const routes = [...destinationBuffer.values()];
  verdictBuffer.clear();
  ratingBuffer.clear();
  destinationBuffer.clear();

  inFlight = inFlight
    .catch(() => {})
    .then(() => (verdicts.length > 0 ? DecisionService.SetVerdictBatch(verdicts) : undefined))
    .then(() => (ratings.length > 0 ? DecisionService.SetRatingBatch(ratings) : undefined))
    .then(() => (routes.length > 0 ? DecisionService.SetDestinationBatch(routes) : undefined))
    .then(
      () => {},
      (err: unknown) => {
        // The verdict is still on screen but not on disk, and a reopen would
        // silently lose it, so say so rather than failing quietly.
        app.notify(`could not save verdicts: ${message(err)}`, "error");
      },
    );
  return inFlight;
}

/**
 * targets resolves what a key applies to, or null when it applies to nothing:
 * a selection wins, otherwise the focused frame alone. While a scan is running
 * the grid is behind the loader, so a verdict key would be marking frames the
 * user cannot see.
 */
function targets(): GroupDTO[] | null {
  if (app.scanning !== null) {
    app.notify("still scanning — hold on");
    return null;
  }
  const chosen = app.targets;
  return chosen.length === 0 ? null : chosen;
}

/**
 * legacyDecision names a verdict in the pre-verdict vocabulary the DTO still
 * carries. The backend recomputes the field on the next open; keeping it in
 * step here stops anything still reading it from showing a stale answer
 * between the keystroke and that reload.
 */
function legacyDecision(v: Verdict, m: Mask): string {
  if (v === "cut") return "drop_all";
  if (v !== "keep") return "none";
  if (m === "j") return "drop_raw";
  if (m === "r") return "drop_jpeg";
  return "keep_all";
}

function record(g: GroupDTO, v: Verdict, m: Mask) {
  g.verdict = v;
  g.mask = m;
  g.decision = legacyDecision(v, m);
  queueVerdict(g);
}

/** The mask a keep on this frame starts from, narrowed to the halves it has. */
function startingMask(g: GroupDTO): Mask {
  return clampMask(verdictOf(g) === "" ? app.defaultKeepMask : maskOf(g), g);
}

/**
 * report says what a run of frames did, and returns whether anything landed.
 * A frame with no identity hash cannot be recorded at all, and a refusal is a
 * mis-press worth feedback rather than a no-op to be swallowed.
 */
function report(changed: number, unrecorded: number, refused: string): boolean {
  if (changed === 0) {
    if (unrecorded > 0) app.notify("this frame has no identity; its verdict cannot be recorded", "error");
    else if (refused !== "") app.notify(refused);
    return false;
  }
  if (refused !== "") app.notify(`${refused} — skipped`);
  else if (unrecorded > 0) app.notify(`${unrecorded} frame(s) have no identity and were skipped`, "error");
  return true;
}

/**
 * setVerdict keeps or cuts whatever the current target is, and then moves on,
 * so a run of verdicts needs no navigation between them.
 *
 * Pressing the same verdict again on a single frame takes it back off. It is
 * the only way to undo a mis-press — nothing in the keymap clears a verdict —
 * and the focus has already moved on by then, so it only happens when the user
 * comes back to the frame deliberately. A selection sets rather than toggles,
 * so a mixed selection lands on one verdict instead of splitting further.
 */
export function setVerdict(v: "keep" | "cut") {
  const frames = targets();
  if (frames === null) return;

  const solo = app.selection.size === 0;
  const clearing = solo && frames.length === 1 && verdictOf(frames[0]) === v;

  let changed = 0;
  let unrecorded = 0;
  for (const g of frames) {
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    if (clearing) record(g, "", maskOf(g));
    else record(g, v, v === "keep" ? startingMask(g) : maskOf(g));
    changed++;
  }

  if (!report(changed, unrecorded, "")) return;
  if (solo && !clearing) app.moveFocus(1);
}

/**
 * setVerdictFor records a verdict on explicit frames, for screens like
 * compare that judge frames other than the grid's current target. No focus
 * movement, no toggling: the caller said exactly what it means.
 */
export function setVerdictFor(frames: GroupDTO[], v: "keep" | "cut") {
  let changed = 0;
  let unrecorded = 0;
  for (const g of frames) {
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    record(g, v, v === "keep" ? startingMask(g) : maskOf(g));
    changed++;
  }
  report(changed, unrecorded, "");
}

/**
 * toggleMask flips whether one half of a pair survives. On a frame nobody has
 * judged yet this implies a keep: a mask says which halves a verdict holds on
 * to, so setting one without a verdict would mean nothing.
 *
 * The focus stays put. Unlike a verdict, adjusting the mask is a refinement of
 * the frame in front of you, usually straight after the key that judged it.
 */
export function toggleMask(half: Half) {
  const frames = targets();
  if (frames === null) return;

  let changed = 0;
  let unrecorded = 0;
  let refused = "";
  for (const g of frames) {
    if (!hasHalf(g, half)) {
      refused = half === "r" ? "no RAW in this frame" : "no JPEG in this frame";
      continue;
    }
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    const other: Half = half === "r" ? "j" : "r";
    const next = hasHalf(g, other) ? toggled(startingMask(g), half) : null;
    if (next === null) {
      // Dropping the frame's only surviving half IS a cut — pressing j on a
      // JPEG-only frame means delete it, not a lecture about the x key. The
      // same press takes the cut back off.
      const v = verdictOf(g);
      record(g, v === "cut" ? "" : "cut", maskOf(g));
      changed++;
      continue;
    }
    const v = verdictOf(g);
    record(g, v === "" ? "keep" : v, next);
    changed++;
  }

  report(changed, unrecorded, refused);
}

/**
 * setDestination routes the current target to a folder and then moves on, the
 * same way a verdict does: routing a run of frames to the same place needs no
 * navigation between them.
 *
 * Naming a destination implies a keep, exactly as toggling a mask does — the
 * backend records the same implication, and it is mirrored here so the tile
 * says so on the next frame rather than after the next reload. A cut keeps its
 * cut: the user typed that, and this did not.
 *
 * It answers how many frames were routed, so the palette can say so and close.
 */
export function setDestination(destination: string): number {
  const frames = targets();
  if (frames === null) return 0;

  const solo = app.selection.size === 0;
  let changed = 0;
  let unrecorded = 0;
  for (const g of frames) {
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    g.destination = destination;
    if (destination !== "" && verdictOf(g) === "") record(g, "keep", startingMask(g));
    queueDestination(g);
    changed++;
  }

  if (!report(changed, unrecorded, "")) return 0;
  if (solo && destination !== "") app.moveFocus(1);
  return changed;
}

/**
 * clearDestination takes the routing off the current target, leaving the
 * verdict and the rating where they are. The focus stays put: unlike routing a
 * run of frames, clearing one is a correction to the frame in front of you.
 */
export function clearDestination(): number {
  const frames = targets();
  if (frames === null) return 0;

  let changed = 0;
  let unrecorded = 0;
  for (const g of frames) {
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    if (g.destination === "") continue;
    g.destination = "";
    queueDestination(g);
    changed++;
  }
  if (changed === 0 && unrecorded === 0) {
    app.notify("this frame is not going anywhere");
    return 0;
  }
  report(changed, unrecorded, "");
  return changed;
}

/**
 * setRating stars the current target, 0 clearing it. Pressing a frame's own
 * rating again clears it, which is how every other culling tool behaves.
 *
 * Ratings do not move the focus: a rating is usually the first of two presses
 * on the same frame, the second being the verdict.
 */
export function setRating(stars: number) {
  if (stars < 0 || stars > MAX_RATING) return;
  const frames = targets();
  if (frames === null) return;

  const solo = app.selection.size === 0;
  const clearing = stars !== 0 && solo && frames.length === 1 && frames[0].rating === stars;
  const value = clearing ? 0 : stars;

  let changed = 0;
  let unrecorded = 0;
  for (const g of frames) {
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    g.rating = value;
    queueRating(g);
    changed++;
  }

  report(changed, unrecorded, "");
}

export function message(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
