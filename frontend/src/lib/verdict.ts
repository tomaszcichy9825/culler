// The verdict model, as everything that draws a frame reads it.
//
// A frame carries three independent things: a verdict (keep, cut, or nothing
// yet), a mask saying which halves of a RAW+JPEG pair the verdict applies to,
// and a star rating that plays no part in what happens to the files. The
// backend owns all three; this module only says what they mean on screen.
//
// Nothing here writes. Recording a verdict is decisions.ts.

import type { GroupDTO } from "./bindings";

export type Verdict = "" | "keep" | "cut";
export type Mask = "rj" | "r" | "j";
/** Which half of a pair is being talked about. */
export type Half = "r" | "j";
/** How far a cut reaches, from `behaviour.cutRemoves` in the config. */
export type CutScope = "both" | "masked";

/**
 * What one half is doing under the frame's current verdict. Absence is a state
 * of its own: the tile draws both halves always, so a frame with no RAW says
 * so rather than leaving a gap the eye has to interpret.
 */
export type HalfState = "kept" | "cut" | "absent";

/** The top of the star scale, matching decide.MaxRating. */
export const MAX_RATING = 5;

/** Both halves, in the order they are drawn. */
export const HALVES: Half[] = ["r", "j"];

export function verdictOf(g: GroupDTO): Verdict {
  return g.verdict === "keep" || g.verdict === "cut" ? g.verdict : "";
}

/** An unset mask means both halves, which is what the store writes for one. */
export function maskOf(g: GroupDTO): Mask {
  return g.mask === "r" || g.mask === "j" ? g.mask : "rj";
}

export function hasHalf(g: GroupDTO, half: Half): boolean {
  return half === "r" ? g.hasRaw : g.hasJpeg;
}

/** maskHolds reports whether m is holding on to the named half. */
export function maskHolds(m: Mask, half: Half): boolean {
  return m === "rj" || m === half;
}

/**
 * clampMask narrows m to the halves the frame actually has, so a keep on a
 * JPEG-only frame never claims to be holding a RAW that was never there.
 */
export function clampMask(m: Mask, g: GroupDTO): Mask {
  if (!g.hasRaw && !g.hasJpeg) return m;
  if (!g.hasRaw) return "j";
  if (!g.hasJpeg) return "r";
  return m;
}

/**
 * toggled is the mask that results from flipping one half. Null means the
 * flip would leave nothing on disk, which is a cut and has its own key —
 * a mask can hold one half or both, never neither.
 */
export function toggled(m: Mask, half: Half): Mask | null {
  const other: Half = half === "r" ? "j" : "r";
  if (m === "rj") return other;
  if (m === half) return null;
  return "rj";
}

/**
 * halfState is what the R or J badge shows. An undecided frame loses nothing,
 * so both its halves read as kept; a cut takes the whole frame unless the user
 * has scoped cuts to the mask, in which case it reads exactly like a keep.
 */
export function halfState(g: GroupDTO, half: Half, cut: CutScope): HalfState {
  if (!hasHalf(g, half)) return "absent";
  switch (verdictOf(g)) {
    case "keep":
      return maskHolds(maskOf(g), half) ? "kept" : "cut";
    case "cut":
      return cut === "masked" && maskHolds(maskOf(g), half) ? "kept" : "cut";
    default:
      return "kept";
  }
}

/** The word the tile and the loupe badge carry. Undecided says nothing. */
export function verdictWord(v: Verdict): string {
  return v === "" ? "" : v.toUpperCase();
}

/**
 * fileLabel is the half's own extension, upper-cased — RAF, NEF, JPG. It comes
 * from the path rather than a table, because the extension the photographer's
 * camera wrote is the one they will look for.
 */
export function fileLabel(path: string, fallback: string): string {
  const dot = path.lastIndexOf(".");
  if (dot === -1 || dot === path.length - 1) return fallback;
  return path.slice(dot + 1).toUpperCase();
}

/**
 * PLAN_TERMS names each plan count in the verdict vocabulary. The backend
 * still keys them in the pre-verdict one, and the mapping is exact:
 * a keep is named by the half it drops, and a cut takes the frame.
 */
export const PLAN_TERMS: Record<string, string> = {
  keep_all: "keep · both halves",
  drop_jpeg: "keep · RAW only",
  drop_raw: "keep · JPEG only",
  drop_all: "cut · whole frame",
};

/** The order plan counts are listed in: gentlest first, whole frame last. */
export const PLAN_ORDER = ["keep_all", "drop_jpeg", "drop_raw", "drop_all"];

/**
 * shotLabel renders the RFC3339 timestamp the scan recorded. An unparseable
 * or absent value is shown as unknown rather than as a broken date.
 */
export function shotLabel(shot: string): string {
  if (shot === "") return "unknown";
  const at = new Date(shot);
  if (Number.isNaN(at.getTime())) return "unknown";
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ` +
    `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`
  );
}

/** kindLabel spells out what files a frame is made of. */
export function kindLabel(kind: string): string {
  switch (kind) {
    case "paired":
      return "RAW + JPEG";
    case "jpeg-only":
      return "JPEG only";
    case "raw-only":
      return "RAW only";
    default:
      return kind === "" ? "unknown" : kind;
  }
}
