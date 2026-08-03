// LIBRARY mode's state: the roots the catalogue covers, the search over them,
// the sessions they group into and what they are holding on disk.
//
// The backend is injected rather than imported. LIBRARY talks to a service the
// shell registers, and this module is written so that the components can be
// mounted in the harness, or against a stub, without a running Wails app. Call
// `connectCatalog` once at startup with the generated binding, and
// `watchCatalogProgress` once to feed the indexing chip.

/** One folder the catalogue covers. Mirrors the backend's RootDTO. */
export interface CatalogRoot {
  path: string;
  volume: string;
  added: string;
  /** RFC3339, or "" when the root has never been indexed. */
  lastIndexed: string;
  frames: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
}

/**
 * One catalogued frame. Mirrors the backend's FrameDTO.
 *
 * The path fields carry the same names GroupDTO uses, so `previewURL` reads a
 * catalogued frame exactly as it reads an open one.
 */
export interface CatalogFrame {
  hash: string;
  dir: string;
  stem: string;
  kind: string;
  /** RFC3339, as recorded at index time. */
  shot: string;
  hasRaw: boolean;
  hasJpeg: boolean;
  rawPath: string;
  jpegPath: string;
  verdict: string;
  rating: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
}

/** The facet chips. Every empty field is "no opinion". */
export interface CatalogFacets {
  kind: string;
  verdict: string;
  minRating: number;
  root: string;
  from: string;
  to: string;
}

export interface CatalogSearch {
  frames: CatalogFrame[];
  total: number;
  offset: number;
  /** How long the query itself took, in milliseconds. Measured, not guessed. */
  elapsed: number;
}

/** One row of a facet list. Mirrors the backend's FacetCountDTO. */
export interface CatalogFacetCount {
  value: string;
  label: string;
  frames: number;
}

/** What the facet lists are drawn from. Mirrors the backend's CountsDTO. */
export interface CatalogCounts {
  total: number;
  kinds: CatalogFacetCount[];
  verdicts: CatalogFacetCount[];
  ratings: CatalogFacetCount[];
}

/** One shoot. Mirrors the backend's SessionDTO. */
export interface CatalogSession {
  id: string;
  start: string;
  end: string;
  spanMinutes: number;
  frames: number;
  kept: number;
  cut: number;
  undecided: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
  source: string;
  dir: string;
  dirs: number;
}

export interface CatalogStorageRoot {
  root: string;
  volume: string;
  frames: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
}

export interface CatalogStorageVolume {
  volume: string;
  frames: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
  roots: string[];
}

export interface CatalogStorage {
  frames: number;
  rawBytes: number;
  jpegBytes: number;
  bytes: number;
  roots: CatalogStorageRoot[];
  volumes: CatalogStorageVolume[];
}

/** What an index pass reports. Mirrors the backend's CatalogProgress. */
export interface CatalogProgress {
  root: string;
  dir: string;
  dirs: number;
  frames: number;
  done: boolean;
  error: string;
}

/**
 * CatalogSource is the backend, as this module needs it. The shell satisfies
 * it with the generated LibraryIndexService binding.
 */
export interface CatalogSource {
  Roots(): Promise<CatalogRoot[]>;
  RegisterRoot(dir: string): Promise<CatalogRoot[]>;
  RemoveRoot(dir: string): Promise<CatalogRoot[]>;
  Reindex(dir: string): Promise<void>;
  Search(query: string, facets: CatalogFacets, limit: number, offset: number): Promise<CatalogSearch>;
  Counts(query: string, facets: CatalogFacets): Promise<CatalogCounts>;
  Sessions(gapHours: number): Promise<CatalogSession[]>;
  Storage(): Promise<CatalogStorage>;
}

export const NO_FACETS: CatalogFacets = {
  kind: "",
  verdict: "",
  minRating: 0,
  root: "",
  from: "",
  to: "",
};

/** The kinds and verdicts the facet chips offer, in the order they are drawn. */
export const KIND_FACETS = ["paired", "raw-only", "jpeg-only"];
export const VERDICT_FACETS = ["keep", "cut", "undecided"];

/** How many results one page holds. The grid asks for the next as it scrolls. */
const PAGE = 240;

/** How long the query field settles before a search goes out. */
const DEBOUNCE_MS = 120;

let source: CatalogSource | null = null;

/** connectCatalog gives the module its backend. Call once, at startup. */
export function connectCatalog(backend: CatalogSource) {
  source = backend;
}

let openFolderHandler: ((dir: string) => void) | null = null;

/**
 * onOpenFolder registers what happens when a result is opened. The shell hands
 * over its own openFolder action; this module does not reach into it, so that
 * LIBRARY has no opinion about how CULL loads a folder.
 */
