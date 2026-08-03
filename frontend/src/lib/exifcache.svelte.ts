// A lazy, shared read of frame metadata, for the panes that only display it.
//
// EXIF mode owns its own copy of this data (lib/exif.svelte.ts) because it is
// editing it: it holds a draft per frame, it has to know what is on disk to
// tell a change from a no-op, and it reloads the moment a write lands. Nothing
// here does any of that. The inspector and the table want one thing — the
// shutter speed of a frame that happens to be on screen — and they want it
// without a mode, a rail, or a draft.
//
// The rule is that a pane asks for what it is drawing and never for the folder.
// A card of nine hundred frames is nine hundred file reads, and the user has
// scrolled past eight hundred and eighty of them; requesting per rendered row
// keeps the cost proportional to what is actually being looked at. Everything
// asked for is remembered for the life of the session, so scrolling back up
// costs nothing, and a path is only ever read once no matter how many panes ask
// for it in the same frame.

import { SvelteMap } from "svelte/reactivity";
import { ExifService } from "./bindings";
import type { GroupDTO } from "./bindings";
import type { FrameExifDTO } from "./exif.svelte";
import { groupKey } from "./state.svelte";

/** How ExifService.Read is called. An interface so the bench can stand in. */
export type ExifReader = (paths: string[]) => Promise<Record<string, FrameExifDTO>>;

/**
 * Paths per call. Large enough that a screen of table rows is one read, small
 * enough that a fast scroll does not hand the backend a thousand files it will
 * have finished reading long after they left the viewport.
 */
const BATCH = 24;

/** Calls in flight at once, so a scroll cannot queue up unbounded work. */
const IN_FLIGHT = 3;

/**
 * Requests arriving in the same tick are collected before anything is sent.
 * Twenty rows rendering one after another is one read, not twenty.
 */
const COALESCE_MS = 16;

interface Wanted {
  /** The cache key: the frame's identity, not the file's path. */
  key: string;
  path: string;
}

/**
 * exifPathOf is the file a frame's metadata is read from. The JPEG when there
 * is one — it carries the same capture data as its RAW and is a fraction of the
 * size to open — and the RAW otherwise.
 */
export function exifPathOf(g: GroupDTO): string {
  if (g.hasJpeg && g.jpegPath !== "") return g.jpegPath;
  if (g.hasRaw && g.rawPath !== "") return g.rawPath;
  return "";
}

/** valueOf is what a frame carries for one tag, or "" when it carries none. */
export function valueOf(frame: FrameExifDTO | undefined, tag: string): string {
  if (frame === undefined) return "";
  const field = (frame.fields ?? []).find((f) => f.tag === tag);
  if (field === undefined || !field.present) return "";
  return field.value;
}

class ExifCache {
  /** Frame identity → what the backend said about it. */
  #frames = new SvelteMap<string, FrameExifDTO>();

  /**
   * Every path that has been asked about, whether it is queued, in flight or
   * long since answered. A path leaves this set only when the cache is cleared,
   * which is what makes a second request for the same file free — and what
   * stops a failed read being retried on every scroll tick.
   */
  #asked = new Set<string>();

  #queue: Wanted[] = [];
  #running = 0;
  #timer: ReturnType<typeof setTimeout> | null = null;

  #read: ExifReader = async (paths) => ((await ExifService.Read(paths)) ?? {}) as Record<string, FrameExifDTO>;

  /** Backend calls made. The bench asserts that N rows do not make N reads. */
  calls = 0;

  /** useReader swaps the backend out, for the bench and for a fake folder. */
  useReader(read: ExifReader) {
    this.#read = read;
  }

  /**
   * request asks for the metadata of frames that are on screen. It returns
   * nothing: a caller draws what `get` has, and redraws when the answer lands.
   * Asking twice for the same frame is free and is the expected usage — a row
   * requests on every render.
   */
  request(groups: GroupDTO[]) {
    let added = false;
    for (const g of groups) {
      const path = exifPathOf(g);
      if (path === "" || this.#asked.has(path)) continue;
      this.#asked.add(path);
      this.#queue.push({ key: groupKey(g), path });
      added = true;
    }
    if (!added || this.#timer !== null) return;
    this.#timer = setTimeout(() => {
      this.#timer = null;
      this.#drain();
    }, COALESCE_MS);
  }

  /** get is the metadata of one frame, or undefined while it is unknown. */
  get(key: string): FrameExifDTO | undefined {
    return this.#frames.get(key);
  }

  /** of is the same thing for callers that are holding the frame itself. */
  of(g: GroupDTO): FrameExifDTO | undefined {
    return this.#frames.get(groupKey(g));
  }

  /** clear forgets everything, which is what a new folder wants. */
  clear() {
    this.#frames.clear();
    this.#asked.clear();
    this.#queue = [];
    this.calls = 0;
  }

  #drain() {
    while (this.#running < IN_FLIGHT && this.#queue.length > 0) {
      const chunk = this.#queue.splice(0, BATCH);
      this.#running++;
      this.calls++;
      void this.#read(chunk.map((w) => w.path))
        .then((byPath) => {
          for (const w of chunk) {
            const frame = byPath[w.path];
            if (frame !== undefined) this.#frames.set(w.key, frame);
          }
        })
        .catch(() => {
          // Metadata is decoration on these two screens. A read that fails
          // leaves the rows reading as they did — an em-dash — rather than
          // taking a pane down, and is not tried again: a row that re-requests
          // on every scroll would otherwise hammer a backend that is down.
        })
        .finally(() => {
          this.#running--;
          this.#drain();
        });
    }
  }
}

export const exifCache = new ExifCache();
