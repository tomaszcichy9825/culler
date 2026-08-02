// Setting a decision and getting it to disk are deliberately separate.
//
// A keystroke updates the group in memory and returns; the badge renders on
// the next frame with nothing waiting on the database. The changed frames
// collect in a buffer that flushes on a ticker, and again before anything that
// depends on the database being current (an apply, leaving the folder).

import { DecisionService } from "./bindings";
import type { DecisionItem, GroupDTO } from "./bindings";
import { app } from "./state.svelte";
import type { Decision } from "./state.svelte";

/** How often buffered decisions are written out. */
const FLUSH_MS = 500;

const buffer = new Map<string, DecisionItem>();
let timer: ReturnType<typeof setTimeout> | null = null;
let inFlight: Promise<void> = Promise.resolve();

/**
 * applicable reports whether a decision means anything for a frame. Dropping
 * the RAW of a JPEG-only frame is not a no-op to be swallowed — it is a
 * mis-press that deserves feedback, so the caller toasts the reason.
 */
function applicable(g: GroupDTO, d: Decision): string {
  if (d === "drop_raw" && !g.hasRaw) return "no RAW in this frame";
  if (d === "drop_jpeg" && !g.hasJpeg) return "no JPEG in this frame";
  return "";
}

function queue(g: GroupDTO) {
  buffer.set(g.hash, { hash: g.hash, dir: g.dir, stem: g.stem, decision: g.decision });
  if (timer === null) {
    timer = setTimeout(() => {
      timer = null;
      void flush();
    }, FLUSH_MS);
  }
}

/**
 * flush writes every buffered decision in one transaction. Flushes are
 * chained rather than overlapped so two batches can never land out of order
 * and leave the database holding the older of two decisions on a frame.
 */
export function flush(): Promise<void> {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  if (buffer.size === 0) return inFlight;

  const items = [...buffer.values()];
  buffer.clear();
  inFlight = inFlight
    .catch(() => {})
    .then(() => DecisionService.SetBatch(items))
    .then(
      () => {},
      (err: unknown) => {
        // The decision is still on screen but not on disk, and a reopen would
        // silently lose it, so say so rather than failing quietly.
        app.notify(`could not save decisions: ${message(err)}`, "error");
      },
    );
  return inFlight;
}

/**
 * setDecision applies d to whatever the current action target is: the whole
 * selection when there is one, otherwise the focused frame, which then
 * advances so a run of decisions needs no navigation between them.
 */
export function setDecision(d: Decision) {
  const targets = app.targets;
  if (targets.length === 0) return;

  const solo = app.selection.size === 0;
  let changed = 0;
  let refused = "";
  let unrecorded = 0;

  for (const g of targets) {
    const reason = applicable(g, d);
    if (reason !== "") {
      refused = reason;
      continue;
    }
    if (g.hash === "") {
      unrecorded++;
      continue;
    }
    g.decision = d;
    queue(g);
    changed++;
  }

  if (changed === 0) {
    if (unrecorded > 0) app.notify("this frame has no identity; its decision cannot be recorded", "error");
    else if (refused !== "") app.notify(refused);
    return;
  }
  if (refused !== "") app.notify(`${refused} — skipped`);
  else if (unrecorded > 0) app.notify(`${unrecorded} frame(s) have no identity and were skipped`, "error");

  if (solo) app.moveFocus(1);
}

export function message(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
