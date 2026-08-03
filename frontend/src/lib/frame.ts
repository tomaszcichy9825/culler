// What the loupe's panels say about a frame.
//
// verdict.ts owns the model — what a verdict and a mask mean, what each half
// is doing under them. This is the layer above it: the same facts arranged
// the way the loupe-first rail and the overlay's card draw them, so the two
// screens share one set of derivations instead of each growing its own.
//
// The rule this file exists to keep: nothing is invented. Every value comes
// off GroupDTO, and a fact the backend does not carry yet — exposure, lens,
// file sizes — is left out rather than guessed at. A loupe that shows a
// plausible aperture is worse than one that shows none, because the user
// cannot tell which of the two it is doing.

import type { GroupDTO } from "./bindings";
import { formatBytes } from "./preview";
import { HALVES, fileLabel, halfState, kindLabel, shotLabel, verdictOf, verdictWord } from "./verdict";
import type { CutScope, Half, HalfState } from "./verdict";

export interface FileRow {
  half: Half;
  name: string;
  state: HalfState;
  /** Formatted size, or "" when the backend has not told us one. */
  size: string;
}

/** Sizes in bytes for a frame's halves, when a caller happens to have them. */
export interface Bytes {
  raw?: number;
  jpeg?: number;
}

/** fileName is what to call one half in a list: its basename, or the stem. */
export function fileName(g: GroupDTO, half: Half): string {
  const path = half === "r" ? g.rawPath : g.jpegPath;
  if (path === "") return g.stem;
  return path.split(/[/\\]/).pop() || g.stem;
}

/**
 * fileRows is the "files kept" block: one row per half the frame actually
 * has. A half that is not there gets no row — the filmstrip badges are where
 * absence is worth drawing, because there the two badges are a fixed pair and
 * a gap would have to be interpreted.
 *
 * Sizes are optional because GroupDTO carries none. The design draws a size
 * against each row and a delta against the cut one; both appear the moment a
 * caller can supply the bytes, and until then the rows simply do without.
 */
export function fileRows(g: GroupDTO, cut: CutScope, bytes?: Bytes): FileRow[] {
  const rows: FileRow[] = [];
  for (const half of HALVES) {
    const state = halfState(g, half, cut);
    if (state === "absent") continue;
    const n = half === "r" ? bytes?.raw : bytes?.jpeg;
    const size = n === undefined ? "" : `${state === "cut" ? "−" : ""}${formatBytes(n)}`;
    rows.push({ half, name: fileName(g, half), state, size });
  }
  return rows;
}

/**
 * verdictBadge is the chip over the top-left corner of the image. It names
 * what would survive rather than repeating the mask, because "RAW ONLY" is
 * the fact being decided and "rj" is an implementation detail. On a frame
 * made of one file the survivor is not worth saying twice, so the badge is
 * just the verdict.
 */
export function verdictBadge(g: GroupDTO, cut: CutScope): { label: string; tone: "keep" | "cut" | "none" } {
  const verdict = verdictOf(g);
  if (verdict === "") return { label: "UNDECIDED", tone: "none" };

  const word = verdictWord(verdict);
  if (g.kind !== "paired") return { label: word, tone: verdict };

  const raw = halfState(g, "r", cut) === "kept";
  const jpeg = halfState(g, "j", cut) === "kept";
  if (raw && jpeg) return { label: `${word} · RAW + JPEG`, tone: verdict };
  if (raw) return { label: `${word} · RAW ONLY`, tone: verdict };
  if (jpeg) return { label: `${word} · JPEG ONLY`, tone: verdict };
  return { label: word, tone: verdict };
}

/**
 * shotLine is the capture time as the panel headers draw it, with the
 * separator the design uses between the date and the clock.
 */
export function shotLine(g: GroupDTO): string {
  return shotLabel(g.shot).replace(" ", " · ");
}

export interface FactRow {
  key: string;
  value: string;
  /** `dim` for an absent value, `warn` for something the scan flagged. */
  tone?: "dim" | "warn";
}

export interface FactSection {
  title: string;
  rows: FactRow[];
}

/**
 * factSections is the stack of labelled sections beneath the verdict block.
 *
 * The design calls these the EXIF sections, and they are where EXIF will go.
 * The scan does not read EXIF yet beyond the capture time, so what is drawn
 * is what it genuinely knows: the frame, its files, and anything it warned
 * about. An `Exposure` section is a matter of appending to this list once the
 * DTO carries one.
 */
export function factSections(g: GroupDTO): FactSection[] {
  const shot = shotLabel(g.shot);
  const sections: FactSection[] = [
    {
      title: "Frame",
      rows: [
        { key: "file", value: g.stem },
        { key: "files", value: kindLabel(g.kind) },
        shot === "unknown" ? { key: "shot", value: "not recorded", tone: "dim" } : { key: "shot", value: shot },
        g.rating > 0 ? { key: "rating", value: `${g.rating} of 5` } : { key: "rating", value: "unrated", tone: "dim" },
      ],
    },
    {
      title: "Files",
      rows: [
        g.hasRaw
          ? { key: fileLabel(g.rawPath, "RAW").toLowerCase(), value: fileName(g, "r") }
          : { key: "raw", value: "none", tone: "dim" },
        g.hasJpeg
          ? { key: fileLabel(g.jpegPath, "JPEG").toLowerCase(), value: fileName(g, "j") }
          : { key: "jpeg", value: "none", tone: "dim" },
        ...(g.sidecars > 0 ? [{ key: "sidecars", value: String(g.sidecars) }] : []),
        { key: "folder", value: g.dir },
      ],
    },
  ];

  const warnings = g.warnings ?? [];
  if (warnings.length > 0) {
    sections.push({
      title: "Warnings",
      rows: warnings.map((w, i) => ({ key: `note ${i + 1}`, value: w, tone: "warn" as const })),
    });
  }
  return sections;
}
