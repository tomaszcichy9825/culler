// A headless bench for the settings screen.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the screen has to be verifiable
// without a backend — a draft edited and written, a rejection landing on the
// field that caused it, a chord recorded and a conflict refused, a theme
// applied — and none of that needs a photograph or a running Go process.
//
// The fake config port stands in for ConfigService. It is a real round trip:
// the "file" is JSON text, so what the screen sends is serialised and parsed
// back exactly as config.Save and config.Load would, and its keymap check is
// the duplicate rule config.Validate enforces. What it cannot prove is that Go
// accepts the payload — for that the shape of every write is asserted here and
// checked against internal/config/config.go by eye.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9347
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1400,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9347/src/harness/settings.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import SettingsView from "../components/SettingsView.svelte";
import type { Config } from "../lib/bindings";
import {
  CollisionPolicy,
  CutScope,
  KeepMask,
  TrashMode,
} from "../../bindings/github.com/tomaszcichy9825/culler/internal/config/models.js";
import { appearance } from "../lib/appearance.svelte";
import { DEFAULT_KEYMAP, chordFromEvent } from "../lib/keymapCatalogue";
import { PAGES, settings } from "../lib/settings.svelte";
import type { ConfigPort, PageId } from "../lib/settings.svelte";

interface Result {
  name: string;
  pass: boolean;
  detail: string;
}

const results: Result[] = [];

function check(name: string, pass: boolean, detail = "") {
  results.push({ name, pass, detail: pass ? "" : detail });
}

