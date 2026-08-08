// MAP mode's state: where the open folder's photographs were taken, how those
// positions cluster on screen, and whether the user has agreed to fetch tiles.
//
// The backend is injected rather than imported, on the same terms as the
// catalogue in library.svelte.ts: the components mount in the harness, or
// against a stub, without a running Wails app. Call `connectMap` once at
// startup with the generated binding, `watchMapProgress` once to feed the
// progress chip, and `onOpenFrame` once with the shell's own open action.
//
// Two things are deliberately absent. There is no reverse geocoding: naming a
// coordinate means sending it to somebody else's server, which is the one thing
// the design carves out as ask-first and which this mode does not do at all —
// a place is called by its coordinates. And there is no geotagging: writing
// positions into files goes through EXIF mode's write plan, which already backs
// up what it replaces.

import type { GroupDTO } from "./bindings";
import { forget, remember, stored } from "./persist";

/** One frame that carries coordinates. Mirrors the backend's PositionDTO. */
export interface MapPosition {
  dir: string;
  stem: string;
  kind: string;
  hasRaw: boolean;
  hasJpeg: boolean;
  rawPath: string;
  jpegPath: string;
  /** RFC3339, as the camera recorded it. */
  shot: string;
  latitude: number;
  longitude: number;
  altitude: number;
  hasAltitude: boolean;
}

/** One folder's positions and the account of what had none. Mirrors PositionsDTO. */
export interface MapPositions {
  dir: string;
  frames: MapPosition[];
  total: number;
  positioned: number;
  unpositioned: number;
  unreadable: number;
}

/** What a positions read reports as it goes. Mirrors the backend's MapProgress. */
export interface MapProgress {
  dir: string;
  done: number;
  total: number;
}

/** One frame a scope names for the map, by folder and stem. Mirrors ScopeRef. */
export interface ScopeRef {
  dir: string;
  stem: string;
}

/** MapSource is the backend, as this module needs it. */
export interface MapSource {
  Positions(dir: string): Promise<MapPositions>;
  PositionsScope(refs: ScopeRef[]): Promise<MapPositions>;
}

/**
 * OpenFrameHandler is how the map hands a frame back to the shell, and is the
 * same contract the catalogue uses: `dir` is the folder to load, and `hash`
 * names one frame in it so the grid lands on the pin the user pressed ⏎ on
 * rather than at the top of the folder.
 *
 * The hash is optional because the backend's positions do not carry one —
 * hashing a folder is a full read of every file in it, which is far too much to
 * pay for a map. `attach` is how a hash gets in: give the map the open folder's
 * frames and it joins them on (dir, stem), which is the key the scanner groups
 * on. Without that join the handler is called with the folder alone, and a
 * shell is free to open it and focus nothing.
 */
export type OpenFrameHandler = (dir: string, hash?: string) => void;

let source: MapSource | null = null;
let openFrameHandler: OpenFrameHandler | null = null;

/** connectMap gives the module its backend. Call once, at startup. */
export function connectMap(backend: MapSource) {
  source = backend;
}

/** onOpenFrame registers what happens when a pin is opened. */
export function onOpenFrame(handler: OpenFrameHandler) {
  openFrameHandler = handler;
}

// --- tile consent -----------------------------------------------------------
// Map tiles are the one network call culler makes, and the app's non-goals say
// no cloud. The amendment that lets the map exist makes tiles opt-in, so the
// gate is a real gate: no tile layer is constructed, and so no request is made,
// until the answer on disk says yes.

/** Where the answer is kept. Read before the first tile layer is built. */
export const TILE_CONSENT_KEY = "culler.map.tiles";

export type TileConsent = "unasked" | "granted" | "declined";

/** The tile endpoint and the attribution that using it obliges. */
export const TILE_URL = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
export const TILE_ATTRIBUTION = "© OpenStreetMap contributors";
export const TILE_MAX_ZOOM = 19;

