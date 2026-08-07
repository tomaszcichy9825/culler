// IMPORT mode's state: the cards plugged in, what is on the one selected,
// where the review in CULL routed it, and the execution that carries it into
// the library.
//
// The backend is injected rather than imported, the way LIBRARY does it: call
// `connectImport` once at startup with the generated binding, and
// `watchImportProgress` once to feed the progress bar. Everything in here works
// against a stub, which is how the screens are verified without a card.
//
// The formatters are this module's own rather than shared. IMPORT is the one
// screen that has to be right about sizes while the rest of the app is being
// restructured around it, and a number printed here must not change because a
// neighbouring mode moved a helper.

/** One removable volume with a shallow look at what is on it. Mirrors CardDTO. */
export interface ImportCard {
  path: string;
  name: string;
  total: number;
  free: number;
  network: boolean;
  hasDcim: boolean;
  /** The folder to open in CULL first. */
  dir: string;
  folders: number;
  frames: number;
  /** Whether `frames` was extrapolated from one folder rather than counted. */
  estimated: boolean;
  error: string;
}

/** One image folder on a card. Mirrors CardDirDTO. */
export interface ImportCardDir {
  path: string;
  name: string;
  frames: number;
  files: number;
  bytes: number;
  first: string;
  last: string;
}

/** What a card holds, worked out on demand. Mirrors CardSummaryDTO. */
export interface ImportCardSummary {
  path: string;
  name: string;
  network: boolean;
  hasDcim: boolean;
  dirs: ImportCardDir[];
  frames: number;
  files: number;
  bytes: number;
  sessions: number;
  first: string;
  last: string;
  /** How many of `sampled` frames the catalogue already holds. */
  imported: number;
  sampled: number;
}

/** One destination the folder's frames are routed to. Mirrors ImportRouteDTO. */
export interface ImportRoute {
  destination: string;
  path: string;
  frames: number;
  files: number;
  bytes: number;
}

/** The routing state of one folder. Mirrors ImportPlanDTO. */
export interface ImportPlan {
  dir: string;
  frames: number;
  routed: number;
  cut: number;
  unrouted: number;
  undecided: number;
  routes: ImportRoute[];
  files: number;
  bytes: number;
  verb: string;
  libraryRoot: string;
  network: boolean;
  /** The volume behind each route. It rides with the plan: working it out
      needs the plan, and the plan needs a scan of the whole folder. */
  space: ImportSpace[];
}

/** One route and the volume it lands on. Mirrors DestinationSpaceDTO. */
export interface ImportSpace {
  destination: string;
  path: string;
  frames: number;
  bytes: number;
  volume: string;
  volumeName: string;
  free: number;
  total: number;
  network: boolean;
  removable: boolean;
  fits: boolean;
}

/** What a running import reports. Mirrors the backend's ImportProgress. */
export interface ImportProgress {
  dir: string;
  phase: string;
  files: number;
  total: number;
  bytes: number;
  complete: boolean;
  error: string;
}

/** One executed action, as the journal recorded it. Mirrors ResultDTO. */
export interface ImportResult {
  verb: string;
  src: string;
  dst: string;
  outcome: string;
  err: string;
}

/** The journal record of one import. Mirrors BatchDTO. */
export interface ImportBatch {
  id: string;
  time: string;
  description: string;
  actions: ImportResult[];
}

/** The phases an import passes through, as the backend names them. */
export const PHASE_SCAN = "scan";
export const PHASE_COPY = "copy";
export const PHASE_MOVE = "move";
export const PHASE_BACKUP = "backup";

/** What each phase is called on the screen. */
const PHASE_LABELS: Record<string, string> = {
  [PHASE_SCAN]: "reading the card",
  [PHASE_COPY]: "copying into the library",
  [PHASE_MOVE]: "moving into the library",
  [PHASE_BACKUP]: "writing the second copy",
};

export function phaseLabel(phase: string): string {
  return PHASE_LABELS[phase] ?? phase;
}

/**
 * ImportSource is the backend, as this module needs it. The shell satisfies it
 * with the generated ImportService binding.
 */
export interface ImportSource {
  DetectCards(): Promise<ImportCard[]>;
  CardSummary(path: string): Promise<ImportCardSummary>;
  ImportPlan(dir: string): Promise<ImportPlan>;
  Execute(dir: string, backupDest: string): Promise<ImportBatch>;
}

let source: ImportSource | null = null;

/** connectImport gives the module its backend. Call once, at startup. */
export function connectImport(backend: ImportSource) {
  source = backend;
}

/**
 * OpenFolderHandler is how IMPORT hands a folder to CULL. It is the same shape
 * LIBRARY registers, and the shell passes its own openFolder action to both.
 */
export type OpenFolderHandler = (dir: string) => void;

let openFolderHandler: OpenFolderHandler | null = null;

