// EXIF mode's own state: the frames on the rail, the form the middle pane
// draws, and the one explicit write that puts any of it on disk.
//
// Nothing here touches a file. The user edits a draft, the draft is compared
// against what the frames actually carry, and only `write` sends anything to
// the backend — which then plans, backs up and journals, so a write is undone
// with the same ⌘Z as a cull. That separation is why "3 unwritten" can sit in
// the title bar honestly: it is a count of drafted changes, not of files
// already altered.
//
// The mode has two sub-layouts and they are the same form. In `single frame`
// the draft belongs to the frame under the cursor; in `batch` a value typed
// once is written into every selected frame's draft, and a field whose frames
// disagree reads ⟨mixed⟩ until something replaces it. Modelling batch as "the
// same edit, applied to more drafts" is what keeps the write plan, the
// unwritten count and the dirty marks from needing a second implementation.

import { shell } from "./shell.svelte";

/** The value shown for a field whose selected frames do not agree. */
export const MIXED = "⟨mixed⟩";

/** Which sub-layout of EXIF edits one frame, and which edits the selection. */
export const SINGLE_FRAME = 0;
export const BATCH = 1;

/**
 * The DTOs internal/app/exifservice.go returns. They are declared here rather
 * than imported from the generated bindings because the service is registered
 * in main.go by the shell, and this module has to compile before that happens.
 * Once `wails3 generate bindings` has run, these can be replaced with the
 * generated types; the shapes are the same by construction.
 */
export interface ExifFieldDTO {
  tag: string;
  label: string;
  section: string;
  value: string;
  present: boolean;
  writable: boolean;
}

export interface FrameExifDTO {
  path: string;
  stem: string;
  kind: "jpeg" | "raw";
  sidecar: string;
  fields: ExifFieldDTO[];
  error: string;
}

/** A location to write onto a frame. Mirrors the backend's GPSCoordDTO. */
export interface GPSCoordDTO {
  latitude: number;
  longitude: number;
  altitude: number;
  hasAltitude: boolean;
}

export interface ExifEditDTO {
  path: string;
  dateTimeOriginal: string | null;
  artist: string | null;
  copyright: string | null;
  stripGps: boolean;
  /** A location the user set — a dropped pin or one copied from another frame. */
  setGps: GPSCoordDTO | null;
}

export interface ExifWriteRowDTO {
  sign: string;
  target: string;
  tag: string;
  value: string;
  method: string;
}

export interface ExifPlanDTO {
  description: string;
  rows: ExifWriteRowDTO[];
  writes: number;
  frames: number;
  files: number;
  backupDir: string;
  assurances: string[];
  warnings: string[];
}

/**
 * The three calls the editor makes. It is an interface so the screen can be
 * driven without a backend, exactly as the settings screen is: the harness
 * supplies a fake port and the shell supplies the real service.
 */
export interface ExifPort {
  read(paths: string[]): Promise<Record<string, FrameExifDTO>>;
  plan(edits: ExifEditDTO[]): Promise<ExifPlanDTO>;
  apply(edits: ExifEditDTO[]): Promise<{ actions?: { outcome: string }[]; description?: string }>;
}

/**
 * Until the shell hands over the real service, every call says so rather than
 * failing with something the user cannot act on. The form still draws — an
 * editor that renders and cannot write is more use than a blank pane.
 */
const UNWIRED = "the metadata service is not connected yet";

const unwiredPort: ExifPort = {
  read: () => Promise.reject(new Error(UNWIRED)),
  plan: () => Promise.reject(new Error(UNWIRED)),
  apply: () => Promise.reject(new Error(UNWIRED)),
};

/** The tags this app writes, and how an edit names each one. */
export const WRITABLE_TAGS = ["DateTimeOriginal", "Artist", "Copyright"] as const;
export type WritableTag = (typeof WRITABLE_TAGS)[number];

function isWritableTag(tag: string): tag is WritableTag {
  return (WRITABLE_TAGS as readonly string[]).includes(tag);
}

/** How a field row is drawn: the four states screen 3a specifies. */
export type FieldState = "clean" | "dirty" | "mixed" | "locked";

