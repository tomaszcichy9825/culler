// The settings draft: the copy of the configuration the user edits, and the
// one explicit write that replaces the file on disk.
//
// The config file stays the source of truth. Nothing here mutates the running
// configuration — the draft is edited freely, ConfigService.Save validates it
// as a whole and rejects it whole, and the draft is then reloaded from the
// backend so a value it normalised is seen rather than assumed. A save that
// fails leaves both the file and the running settings exactly as they were,
// and the reason is shown against the field that caused it.
//
// The keymap page edits the same draft: a binding is a config line like any
// other, and the duplicate rule enforced here is the one config.Validate
// enforces on the way in, so the recorder cannot build a config the backend
// would refuse.

import { ConfigService } from "./bindings";
import type { Config } from "./bindings";
// The generated enums are values, not types, so they cannot come through
// lib/bindings.ts, which re-exports Config as a type only. This is the one
// deep import into the generated bindings.
import {
  CollisionPolicy,
  CutScope,
  KeepMask,
  TrashMode,
} from "../../bindings/github.com/tomaszcichy9825/culler/internal/config/models.js";
import { message } from "./decisions";
import { parseChord } from "./keymap";
import { DEFAULT_KEYMAP, defaultChords, isConfigDefault, sameChords } from "./keymapCatalogue";

export type PageId = "general" | "keymap" | "culling" | "files" | "catalogue" | "appearance" | "advanced";

export interface PageSpec {
  id: PageId;
  /** As drawn in the nav, in the fixed order the design gives. */
  label: string;
  /** The status-bar chip for the page. */
  chip: string;
}

/** Fixed order, fixed names. */
export const PAGES: PageSpec[] = [
  { id: "general", label: "General", chip: "GENERAL" },
  { id: "keymap", label: "Keymap", chip: "KEYMAP" },
  { id: "culling", label: "Culling", chip: "CULLING" },
  { id: "files", label: "Files & writes", chip: "FILES" },
  { id: "catalogue", label: "Catalogue", chip: "CATALOGUE" },
  { id: "appearance", label: "Appearance", chip: "APPEARANCE" },
  { id: "advanced", label: "Advanced", chip: "ADVANCED" },
];

/**
 * The reason a draft cannot be written, against the field that caused it.
 * The rules mirror config.Validate — a draft that passes here is one the
 * backend accepts, and a backend error that arrives anyway is shown the same
 * way rather than being swallowed.
 */
export interface Issue {
  field: string;
  text: string;
}

/** Which page each field is drawn on, so an error can bring its page forward. */
const FIELD_PAGE: Record<string, PageId> = {
  "behaviour.defaultKeepMask": "culling",
  "behaviour.cutRemoves": "culling",
  "behaviour.bulkConfirmThreshold": "culling",
  "behaviour.collisionPolicy": "culling",
  "behaviour.trashMode": "culling",
  "behaviour.rejectedFolderName": "culling",
  rawExts: "files",
  jpegExts: "files",
  sidecarExts: "files",
  "behaviour.slowScanHintSeconds": "general",
  "behaviour.localReadSlots": "advanced",
  "behaviour.networkReadSlots": "advanced",
  "behaviour.networkHashWorkers": "advanced",
  keymap: "keymap",
};

export function pageForField(field: string): PageId | null {
  return FIELD_PAGE[field] ?? null;
}

/**
 * fieldForMessage reads a backend error back to the field it came from, so a
 * rejection lands on the control that caused it instead of only in a banner.
 * The phrases are the ones config.Validate writes.
 */
export function fieldForMessage(text: string): string | null {
  const m = text.toLowerCase();
  if (m.includes("bound to both")) return "keymap";
  if (m.includes("collision policy")) return "behaviour.collisionPolicy";
  if (m.includes("rejected folder name")) return "behaviour.rejectedFolderName";
  if (m.includes("trash mode")) return "behaviour.trashMode";
  if (m.includes("keep mask")) return "behaviour.defaultKeepMask";
  if (m.includes("cut removes")) return "behaviour.cutRemoves";
  if (m.includes("bulk confirm threshold")) return "behaviour.bulkConfirmThreshold";
  for (const name of ["localReadSlots", "networkReadSlots", "networkHashWorkers", "slowScanHintSeconds"]) {
    if (text.includes(name)) return `behaviour.${name}`;
  }
  return null;
}

