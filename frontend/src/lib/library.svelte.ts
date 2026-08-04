// The catalogue's state: the roots it covers, the search over them, the
// sessions they group into and what they are holding on disk.
//
// The catalogue is no longer a room of its own. Its tree is CULL's sidebar,
// its search is the bar `/` opens over the grid, and its sessions are a group
// under Sources — so this module is the one place that knows what is indexed,
// and three parts of one screen read it.
//
// The backend is injected rather than imported. The catalogue talks to a
// service the shell registers, and this module is written so that the
// components can be mounted in the harness, or against a stub, without a
// running Wails app. Call `connectCatalog` once at startup with the generated
// binding, and `watchCatalogProgress` once to feed the indexing chip.

import type { GroupDTO } from "./bindings";

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

/**
 * One folder in the tree. Mirrors the backend's TreeNodeDTO.
 *
 * `frames` counts everything at or under the folder; `direct` is what is filed
 * in the folder itself. `undecided` is measured against the decision store as
 * it stands now, or UNDECIDED_UNKNOWN on a folder too big to have counted.
 */
export interface CatalogTreeNode {
  path: string;
  name: string;
  frames: number;
  direct: number;
  undecided: number;
  bytes: number;
  hasDirs: boolean;
  isRoot: boolean;
}

/** What a node reports when its undecided count was not worked out. */
export const UNDECIDED_UNKNOWN = -1;

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
  /**
   * The tree's two calls. They are optional so that a shell which has not
   * wired them yet still type-checks: without them the tree draws the roots
   * flat, with no expansion, rather than failing to mount.
   */
  TreeRoots?(): Promise<CatalogTreeNode[]>;
  TreeChildren?(dir: string): Promise<CatalogTreeNode[]>;
}

/**
 * frameToGroup is a catalogued frame as the grid reads one. The two carry the
 * same paths under the same names deliberately, so a result renders through
 * exactly the tile, the loupe and the table an open folder does, and the grid
 * needs no idea that it is showing an index rather than a directory.
 *
 * What the index does not record comes back empty rather than invented: a
 * search result has no sidecar count, no warnings and no destination, and its
 * mask is left unset, which everything that reads one takes as both halves.
 */
