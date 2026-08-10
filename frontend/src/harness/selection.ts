// A headless bench for range selection: the anchor, the sweep, and the three
// cull layouts it has to behave the same in.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It mounts the whole shell rather than a component,
// because the thing being checked is a collaboration — a click on a tile, a
// Shift held with an arrow, the table's own sort, the filter effect that
// derives what is on screen — and any of those tested alone would pass while
// the flow the user actually performs did not.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9351
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1500,1000 \
//     --virtual-time-budget=20000 --dump-dom \
//     http://127.0.0.1:9351/src/harness/selection.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount } from "svelte";
import App from "../App.svelte";
import { sortRows, table } from "../components/TableView.svelte";
import type { GroupDTO } from "../lib/bindings";
import { geotag } from "../lib/geotag.svelte";
import { buildLookup, eventSignature, isMac, unshiftedSignature } from "../lib/keymap";
import { DEFAULT_KEYMAP } from "../lib/keymapCatalogue";
import { NO_FILTER, palette } from "../lib/palette.svelte";
import { shell } from "../lib/shell.svelte";
import { gridSort } from "../lib/sort.svelte";
import { app, groupKey } from "../lib/state.svelte";

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

const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";
/** Enough frames for three rows of four, so a vertical step is a real row. */
const COUNT = 12;

/**
 * The bench's frames. The clock runs forward with the stem, and the grid's
 * default sort is newest first, so the sheet shows them in reverse stem order
 * — which is the point: array order, sheet order and the table's own stem sort
 * are three different orders here, and a range measured in the wrong one shows
 * up immediately.
 */
function frame(n: number, over: Partial<GroupDTO> = {}): GroupDTO {
  const stem = `DSCF12${String(n).padStart(2, "0")}`;
  return {
    dir: DIR,
    stem,
    kind: "paired",
    hasRaw: true,
    hasJpeg: true,
    rawPath: `${DIR}/${stem}.RAF`,
    jpegPath: `${DIR}/${stem}.JPG`,
    sidecars: 0,
    shot: `2026-07-18T10:${String(n).padStart(2, "0")}:00Z`,
    warnings: null,
    verdict: "",
    mask: "rj",
    rating: 0,
    hash: `hash-${stem}`,
    destination: "",
    verb: "",
    decision: "",
    ...over,
  };
}

/** Two RAW-only frames, so a kind filter has something to hide. */
function seed(): GroupDTO[] {
  const out: GroupDTO[] = [];
  for (let n = 0; n < COUNT; n++) {
    const raw = n === 3 || n === 7;
    out.push(raw ? frame(n, { kind: "raw-only", hasJpeg: false, jpegPath: "" }) : frame(n));
  }
  return out;
}

/** Puts the seeded folder back on the grid, undecided and unselected. */
function reset() {
  palette.setFilter({ ...NO_FILTER });
  gridSort.setField("shot"); // newest first, whatever a previous run left behind
  const groups = seed();
  app.folder = { dir: DIR, network: false, groups };
  app.allGroups = groups;
  app.focusIndex = 0;
  app.clearSelection();
  app.compare = null;
  app.plan = null;
  app.scanning = null;
  app.error = "";
  app.gridColumns = 4;
  flushSync();
}

/** The keys of the frames on screen, in the order they are shown. */
function shownKeys(): string[] {
  return app.groups.map(groupKey);
}

/** The selection as sheet positions, so an assertion reads like the screen. */
function selectedPositions(): number[] {
  const out: number[] = [];
  shownKeys().forEach((k, i) => {
    if (app.selection.has(k)) out.push(i);
  });
  return out;
}

function stems(list: GroupDTO[]): string[] {
  return list.map((g) => g.stem);
}

/** The tiles the contact sheet has drawn, by the stem in each footer. */
function tiles(): Map<string, HTMLElement> {
  const out = new Map<string, HTMLElement>();
  for (const el of document.querySelectorAll<HTMLElement>(".tile")) {
    const stem = el.querySelector(".stem")?.textContent?.trim() ?? "";
    if (stem !== "") out.set(stem, el);
  }
  return out;
}

