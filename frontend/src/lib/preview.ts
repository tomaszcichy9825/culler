// Preview URLs point at the asset server's /preview route, which streams the
// JPEG on disk when there is one and otherwise extracts the full-size preview
// embedded in the RAW. That second tier is what makes RAW-only frames viewable.

import type { GroupDTO } from "./bindings";

/**
 * previewURL builds the asset-server URL for a frame. size "grid" lets the
 * backend serve a locally cached thumbnail keyed on the frame's content hash
 * — instant on revisits, even over the network. "full" is byte-accurate
 * passthrough for the loupe, where colour fidelity matters.
 */
export function previewURL(g: GroupDTO, size: "grid" | "full" = "full"): string {
  const cache = size === "grid" && g.hash !== "" ? `&size=grid&hash=${encodeURIComponent(g.hash)}` : "";
  if (g.hasJpeg && g.jpegPath !== "") {
    return `/preview?path=${encodeURIComponent(g.jpegPath)}&tier=jpeg${cache}`;
  }
  if (g.hasRaw && g.rawPath !== "") {
    return `/preview?path=${encodeURIComponent(g.rawPath)}&tier=embedded${cache}`;
  }
  return "";
}

/**
 * developURL is tier 3: the RAW demosaiced at full resolution, which is the
 * only thing that puts real detail under a 1:1 zoom on a frame that has no
 * JPEG. It costs seconds and it only exists in a build made with -tags libraw,
 * so it is strictly an upgrade over the embedded preview and never a
 * requirement — an empty string means don't bother asking.
 */
export function developURL(g: GroupDTO): string {
  if (g.hasJpeg || !g.hasRaw || g.rawPath === "") return "";
  const cache = g.hash !== "" ? `&hash=${encodeURIComponent(g.hash)}` : "";
  return `/preview?path=${encodeURIComponent(g.rawPath)}&tier=develop${cache}`;
}

/** kindBadge is the one-letter marker for what files a frame is made of. */
export function kindBadge(kind: string): string {
  switch (kind) {
    case "paired":
      return "P";
    case "jpeg-only":
      return "J";
    case "raw-only":
      return "R";
    default:
      return "?";
  }
}

/** decisionBadge is the key that set the decision, which is how it reads back. */
export function decisionBadge(decision: string): string {
  switch (decision) {
    case "keep_all":
      return "1";
    case "drop_raw":
      return "2";
    case "drop_jpeg":
      return "3";
    case "drop_all":
      return "4";
    default:
      return "";
  }
}

export function decisionLabel(decision: string): string {
  switch (decision) {
    case "keep_all":
      return "keep all";
    case "drop_raw":
      return "drop RAW";
    case "drop_jpeg":
      return "drop JPEG";
    case "drop_all":
      return "drop both";
    default:
      return decision;
  }
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let value = n / 1024;
  let i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${units[i]}`;
}