/** onOpenFolder registers what "review in CULL" does. */
export function onOpenFolder(handler: OpenFolderHandler) {
  openFolderHandler = handler;
}

/** RevealHandler opens a finished import's folder in the file manager. */
export type RevealHandler = (path: string) => void;

let revealHandler: RevealHandler | null = null;

/**
 * onReveal registers what "open library folder" does. Without one the button
 * is not drawn: an affordance that does nothing is worse than none.
 */
export function onReveal(handler: RevealHandler) {
  revealHandler = handler;
}

/** Whether a reveal handler has been registered, for the components. */
export function canReveal(): boolean {
  return revealHandler !== null;
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

class ImportState {
  /** The removable volumes found, in the order the lister gave them. */
  cards = $state<ImportCard[]>([]);
  /** Which card has the keyboard, as an index into `cards`. */
  cardIndex = $state(0);
  /** The path of the selected card, or null before anything is chosen. */
  selectedPath = $state<string | null>(null);

  summary = $state<ImportCardSummary | null>(null);

  /** The folder the routing is being read for: one image folder on the card. */
  dir = $state<string | null>(null);
  plan = $state<ImportPlan | null>(null);

  /** The volume behind each route, which the plan already carries. */
  get space(): ImportSpace[] {
    return this.plan?.space ?? [];
  }

  /** Whether the optional second copy is on, and where it goes. */
  backup = $state(false);
  backupPath = $state("");

  /** The last report from a running import, or null when none is running. */
  progress = $state<ImportProgress | null>(null);
  running = $state(false);
  /** The batch the last import journaled, which is the done state. */
  batch = $state<ImportBatch | null>(null);

  loading = $state(false);
  error = $state<string | null>(null);
  /** True once cards have been looked for, so an empty list can say which. */
  detected = $state(false);

  get card(): ImportCard | null {
    return this.cards.find((c) => c.path === this.selectedPath) ?? null;
  }

  /** The card under the keyboard, which is not always the selected one. */
  get focusedCard(): ImportCard | null {
    return this.cards[this.cardIndex] ?? null;
  }

  /** How much of the selected card the catalogue already holds, 0–100. */
  get importedPercent(): number {
    const s = this.summary;
    if (s === null || s.sampled === 0) return 0;
    return percent(s.imported, s.sampled);
  }

  /** Whether anything in the plan is routed nowhere, which is the warning. */
  get hasUnrouted(): boolean {
    return (this.plan?.unrouted ?? 0) > 0;
  }

  /** Whether an import would carry anything. */
  get ready(): boolean {
    return this.dir !== null && (this.plan?.routed ?? 0) > 0 && !this.running;
  }

  /** The destinations that will not fit on the volume they land on. */
  get overfull(): ImportSpace[] {
    return this.space.filter((s) => !s.fits);
  }

  /** How far the running import has got, 0–100, or null while unmeasurable. */
  get percentDone(): number | null {
    const p = this.progress;
    if (p === null || p.total <= 0) return null;
    if (p.phase === PHASE_SCAN || p.phase === PHASE_MOVE) return null;
    return percent(p.files, p.total);
  }

  #ticket = 0;

  /**
   * Rising ticket for loadFolder, separate from #ticket: selectCard holds the
   * card ticket across its own await of loadFolder, so the two must not share
   * a counter or a selection would invalidate itself.
   */
  #planTicket = 0;

  /** refresh looks for cards again and keeps the selection if it survived. */
  async refresh() {
    if (source === null) return;
    this.loading = true;
    this.error = null;
    try {
      this.cards = (await source.DetectCards()) ?? [];
      this.detected = true;
      if (this.cards.length === 0) {
        this.selectedPath = null;
        this.summary = null;
        return;
      }
      const kept = this.cards.findIndex((c) => c.path === this.selectedPath);
      if (kept >= 0) {
        this.cardIndex = kept;
      } else {
        this.cardIndex = 0;
        await this.selectCard(0);
      }
    } catch (error) {
      this.error = message(error);
    } finally {
      this.loading = false;
    }
  }

  focusCard(index: number) {
    if (index < 0 || index >= this.cards.length) return;
    this.cardIndex = index;
  }

  /** selectCard reads the card at index and loads the routing of its first folder. */
  async selectCard(index: number) {
    const card = this.cards[index];
    if (card === undefined || source === null) return;
    this.cardIndex = index;
    this.selectedPath = card.path;
    this.summary = null;
    this.batch = null;

    const ticket = ++this.#ticket;
    this.loading = true;
    this.error = null;
    try {
      const summary = await source.CardSummary(card.path);
      if (ticket !== this.#ticket) return; // a newer selection has answered
      this.summary = summary;
      await this.loadFolder(summary.dirs[0]?.path ?? card.dir);
    } catch (error) {
      if (ticket !== this.#ticket) return;
      this.error = message(error);
    } finally {
      if (ticket === this.#ticket) this.loading = false;
    }
  }

  /**
   * loadFolder reads the routing of one folder. It is called for the card's
   * first image folder, and again for whichever folder the user was last
   * culling, so the route table always describes what CULL is showing.
   */
  async loadFolder(dir: string) {
    if (source === null || dir === "") return;
    const ticket = ++this.#planTicket;
    // A different folder must not sit beside the old plan while its own is in
    // flight: Execute reads this.dir but the confirm reads the plan's numbers,
    // and the two have to describe the same folder at every moment.
    if (this.dir !== dir) this.plan = null;
    this.dir = dir;
    this.error = null;
    try {
      const plan = await source.ImportPlan(dir);
      if (ticket !== this.#planTicket) return; // a newer folder has answered
      this.plan = plan;
    } catch (error) {
      if (ticket !== this.#planTicket) return;
      this.error = message(error);
      this.plan = null;
    }
  }

  /** reloadPlan re-reads the routing of the folder already loaded. */
  async reloadPlan() {
    if (this.dir !== null) await this.loadFolder(this.dir);
  }

  /** review hands the folder to CULL, which is where the routing is done. */
  review(dir?: string) {
    const target = dir ?? this.dir ?? this.card?.dir ?? "";
    if (target !== "") openFolderHandler?.(target);
  }

  /** reveal opens a folder in the file manager, once an import has finished. */
  reveal(path: string) {
    if (path !== "") revealHandler?.(path);
  }

  setBackup(on: boolean) {
    this.backup = on;
  }

  setBackupPath(path: string) {
    this.backupPath = path;
  }

  /**
   * execute runs the import. The progress bar is fed by the backend's events,
   * not by this call, which only settles once every file has landed.
   */
  async execute() {
    if (source === null || this.dir === null || this.running) return;
    const dest = this.backup ? this.backupPath.trim() : "";
    if (this.backup && dest === "") {
      this.error = "the second copy has nowhere to go — name a backup folder";
      return;
    }
    this.running = true;
    this.error = null;
    this.batch = null;
    this.progress = { dir: this.dir, phase: PHASE_SCAN, files: 0, total: 0, bytes: 0, complete: false, error: "" };
    try {
      this.batch = await source.Execute(this.dir, dest);
    } catch (error) {
      this.error = message(error);
    } finally {
      this.running = false;
      // What was routed has been cleared by the import, so the route table is
      // now describing a folder that has already gone.
      await this.reloadPlan();
    }
  }

  /**
   * applyProgress takes one report from a running import. The last report
   * carries any error the batch met, which stays on the screen after the bar
   * has finished — a partial import is the one case where the user has to be
   * told something rather than shown a green tick.
   */
  applyProgress(progress: ImportProgress) {
    this.progress = progress;
    if (progress.complete && progress.error !== "") this.error = progress.error;
  }

  /** How many actions of the finished batch failed, which the done state shows. */
  get failed(): number {
    return (this.batch?.actions ?? []).filter((a) => a.outcome !== "ok").length;
  }
}

export const importState = new ImportState();

/**
 * watchImportProgress subscribes to the backend's import progress for the life
 * of the app. Call it once, at startup, beside watchCatalogProgress.
 */
export async function watchImportProgress() {
  const { Events } = await import("@wailsio/runtime");
  Events.On("import:progress", (event: { data: ImportProgress }) => {
    if (event.data) importState.applyProgress(event.data);
  });
}

// --- formatting ---------------------------------------------------------

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
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${UNITS[unit]}`;
}

/** formatCount groups thousands, so 1204 reads as 1,204. */
export function formatCount(n: number): string {
  return n.toLocaleString("en-GB");
}

/**
 * percent returns a share of a total, clamped so a rounding error cannot push
 * a bar past its track.
 */
export function percent(part: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, (part / total) * 100));
}

/**
 * The timestamp is sliced rather than read through Date, for the same reason
 * the rest of the app does it: it carries the camera's own offset, and a frame
 * shot at dusk must not move to the middle of the afternoon because the laptop
 * is in another zone.
 */
const STAMP = /^(\d{4})-(\d{2})-(\d{2})[T ](\d{2}):(\d{2})/;

/** formatDate prints a shot date: 2026-05-01. */
export function formatDate(shot: string): string {
  const m = STAMP.exec(shot);
  return m === null ? "" : `${m[1]}-${m[2]}-${m[3]}`;
}

/** formatSpan prints the two ends of a card: 2026-05-01 → 2026-05-03. */
export function formatSpan(first: string, last: string): string {
  const from = formatDate(first);
  const to = formatDate(last);
  if (from === "") return "";
  return from === to ? from : `${from} → ${to}`;
}

/** The last part of a path, which is what the user calls a folder. */
export function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter((p) => p !== "");
  return parts[parts.length - 1] ?? path;
}