/** A click carrying whatever modifiers a gesture holds down. */
function clickOn(el: Element, mods: MouseEventInit = {}) {
  el.dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true, ...mods }));
  flushSync();
}

/** The primary modifier, as the platform spells it. */
const MOD: MouseEventInit = isMac ? { metaKey: true } : { ctrlKey: true };

/** A key press at the window, which is where the app's one listener sits. */
function press(key: string, over: KeyboardEventInit = {}) {
  window.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...over }));
  flushSync();
}

// ---- the contact sheet -------------------------------------------------------

function sheetClicks() {
  reset();
  shell.setMode("cull");
  shell.setLayout(0);
  app.view = "grid";
  flushSync();

  const drawn = tiles();
  eq("sheet · every frame is drawn", drawn.size, COUNT);
  const order = stems(app.groups);

  // Click A: focus lands, nothing is left selected, and this is the anchor.
  clickOn(drawn.get(order[0])!);
  eq("click · focus lands on the clicked frame", app.focusIndex, 0);
  eq("click · and leaves no selection behind", app.selection.size, 0);
  eq("click · the clicked frame becomes the anchor", app.anchor, groupKey(app.groups[0]));

  // Shift-click E: A..E, in the order on screen.
  clickOn(drawn.get(order[4])!, { shiftKey: true });
  eq("shift-click · A..E are selected, in visible order", selectedPositions(), [0, 1, 2, 3, 4]);
  eq("shift-click · focus is at the far end", app.focusIndex, 4);
  eq("shift-click · the anchor has not moved", app.anchor, groupKey(app.groups[0]));
  eq("shift-click · the selection is what actions target", stems(app.targets), order.slice(0, 5));

  // Shrinking with the pointer works the same way.
  clickOn(drawn.get(order[2])!, { shiftKey: true });
  eq("shift-click · clicking back towards the anchor shrinks it", selectedPositions(), [0, 1, 2]);

  // And past the anchor, the other way.
  clickOn(drawn.get(order[0])!);
  clickOn(drawn.get(order[4])!, { shiftKey: true });
  clickOn(drawn.get(order[6])!);
  clickOn(drawn.get(order[2])!, { shiftKey: true });
  eq("shift-click · a fresh anchor sweeps backwards too", selectedPositions(), [2, 3, 4, 5, 6]);

  // The mark has to read from across the room, not just exist in the DOM.
  const marked = [...document.querySelectorAll<HTMLElement>(".tile.selected")];
  eq("sheet · every selected frame is marked", marked.length, 5);
  check(
    "sheet · a selected tile carries a visible ring",
    marked.every((el) => getComputedStyle(el).boxShadow !== "none"),
    marked.map((el) => getComputedStyle(el).boxShadow).join(" / "),
  );
  eq(
    "sheet · and says so to assistive tech",
    marked.every((el) => el.getAttribute("aria-selected") === "true"),
    true,
  );

  // ⌘-click toggles one frame and leaves the anchor where it was.
  clickOn(drawn.get(order[0])!);
  const anchor = app.anchor;
  clickOn(drawn.get(order[6])!, MOD);
  eq("mod-click · toggles the one frame in", selectedPositions(), [6]);
  eq("mod-click · without moving the anchor", app.anchor, anchor);
  eq("mod-click · focus follows the click", app.focusIndex, 6);
  clickOn(drawn.get(order[6])!, MOD);
  eq("mod-click · and the same click takes it out again", app.selection.size, 0);

  // A hand-picked frame survives a sweep that does not cover it.
  clickOn(drawn.get(order[0])!);
  clickOn(drawn.get(order[9])!, MOD);
  clickOn(drawn.get(order[2])!, { shiftKey: true });
  eq("mod-click · a hand-picked frame survives a later sweep", selectedPositions(), [0, 1, 2, 9]);
}

// ---- shift and the arrows ----------------------------------------------------