function readConsent(): TileConsent {
  // A webview with storage disabled has not consented to anything, which is
  // the answer that fetches nothing.
  return stored<TileConsent>(
    TILE_CONSENT_KEY,
    (raw) => (raw === "granted" || raw === "declined" ? raw : null),
    "unasked",
  );
}

function writeConsent(value: TileConsent) {
  if (value === "unasked") forget(TILE_CONSENT_KEY);
  else remember(TILE_CONSENT_KEY, value);
}

// --- clustering -------------------------------------------------------------

/** A screen position, as Leaflet projects one. */
export interface Point {
  x: number;
  y: number;
}

/** One pin: the frames that fell in the same cell of the screen grid. */
export interface MapCluster {
  /** Stable across a redraw at the same zoom, so a selection survives one. */
  id: string;
  /** The mean of the members' coordinates, which is where the pin is drawn. */
  latitude: number;
  longitude: number;
  frames: MapPosition[];
  count: number;
}

/** How wide a cluster cell is on screen. Comfortably wider than a pin. */
export const CLUSTER_CELL = 64;

/**
 * clusterPositions groups frames that land within the same square of the
 * screen. It takes the projection rather than a map, so the maths is testable
 * without Leaflet and the same function serves the pin layer and the places
 * list — the left rail lists exactly the pins that are drawn, which is the
 * whole point of it.
 *
 * Cells are cut in projected pixels, not in degrees: a degree of longitude is
 * a different distance at every latitude, and a grid in degrees would cluster
 * differently at the top of the map than at the bottom of it.
 *
 * Order is the order the frames arrived, which the backend already sorted into
 * capture order — so the places list reads oldest first and does not reshuffle
 * under the pointer.
 */
export function clusterPositions(
  positions: MapPosition[],
  project: (p: MapPosition) => Point,
  cell: number = CLUSTER_CELL,
): MapCluster[] {
  const size = cell > 0 ? cell : CLUSTER_CELL;
  const byCell = new Map<string, MapPosition[]>();
  const order: string[] = [];

  for (const p of positions) {
    const at = project(p);
    if (!Number.isFinite(at.x) || !Number.isFinite(at.y)) continue;
    const key = `${Math.floor(at.x / size)}:${Math.floor(at.y / size)}`;
    const bucket = byCell.get(key);
    if (bucket === undefined) {
      byCell.set(key, [p]);
      order.push(key);
    } else {
      bucket.push(p);
    }
  }

  return order.map((key) => {
    const frames = byCell.get(key)!;
    let lat = 0;
    let lon = 0;
    for (const f of frames) {
      lat += f.latitude;
      lon += f.longitude;
    }
    return {
      id: `${key}·${frames[0].dir}/${frames[0].stem}`,
      latitude: lat / frames.length,
      longitude: lon / frames.length,
      frames,
      count: frames.length,
    };
  });
}

// --- formatting -------------------------------------------------------------

/**
 * formatCoords prints a position the way the inspector shows one and the way
 * a click-to-copy hands it over: six decimals, which is about a tenth of a
 * metre and more than any camera knows.
 */
export function formatCoords(lat: number, lon: number): string {
  return `${lat.toFixed(6)}, ${lon.toFixed(6)}`;
}

/**
 * placeName is what a cluster is called in the PLACES list. It is its
 * coordinates, at four decimals — about eleven metres, which is the resolution
 * a name is useful at. There is no reverse geocoding in this mode, so a place
 * has no other name to be called by, and inventing one from the folder would
 * give every pin in a folder the same label.
 */
export function placeName(cluster: MapCluster): string {
  return `${cluster.latitude.toFixed(4)}, ${cluster.longitude.toFixed(4)}`;
}

/** The last part of a path, which is what the user calls a folder. */
export function leafName(path: string): string {
  const parts = path.split(/[/\\]/).filter((p) => p !== "");
  return parts[parts.length - 1] ?? path;
}

