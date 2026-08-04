// Emptying the rejects: the survey of what is in the _Rejected folders, the
// typed confirmation, and the run itself.
//
// This is the only command in the application that destroys anything. Nothing
// here is reversible once it has run — there is no undo to fall back on and no
// trash to fish files out of — so the store keeps the two halves apart: a
// survey that has touched nothing, and an execution that only becomes possible
// once the user has typed the word.
//
// The backend is injected rather than imported, the way IMPORT and LIBRARY do
// it: call `connectRejects` once at startup with the generated binding and
// `watchRejectsProgress` once to feed the progress bar. Everything in here
// works against a stub, which is how the dialog is verified without deleting a
// photograph.

import { library } from "./library.svelte";
import { app } from "./state.svelte";

/** One folder's rejects. Mirrors the backend's RejectedDirDTO. */
export interface RejectedDir {
  /** The culled folder. */
  dir: string;
  /** Its rejected subfolder, which is the only thing that gets emptied. */
  path: string;
  raw: number;
  jpeg: number;
  pairs: number;
  sidecars: number;
  other: number;
  files: number;
  bytes: number;
}

/** What emptying would destroy. Mirrors RejectsSurveyDTO. */
export interface RejectsSurvey {
  /** The rejected folder name the survey looked for. */
  folder: string;
  dirs: RejectedDir[];
  raw: number;
  jpeg: number;
  pairs: number;
  sidecars: number;
  other: number;
  files: number;
  totalBytes: number;
}

/** What emptying actually did. Mirrors RejectsResultDTO. */
export interface RejectsResult {
  batchId: string;
  deleted: number;
  failed: number;
  bytes: number;
  errors: string[];
}

/** How far a run has got. Mirrors the backend's RejectsProgress. */
export interface RejectsProgress {
  done: number;
  total: number;
}

/**
 * RejectsSource is the backend, as this module needs it. The shell satisfies
 * it with the generated RejectsService binding.
 */
export interface RejectsSource {
  Survey(dirs: string[]): Promise<RejectsSurvey>;
  Empty(dirs: string[]): Promise<RejectsResult>;
}

let source: RejectsSource | null = null;

/** connectRejects gives the module its backend. Call once, at startup. */
export function connectRejects(backend: RejectsSource) {
  source = backend;
}

/**
 * The word that has to be typed in full before the command will run.
 *
 * The friction is a typed word rather than a held key. The dialog is drawn
 * rather than native, and a hold has nothing to say for itself in a drawn
 * panel: it needs a pointer or a key repeat, it cannot be seen to be
 * progressing without inventing a second progress indicator, and it is
 * unreachable for anyone driving the app by keyboard alone, which is how this
 * app is meant to be driven. Typing six letters is deliberate, visible as it
 * happens, taken back with backspace, and — unlike a confirm button under the
 * Enter key that applies everything else in the app — it cannot happen by
 * reflex.
 */
export const CONFIRM_WORD = "DELETE";

const EMPTY_SURVEY: RejectsSurvey = {
  folder: "",
  dirs: [],
  raw: 0,
  jpeg: 0,
  pairs: 0,
  sidecars: 0,
  other: 0,
  files: 0,
  totalBytes: 0,
};

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * targetDirs is what the command surveys: the folder on screen and every
 * catalogue root. The backend looks at each one's rejected subfolder and
 * nowhere else, so a folder that was never culled simply contributes nothing.
 */
export function targetDirs(): string[] {
  const dirs: string[] = [];
  const open = app.folder?.dir ?? "";
  if (open !== "") dirs.push(open);
  for (const root of library.roots) {
    if (root.path !== "") dirs.push(root.path);
  }
  return dirs;
}

class RejectsState {
  /** Whether the dialog is up. */
  open = $state(false);
  /** Set while the survey is being taken, before anything is shown. */
  surveying = $state(false);
  /** What emptying would destroy, or null while the survey is still out. */
  survey = $state<RejectsSurvey | null>(null);
  /** What has been typed towards the confirmation word. */
  typed = $state("");
  /** Set for the length of the run. Nothing can be typed or cancelled into it. */
  running = $state(false);
  /** How far the run has got, or null when one is not going. */
  progress = $state<RejectsProgress | null>(null);
  /** What went wrong, either surveying or destroying. */
  error = $state("");
  /** The result of a run that could not finish cleanly, kept on screen. */
  result = $state<RejectsResult | null>(null);

  /** Whether there is anything at all to destroy. */
  get anything(): boolean {
    return (this.survey?.files ?? 0) > 0;
  }

  /** Whether the confirmation has been typed in full. */
  get confirmed(): boolean {
    return this.typed === CONFIRM_WORD;
  }

