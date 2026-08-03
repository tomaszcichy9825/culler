// Luma histogram sampling, shared by the inspector and the loupe card so a
// frame is only ever read once. The sample is drawn small — 6k pixels is
// plenty for a 40-bin shape — and cached by frame identity, because arrowing
// along a burst and back would otherwise re-read the same pixels every time.

export const BINS = 40;
const SAMPLE_W = 96;
const SAMPLE_H = 64;

const cache = new Map<string, number[]>();

/** The shape already sampled for a frame, if any. */
export function cachedHistogram(id: string): number[] | undefined {
  return cache.get(id);
}

/**
 * sampleHistogram reads a loaded image into a luma histogram, as bar heights
 * 0–100. Previews come from the app's own origin, so the canvas is never
 * tainted; a reader that throws anyway returns null rather than breaking the
 * caller's pane.
 */
export function sampleHistogram(img: HTMLImageElement, id: string): number[] | null {
  const hit = cache.get(id);
  if (hit !== undefined) return hit;

  const canvas = document.createElement("canvas");
  canvas.width = SAMPLE_W;
  canvas.height = SAMPLE_H;
  const ctx = canvas.getContext("2d", { willReadFrequently: true });
  if (ctx === null) return null;
  let pixels: Uint8ClampedArray;
  try {
    ctx.drawImage(img, 0, 0, SAMPLE_W, SAMPLE_H);
    pixels = ctx.getImageData(0, 0, SAMPLE_W, SAMPLE_H).data;
  } catch {
    return null;
  }

  const counts = new Array<number>(BINS).fill(0);
  for (let i = 0; i < pixels.length; i += 4) {
    const luma = pixels[i] * 0.2126 + pixels[i + 1] * 0.7152 + pixels[i + 2] * 0.0722;
    counts[Math.min(BINS - 1, Math.floor((luma / 256) * BINS))]++;
  }
  const peak = Math.max(1, ...counts);
  const shape = counts.map((c) => Math.round((c / peak) * 100));
  cache.set(id, shape);
  return shape;
}