/**
 * The timestamp is sliced rather than read through Date, for the same reason
 * the rest of the app does it: it carries the camera's own offset, and a frame
 * shot at dusk must not move to the middle of the afternoon because the laptop
 * is in another zone.
 */
const STAMP = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2}):(\d{2})/;

/** formatClock prints a frame's time of day: 19:42:07. */
export function formatClock(shot: string): string {
  const m = STAMP.exec(shot);
  return m === null ? "" : `${m[4]}:${m[5]}:${m[6]}`;
}

/** formatDate prints a frame's day: 2026-07-18. */
export function formatDate(shot: string): string {
  const m = STAMP.exec(shot);
  return m === null ? "" : `${m[1]}-${m[2]}-${m[3]}`;
}

/** formatAltitude prints a height the way the inspector lists one. */
export function formatAltitude(metres: number): string {
  return `${Math.round(metres)} m`;
}

/**
 * positionToGroup is a positioned frame as the preview route reads one. The
 * hash is filled in from the open folder when the map has been given it, and
 * left empty otherwise — an empty hash costs the grid-sized cache and nothing
 * else, since previewURL falls back to serving the file itself.
 */
export function positionToGroup(p: MapPosition, hash = ""): GroupDTO {
  return {
    dir: p.dir,
    stem: p.stem,
    kind: p.kind,
    hasRaw: p.hasRaw,
    hasJpeg: p.hasJpeg,
    rawPath: p.rawPath,
    jpegPath: p.jpegPath,
    sidecars: 0,
    shot: p.shot,
    warnings: [],
    verdict: "",
    mask: "",
    rating: 0,
    hash,
    destination: "",
    decision: "",
  };
}