  /** Whether the button and ⏎ will do anything. */
  get armed(): boolean {
    return this.anything && this.confirmed && !this.running;
  }

  /**
   * request opens the dialog and surveys. Nothing is destroyed by this and
   * nothing can be: the survey only counts. Passing dirs explicitly is for the
   * bench; the command surveys the open folder and the catalogue roots.
   */
  async request(dirs: string[] = targetDirs()): Promise<void> {
    this.open = true;
    this.typed = "";
    this.error = "";
    this.result = null;
    this.progress = null;
    this.survey = null;
    if (source === null) {
      this.error = "the rejects service is not connected";
      return;
    }
    this.surveying = true;
    try {
      this.survey = (await source.Survey(dirs)) ?? EMPTY_SURVEY;
    } catch (err) {
      this.survey = null;
      this.error = message(err);
    } finally {
      this.surveying = false;
    }
  }

  /**
   * type takes one character towards the confirmation word. Case is not part
   * of the friction — the point is typing the word rather than holding shift —
   * and anything that is not the next letter of it is ignored, so a stray key
   * cannot leave a half-typed word looking like a mistake the user has to
   * clear.
   */
  type(ch: string) {
    if (this.running || ch.length !== 1) return;
    const next = this.typed + ch.toUpperCase();
    if (CONFIRM_WORD.startsWith(next)) this.typed = next;
  }

  backspace() {
    if (!this.running) this.typed = this.typed.slice(0, -1);
  }

  /** cancel closes the dialog. A run in progress is not interrupted by it. */
  cancel() {
    if (this.running) return;
    this.open = false;
    this.survey = null;
    this.typed = "";
    this.error = "";
    this.result = null;
    this.progress = null;
  }

  /** applyProgress takes one report from the backend. */
  applyProgress(p: RejectsProgress) {
    if (this.running) this.progress = p;
  }

  /**
   * confirm destroys the surveyed files. It answers whether everything went,
   * and it refuses unless the word has been typed — the dialog is not the only
   * caller, and the guard belongs with the thing that does the deleting.
   *
   * A clean run closes the dialog and says what it reclaimed. A run with
   * failures in it stays on screen with them listed: the files that did go are
   * gone either way, and the ones that did not are the user's to deal with.
   */
  async confirm(): Promise<boolean> {
    if (!this.armed) return false;
    const dirs = (this.survey?.dirs ?? []).map((d) => d.dir);
    if (dirs.length === 0) return false;
    if (source === null) {
      this.error = "the rejects service is not connected";
      return false;
    }

    this.running = true;
    this.error = "";
    this.progress = { done: 0, total: this.survey?.files ?? 0 };
    try {
      const result = await source.Empty(dirs);
      this.result = result;
      if (result.failed > 0) {
        this.error = `${result.failed} file(s) could not be removed`;
        app.notify(`${result.failed} reject(s) could not be removed`, "error");
        this.typed = "";
        return false;
      }
      app.notify(`emptied ${result.deleted} reject(s) · ${formatSize(result.bytes)} reclaimed`);
      this.open = false;
      this.survey = null;
      this.typed = "";
      this.progress = null;
      return true;
    } catch (err) {
      this.error = message(err);
      this.typed = "";
      return false;
    } finally {
      this.running = false;
    }
  }
}

export const rejects = new RejectsState();

/**
 * watchRejectsProgress subscribes to the backend's progress for the life of the
 * app. Call it once, at startup, beside watchImportProgress.
 */
export async function watchRejectsProgress() {
  const { Events } = await import("@wailsio/runtime");
  Events.On("rejects:progress", (event: { data: RejectsProgress }) => {
    if (event.data) rejects.applyProgress(event.data);
  });
}

const UNITS = ["B", "KB", "MB", "GB", "TB"];

/** formatSize prints a size the way the rest of the app does: 24.6 GB, 940 MB. */
export function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${UNITS[unit]}`;
}

/**
 * summarise is the one-line count the dialog leads with, and the toast the
 * command palette row shows before anything is opened.
 */
export function summarise(s: RejectsSurvey | null): string {
  if (s === null || s.files === 0) return "nothing to empty";
  const parts: string[] = [];
  if (s.raw > 0) parts.push(`${s.raw} RAW`);
  if (s.jpeg > 0) parts.push(`${s.jpeg} JPEG`);
  if (s.pairs > 0) parts.push(`${s.pairs} pair${s.pairs === 1 ? "" : "s"}`);
  if (s.sidecars > 0) parts.push(`${s.sidecars} sidecar${s.sidecars === 1 ? "" : "s"}`);
  if (s.other > 0) parts.push(`${s.other} other`);
  return `${parts.join(" · ")} · ${formatSize(s.totalBytes)}`;
}