export function frameToGroup(frame: CatalogFrame): GroupDTO {
  return {
    dir: frame.dir,
    stem: frame.stem,
    kind: frame.kind,
    hasRaw: frame.hasRaw,
    hasJpeg: frame.hasJpeg,
    rawPath: frame.rawPath,
    jpegPath: frame.jpegPath,
    sidecars: 0,
    shot: frame.shot,
    warnings: [],
    verdict: frame.verdict,
    mask: "",
    rating: frame.rating,
    hash: frame.hash,
    destination: "",
    decision: "",
  };
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

/**
 * OpenFolderHandler is how the catalogue hands a folder to the grid.
 *
 * `dir` is the folder to load. `focusHash` names one frame in it and is what a
 * search result passes, so that opening a tile lands on the frame the user was
 * looking at rather than at the top of its folder. A tree node and a session
 * row have no one frame in mind and leave it out; a handler that does not know
 * the hash — the frame has moved, the folder has changed — is free to ignore
 * it and open the folder anyway.
 */
export type OpenFolderHandler = (dir: string, focusHash?: string) => void;

let openFolderHandler: OpenFolderHandler | null = null;

/**
 * onOpenFolder registers what happens when a result is opened. The shell hands
 * over its own openFolder action; this module does not reach into it, so that
 * the catalogue has no opinion about how the grid loads a folder.
 */
export function onOpenFolder(handler: OpenFolderHandler) {
  openFolderHandler = handler;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** trimTrailing drops trailing separators so paths compare consistently. */
function trimTrailing(path: string): string {
  return path.length > 1 ? path.replace(/\/+$/, "") : path;
}

/**
 * covers reports whether root contains dir. The comparison is on whole path
 * segments, so /Volumes/CardTwo is not treated as living inside /Volumes/Card.
 */
export function covers(root: string, dir: string): boolean {
  const r = trimTrailing(root);
  const d = trimTrailing(dir);
  return d === r || d.startsWith(r === "/" ? "/" : `${r}/`);
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

  /** The tree's top level, and the children of every node opened so far. */
  treeRoots = $state<CatalogTreeNode[]>([]);
  treeChildren = $state<Record<string, CatalogTreeNode[]>>({});
  /** Paths whose children are showing, and paths waiting for theirs. */
  expanded = $state<Set<string>>(new Set());
  loadingNodes = $state<Set<string>>(new Set());
  /** Which tree row has the keyboard, as an index into the visible rows. */
  treeIndex = $state(0);

  sessions = $state<CatalogSession[]>([]);
  /** Which session row has the keyboard. */
  sessionIndex = $state(0);
  /** The break that ends a session, in hours. Four is the backend's default. */
  sessionGapHours = $state(4);
  selectedSession = $state<string | null>(null);

  storage = $state<CatalogStorage | null>(null);
  /** Whether the storage view is up as a full pane over the shell. */
  storageOpen = $state(false);

  /**
   * Whether the search bar is up. While it is, the grid is showing index
   * results rather than the open folder's frames, and Esc puts the folder
   * back — nothing about the folder itself has changed underneath.
   */
  searchOpen = $state(false);

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

  /**
   * openSearch puts the bar up over the grid. It starts empty rather than on
   * the last query: the first keystroke would otherwise edit the tail of
   * something the user cannot see the start of.
   */
  openSearch() {
    this.searchOpen = true;
    this.query = "";
    this.results = [];
    this.total = 0;
    this.searched = false;
    this.focusIndex = 0;
  }

  /** closeSearch drops the results and lets the open folder back onto the grid. */
  closeSearch() {
    if (this.#timer !== null) {
      clearTimeout(this.#timer);
      this.#timer = null;
    }
    // Retire the ticket so a search still in flight cannot land results onto
    // a grid that has gone back to showing a folder.
    this.#ticket++;
    this.searchOpen = false;
    this.query = "";
    this.results = [];
    this.total = 0;
    this.searched = false;
    this.loading = false;
    this.focusIndex = 0;
  }

  toggleSearch() {
    if (this.searchOpen) this.closeSearch();
    else this.openSearch();
  }

  /**
   * openSession shows a session's frames — which may span several folders —
   * as index results: a search over the session's time range. Opening the
   * session's first folder would silently drop the rest of the shoot.
   */
  openSession(s: CatalogSession) {
    this.openSearch();
    // The backend's To is exclusive; a second past the last frame keeps it in.
    const past = new Date(new Date(s.end).getTime() + 1000).toISOString();
    this.facets = { ...NO_FACETS, from: s.start, to: past };
    void this.search();
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

  /**
   * open hands a result to the shell, which loads its folder into CULL and
   * focuses the frame itself — the tile the user pressed ⏎ on is the one they
   * arrive at.
   */
  open(frame: CatalogFrame) {
    if (frame.dir !== "") openFolderHandler?.(frame.dir, frame.hash);
  }

  /**
   * openDir is the same handoff from a tree node or a session row, which name
   * a folder and no particular frame in it.
   */
  openDir(dir: string) {
    if (dir !== "") openFolderHandler?.(dir);
  }

  /**
   * openAt is the handoff from a search result once it is on the grid, where
   * it is a frame like any other and no longer a CatalogFrame. A frame whose
   * primary file could not be hashed opens its folder and nothing more.
   */
  openAt(dir: string, hash: string) {
    if (dir === "") return;
    openFolderHandler?.(dir, hash === "" ? undefined : hash);
  }

  /** Whether some root already covers dir, so opening it adds nothing. */
  covered(dir: string): boolean {
    const path = trimTrailing(dir.trim());
    return path !== "" && this.roots.some((root) => covers(root.path, path));
  }

  /**
   * registerIfNew brings a folder the user opened into the catalogue, unless a
   * root already covers it. Opening a folder is not asking to index the world,
   * so this is deliberately quiet: no reindex is kicked off and a failure is
   * swallowed rather than raised over a folder that opened perfectly well.
   */
  async registerIfNew(dir: string) {
    const path = trimTrailing(dir.trim());
    if (source === null || path === "" || this.covered(path)) return;
    try {
      this.roots = await source.RegisterRoot(path);
      // A new root can absorb ones already registered, so the top of the tree
      // can have changed shape before a single frame has been indexed.
      this.treeChildren = {};
      await this.loadTree();
    } catch {
      // The folder is open either way; a catalogue that did not take it is
      // worth neither an error banner nor a second attempt.
    }
  }

  /**
   * adopt registers folders the sidebar used to keep for itself, which is how
   * roots saved before the catalogue became the tree's source survive the
   * change. Ones a root already covers are skipped, so it is safe to call with
   * whatever was in storage.
   */
  async adopt(paths: string[]) {
    if (source === null) return;
    if (this.roots.length === 0) await this.loadRoots();
    for (const path of paths) {
      if (this.covered(path)) continue;
      try {
        this.roots = await source.RegisterRoot(trimTrailing(path.trim()));
      } catch (error) {
        this.error = message(error);
      }
    }
    this.treeChildren = {};
    await this.loadTree();
  }

  selectSession(id: string) {
    this.selectedSession = id;
    const index = this.sessions.findIndex((s) => s.id === id);
    if (index >= 0) this.sessionIndex = index;
  }

  async loadRoots() {
    if (source === null) return;
    try {
      this.roots = await source.Roots();
    } catch (error) {
      this.error = message(error);
    }
  }

  // --- the folder tree ---------------------------------------------------
  // Lazy: the top level arrives with the roots, and a node's children are
  // fetched the first time it is opened. The counts under a folder are
  // measured against the decision store, which is not free, so nothing is
  // fetched for a node the user has not asked to see.

  #treeAsked = false;

  /**
   * ensureTree fills the top level the first time the tree is drawn, so the
   * pane is populated whether or not anything else has refreshed LIBRARY yet.
   * Subsequent mounts reuse what is already there; loadTree is the way to ask
   * again.
   */
  async ensureTree() {
    if (this.#treeAsked) return;
    this.#treeAsked = true;
    if (this.roots.length === 0) await this.loadRoots();
    await this.loadTree();
  }

  /** loadTree replaces the top level. Children already loaded are kept. */
  async loadTree() {
    this.#treeAsked = true;
    if (source?.TreeRoots === undefined) {
      // A shell that has not wired the tree still gets the roots, flat.
      this.treeRoots = this.roots.map((root) => ({
        path: root.path,
        name: basename(root.path),
        frames: root.frames,
        direct: root.frames,
        undecided: UNDECIDED_UNKNOWN,
        bytes: root.bytes,
        hasDirs: false,
        isRoot: true,
      }));
      return;
    }
    try {
      this.treeRoots = (await source.TreeRoots()) ?? [];
    } catch (error) {
      this.error = message(error);
    }
  }

  /** The children of a node, or undefined while they have never been asked for. */
  childrenOf(path: string): CatalogTreeNode[] | undefined {
    return this.treeChildren[path];
  }

  async expandNode(path: string) {
    if (this.expanded.has(path)) return;
    this.expanded = new Set(this.expanded).add(path);
    if (this.treeChildren[path] !== undefined || source?.TreeChildren === undefined) return;

    this.loadingNodes = new Set(this.loadingNodes).add(path);
    try {
      const children = (await source.TreeChildren(path)) ?? [];
      this.treeChildren = { ...this.treeChildren, [path]: children };
    } catch (error) {
      this.error = message(error);
      // Left unexpanded rather than showing an empty folder that is not empty.
      this.collapseNode(path);
    } finally {
      const loading = new Set(this.loadingNodes);
      loading.delete(path);
      this.loadingNodes = loading;
    }
  }

  collapseNode(path: string) {
    const expanded = new Set(this.expanded);
    expanded.delete(path);
    this.expanded = expanded;
  }

  async toggleNode(path: string) {
    if (this.expanded.has(path)) this.collapseNode(path);
    else await this.expandNode(path);
  }

  focusNode(index: number) {
    this.treeIndex = Math.max(0, index);
  }

  focusSession(index: number) {
    if (index < 0 || index >= this.sessions.length) return;
    this.sessionIndex = index;
    this.selectedSession = this.sessions[index].id;
  }

  async addRoot(dir: string) {
    if (source === null || dir.trim() === "") return;
    this.error = null;
    try {
      this.roots = await source.RegisterRoot(dir.trim());
      // A folder added on top of folders already registered absorbs them, so
      // the top of the tree can have changed shape before a single frame has
      // been indexed. Redraw it now rather than at the end of the pass.
      this.treeChildren = {};
      await this.loadTree();
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

  #sessionsAsked = false;

  /**
   * ensureSessions fills the Sessions group the first time it is drawn, on the
   * same terms as ensureTree: the sidebar populates itself, and asking again
   * is loadSessions' business.
   */
  async ensureSessions() {
    if (this.#sessionsAsked) return;
    this.#sessionsAsked = true;
    await this.loadSessions();
  }

  async loadSessions() {
    if (source === null) return;
    this.#sessionsAsked = true;
    try {
      this.sessions = await source.Sessions(this.sessionGapHours);
      if (this.session === null) this.selectedSession = this.sessions[0]?.id ?? null;
      this.sessionIndex = Math.max(
        0,
        this.sessions.findIndex((s) => s.id === this.selectedSession),
      );
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
    // After the roots, because the tree falls back to them when the shell has
    // not wired the tree calls.
    this.treeChildren = {};
    await this.loadTree();
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

/**
 * sessionLabel names a shoot the way the sidebar lists it: the day it was shot
 * on, and the day it ran into as well when it crossed midnight. A session the
 * backend gave no usable timestamps for falls back to its folder, which is at
 * least something the user can recognise.
 */
export function sessionLabel(session: CatalogSession): string {
  const from = formatDate(session.start);
  const to = formatDate(session.end);
  if (from === "") return basename(session.dir) || "session";
  return from === to || to === "" ? from : `${from} → ${to}`;
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