/** One row of the form, already resolved against the frames it covers. */
export interface FieldRow {
  tag: string;
  label: string;
  section: string;
  /** What the row shows: the drafted value, the shared value, or MIXED. */
  value: string;
  /** The value on disk, struck through beside a dirty row. Empty if absent. */
  previous: string;
  state: FieldState;
  writable: boolean;
  /** False when no covered frame carries the tag at all. */
  present: boolean;
}

/** message pulls a readable line out of whatever a rejected call threw. */
export function message(err: unknown): string {
  if (err instanceof Error) return err.message;
  if (typeof err === "string") return err;
  return String(err);
}

class ExifState {
  /** The frames on the rail, in the order the grid had them. */
  frames = $state<FrameExifDTO[]>([]);
  /** Which rail row holds the cursor. */
  index = $state(0);

  /** path → tag → the value the user typed. The draft, and nothing else. */
  edits = $state<Record<string, Record<string, string>>>({});
  /** path → whether this frame's GPS is drafted for removal. */
  strip = $state<Record<string, boolean>>({});

  /** The tag currently in edit, or null when the form is just being read. */
  editingTag = $state<string | null>(null);
  /** What is in the field while it is being edited, before ⏎ commits it. */
  buffer = $state("");

  /** The plan awaiting confirmation. Non-null means the dialog is up. */
  plan = $state<ExifPlanDTO | null>(null);

  loading = $state(false);
  writing = $state(false);
  /** The last thing that went wrong, verbatim. Cleared by the next attempt. */
  error = $state("");

  #port: ExifPort = unwiredPort;

  /** usePort swaps the backend out — for the shell's wiring and the harness. */
  usePort(port: ExifPort) {
    this.#port = port;
  }

  /** Whether the batch sub-layout is showing, which is what ⌥2 and Tab pick. */
  get batch(): boolean {
    return shell.mode === "exif" && shell.layout === BATCH;
  }

  get focused(): FrameExifDTO | null {
    return this.frames[this.index] ?? null;
  }

  /** The frames an edit reaches: the selection in batch, otherwise one frame. */
  get targets(): FrameExifDTO[] {
    if (this.batch) return this.frames;
    const one = this.focused;
    return one ? [one] : [];
  }

  /**
   * rows is the form. Every field the covered frames carry appears once, in
   * the order the backend declared them, with its value resolved across the
   * frames: a shared value shows itself, a disagreement shows MIXED, and a
   * drafted value wins over both.
   */
  get rows(): FieldRow[] {
    const targets = this.targets;
    if (targets.length === 0) return [];

    const order: string[] = [];
    const spec = new Map<string, ExifFieldDTO>();
    for (const frame of targets) {
      for (const f of frame.fields ?? []) {
        if (!spec.has(f.tag)) {
          spec.set(f.tag, f);
          order.push(f.tag);
        }
      }
    }

    return order.map((tag) => {
      const f = spec.get(tag) as ExifFieldDTO;
      const onDisk = sharedValue(targets, tag);
      const drafted = sharedDraft(this.edits, targets, tag);
      // A field is writable only where every covered frame can take it — a
      // RAW in the selection locks the row for the JPEGs beside it, because
      // one ⏎ must not write to some frames and skip others in silence.
      const writable = targets.every((frame) => (frame.fields ?? []).some((x) => x.tag === tag && x.writable));

      let state: FieldState = "clean";
      let value = onDisk === null ? MIXED : onDisk;
      let previous = "";
      if (!writable) {
        state = "locked";
      } else if (drafted !== undefined) {
        state = "dirty";
        value = drafted;
        previous = onDisk === null ? MIXED : onDisk;
      } else if (onDisk === null) {
        state = "mixed";
      }

      return {
        tag,
        label: f.label,
        section: f.section,
        value,
        previous,
        state,
        writable,
        present: targets.some((frame) => (frame.fields ?? []).some((x) => x.tag === tag && x.present)),
      };
    });
  }

  /** The rows a ⇥ walks: the editable ones, in form order. */
  get editableRows(): FieldRow[] {
    return this.rows.filter((r) => r.writable);
  }