/** frameKey is the identity a position and an open frame agree on. */
export function frameKey(dir: string, stem: string): string {
  return `${dir}/${stem}`;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// --- the state --------------------------------------------------------------

class MapState {
  /** The folder the positions belong to, or "" before anything is loaded. */
  dir = $state("");

  /** Frames with coordinates, oldest first. */
  positions = $state<MapPosition[]>([]);

  /** What the folder held, as the backend accounted for it. */
  total = $state(0);
  unpositioned = $state(0);
  unreadable = $state(0);

  loading = $state(false);
  error = $state<string | null>(null);

  /** The last report from a running read, or null when nothing is reading. */
  progress = $state<MapProgress | null>(null);

  /** Whether the user has agreed to fetch tiles. Read from storage at startup. */
  consent = $state<TileConsent>(readConsent());

  /** The clusters currently drawn, recomputed by the map pane as it moves. */
  clusters = $state<MapCluster[]>([]);

  /** Which cluster has the keyboard, as an index into `clusters`. */
  clusterIndex = $state(0);

  /** Which frame within the focused cluster the inspector is showing. */
  frameIndex = $state(0);

  /** Identity → hash, from the open folder, for grid-sized previews. */
  #hashes = new Map<string, string>();

  get tilesEnabled(): boolean {
    return this.consent === "granted";
  }

  /** True once the first-use panel has been answered either way. */
  get asked(): boolean {
    return this.consent !== "unasked";
  }

  get cluster(): MapCluster | null {
    return this.clusters[this.clusterIndex] ?? null;
  }

  /** The frame the inspector is drawing, or null when nothing is focused. */
  get frame(): MapPosition | null {
    return this.cluster?.frames[this.frameIndex] ?? null;
  }

  /** How the "n frames have no GPS" chip reads, or "" when they all have one. */
  get withoutGPS(): string {
    const n = this.unpositioned + this.unreadable;
    if (n === 0) return "";
    return `${n} ${n === 1 ? "frame has" : "frames have"} no GPS`;
  }

  /** The hash of a positioned frame, or "" when the map has not been given one. */
  hashOf(p: MapPosition): string {
    return this.#hashes.get(frameKey(p.dir, p.stem)) ?? "";
  }

  /**
   * attach hands the map the open folder's frames, purely so a pin can draw
   * the same cached thumbnail the grid does. Nothing about the map depends on
   * it: a map that was never given them draws full-size previews instead.
   */
  attach(groups: GroupDTO[]) {
    const hashes = new Map<string, string>();
    for (const g of groups) {
      if (g.hash !== "") hashes.set(frameKey(g.dir, g.stem), g.hash);
    }
    this.#hashes = hashes;
  }

  grantTiles() {
    this.consent = "granted";
    writeConsent("granted");
  }

  declineTiles() {
    this.consent = "declined";
    writeConsent("declined");
  }

  /** forgetConsent puts the question back, which is what a settings reset does. */
  forgetConsent() {
    this.consent = "unasked";
    writeConsent("unasked");
  }

  /**
   * setClusters replaces what the map is drawing and keeps the focus inside
   * it. It works out the new focus from the array it was handed rather than
   * from `this.clusters` after assigning: a caller in an effect that read the
   * clusters back would be reading state it had just written, which Svelte
   * quite rightly refuses to keep re-running.
   */
  setClusters(clusters: MapCluster[]) {
    const index = this.clusterIndex >= clusters.length ? Math.max(0, clusters.length - 1) : this.clusterIndex;
    const frames = clusters[index]?.count ?? 0;
    this.clusters = clusters;
    if (this.clusterIndex !== index) this.clusterIndex = index;
    if (this.frameIndex >= frames) this.frameIndex = 0;
  }

  focusCluster(index: number) {
    if (index < 0 || index >= this.clusters.length) return;
    this.clusterIndex = index;
    this.frameIndex = 0;
  }

  focusFrame(index: number) {
    if (index < 0 || index >= (this.cluster?.count ?? 0)) return;
    this.frameIndex = index;
  }

  /** open hands the focused frame to the shell. */
  open(p: MapPosition | null = this.frame) {
    if (p === null || p.dir === "") return;
    const hash = this.hashOf(p);
    openFrameHandler?.(p.dir, hash === "" ? undefined : hash);
  }

  /**
   * load reads a folder's positions. Calling it with the folder already loaded
   * is a no-op unless `force` says otherwise, so a pane can call it on mount.
   */
  async load(dir: string, force = false) {
    if (source === null || dir === "") return;
    this.#lastLoad = () => this.load(dir, true);
    // The dir is claimed before the await, the way loadScope claims its scope
    // key: the pane's effect re-runs on every batch of a streamed scan, and a
    // guard that only held once the read had landed re-fired a full Positions
    // read — and cleared the clusters — for each one, pinning the pane on
    // "reading positions" for the length of the scan.
    if (!force && dir === this.dir) return;
    this.dir = dir;

    const seq = ++this.#loadSeq;
    this.loading = true;
    this.error = null;
    this.progress = null;
    // The clusters describe the folder being replaced, and a pane that reads
    // them between here and the map's recut would draw the last folder's pins
    // over the new one's frames.
    this.clusters = [];
    try {
      const got = await source.Positions(dir);
      // A newer read started while this one was in flight — opening B while A's
      // slow network read was still out — so its answer is stale and must not
      // paint B's map with A's pins. `dir` is not overwritten with the echo:
      // the guard above compares against what the caller asks with, and a
      // backend that resolved the path differently would defeat it.
      if (seq !== this.#loadSeq) return;
      this.positions = got.frames ?? [];
      this.total = got.total;
      this.unpositioned = got.unpositioned;
      this.unreadable = got.unreadable;
      this.clusterIndex = 0;
      this.frameIndex = 0;
    } catch (error) {
      if (seq !== this.#loadSeq) return;
      // The claim taken on `dir` above only stands for a read that answered.
      // A failed read hands it back — otherwise the guard would short-circuit
      // every later run of the pane's effect, pinning the error on screen with
      // no retry until the user navigated away. The pane's effect re-fires on
      // each batch of a streamed scan, so the next batch (or the next visit)
      // simply tries again.
      this.dir = "";
      this.error = message(error);
      this.positions = [];
      this.total = 0;
      this.unpositioned = 0;
      this.unreadable = 0;
      this.clusters = [];
    } finally {
      // Only the newest read owns the busy flag; a superseded one clearing it
      // would blank the state the live read is still filling.
      if (seq === this.#loadSeq) {
        this.loading = false;
        this.progress = null;
      }
    }
  }

  /** The scope last read, so an unchanged scope does not re-read on every tick. */
  #scopeKey = "";

  /** Bumped on every read; a response from an older one is stale and dropped. */
  #loadSeq = 0;

  /** How to re-read what is currently shown, for a refresh after a geotag write. */
  #lastLoad: (() => Promise<void>) | null = null;

  /** reload re-reads whatever the map last loaded, folder or scope, forcing it
   *  past the no-op guard. It is how a geotag write shows up on the map. */
  reload(): Promise<void> {
    return this.#lastLoad?.() ?? Promise.resolve();
  }

  /**
   * loadScope reads the positions of the frames a scope names, which may span
   * folders — a session view. It is load's counterpart for MAP following the
   * global scope rather than a single open folder: only the named frames are
   * plotted, so a session that is a subset of a folder does not drag the rest
   * of that folder onto the map.
   *
   * Like load it is a no-op when the scope has not changed, so the pane can
   * drive it from an effect that also fires while a folder streams in.
   */
  async loadScope(refs: ScopeRef[], force = false) {
    if (source === null) return;
    this.#lastLoad = () => this.loadScope(refs, true);
    const key = refs
      .map((r) => frameKey(r.dir, r.stem))
      .sort()
      .join(" ");
    if (!force && key === this.#scopeKey && this.positions.length > 0) return;
    this.#scopeKey = key;

    if (refs.length === 0) {
      this.clear();
      return;
    }

    const seq = ++this.#loadSeq;
    this.loading = true;
    this.error = null;
    this.progress = null;
    this.clusters = [];
    try {
      const got = await source.PositionsScope(refs);
      if (seq !== this.#loadSeq) return;
      this.dir = got.dir;
      this.positions = got.frames ?? [];
      this.total = got.total;
      this.unpositioned = got.unpositioned;
      this.unreadable = got.unreadable;
      this.clusterIndex = 0;
      this.frameIndex = 0;
    } catch (error) {
      if (seq !== this.#loadSeq) return;
      this.error = message(error);
      this.positions = [];
      this.total = 0;
      this.unpositioned = 0;
      this.unreadable = 0;
      this.clusters = [];
    } finally {
      if (seq === this.#loadSeq) {
        this.loading = false;
        this.progress = null;
      }
    }
  }

  /** clear forgets a folder, which is what opening another one wants. */
  clear() {
    this.dir = "";
    this.positions = [];
    this.clusters = [];
    this.total = 0;
    this.unpositioned = 0;
    this.unreadable = 0;
    this.clusterIndex = 0;
    this.frameIndex = 0;
    this.error = null;
    this.progress = null;
    this.#hashes = new Map();
    this.#scopeKey = "";
  }

  applyProgress(progress: MapProgress) {
    // Progress only means anything while a read is in flight. The old guard
    // compared against `this.dir`, which is not set until the read finishes, so
    // every folder after the first showed no progress at all — the new folder's
    // events never matched the previous folder's still-current dir.
    if (!this.loading) return;
    this.progress = progress.done >= progress.total ? null : progress;
  }
}

export const mapState = new MapState();

/**
 * watchMapProgress subscribes to the backend's position reads for the life of
 * the app. Call it once, at startup, beside watchCatalogProgress.
 */
export async function watchMapProgress() {
  const { Events } = await import("@wailsio/runtime");
  Events.On("map:progress", (event: { data: MapProgress }) => {
    if (event.data) mapState.applyProgress(event.data);
  });
}