function eq(name: string, actual: unknown, expected: unknown) {
  const same = JSON.stringify(actual) === JSON.stringify(expected);
  check(name, same, `expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

const settle = (ms = 0) => new Promise((r) => setTimeout(r, ms));

function text(el: Element | null): string {
  return (el?.textContent ?? "").replace(/\s+/g, " ").trim();
}

function texts(root: ParentNode, sel: string): string[] {
  return [...root.querySelectorAll(sel)].map((e) => text(e));
}

function click(el: Element | null | undefined) {
  (el as HTMLElement | undefined)?.click();
  flushSync();
}

function press(target: EventTarget, init: KeyboardEventInit) {
  target.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...init }));
  flushSync();
}

/** The stock config, as config.Default() builds it. */
function stockConfig(): Config {
  return {
    behaviour: {
      collisionPolicy: CollisionPolicy.CollisionRenameSuffix,
      bulkConfirmThreshold: 20,
      trashMode: TrashMode.TrashSystem,
      rejectedFolderName: "_Rejected",
      defaultKeepMask: KeepMask.KeepMaskBoth,
      cutRemoves: CutScope.CutRemovesBoth,
      libraryRoot: "~/Pictures",
      moveOnImport: false,
      verifyCopies: true,
      xmpExport: false,
      localReadSlots: 16,
      networkReadSlots: 4,
      networkHashWorkers: 4,
      slowScanHintSeconds: 10,
    },
    keymap: JSON.parse(JSON.stringify(DEFAULT_KEYMAP)) as Config["keymap"],
    rawExts: [".raf", ".arw", ".cr3", ".nef", ".dng"],
    jpegExts: [".jpg", ".jpeg", ".heic"],
    sidecarExts: [".xmp", ".aae", ".dop"],
  };
}

/**
 * The file, as text. Saving serialises and parsing is what the next read gets,
 * so a value that cannot survive JSON is caught here rather than on a card.
 */
class FakeFile implements ConfigPort {
  contents = JSON.stringify(stockConfig(), null, 2);
  writes: Config[] = [];
  /** Set to make the next save fail the way the backend would. */
  reject: string | null = null;

  async get(): Promise<Config> {
    return JSON.parse(this.contents) as Config;
  }

  async path(): Promise<string> {
    return "/tmp/culler-harness/config.json";
  }

  async save(c: Config): Promise<void> {
    if (this.reject !== null) throw new Error(this.reject);
    // config.validateKeymap: literally the same chord under two actions.
    const owner = new Map<string, string>();
    for (const action of Object.keys(c.keymap ?? {}).sort()) {
      for (const chord of c.keymap?.[action] ?? []) {
        const prev = owner.get(chord);
        if (prev !== undefined && prev !== action) {
          throw new Error(`key "${chord}" is bound to both "${prev}" and "${action}"`);
        }
        owner.set(chord, action);
      }
    }
    this.writes.push(JSON.parse(JSON.stringify(c)) as Config);
    this.contents = JSON.stringify(c, null, 2);
  }
}

const file = new FakeFile();

function stage(): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = "position:relative;width:1200px;height:820px";
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

function row(host: ParentNode, field: string): HTMLElement | null {
  return host.querySelector<HTMLElement>(`[data-field="${field}"]`);
}

/** The chip labels of a row, with the selected one marked. */
function chips(host: ParentNode, field: string): string[] {
  const el = row(host, field);
  if (el === null) return [];
  return [...el.querySelectorAll(".ctl")].map((c) => `${text(c)}${c.classList.contains("on") ? "*" : ""}`);
}

function chipButton(host: ParentNode, field: string, label: string): HTMLElement | undefined {
  return [...(row(host, field)?.querySelectorAll<HTMLElement>("button.ctl") ?? [])].find((b) => text(b) === label);
}

function navButton(host: ParentNode, page: string): HTMLElement | null {
  return host.querySelector<HTMLElement>(`[data-page="${page}"]`);
}

function accentToken(): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--accent").trim();
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(raw);
  return m === null ? raw : `rgb(${parseInt(m[1], 16)}, ${parseInt(m[2], 16)}, ${parseInt(m[3], 16)})`;
}

// ---- the run -----------------------------------------------------------------

/**
 * Anything Svelte complains about is a failure in its own right. An effect
 * that reads and writes the same state takes the whole effect tree down with
 * it and leaves a screen that renders once and then quietly stops updating —
 * which every assertion below would otherwise have to notice for itself.
 */
const complaints: string[] = [];
window.addEventListener("error", (e) => complaints.push(`error: ${e.message}`));
window.addEventListener("unhandledrejection", (e) => complaints.push(`rejection: ${String(e.reason)}`));
const passThrough = console.error.bind(console);
console.error = (...args: unknown[]) => {
  complaints.push(`console: ${args.map((a) => String(a)).join(" ")}`);
  passThrough(...args);
};

async function run() {
  for (const key of ["culler.theme", "culler.accent", "culler.mono"]) localStorage.removeItem(key);

  settings.useConfigPort(file);
  // The hook the shell uses to re-read the running config after a write.
  let reloads = 0;
  settings.onSaved = () => (reloads += 1);
  settings.openView("culling");

  const host = stage();
  const view = mount(SettingsView, { target: host });
  await settle();
  flushSync();

  // ---- loading -------------------------------------------------------------

  eq("load · path banner", text(host.querySelector(".path")), "/tmp/culler-harness/config.json");
  eq("load · draft matches the file", settings.draft, JSON.parse(file.contents));
  eq("load · nothing is dirty yet", settings.dirty, false);
  eq(
    "nav · seven pages in the design's order",
    texts(host, ".navrow .name"),
    ["General", "Keymap", "Culling", "Files & writes", "Catalogue", "Appearance", "Advanced"],
  );
  eq("status · the page chip follows the nav", text(host.querySelector(".chip")), "CULLING");

  // ---- a value edited and written ------------------------------------------

  eq("culling · the config's keep mask is the one lit", chips(host, "behaviour.defaultKeepMask"), [
    "both*",
    "raw only",
    "jpeg only",
  ]);

  click(chipButton(host, "behaviour.defaultKeepMask", "raw only"));
  eq("edit · the draft takes the new value", settings.draft?.behaviour.defaultKeepMask, "r");
  eq("edit · the screen is dirty", settings.dirty, true);
  eq("edit · the file is untouched", (JSON.parse(file.contents) as Config).behaviour.defaultKeepMask, "rj");
  check("edit · a write pill appears", host.querySelector(".pill.write") !== null);
  check(
    "edit · the status line says nothing is written",
    text(host.querySelector(".state")).includes("nothing is written until"),
    text(host.querySelector(".state")),
  );

  click(host.querySelector(".pill.write"));
  await settle();
  flushSync();

  eq("save · one write reached the backend", file.writes.length, 1);
  eq(
    "save · the payload carries every top-level field",
    Object.keys(file.writes[0]).sort(),
    ["behaviour", "jpegExts", "keymap", "rawExts", "sidecarExts"],
  );
  eq(
    "save · the payload carries every behaviour field",
    Object.keys(file.writes[0].behaviour).sort(),
    [
      "bulkConfirmThreshold",
      "collisionPolicy",
      "cutRemoves",
      "defaultKeepMask",
      "localReadSlots",
      "networkHashWorkers",
      "networkReadSlots",
      "rejectedFolderName",
      "slowScanHintSeconds",
      "trashMode",
    ],
  );
  eq("save · the edited value is in the payload", file.writes[0].behaviour.defaultKeepMask, "r");
  eq("save · the file now says so", (JSON.parse(file.contents) as Config).behaviour.defaultKeepMask, "r");
  eq("save · the screen is clean again", settings.dirty, false);
  check("save · and says it was written", text(host.querySelector(".state")).includes("written"));

  // put it back, so later checks start from the stock value
  click(chipButton(host, "behaviour.defaultKeepMask", "both"));
  click(host.querySelector(".pill.write"));
  await settle();
  flushSync();
  eq("save · the round trip returns the stock value", settings.draft?.behaviour.defaultKeepMask, "rj");
  eq("save · the app was told to re-read the config", reloads, 2);

  // ---- a value the backend would refuse ------------------------------------

  click(navButton(host, "advanced"));
  const slots = row(host, "behaviour.localReadSlots")?.querySelector("input");
  slots!.value = "0";
  slots!.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();

  eq(
    "invalid · the rule is stated on the row",
    text(row(host, "behaviour.localReadSlots")?.querySelector(".error") ?? null),
    "must be at least 1",
  );
  const before = file.writes.length;
  await settings.save();
  flushSync();
  eq("invalid · nothing was sent", file.writes.length, before);
  eq("invalid · and the banner says why", text(host.querySelector(".banner")), "must be at least 1");

  slots!.value = "16";
  slots!.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
  eq("invalid · fixing it clears the row", row(host, "behaviour.localReadSlots")?.querySelector(".error"), null);

  // ---- a rejection from the backend ----------------------------------------

  file.reject = 'key "x" is bound to both "verdict-cut" and "zoom"';
  settings.patch({ networkReadSlots: 5 });
  flushSync();
  await settings.save();
  await settle();
  flushSync();
  file.reject = null;

  eq("rejected · the backend's words are shown", text(host.querySelector(".banner")), settings.error);
  check("rejected · verbatim", settings.error.includes("bound to both"), settings.error);
  eq("rejected · the page holding the field comes forward", settings.page, "keymap");
  eq("rejected · the draft is not thrown away", settings.draft?.behaviour.networkReadSlots, 5);
  eq("rejected · and the file is untouched", (JSON.parse(file.contents) as Config).behaviour.networkReadSlots, 4);
  settings.revert();
  flushSync();
  eq("revert · the draft goes back to the file", settings.dirty, false);

  // ---- the keymap recorder --------------------------------------------------

  click(navButton(host, "keymap"));
  const keepRow = host.querySelector<HTMLElement>('[data-action="verdict-keep"]')!;
  eq("keymap · the row shows what the config binds", texts(keepRow, "kbd"), ["K"]);

  click(keepRow);
  check("recorder · opens on the clicked action", settings.recording?.action === "verdict-keep");

  press(keepRow, { key: "q" });
  eq("recorder · takes the pressed chord", settings.recording?.chord, "q");
  eq("recorder · a free chord has no conflict", settings.recording?.conflict, null);
  eq("recorder · the box shows the chord", texts(host.querySelector(".record")!, "kbd"), ["Q"]);

  press(keepRow, { key: "Enter" });
  eq("recorder · return binds it", settings.chordsFor("verdict-keep"), ["q"]);
  eq("recorder · and closes", settings.recording, null);
  eq("keymap · the row is marked as changed", text(host.querySelector('[data-action="verdict-keep"] .mark')), "•");
  eq("keymap · the nav badge counts it", text(host.querySelector(".navrow .badge")), "1");

  // a chord another action owns
  click(host.querySelector('[data-action="verdict-keep"]'));
  press(keepRow, { key: "x" });
  eq("conflict · the owner is named", settings.recording?.conflict, "verdict-cut");
  check(
    "conflict · and stated on screen",
    text(host.querySelector(".clash-note")).includes("already bound to verdict-cut"),
    text(host.querySelector(".clash-note")),
  );

  press(keepRow, { key: "Enter" });
  eq("conflict · return will not bind it", settings.chordsFor("verdict-keep"), ["q"]);
  check("conflict · the recorder stays open", settings.recording !== null);
  eq("conflict · verdict-cut keeps its chord", settings.chordsFor("verdict-cut"), ["x"]);

  click([...host.querySelectorAll<HTMLElement>(".buttons button")].find((b) => text(b).startsWith("take it")));
  eq("conflict · taking it moves the chord", settings.chordsFor("verdict-keep"), ["x"]);
  eq("conflict · and unbinds the other action", settings.chordsFor("verdict-cut"), []);
  eq("conflict · which leaves no duplicate to reject", settings.issueFor("keymap"), "");

  // the whole point: a keymap the recorder produced is one the backend takes
  await settings.save();
  await settle();
  flushSync();
  eq("conflict · the backend accepts the result", settings.error, "");
  eq("conflict · the file has the moved chord", (JSON.parse(file.contents) as Config).keymap?.["verdict-keep"], ["x"]);

  // reset, one action at a time
  settings.resetAction("verdict-keep");
  settings.resetAction("verdict-cut");
  flushSync();
  eq("reset · verdict-keep is stock again", settings.chordsFor("verdict-keep"), ["k"]);
  eq("reset · verdict-cut is stock again", settings.chordsFor("verdict-cut"), ["x"]);
  eq("reset · nothing counts as changed", settings.changedActions, []);

  // an action the stock config file does not carry
  settings.startRecording("toggle-sidebar");
  press(keepRow, { key: "b", metaKey: true });
  eq("recorder · modifiers are written the config's way", settings.recording?.chord, "mod+b");
  settings.commit();
  flushSync();
  eq("shell action · recording writes it into the config", settings.chordsFor("toggle-sidebar"), ["mod+b"]);
  settings.resetAction("toggle-sidebar");
  flushSync();
  eq("shell action · resetting removes the line again", settings.draft?.keymap?.["toggle-sidebar"], undefined);

  await settings.save();
  await settle();
  flushSync();
  eq("keymap · the file is back to the stock bindings", (JSON.parse(file.contents) as Config).keymap, DEFAULT_KEYMAP);

  // chordFromEvent on the keys that need special handling
  eq(
    "chords · a digit comes from the physical key",
    chordFromEvent(new KeyboardEvent("keydown", { key: "¡", code: "Digit1", altKey: true })),
    "alt+1",
  );
  eq("chords · space is named", chordFromEvent(new KeyboardEvent("keydown", { key: " " })), "space");
  eq(
    "chords · a named key keeps its name",
    chordFromEvent(new KeyboardEvent("keydown", { key: "ArrowLeft" })),
    "ArrowLeft",
  );
  eq(
    "chords · shift is not added to punctuation",
    chordFromEvent(new KeyboardEvent("keydown", { key: "?", shiftKey: true })),
    "?",
  );
  eq("chords · a modifier alone is not a chord", chordFromEvent(new KeyboardEvent("keydown", { key: "Shift" })), null);

  // ---- the filter -----------------------------------------------------------

  click(navButton(host, "culling"));
  settings.filter = "rejected";
  flushSync();
  const shown = texts(host, ".setting-row .name");
  check("filter · keeps the rows that match", shown.includes("Rejected folder name"), shown.join(" · "));
  check("filter · drops the rest", !shown.includes("Second press"), shown.join(" · "));
  const visibleGroups = [...host.querySelectorAll<HTMLElement>("section.group")]
    .filter((g) => getComputedStyle(g).display !== "none")
    .map((g) => text(g.querySelector(".title")));
  eq("filter · and takes the emptied groups with it", visibleGroups, ["Applying"]);
  settings.filter = "";
  flushSync();
  check("filter · clearing it brings them back", texts(host, ".setting-row .name").length > 8);

  // ---- appearance -----------------------------------------------------------

  click(navButton(host, "appearance"));
  appearance.setScheme("light");
  flushSync();
  eq("theme · light is applied to the document", document.documentElement.getAttribute("data-theme"), "light");
  eq("theme · and the light accent is in force", accentToken(), "rgb(47, 111, 224)");

  appearance.setAccent("purple");
  flushSync();
  eq("accent · purple in the light theme", accentToken(), "rgb(141, 63, 176)");

  appearance.setScheme("dark");
  flushSync();
  eq("accent · the same accent redrawn for dark", accentToken(), "rgb(198, 120, 221)");
  eq(
    "accent · the washes move with it",
    getComputedStyle(document.documentElement).getPropertyValue("--accent-wash-16").trim(),
    "rgba(198, 120, 221, 0.16)",
  );

  appearance.setAccent("blue");
  flushSync();
  eq("accent · blue restores the authored token", accentToken(), "rgb(97, 175, 239)");
  eq("appearance · the choice is remembered", localStorage.getItem("culler.accent"), "blue");
  eq("appearance · as is the scheme, under the key index.html reads", localStorage.getItem("culler.theme"), "dark");

  appearance.setMono("IBM Plex Mono");
  flushSync();
  check(
    "face · the mono token is overridden",
    getComputedStyle(document.documentElement).getPropertyValue("--font-mono").includes("IBM Plex Mono"),
  );
  appearance.setMono("");
  flushSync();
  check(
    "face · and cleared back to the bundled one",
    getComputedStyle(document.documentElement).getPropertyValue("--font-mono").includes("JetBrains Mono"),
  );

  // ---- closing --------------------------------------------------------------

  settings.patch({ bulkConfirmThreshold: 99 });
  flushSync();
  press(window, { key: "Escape" });
  eq("esc · a dirty draft is not discarded on the first press", settings.open, true);
  check(
    "esc · it says what a second press will do",
    text(host.querySelector(".statusbar")).includes("again to discard"),
  );
  press(window, { key: "Escape" });
  eq("esc · the second press closes", settings.open, false);

  settings.openView("general");
  flushSync();
  settings.revert();
  flushSync();
  press(window, { key: "Escape" });
  eq("esc · a clean draft closes at once", settings.open, false);

  unmount(view);
  check("nothing complained on the way through", complaints.length === 0, complaints.join(" || ").slice(0, 900));

  // With #page=<id> the screen is left up afterwards, which is how it is
  // screenshotted: chrome --headless --screenshot against this file.
  const wanted = /#page=([a-z]+)/.exec(location.hash)?.[1];
  if (wanted !== undefined && PAGES.some((p) => p.id === wanted)) {
    document.getElementById("results")!.style.display = "none";
    settings.openView(wanted as PageId);
    mount(SettingsView, { target: stage() });
    await settle();
    flushSync();
  }
}

void run()
  .catch((err: unknown) => {
    check("harness", false, `${String(err)}\n${(err as Error)?.stack ?? ""}`);
  })
  .finally(() => {
    const failed = results.filter((r) => !r.pass);
    document.getElementById("results")!.textContent = JSON.stringify(
      { total: results.length, failed: failed.length, results },
      null,
      1,
    );
    document.title = failed.length === 0 ? `PASS ${results.length}/${results.length}` : `FAIL ${failed.length}`;
  });