function sheetArrows() {
  reset();
  shell.setLayout(0);
  app.view = "grid";
  flushSync();

  const drawn = tiles();
  const order = stems(app.groups);
  eq("grid · four columns, so a vertical step is four frames", app.cols, 4);

  clickOn(drawn.get(order[0])!);
  press("ArrowDown", { shiftKey: true });
  eq("shift+↓ · one row down extends by a row", selectedPositions(), [0, 1, 2, 3, 4]);
  press("ArrowDown", { shiftKey: true });
  eq("shift+↓ · twice extends two rows", selectedPositions(), [0, 1, 2, 3, 4, 5, 6, 7, 8]);
  eq("shift+↓ · focus is at the far end", app.focusIndex, 8);

  press("ArrowUp", { shiftKey: true });
  eq("shift+↑ · arrowing back towards the anchor shrinks it", selectedPositions(), [0, 1, 2, 3, 4]);
  press("ArrowUp", { shiftKey: true });
  eq("shift+↑ · back to the anchor is the anchor alone", selectedPositions(), [0]);

  // Sideways, and past the anchor the other way.
  clickOn(drawn.get(order[5])!);
  press("ArrowRight", { shiftKey: true });
  eq("shift+→ · one frame at a time", selectedPositions(), [5, 6]);
  press("ArrowLeft", { shiftKey: true });
  press("ArrowLeft", { shiftKey: true });
  eq("shift+← · grows the other side of the anchor", selectedPositions(), [4, 5]);

  // An unshifted arrow is still plain navigation, and leaves the selection be.
  const before = selectedPositions();
  press("ArrowRight");
  eq("→ · a plain arrow moves focus only", selectedPositions(), before);
  eq("→ · and focus really moved", app.focusIndex, 5);

  // The keys the whole selection then answers to.
  clickOn(drawn.get(order[0])!);
  press("ArrowDown", { shiftKey: true });
  press("k");
  eq("k · marks every selected frame", app.groups.slice(0, 5).map((g) => g.verdict), ["keep", "keep", "keep", "keep", "keep"]);
  eq("k · and nothing else", app.groups[5].verdict, "");
  eq("k · a selection does not advance the focus", app.focusIndex, 4);
  press("3");
  eq("3 · rates the whole selection", app.groups.slice(0, 5).map((g) => g.rating), [3, 3, 3, 3, 3]);
  eq("selection · is what a geotag would place", geotag.targetCount, 5);

  // ⌘A and Esc still mean what they meant.
  press("a", isMac ? { metaKey: true } : { ctrlKey: true });
  eq("⌘A · selects everything on the sheet", app.selection.size, COUNT);
  press("ArrowDown", { shiftKey: true });
  eq("⌘A · then a sweep starts a fresh range from the anchor", app.selection.size, 5);
  press("Escape");
  eq("esc · clears the selection first", app.selection.size, 0);
  eq("esc · and forgets the anchor with it", app.anchor, null);
}

// ---- a sort flip, and a filter, mid-selection ---------------------------------