const POSITIVE_FIELDS = [
  "localReadSlots",
  "networkReadSlots",
  "networkHashWorkers",
  "slowScanHintSeconds",
] as const;

/**
 * The values each enumerated setting may take. The generated enums carry a
 * $zero member for Go's empty string, which is not a choice — the backend
 * refuses it — so the lists are written out rather than taken from the enum.
 */
export const COLLISION_POLICIES: CollisionPolicy[] = [
  CollisionPolicy.CollisionSkip,
  CollisionPolicy.CollisionRenameSuffix,
  CollisionPolicy.CollisionOverwrite,
];
export const TRASH_MODES: TrashMode[] = [TrashMode.TrashSystem, TrashMode.TrashRejectedFolder];
export const KEEP_MASKS: KeepMask[] = [KeepMask.KeepMaskBoth, KeepMask.KeepMaskRAW, KeepMask.KeepMaskJPEG];
export const CUT_SCOPES: CutScope[] = [CutScope.CutRemovesBoth, CutScope.CutRemovesMasked];

/**
 * duplicateChord is config.validateKeymap: a chord bound to two actions makes
 * the winner depend on map order, so the file is refused. Chords are compared
 * literally there, and by the signature they resolve to here — "mod+z" and
 * "shift+mod+z" are different bindings, but "mod+shift+z" and "shift+mod+z"
 * are the same one, and only this side can see that.
 */
export function duplicateChord(keymap: Config["keymap"]): { chord: string; a: string; b: string } | null {
  const owner = new Map<string, { action: string; chord: string }>();
  for (const action of Object.keys(keymap ?? {}).sort()) {
    for (const chord of keymap?.[action] ?? []) {
      const sig = parseChord(chord);
      const prev = owner.get(sig);
      if (prev !== undefined && prev.action !== action) {
        return { chord: prev.chord, a: prev.action, b: action };
      }
      owner.set(sig, { action, chord });
    }
  }
  return null;
}

/** validate mirrors config.Validate, so the draft is judged before it is sent. */
export function validate(c: Config): Issue[] {
  const issues: Issue[] = [];
  const b = c.behaviour;

  if (!COLLISION_POLICIES.includes(b.collisionPolicy)) {
    issues.push({ field: "behaviour.collisionPolicy", text: `unknown collision policy "${b.collisionPolicy}"` });
  }
  if (!TRASH_MODES.includes(b.trashMode)) {
    issues.push({ field: "behaviour.trashMode", text: `unknown trash mode "${b.trashMode}"` });
  } else if (b.trashMode === TrashMode.TrashRejectedFolder && b.rejectedFolderName.trim() === "") {
    issues.push({
      field: "behaviour.rejectedFolderName",
      text: "a folder name is needed when rejects go to a folder",
    });
  }
  if (!KEEP_MASKS.includes(b.defaultKeepMask)) {
    issues.push({ field: "behaviour.defaultKeepMask", text: `unknown keep mask "${b.defaultKeepMask}"` });
  }
  if (!CUT_SCOPES.includes(b.cutRemoves)) {
    issues.push({ field: "behaviour.cutRemoves", text: `unknown cut scope "${b.cutRemoves}"` });
  }
  if (!Number.isFinite(b.bulkConfirmThreshold) || b.bulkConfirmThreshold < 0) {
    issues.push({ field: "behaviour.bulkConfirmThreshold", text: "must not be negative" });
  }
  for (const name of POSITIVE_FIELDS) {
    const v = b[name];
    if (!Number.isFinite(v) || v < 1) issues.push({ field: `behaviour.${name}`, text: "must be at least 1" });
  }

  const dup = duplicateChord(c.keymap);
  if (dup !== null) {
    issues.push({ field: "keymap", text: `${dup.chord} is bound to both ${dup.a} and ${dup.b}` });
  }
  return issues;
}

/** The chord being recorded, and what it would collide with. */
export interface Recording {
  action: string;
  /** Null until a key is pressed. */
  chord: string | null;
  /** The action that already owns the chord, if any. */
  conflict: string | null;
}

/**
 * The three calls the settings screen makes. It is an interface so the screen
 * can be driven without a backend — the default port is the real service.
 */
export interface ConfigPort {
  get(): Promise<Config>;
  save(c: Config): Promise<void>;
  path(): Promise<string>;
}