export function onOpenFolder(handler: (dir: string) => void) {
  openFolderHandler = handler;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

class LibraryState {
  /** The folders the catalogue covers. */
  roots = $state<CatalogRoot[]>([]);

  /** The query field, verbatim — `key:value` tokens and all. */
  query = $state("");
  facets = $state<CatalogFacets>({ ...NO_FACETS });

  /** The loaded page or pages of results, oldest request first. */
  results = $state<CatalogFrame[]>([]);
  total = $state(0);
  elapsed = $state(0);

  /** What the facet lists are drawn from, or null before the first search. */
  counts = $state<CatalogCounts | null>(null);

  sessions = $state<CatalogSession[]>([]);
  /** The break that ends a session, in hours. Four is the backend's default. */
  sessionGapHours = $state(4);
  selectedSession = $state<string | null>(null);

  storage = $state<CatalogStorage | null>(null);

  /** Which result has the keyboard, as an index into `results`. */
  focusIndex = $state(0);

  loading = $state(false);
  error = $state<string | null>(null);

  /** The last progress from a running pass, or null when nothing is indexing. */
  indexing = $state<CatalogProgress | null>(null);

  /** Whether every result the catalogue holds is already loaded. */
  get complete(): boolean {
    return this.results.length >= this.total;
  }

  get focused(): CatalogFrame | null {
    return this.results[this.focusIndex] ?? null;
  }

  get session(): CatalogSession | null {
    return this.sessions.find((s) => s.id === this.selectedSession) ?? null;
  }

  /** True once a search has run, so an empty grid can tell empty from unasked. */
  searched = $state(false);

  #timer: ReturnType<typeof setTimeout> | null = null;
  /** Rising ticket, so a slow search cannot land on top of a newer one. */
  #ticket = 0;

  /** setQuery types into the field and schedules the search behind it. */
  setQuery(value: string) {
    this.query = value;
    this.scheduleSearch();
  }

  /** setFacet flips one chip. Passing the value it already holds clears it. */
  setFacet<K extends keyof CatalogFacets>(key: K, value: CatalogFacets[K]) {
    const cleared = key === "minRating" ? 0 : "";
    const next = this.facets[key] === value ? (cleared as CatalogFacets[K]) : value;
    this.facets = { ...this.facets, [key]: next };
    void this.search();
  }

  clearFacets() {
    this.facets = { ...NO_FACETS };
    void this.search();
  }

  /** How many chips are currently narrowing the search. */
  get activeFacets(): number {
    return (Object.keys(NO_FACETS) as (keyof CatalogFacets)[]).filter(
      (key) => this.facets[key] !== NO_FACETS[key],
    ).length;
  }

  scheduleSearch() {
    if (this.#timer !== null) clearTimeout(this.#timer);
    this.#timer = setTimeout(() => {
      this.#timer = null;
      void this.search();
    }, DEBOUNCE_MS);
  }

  /** search replaces the results with the first page of a fresh query. */
  async search() {
    if (source === null) return;
    const ticket = ++this.#ticket;
    this.loading = true;
    this.error = null;
    try {
      const facets = { ...this.facets };
      const [page, counts] = await Promise.all([
        source.Search(this.query, facets, PAGE, 0),
        source.Counts(this.query, facets),
      ]);
      if (ticket !== this.#ticket) return; // a newer search has already answered
      this.results = page.frames ?? [];
      this.total = page.total;
      this.elapsed = page.elapsed;
      this.counts = counts;
      this.focusIndex = 0;
      this.searched = true;
    } catch (error) {
      if (ticket !== this.#ticket) return;
      this.error = message(error);
      this.results = [];
      this.total = 0;
    } finally {
      if (ticket === this.#ticket) this.loading = false;
    }
  }

  /** loadMore appends the next page, which is what scrolling to the end does. */
  async loadMore() {
    if (source === null || this.loading || this.complete) return;
    const ticket = this.#ticket;
    const offset = this.results.length;
    this.loading = true;
    try {
      const page = await source.Search(this.query, { ...this.facets }, PAGE, offset);
      // Only append if nothing has changed the query underneath: a page from
      // the previous search would interleave two different results.
      if (ticket !== this.#ticket || offset !== this.results.length) return;
      this.results = [...this.results, ...(page.frames ?? [])];
      this.total = page.total;
    } catch (error) {
      this.error = message(error);
    } finally {
      this.loading = false;
    }
  }

  focus(index: number) {
    if (index < 0 || index >= this.results.length) return;
    this.focusIndex = index;
  }

  /** open hands a result's folder to the shell, which loads it into CULL. */
  open(frame: CatalogFrame) {
    this.openDir(frame.dir);
  }

  /** openDir is the same handoff from a session or a root, which has no frame. */
  openDir(dir: string) {
    if (dir !== "") openFolderHandler?.(dir);
  }

  selectSession(id: string) {
    this.selectedSession = id;
  }

  async loadRoots() {
    if (source === null) return;
    try {
      this.roots = await source.Roots();
    } catch (error) {
      this.error = message(error);
    }
  }

  async addRoot(dir: string) {
    if (source === null || dir.trim() === "") return;
    this.error = null;
    try {
      this.roots = await source.RegisterRoot(dir.trim());
      await this.reindex(dir.trim());
    } catch (error) {
      this.error = message(error);
    }
  }

  async removeRoot(dir: string) {
    if (source === null) return;
    this.error = null;
    try {
      this.roots = await source.RemoveRoot(dir);
      await this.refresh();
    } catch (error) {
      this.error = message(error);
    }
  }

  /** reindex starts a background pass. An empty dir covers every root. */
  async reindex(dir = "") {
    if (source === null) return;
    this.error = null;
    try {
      await source.Reindex(dir);
      this.indexing = { root: dir, dir: "", dirs: 0, frames: 0, done: false, error: "" };
    } catch (error) {
      this.error = message(error);
    }
  }

  async loadSessions() {
    if (source === null) return;
    try {
      this.sessions = await source.Sessions(this.sessionGapHours);
      if (this.session === null) this.selectedSession = this.sessions[0]?.id ?? null;
    } catch (error) {
      this.error = message(error);
    }
  }

  setSessionGap(hours: number) {
    this.sessionGapHours = hours;
    void this.loadSessions();
  }

  async loadStorage() {
    if (source === null) return;
    try {
      this.storage = await source.Storage();
    } catch (error) {
      this.error = message(error);
    }
  }

  /** refresh reloads everything the catalogue can answer. */
  async refresh() {
    await Promise.all([this.loadRoots(), this.search(), this.loadSessions(), this.loadStorage()]);
  }

  /**
   * applyProgress takes one report from a running index pass. The final report
   * clears the chip and pulls the new rows in, since what the user is looking
   * at has just changed underneath them.
   */
  applyProgress(progress: CatalogProgress) {
    if (!progress.done) {
      this.indexing = progress;
      return;
    }
    this.indexing = null;
    if (progress.error !== "") {
      this.error = progress.error;
      return;
    }
    void this.refresh();
  }
}

export const library = new LibraryState();

/**
 * watchCatalogProgress subscribes to the backend's index progress for the life
 * of the app. Call it once, at startup, beside watchScanProgress.
 */
export async function watchCatalogProgress() {
  const { Events } = await import("@wailsio/runtime");
  Events.On("catalog:progress", (event: { data: CatalogProgress }) => {
    if (event.data) library.applyProgress(event.data);
  });
}

// --- formatting ---------------------------------------------------------
// Shared by every LIBRARY view, and pure, so a component never has to work out
// how to print a size or a span for itself.

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** formatBytes prints a size the way the design does: 24.6 GB, 940 MB, 0 B. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit === 0 ? 0 : value < 10 ? 1 : value < 100 ? 1 : 0;
  return `${value.toFixed(digits)} ${UNITS[unit]}`;
}

/** formatCount groups thousands, so 1204 reads as 1,204. */
export function formatCount(n: number): string {
  return n.toLocaleString("en-GB");
}

/** formatSpan prints a session's length: 4h 12m, 38m, or a dash for nothing. */
export function formatSpan(minutes: number): string {
  if (!Number.isFinite(minutes) || minutes <= 0) return "—";
  const hours = Math.floor(minutes / 60);
  const rest = Math.round(minutes % 60);
  if (hours === 0) return `${rest}m`;
  return rest === 0 ? `${hours}h` : `${hours}h ${rest}m`;
}

/**
 * The timestamp is sliced rather than read through Date, for the same reason
 * the table does it: it carries the camera's own offset, and a frame shot at
 * dusk must not move to the middle of the afternoon because the laptop is in
 * another zone.
 */
const STAMP = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/;

/** formatDate prints the shot date: 2026-05-01. */
export function formatDate(shot: string): string {
  const m = STAMP.exec(shot);
  return m === null ? "" : `${m[1]}-${m[2]}-${m[3]}`;
}

/** formatClock prints the shot time: 19:42. */
export function formatClock(shot: string): string {
  const m = STAMP.exec(shot);
  return m === null ? "" : `${m[4]}:${m[5]}`;
}

/** The last part of a path, which is what the user calls a folder. */
export function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter((p) => p !== "");
  return parts[parts.length - 1] ?? path;
}

/**
 * percent returns a share of a total as a CSS width, clamped so a rounding
 * error cannot push a stacked bar's last segment onto its own line.
 */
export function percent(part: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, (part / total) * 100));
}