function sortAndFilter() {
  reset();
  shell.setLayout(0);
  app.view = "grid";
  flushSync();

  const drawn = tiles();
  const order = stems(app.groups);
  clickOn(drawn.get(order[0])!);
  clickOn(drawn.get(order[4])!, { shiftKey: true });
  const chosen = stems(app.targets);
  eq("sort · five frames selected before the flip", chosen.length, 5);

  gridSort.reverse();
  flushSync();
  eq("sort · flipping the sort turns the sheet round", app.groups[0].stem, order[COUNT - 1]);
  eq("sort · the selection survives it, frame for frame", stems(app.targets).sort(), [...chosen].sort());
  eq("sort · and the anchor still names the same frame", app.anchor, `${DIR}/${order[0]}`);

  const after = tiles();
  const flipped = stems(app.groups);
  clickOn(after.get(flipped[1])!);
  clickOn(after.get(flipped[3])!, { shiftKey: true });
  eq("sort · a sweep after the flip runs in the new visible order", selectedPositions(), [1, 2, 3]);

  // A filter that hides half the selection: the keys stay, the actions do not
  // reach the hidden frames, and lifting the filter brings them back.
  reset();
  flushSync();
  const fresh = tiles();
  const shown = stems(app.groups);
  clickOn(fresh.get(shown[0])!);
  clickOn(fresh.get(shown[COUNT - 1])!, { shiftKey: true });
  eq("filter · the whole folder is selected", app.selection.size, COUNT);

  palette.setFilter({ ...NO_FILTER, kind: "pair" });
  flushSync();
  eq("filter · the sheet narrows", app.groups.length, COUNT - 2);
  eq("filter · the selection keeps every key", app.selection.size, COUNT);
  eq("filter · but nothing acts on a frame that is hidden", app.targets.length, COUNT - 2);
  check(
    "filter · and no hidden frame is among the targets",
    app.targets.every((g) => g.hasJpeg),
    stems(app.targets).join(","),
  );

  palette.setFilter({ ...NO_FILTER });
  flushSync();
  eq("filter · lifting it brings the hidden frames back to the selection", app.targets.length, COUNT);
}

// ---- the table walks its own sort ---------------------------------------------

function tableRange() {
  reset();
  shell.setLayout(2);
  app.view = "grid";
  flushSync();

  // Scoped to the table's own canvas: ".row" is a common class, and the
  // palettes and the loupe card carry rows of their own.
  const rows = () => [...document.querySelectorAll<HTMLElement>(".table .canvas .row")];
  check("table · the table is on screen", rows().length === COUNT, `${rows().length} rows`);

  // Sort by stem ascending, which is the reverse of the sheet's shot order —
  // so a range walked in the array's order would be visibly wrong.
  const header = document.querySelector<HTMLButtonElement>(".head .c-stem .sort")!;
  header.click();
  flushSync();
  if (!table.sort().ascending) {
    header.click();
    flushSync();
  }
  eq("table · sorted by stem, ascending", `${table.sort().column}:${table.sort().ascending}`, "stem:true");

  const tableOrder = sortRows(app.groups, table.sort()).map((row) => row.group.stem);
  check(
    "table · which is not the order the sheet holds",
    tableOrder.join(",") !== stems(app.groups).join(","),
    tableOrder.join(","),
  );

  const rowFor = (stem: string) => rows().find((r) => r.querySelector(".c-stem")?.textContent?.trim() === stem)!;
  clickOn(rowFor(tableOrder[2]));
  eq("table · a row click focuses that frame", app.groups[app.focusIndex].stem, tableOrder[2]);
  eq("table · and leaves no selection behind", app.selection.size, 0);

  press("ArrowDown", { shiftKey: true });
  press("ArrowDown", { shiftKey: true });
  eq(
    "table · shift+↓ walks the table's own sort, one row per press",
    stems(app.targets).sort(),
    tableOrder.slice(2, 5).sort(),
  );
  eq("table · focus is on the third row down", app.groups[app.focusIndex].stem, tableOrder[4]);

  press("ArrowUp", { shiftKey: true });
  eq("table · and arrowing back shrinks it", stems(app.targets).sort(), tableOrder.slice(2, 4).sort());

  clickOn(rowFor(tableOrder[8]), { shiftKey: true });
  eq(
    "table · shift-click sweeps the table's order too",
    stems(app.targets).sort(),
    tableOrder.slice(2, 9).sort(),
  );

  const marked = rows().filter((r) => r.className.includes("selected"));
  eq("table · every selected row is marked", marked.length, 7);
  const plain = rows().find((r) => !r.className.includes("selected"))!;
  check(
    "table · a selected row is washed, not just hairlined",
    getComputedStyle(marked[0]).backgroundColor !== getComputedStyle(plain).backgroundColor,
    `${getComputedStyle(marked[0]).backgroundColor} vs ${getComputedStyle(plain).backgroundColor}`,
  );
}

// ---- loupe-first, where the filmstrip is the sheet -----------------------------