const wailsPort: ConfigPort = {
  get: () => ConfigService.Get(),
  save: (c) => ConfigService.Save(c),
  path: () => ConfigService.Path(),
};

function clone(c: Config): Config {
  return JSON.parse(JSON.stringify(c)) as Config;
}

/** A key-ordered rendering, so a reordered keymap is not mistaken for an edit. */
function canonical(c: Config | null): string {
  if (c === null) return "";
  return JSON.stringify(c, (_k, v: unknown) => {
    if (v === null || typeof v !== "object" || Array.isArray(v)) return v;
    const obj = v as Record<string, unknown>;
    return Object.fromEntries(Object.keys(obj).sort().map((k) => [k, obj[k]]));
  });
}

class SettingsState {
  /** Whether the settings screen is showing. The shell reads this. */
  open = $state(false);
  page = $state<PageId>("general");
  /** The title-bar filter. Rows whose name or description miss it are hidden. */
  filter = $state("");

  /** The edited copy, and the one that was loaded, for comparison. */
  draft = $state<Config | null>(null);
  loaded = $state<Config | null>(null);

  /** Where the file lives, for the banner. */
  path = $state("");
  loading = $state(false);
  saving = $state(false);
  /** The last error a load or a save produced, verbatim. */
  error = $state("");
  /** Set for a moment after a successful write. */
  written = $state(false);

  recording = $state<Recording | null>(null);

  /**
   * Called after the file has been written and re-read. The running app reads
   * the config once at startup, so whoever owns that read hangs their reload
   * here — otherwise a rebound key would not take effect until a restart.
   */
  onSaved: (() => void) | null = null;

  private port: ConfigPort = wailsPort;

  /** useConfigPort swaps the backend out. For the harness only. */
  useConfigPort(port: ConfigPort) {
    this.port = port;
  }

  get dirty(): boolean {
    return this.draft !== null && canonical(this.draft) !== canonical(this.loaded);
  }

  get issues(): Issue[] {
    return this.draft === null ? [] : validate(this.draft);
  }

  issueFor(field: string): string {
    return this.issues.find((i) => i.field === field)?.text ?? "";
  }

  /**
   * errorFor is what a control should say about itself: the rule it breaks, or
   * the backend's own words when a rejected save named this field.
   */
  errorFor(field: string): string {
    const issue = this.issueFor(field);
    if (issue !== "") return issue;
    return this.error !== "" && fieldForMessage(this.error) === field ? this.error : "";
  }

  /** How many actions differ from the chords they ship with. */
  get changedActions(): string[] {
    const keymap = this.draft?.keymap ?? {};
    const names = new Set([...Object.keys(keymap), ...Object.keys(DEFAULT_KEYMAP)]);
    const changed: string[] = [];
    for (const action of names) {
      if (!sameChords(keymap[action] ?? [], defaultChords(action))) changed.push(action);
    }
    return changed.sort();
  }

  async load() {
    this.loading = true;
    this.error = "";
    // Reopening the screen should not still be reporting the last write.
    this.written = false;
    try {
      const [cfg, path] = await Promise.all([this.port.get(), this.port.path()]);
      this.loaded = clone(cfg);
      this.draft = clone(cfg);
      this.path = path;
    } catch (err) {
      this.error = `could not read the settings file: ${message(err)}`;
    } finally {
      this.loading = false;
    }
  }

  /**
   * save writes the draft. A draft that fails the rules is not sent at all —
   * the reason is already on screen — and the page holding the first offending
   * field is brought forward. Returns whether the file was written.
   */
  async save(): Promise<boolean> {
    const draft = this.draft;
    if (draft === null || this.saving) return false;

    const issues = validate(draft);
    if (issues.length > 0) {
      this.error = issues[0].text;
      const page = pageForField(issues[0].field);
      if (page !== null) this.page = page;
      return false;
    }

    this.saving = true;
    this.error = "";
    try {
      await this.port.save(clone(draft));
    } catch (err) {
      // The backend rejected it whole: the file and the running settings are
      // untouched, and the draft stays as it is so the value can be fixed.
      this.error = message(err);
      const field = fieldForMessage(this.error);
      const page = field === null ? null : pageForField(field);
      if (page !== null) this.page = page;
      return false;
    } finally {
      this.saving = false;
    }

    // Adopt what the file now says rather than what was sent.
    try {
      const cfg = await this.port.get();
      this.loaded = clone(cfg);
      this.draft = clone(cfg);
    } catch (err) {
      this.error = `written, but could not re-read the file: ${message(err)}`;
      this.loaded = clone(draft);
    }
    this.written = true;
    this.onSaved?.();
    return true;
  }