  /** The sections the form draws, in first-declared order. */
  get sections(): { name: string; rows: FieldRow[] }[] {
    const out: { name: string; rows: FieldRow[] }[] = [];
    for (const row of this.rows) {
      let group = out.find((g) => g.name === row.section);
      if (group === undefined) {
        group = { name: row.section, rows: [] };
        out.push(group);
      }
      group.rows.push(row);
    }
    return out;
  }

  /** Whether a frame carries any drafted change, which is its dirty dot. */
  isDirty(path: string): boolean {
    return Object.keys(this.edits[path] ?? {}).length > 0 || this.strip[path] === true;
  }

  /**
   * unwritten is the number the title bar chips: one per drafted tag per
   * frame, plus one per frame drafted to lose its GPS. It counts writes, not
   * frames, which is what the write plan will list.
   */
  get unwritten(): number {
    let n = 0;
    for (const frame of this.frames) {
      n += Object.keys(this.edits[frame.path] ?? {}).length;
      if (this.strip[frame.path]) n++;
    }
    return n;
  }

  /** The frames carrying a drafted change, which is what a write acts on. */
  get dirtyFrames(): FrameExifDTO[] {
    return this.frames.filter((f) => this.isDirty(f.path));
  }

  /** editsDTO is the draft in the shape ExifService takes. */
  get editsDTO(): ExifEditDTO[] {
    return this.dirtyFrames.map((frame) => {
      const drafted = this.edits[frame.path] ?? {};
      return {
        path: frame.path,
        dateTimeOriginal: drafted.DateTimeOriginal ?? null,
        artist: drafted.Artist ?? null,
        copyright: drafted.Copyright ?? null,
        stripGps: this.strip[frame.path] === true,
        setGps: null,
      };
    });
  }

  // ---- moving about ---------------------------------------------------------

  setIndex(index: number) {
    if (this.frames.length === 0) return;
    this.index = Math.max(0, Math.min(index, this.frames.length - 1));
    // The form belongs to the frame; moving off it abandons a half-typed row
    // rather than carrying the text to a frame it was not typed for.
    this.editingTag = null;
  }

  /** beginEdit puts a row into edit with its current value in the field. */
  beginEdit(tag: string) {
    const row = this.rows.find((r) => r.tag === tag);
    if (row === undefined || !row.writable) return;
    this.editingTag = tag;
    this.buffer = row.value === MIXED ? "" : row.value;
  }

  /** nextField is ⇥: the next editable row, wrapping to the first. */
  nextField(step = 1) {
    const rows = this.editableRows;
    if (rows.length === 0) return;
    const at = rows.findIndex((r) => r.tag === this.editingTag);
    const next = at < 0 ? 0 : (at + step + rows.length) % rows.length;
    this.commit();
    this.beginEdit(rows[next].tag);
  }

  /**
   * commit takes what is in the field and drafts it onto every covered frame.
   * A value identical to what the frames already carry is not a change and is
   * dropped, so tabbing through a form does not invent an edit per row.
   */
  commit() {
    const tag = this.editingTag;
    if (tag === null || !isWritableTag(tag)) {
      this.editingTag = null;
      return;
    }
    const value = this.buffer;
    const targets = this.targets;
    const edits = { ...this.edits };

    for (const frame of targets) {
      const current = valueOf(frame, tag);
      const draft = { ...(edits[frame.path] ?? {}) };
      if (value === current) delete draft[tag];
      else draft[tag] = value;
      if (Object.keys(draft).length === 0) delete edits[frame.path];
      else edits[frame.path] = draft;
    }
    this.edits = edits;
    this.editingTag = null;
  }

  /** revert is esc inside the form: throw the field away, keep the draft. */
  revert() {
    this.editingTag = null;
    this.buffer = "";
  }

  /** clearField drafts an empty value, which removes the tag on write. */
  clearField(tag: string) {
    if (!isWritableTag(tag)) return;
    this.editingTag = tag;
    this.buffer = "";
    this.commit();
  }