function filmstripRange() {
  reset();
  shell.setLayout(1);
  app.view = "loupe";
  flushSync();

  const frames = () => [...document.querySelectorAll<HTMLElement>(".strip .frame")];
  eq("filmstrip · one frame per group", frames().length, COUNT);

  clickOn(frames()[1]);
  eq("filmstrip · a click focuses that frame", app.focusIndex, 1);
  eq("filmstrip · and leaves no selection behind", app.selection.size, 0);

  clickOn(frames()[5], { shiftKey: true });
  eq("filmstrip · shift-click sweeps the strip", selectedPositions(), [1, 2, 3, 4, 5]);

  press("ArrowRight", { shiftKey: true });
  eq("filmstrip · shift+→ extends one frame at a time", selectedPositions(), [1, 2, 3, 4, 5, 6]);
  press("ArrowLeft", { shiftKey: true });
  press("ArrowLeft", { shiftKey: true });
  eq("filmstrip · and arrowing back shrinks it", selectedPositions(), [1, 2, 3, 4]);

  const marked = [...document.querySelectorAll<HTMLElement>(".strip .frame.selected")];
  eq("filmstrip · every selected frame is marked", marked.length, 4);
  check(
    "filmstrip · a selected thumbnail carries a visible ring",
    marked.every((el) => getComputedStyle(el.querySelector(".thumb")!).boxShadow !== "none"),
    marked.map((el) => getComputedStyle(el.querySelector(".thumb")!).boxShadow).join(" / "),
  );

  // Zoomed, the arrows pan the photograph. Shift must not turn that into a
  // selection sweep behind the user's back.
  const before = selectedPositions();
  app.zoom = true;
  flushSync();
  press("ArrowRight", { shiftKey: true });
  eq("filmstrip · zoomed, shift+→ pans rather than extending", selectedPositions(), before);
  app.resetZoom();
  flushSync();
}

// ---- the binding itself -------------------------------------------------------

function bindings() {
  // Shift+arrow is deliberately not a chord of its own: nothing binds it, and
  // the press is read a second time without Shift to find the focus action it
  // modifies. Both halves of that have to hold, or the four ids would have to
  // be mirrored into Go's DefaultKeymap and the settings catalogue as well.
  const lookup = buildLookup(DEFAULT_KEYMAP);
  const down = new KeyboardEvent("keydown", { key: "ArrowDown", shiftKey: true });
  check("keymap · nothing binds the shifted arrow", lookup.get(eventSignature(down)) === undefined, eventSignature(down));
  eq("keymap · read without shift it is the focus action", lookup.get(unshiftedSignature(down)), "focus-down");

  const shiftC = new KeyboardEvent("keydown", { key: "c", shiftKey: true });
  eq("keymap · a chord bound with shift is untouched", lookup.get(eventSignature(shiftC)), "enter-compare");
}

// ---- run ---------------------------------------------------------------------

async function run() {
  // No last folder, so the shell mounts without chasing a scan that would land
  // on top of the frames this bench seeds.
  localStorage.removeItem("culler.lastFolder");
  localStorage.removeItem("culler.tableSort");

  const host = document.createElement("div");
  host.style.cssText = "position:relative;width:1440px;height:900px;overflow:hidden";
  document.getElementById("mounts")!.appendChild(host);
  mount(App, { target: host });
  await settle(80);
  flushSync();

  app.keymap = JSON.parse(JSON.stringify(DEFAULT_KEYMAP)) as Record<string, string[]>;
  app.cutRemoves = "both";
  app.defaultKeepMask = "rj";
  flushSync();

  bindings();
  sheetClicks();
  sheetArrows();
  sortAndFilter();
  tableRange();
  filmstripRange();

  const failed = results.filter((r) => !r.pass);
  document.getElementById("results")!.textContent = JSON.stringify(
    { total: results.length, failed: failed.length, results },
    null,
    1,
  );
  document.title = failed.length === 0 ? `PASS ${results.length}/${results.length}` : `FAIL ${failed.length}`;
}

void run().catch((err: unknown) => {
  document.getElementById("results")!.textContent = `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