  /** revert throws the draft away and goes back to what was loaded. */
  revert() {
    if (this.loaded === null) return;
    this.draft = clone(this.loaded);
    this.error = "";
    this.recording = null;
  }

  /** openView shows the screen; the screen itself reads the file as it mounts. */
  openView(page?: PageId) {
    if (page !== undefined) this.page = page;
    this.open = true;
  }

  close() {
    this.open = false;
    this.recording = null;
    this.error = "";
  }

  toggle() {
    if (this.open) this.close();
    else this.openView();
  }

  show(page: PageId) {
    this.page = page;
    this.recording = null;
  }

  // ---- editing -------------------------------------------------------------

  /** patch edits the behaviour block of the draft. */
  patch(values: Partial<Config["behaviour"]>) {
    if (this.draft === null) return;
    this.draft.behaviour = { ...this.draft.behaviour, ...values };
    this.written = false;
  }

  setExts(field: "rawExts" | "jpegExts" | "sidecarExts", exts: string[]) {
    if (this.draft === null) return;
    this.draft[field] = exts;
    this.written = false;
  }

  chordsFor(action: string): string[] {
    return this.draft?.keymap?.[action] ?? [];
  }

  /** isChanged reports whether an action's chords differ from its default. */
  isChanged(action: string): boolean {
    return !sameChords(this.chordsFor(action), defaultChords(action));
  }

  private writeKeymap(action: string, chords: string[] | null) {
    if (this.draft === null) return;
    const keymap = { ...(this.draft.keymap ?? {}) };
    if (chords === null) delete keymap[action];
    else keymap[action] = chords;
    this.draft.keymap = keymap;
    this.written = false;
  }

  /**
   * ownerOf is the action a chord already belongs to, ignoring one action —
   * the one being recorded, which is allowed to keep its own chord.
   */
  ownerOf(chord: string, except: string): string | null {
    const sig = parseChord(chord);
    for (const [action, chords] of Object.entries(this.draft?.keymap ?? {})) {
      if (action === except) continue;
      for (const c of chords ?? []) {
        if (parseChord(c) === sig) return action;
      }
    }
    return null;
  }

  startRecording(action: string) {
    this.recording = { action, chord: null, conflict: null };
  }

  cancelRecording() {
    this.recording = null;
  }

  /** capture takes the pressed chord and says at once what it would collide with. */
  capture(chord: string) {
    const rec = this.recording;
    if (rec === null) return;
    this.recording = { action: rec.action, chord, conflict: this.ownerOf(chord, rec.action) };
  }

  /**
   * commit binds the recorded chord. A chord another action already owns is
   * refused, because a config with the same chord twice is one the backend
   * rejects whole. Passing steal takes it from the other action instead, which
   * is the only way to move a binding without editing the file by hand.
   * Returns whether the binding was made.
   */
  commit(options: { steal?: boolean } = {}): boolean {
    const rec = this.recording;
    if (rec === null || rec.chord === null) return false;
    if (rec.conflict !== null && options.steal !== true) return false;

    if (rec.conflict !== null) {
      const sig = parseChord(rec.chord);
      const left = this.chordsFor(rec.conflict).filter((c) => parseChord(c) !== sig);
      this.writeKeymap(rec.conflict, left.length > 0 || isConfigDefault(rec.conflict) ? left : null);
    }
    this.writeKeymap(rec.action, [rec.chord]);
    this.recording = null;
    return true;
  }

  /**
   * resetAction puts an action back to the chords it ships with. An action the
   * stock config file does not carry is removed from the keymap rather than
   * written out with its shell default, so the file keeps saying only what the
   * user changed.
   */
  resetAction(action: string) {
    const stock = defaultChords(action);
    this.writeKeymap(action, isConfigDefault(action) ? [...stock] : null);
    if (this.recording?.action === action) this.recording = null;
  }

  /** unbind leaves an action with no chord at all. */
  unbind(action: string) {
    this.writeKeymap(action, []);
  }
}

export const settings = new SettingsState();
