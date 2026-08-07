// The metadata editor's state: the frames the inspector is editing, the field
// rows it draws, and the one explicit write that puts any of it on disk.
//
// Nothing here touches a file. The user edits a draft, the draft is compared
// against what the frames actually carry, and only `write` sends anything to
// the backend — which then plans, backs up and journals, so a write is undone
// with the same ⌘Z as a cull. That separation is why "3 unwritten" can sit in
// the title bar honestly: it is a count of drafted changes, not of files
// already altered.
//
// Editing lives in the PHOTOS inspector, and the frames follow the grid's
// action targets: the selection when there is one, the focused frame alone
// otherwise. A value typed once is written into every covered frame's draft,
// and a field whose frames disagree reads ⟨mixed⟩ until something replaces it.
// Modelling a selection as "the same edit, applied to more drafts" is what
// keeps the write plan, the unwritten count and the dirty marks from needing
// a second implementation.

import { app } from "./state.svelte";

/** The value shown for a field whose selected frames do not agree. */
export const MIXED = "⟨mixed⟩";

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

/**
 * The pseudo-tag #searchDrafts uses to record a GPS strip drafted while the
 * search was up. Real tags never start with "#", so it cannot collide.
 */
const STRIP_DRAFT = "#strip";

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
  /** The frames being edited, in the order the grid had them. */
  frames = $state<FrameExifDTO[]>([]);

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

  /** Rising ticket for load, so a stale read cannot land on a newer one. */
  #loadSeq = 0;

  /** usePort swaps the backend out — for the shell's wiring and the harness. */
  usePort(port: ExifPort) {
    this.#port = port;
  }

  /**
   * The frames an edit reaches: every frame that was loaded, which is the
   * grid's own target resolution — the selection, or the focused frame alone.
   */
  get targets(): FrameExifDTO[] {
    return this.frames;
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
   * frames, which is what the write plan will list — and it counts every
   * draft, on screen or off it, because ⌘S writes them all.
   */
  get unwritten(): number {
    let n = 0;
    for (const draft of Object.values(this.edits)) n += Object.keys(draft).length;
    for (const on of Object.values(this.strip)) if (on) n++;
    return n;
  }

  /**
   * draftedPaths is every path carrying a drafted change, whether or not its
   * frame is still loaded: the loaded frames first, in their own order, then
   * the drafts made on frames the targets have since moved off.
   */
  get draftedPaths(): string[] {
    const drafted = new Set<string>();
    for (const [path, draft] of Object.entries(this.edits)) {
      if (Object.keys(draft).length > 0) drafted.add(path);
    }
    for (const [path, on] of Object.entries(this.strip)) {
      if (on) drafted.add(path);
    }
    const ordered: string[] = [];
    for (const frame of this.frames) {
      if (drafted.delete(frame.path)) ordered.push(frame.path);
    }
    return [...ordered, ...drafted];
  }

  /** editsDTO is the whole draft in the shape ExifService takes. */
  get editsDTO(): ExifEditDTO[] {
    return this.draftedPaths.map((path) => {
      const drafted = this.edits[path] ?? {};
      return {
        path,
        dateTimeOriginal: drafted.DateTimeOriginal ?? null,
        artist: drafted.Artist ?? null,
        copyright: drafted.Copyright ?? null,
        stripGps: this.strip[path] === true,
        setGps: null,
      };
    });
  }

  // ---- editing --------------------------------------------------------------

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
      if (value === current) {
        delete draft[tag];
        this.#untagSearchDraft(frame.path, tag);
      } else {
        // Only a draft born during the search belongs to it: re-editing one
        // that existed before the search opened must not re-home it, or
        // closing the search would eat a folder draft it never owned.
        if (this.#searchOpen && draft[tag] === undefined) this.#tagSearchDraft(frame.path, tag);
        draft[tag] = value;
      }
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
      if (on) {
        // Same rule as setValue: a strip drafted before the search opened
        // keeps belonging to the folder it was drafted in.
        if (this.#searchOpen && strip[frame.path] !== true) this.#tagSearchDraft(frame.path, STRIP_DRAFT);
        strip[frame.path] = true;
      } else {
        delete strip[frame.path];
        this.#untagSearchDraft(frame.path, STRIP_DRAFT);
      }
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
    this.#searchDrafts.clear();
    this.#draftGen++;
    this.editingTag = null;
    this.plan = null;
  }

  /** The folder the current drafts were typed in. See setScope. */
  #draftFolder: string | null = null;
  /** Whether the search bar was up at the last setScope call. */
  #searchOpen = false;
  /** path → the tags drafted while the search was up. See setScope. */
  #searchDrafts = new Map<string, Set<string>>();
  /**
   * Rises whenever drafts are pruned — a folder change, a search close, an
   * explicit discard — so an awaited plan can tell that the drafts it
   * describes no longer exist. See requestWrite.
   */
  #draftGen = 0;

  #tagSearchDraft(path: string, tag: string) {
    const tags = this.#searchDrafts.get(path) ?? new Set<string>();
    tags.add(tag);
    this.#searchDrafts.set(path, tags);
  }

  #untagSearchDraft(path: string, tag: string) {
    const tags = this.#searchDrafts.get(path);
    if (tags === undefined) return;
    tags.delete(tag);
    if (tags.size === 0) this.#searchDrafts.delete(path);
  }

  /** Throws away exactly the drafts that were typed while the search was up. */
  #discardSearchDrafts() {
    if (this.#searchDrafts.size === 0) return;
    const edits = { ...this.edits };
    const strip = { ...this.strip };
    for (const [path, tags] of this.#searchDrafts) {
      const draft = { ...(edits[path] ?? {}) };
      for (const tag of tags) {
        if (tag === STRIP_DRAFT) delete strip[path];
        else delete draft[tag];
      }
      if (Object.keys(draft).length === 0) delete edits[path];
      else edits[path] = draft;
    }
    this.edits = edits;
    this.strip = strip;
    this.#searchDrafts.clear();
    this.#draftGen++;
    // A plan put up before the prune describes drafts that are now gone, and
    // a field left mid-edit was a search frame's field.
    this.plan = null;
    this.editingTag = null;
  }

  /**
   * setScope tells the editor which folder is open and whether the search bar
   * is up, and prunes drafts by one rule: the scope a draft belongs to is the
   * folder it was typed in, so only an actual folder change — app.folder.dir
   * changing, including opening a folder from a search result — discards
   * everything. Opening or closing the search does NOT discard drafts while
   * the folder stays the same: ⌘S must still pick up an edit made three
   * frames ago, and a search glanced at and closed again must not eat it.
   *
   * Drafts typed WHILE the search is open are the exception: the editor is
   * fed by the results then, which can span folders, so those drafts belong
   * to that search. They are tagged as they are made (#searchDrafts) and
   * exactly they are dropped when the search closes without the folder
   * changing — which is what keeps a cross-folder stray from lingering into
   * an unrelated ⌘S later. Without the folder-change prune, switching folders
   * left the chip counting edits against files no longer on screen, and ⌘S
   * wrote into the previous folder with only base filenames in the plan to
   * say so.
   *
   * Keying on the folder rather than the frame list is deliberate — arrowing
   * around a folder reloads the editor without changing what the drafts
   * belong to. Every way into a folder — the tree, a search result, a session, a
   * map pin, an import — funnels through the same app.folder change, so one
   * key covers them all.
   */
  setScope(dir: string, searchOpen: boolean) {
    if (this.#draftFolder !== dir) {
      this.#draftFolder = dir;
      this.#searchOpen = searchOpen;
      this.discard();
      return;
    }
    if (this.#searchOpen === searchOpen) return;
    this.#searchOpen = searchOpen;
    if (!searchOpen) this.#discardSearchDrafts();
  }

  // ---- talking to the backend -----------------------------------------------

  /**
   * load reads the metadata of the given frames. Drafts are left alone:
   * arrowing to the next frame reloads what the inspector shows, and a
   * committed edit on the frame just left is exactly what ⌘S is for. A draft
   * only goes away when it is written or explicitly reverted.
   *
   * Reads are serialised by #loadSeq, the way library.search holds a ticket: a
   * slow read that comes back after a newer one must not put stale frames
   * under the cursor.
   */
  async load(paths: string[]) {
    const seq = ++this.#loadSeq;
    this.loading = true;
    this.error = "";
    try {
      const byPath = await this.#port.read(paths);
      if (seq !== this.#loadSeq) return; // a newer read has answered
      this.frames = paths.map((p) => byPath[p]).filter((f): f is FrameExifDTO => f !== undefined);
    } catch (err) {
      if (seq !== this.#loadSeq) return;
      this.frames = [];
      this.error = message(err);
    } finally {
      if (seq === this.#loadSeq) this.loading = false;
    }
  }

  /**
   * requestWrite plans the draft and puts the plan up for confirmation. This
   * is ⌘S; nothing has touched a file when it resolves.
   *
   * The generation is captured before the await: a folder switch (or a search
   * close) while the plan is in flight discards the drafts it describes, and
   * a dialog for them could never be confirmed — confirmWrite would find
   * nothing to write. The stale response is dropped instead of shown.
   */
  async requestWrite() {
    const edits = this.editsDTO;
    if (edits.length === 0) return;
    this.error = "";
    this.writing = true;
    const gen = this.#draftGen;
    try {
      const plan = await this.#port.plan(edits);
      if (gen !== this.#draftGen) return; // the drafts were discarded meanwhile
      this.plan = plan;
    } catch (err) {
      if (gen === this.#draftGen) this.error = message(err);
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
    // A second Enter before the write resolves would re-run it against the
    // already-written files; the plan is only cleared after the await.
    if (this.plan === null || this.writing) return false;
    const edits = this.editsDTO;
    if (edits.length === 0) {
      // The drafts the plan described were discarded while the dialog was up.
      // Silently refusing would leave a confirm that never confirms; close it
      // and say why, the way the other dialogs surface a dead end.
      this.plan = null;
      app.notify("nothing left to write — the drafts were discarded");
      return false;
    }
    this.writing = true;
    this.error = "";
    try {
      const batch = await this.#port.apply(edits);
      const failed = (batch.actions ?? []).filter((a) => a.outcome !== "ok").length;
      this.plan = null;
      if (failed === 0) {
        this.edits = {};
        this.strip = {};
        this.#searchDrafts.clear();
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