  /** toggleStrip drafts, or undrafts, removing the covered frames' GPS. */
  toggleStrip() {
    const targets = this.targets;
    if (targets.length === 0) return;
    const on = !targets.every((f) => this.strip[f.path] === true);
    const strip = { ...this.strip };
    for (const frame of targets) {
      if (on) strip[frame.path] = true;
      else delete strip[frame.path];
    }
    this.strip = strip;
  }

  get stripping(): boolean {
    const targets = this.targets;
    return targets.length > 0 && targets.every((f) => this.strip[f.path] === true);
  }

  /** discard throws every drafted change away. */
  discard() {
    this.edits = {};
    this.strip = {};
    this.editingTag = null;
    this.plan = null;
  }

  // ---- talking to the backend -----------------------------------------------

  /**
   * load reads the metadata of the given frames. Drafts for frames that are no
   * longer on the rail are dropped: a draft with nothing to write it to is a
   * count in the title bar that can never go down.
   */
  async load(paths: string[]) {
    this.loading = true;
    this.error = "";
    try {
      const byPath = await this.#port.read(paths);
      this.frames = paths.map((p) => byPath[p]).filter((f): f is FrameExifDTO => f !== undefined);
      this.index = Math.min(this.index, Math.max(0, this.frames.length - 1));
      const live = new Set(this.frames.map((f) => f.path));
      this.edits = Object.fromEntries(Object.entries(this.edits).filter(([p]) => live.has(p)));
      this.strip = Object.fromEntries(Object.entries(this.strip).filter(([p]) => live.has(p)));
    } catch (err) {
      this.frames = [];
      this.error = message(err);
    } finally {
      this.loading = false;
    }
  }

  /**
   * requestWrite plans the draft and puts the plan up for confirmation. This
   * is ⌘S; nothing has touched a file when it resolves.
   */
  async requestWrite() {
    const edits = this.editsDTO;
    if (edits.length === 0) return;
    this.error = "";
    this.writing = true;
    try {
      this.plan = await this.#port.plan(edits);
    } catch (err) {
      this.error = message(err);
    } finally {
      this.writing = false;
    }
  }

  /**
   * confirmWrite executes the plan on screen. The draft is cleared only for
   * what actually got written: a failed frame keeps its edit so it can be
   * tried again rather than being silently forgotten, which is the same rule
   * the cull's apply follows.
   */
  async confirmWrite(): Promise<boolean> {
    const edits = this.editsDTO;
    if (edits.length === 0 || this.plan === null) return false;
    this.writing = true;
    this.error = "";
    try {
      const batch = await this.#port.apply(edits);
      const failed = (batch.actions ?? []).filter((a) => a.outcome !== "ok").length;
      this.plan = null;
      if (failed === 0) {
        this.edits = {};
        this.strip = {};
      } else {
        this.error = `${failed} write(s) failed; those frames kept their edits`;
      }
      await this.load(this.frames.map((f) => f.path));
      return failed === 0;
    } catch (err) {
      this.plan = null;
      this.error = message(err);
      return false;
    } finally {
      this.writing = false;
    }
  }

  cancelWrite() {
    this.plan = null;
  }
}

/** valueOf is what one frame currently carries for a tag. */
function valueOf(frame: FrameExifDTO, tag: string): string {
  return (frame.fields ?? []).find((f) => f.tag === tag)?.value ?? "";
}

/**
 * sharedValue is the value every frame agrees on, or null when they disagree.
 * Null is the mixed state — distinct from the empty string, which is what they
 * agree on when none of them carries the tag.
 */
function sharedValue(frames: FrameExifDTO[], tag: string): string | null {
  let first: string | null = null;
  for (const frame of frames) {
    const value = valueOf(frame, tag);
    if (first === null) first = value;
    else if (first !== value) return null;
  }
  return first;
}

/**
 * sharedDraft is the drafted value when every covered frame has the same one,
 * and undefined when none of them does. Frames that disagree about a draft
 * report the first one, which can only happen while a batch is being built up.
 */
function sharedDraft(
  edits: Record<string, Record<string, string>>,
  frames: FrameExifDTO[],
  tag: string,
): string | undefined {
  for (const frame of frames) {
    const value = edits[frame.path]?.[tag];
    if (value !== undefined) return value;
  }
  return undefined;
}

export const exifState = new ExifState();
