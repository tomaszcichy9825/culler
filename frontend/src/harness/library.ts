// A headless bench for the catalogue where it now lives: CULL's sidebar tree,
// the search bar `/` opens over the grid, and the Sessions group under it.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the tree's keyboard, the
// register-on-open contract and the search round trip all have to be
// verifiable without a catalogue, a backend or a person to look at it — in
// particular that ⏎ on a search result carries the frame's hash across, that
// ⏎ on a tree node or a session row does not, and that Esc puts the open
// folder back on the grid exactly as it was.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9346
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1400,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9346/src/harness/library.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount } from "svelte";
import SearchBar from "../components/library/SearchBar.svelte";
import Sessions from "../components/library/Sessions.svelte";
import Tree from "../components/Tree.svelte";
import { ACTIONS, showSearchResults } from "../lib/actions";
import type { FolderDTO, GroupDTO } from "../lib/bindings";
import {
  connectCatalog,
  frameToGroup,
  library,
  onOpenFolder,
  sessionLabel,
  UNDECIDED_UNKNOWN,
  type CatalogFacets,
  type CatalogFrame,
  type CatalogRoot,
  type CatalogSession,
  type CatalogSource,
  type CatalogTreeNode,
} from "../lib/library.svelte";
import { visibleGroups } from "../lib/palette.svelte";
import { MODES, shell } from "../lib/shell.svelte";
import { gridSort } from "../lib/sort.svelte";
import { app } from "../lib/state.svelte";

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
  check(name, Object.is(actual, expected), `expected ${String(expected)}, got ${String(actual)}`);
}

function stage(width: number, height: number): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;flex-direction:column;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

function press(el: Element, key: string): KeyboardEvent {
  const e = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  el.dispatchEvent(e);
  flushSync();
  return e;
}

function text(el: Element | null): string {
  return (el?.textContent ?? "").trim();
}

/**
 * Every :focus-visible rule that paints a box-shadow on a selector, read off
 * the loaded stylesheets.
 *
 * The keyboard position in the tree and in Sessions is drawn from a tracked
 * index, deliberately, because the webview drops :focus-visible the moment the
 * mouse moves. A ring declared on top of that draws a second border and then
 * loses it on the next mouse twitch. It cannot be caught by focusing an
 * element here — no headless browser sets the heuristic — so the rule itself
 * is what gets asserted.
 */
function focusRingSelectors(on: string): string[] {
  const out: string[] = [];
  for (const sheet of [...document.styleSheets]) {
    let rules: CSSRuleList;
    try {
      rules = sheet.cssRules;
    } catch {
      continue; // a cross-origin sheet, which none of ours are
    }
    for (const rule of [...rules]) {
      if (!(rule instanceof CSSStyleRule)) continue;
      if (!rule.selectorText.includes(":focus-visible")) continue;
      if (!rule.selectorText.includes(on)) continue;
      if (rule.style.boxShadow !== "") out.push(rule.selectorText);
    }
  }
  return out;
}

const settle = () => new Promise((r) => setTimeout(r, 0));
/** Long enough for the query field's debounce to have fired and answered. */
const settleSearch = () => new Promise((r) => setTimeout(r, 200));

// ---- the stub catalogue ------------------------------------------------------

const ROOT = "/Volumes/Archive";

function node(path: string, over: Partial<CatalogTreeNode> = {}): CatalogTreeNode {
  const parts = path.split("/");
  return {
    path,
    name: parts[parts.length - 1],
    frames: 10,
    direct: 10,
    undecided: 0,
    bytes: 1024,
    hasDirs: false,
    isRoot: false,
    ...over,
  };
}

/** The shape the tree walks: one root, two months, a card under the first. */
const TREE: Record<string, CatalogTreeNode[]> = {
  [ROOT]: [
    node(`${ROOT}/2026-05`, { frames: 30, direct: 12, undecided: 7, hasDirs: true }),
    node(`${ROOT}/2026-06`, { frames: 8, direct: 8, undecided: 0 }),
  ],
  [`${ROOT}/2026-05`]: [node(`${ROOT}/2026-05/100_FUJI`, { frames: 18, direct: 18, undecided: 4 })],
};

function root(path: string): CatalogRoot {
  return {
    path,
    volume: "/Volumes/Archive",
    added: "2026-05-01T09:00:00Z",
    lastIndexed: "2026-05-01T09:30:00Z",
    frames: 38,
    rawBytes: 3000,
    jpegBytes: 900,
    bytes: 3900,
  };
}

function frame(n: number, over: Partial<CatalogFrame> = {}): CatalogFrame {
  const stem = `DSCF${String(1000 + n)}`;
  return {
    hash: `hash-${stem}`,
    dir: `${ROOT}/2026-05`,
    stem,
    kind: "paired",
    shot: `2026-05-0${(n % 3) + 1}T19:4${n % 10}:07Z`,
    hasRaw: true,
    hasJpeg: true,
    rawPath: `${ROOT}/2026-05/${stem}.RAF`,
    jpegPath: `${ROOT}/2026-05/${stem}.JPG`,
    verdict: "",
    rating: 0,
    rawBytes: 3000,
    jpegBytes: 900,
    bytes: 3900,
    ...over,
  };
}

const FRAMES: CatalogFrame[] = Array.from({ length: 9 }, (_, i) => frame(i));

const SESSIONS: CatalogSession[] = [
  {
    id: "1",
    start: "2026-06-01T14:00:00Z",
    end: "2026-06-01T15:00:00Z",
    spanMinutes: 60,
    frames: 8,
    kept: 8,
    cut: 0,
    undecided: 0,
    rawBytes: 900,
    jpegBytes: 300,
    bytes: 1200,
    source: "2026-06",
    dir: `${ROOT}/2026-06`,
    dirs: 1,
  },
  {
    id: "2",
    start: "2026-05-01T09:00:00Z",
    end: "2026-05-02T02:00:00Z",
    spanMinutes: 1020,
    frames: 20,
    kept: 12,
    cut: 3,
    undecided: 5,
    rawBytes: 2000,
    jpegBytes: 800,
    bytes: 2800,
    source: "2026-05",
    dir: `${ROOT}/2026-05`,
    dirs: 1,
  },
];

/** How many times each tree node was asked for its children. */
const fetched: Record<string, number> = {};
/** Every path the catalogue was asked to watch, in order. */
const registered: string[] = [];
/** Every path the catalogue was asked to forget. */
const removed: string[] = [];
/** What Roots() answers. Registering appends to it, as the backend would. */
let watched: CatalogRoot[] = [];

const source: CatalogSource = {
  Roots: async () => watched,
  RegisterRoot: async (dir: string) => {
    registered.push(dir);
    watched = [...watched.filter((r) => !r.path.startsWith(`${dir}/`) && r.path !== dir), root(dir)];
    return watched;
  },
  RemoveRoot: async (dir: string) => {
    removed.push(dir);
    watched = watched.filter((r) => r.path !== dir);
    return watched;
  },
  Reindex: async () => {},
  Search: async (query: string, _f: CatalogFacets, limit: number, offset: number) => {
    const hits = query === "" ? FRAMES : FRAMES.filter((f) => f.stem.includes(query));
    return {
      frames: hits.slice(offset, limit > 0 ? offset + limit : undefined),
      total: hits.length,
      offset,
      elapsed: 1,
    };
  },
  Counts: async () => ({ total: FRAMES.length, kinds: [], verdicts: [], ratings: [] }),
  Sessions: async () => ({ sessions: SESSIONS, hidden: 0, minFrames: 1 }),
  Storage: async () => ({ frames: 0, rawBytes: 0, jpegBytes: 0, bytes: 0, roots: [], volumes: [] }),
  TreeRoots: async () =>
    watched.map((r) =>
      node(r.path, { frames: 38, direct: 0, undecided: UNDECIDED_UNKNOWN, hasDirs: true, isRoot: true }),
    ),
  TreeChildren: async (dir: string) => {
    fetched[dir] = (fetched[dir] ?? 0) + 1;
    return TREE[dir] ?? [];
  },
};

/** Every open the components asked for, in order. */
const opened: { dir: string; hash?: string }[] = [];

/** A folder standing in for the one CULL has open behind the search. */
function folder(dir: string, stems: string[]): FolderDTO {
  const groups: GroupDTO[] = stems.map((stem) => ({
    dir,
    stem,
    kind: "jpeg-only",
    hasRaw: false,
    hasJpeg: true,
    rawPath: "",
    jpegPath: `${dir}/${stem}.JPG`,
    sidecars: 0,
    shot: "2026-04-01T10:00:00Z",
    warnings: [],
    verdict: "",
    mask: "",
    rating: 0,
    hash: `open-${stem}`,
    destination: "",
    verb: "",
    decision: "",
  }));
  return { dir, network: false, groups } as FolderDTO;
}

// ---- the sidebar tree --------------------------------------------------------

async function tree() {
  const host = stage(240, 420);
  mount(Tree, { target: host });
  await library.loadRoots();
  await library.loadTree();
  flushSync();

  const el = host.querySelector<HTMLElement>(".tree")!;
  const rows = () => [...host.querySelectorAll<HTMLElement>(".name")];

  eq("tree · the catalogue's roots are the top level", rows().length, 1);
  eq("tree · a root is named by its folder", text(rows()[0].querySelector(".label")), "Archive");
  eq("tree · the count badge is what is under it", text(rows()[0].querySelector(".count")), "38");
  check(
    "tree · a root with an uncounted undecided draws no badge",
    rows()[0].querySelector(".undecided") === null,
    "a folder that was not counted must not claim it is all judged",
  );
  check("tree · the tree runs its own keyboard", el.dataset.keys === "local", "data-keys must stay local");

  // Expanding
  await library.expandNode(ROOT);
  flushSync();
  eq("tree · expanding a root shows its children", rows().length, 3);
  eq("tree · a child carries its own count", text(rows()[1].querySelector(".count")), "30");
  eq("tree · a child carries what is left to judge", text(rows()[1].querySelector(".undecided")), "7");
  const rowOf = (i: number) => rows()[i].closest<HTMLElement>(".row")!;
  eq("tree · children are indented one level", rowOf(1).style.paddingLeft, "19px");
  eq("tree · children are fetched once", fetched[ROOT], 1);

  await library.expandNode(ROOT);
  flushSync();
  eq("tree · expanding an open node fetches nothing again", fetched[ROOT], 1);

  // The keyboard
  library.focusNode(0);
  rows()[0].focus();
  press(el, "ArrowDown");
  eq("tree · down moves one row", library.treeIndex, 1);
  press(el, "j");
  eq("tree · j moves down as well", library.treeIndex, 2);
  press(el, "k");
  eq("tree · k moves back up", library.treeIndex, 1);

  press(el, "ArrowRight");
  await settle();
  flushSync();
  eq("tree · right opens a closed node", library.expanded.has(`${ROOT}/2026-05`), true);
  press(el, "ArrowLeft");
  eq("tree · left closes an open one", library.expanded.has(`${ROOT}/2026-05`), false);
  press(el, "ArrowLeft");
  eq("tree · left again walks to the parent", library.treeIndex, 0);

  // One keyboard position, drawn once. The row marks it from the tracked
  // index; the button inside used to add a second, thicker ring of its own on
  // :focus-visible, nested a pixel inside the row's — two blue borders that
  // the webview then dropped one of on the next mouse movement. The row's
  // border is the indicator, and the button carries none.
  // Asserted against the stylesheet rather than by focusing something: a
  // headless browser never sets the heuristic behind :focus-visible, so a test
  // that focused a row and read its box-shadow would pass whether the rule
  // existed or not — which is what the first version of this did.
  eq("tree · no focus ring is declared on a row button", focusRingSelectors(".name").join(", "), "");
  check(
    "tree · the cursor row still marks the keyboard position",
    getComputedStyle(rowOf(library.treeIndex)).boxShadow !== "none",
    "the keyboard position has to be visible somewhere",
  );

  press(el, "End");
  eq("tree · End goes to the last row", library.treeIndex, rows().length - 1);
  press(el, "Home");
  eq("tree · Home goes back to the first", library.treeIndex, 0);

  // Opening
  opened.length = 0;
  library.focusNode(1);
  press(el, "Enter");
  eq("tree · return opens the folder", opened[0]?.dir, `${ROOT}/2026-05`);
  eq("tree · a folder names no frame", opened[0]?.hash, undefined);

  opened.length = 0;
  rows()[2].click();
  flushSync();
  eq("tree · a click opens it too", opened[0]?.dir, `${ROOT}/2026-06`);

  // Removing
  removed.length = 0;
  library.focusNode(0);
  press(el, "Delete");
  await settle();
  eq("tree · delete on a root stops watching it", removed[0], ROOT);

  library.focusNode(1);
  press(el, "Delete");
  await settle();
  eq("tree · delete on a child is not a removal", removed.length, 1);

  // Put the root back for the rest of the bench.
  await source.RegisterRoot(ROOT);
  registered.length = 0;
  await library.loadRoots();
  await library.loadTree();
  flushSync();

  host.remove();
}

// ---- joining the catalogue ---------------------------------------------------

async function registerOnOpen() {
  registered.length = 0;

  await library.registerIfNew(`${ROOT}/2026-05/100_FUJI`);
  eq("open · a folder a root already covers registers nothing", registered.length, 0);

  await library.registerIfNew("/Volumes/CardTwo/DCIM");
  eq("open · a folder outside every root joins the catalogue", registered[0], "/Volumes/CardTwo/DCIM");

  await library.registerIfNew("/Volumes/CardTwo/DCIM/");
  eq("open · a trailing separator is the same folder", registered.length, 1);

  // The segment comparison: a sibling with a shared prefix is not covered.
  await library.registerIfNew("/Volumes/CardTwoExtra");
  eq("open · a shared prefix is not the same volume", registered.length, 2);

  // Migration: what a previous version kept in local storage is handed over,
  // and only the parts of it the catalogue does not already hold.
  registered.length = 0;
  await library.adopt([`${ROOT}/2026-06`, "/Users/t/Pictures"]);
  eq("migrate · a saved root already covered is skipped", registered.includes(`${ROOT}/2026-06`), false);
  eq("migrate · a saved root the catalogue lacks is registered", registered.includes("/Users/t/Pictures"), true);
}

// ---- the grid sort -----------------------------------------------------------

async function sorting() {
  // visibleGroups is the one seam that derives app.groups, so the order it
  // returns is the order the sheet, the loupe and auto-advance all walk.
  library.closeSearch();
  const f = folder("/Users/t/Pictures/2026-04", ["IMG_0101", "IMG_0102", "IMG_0103", "IMG_0104"]);
  const [one, two, three, four] = f.groups!;
  one.shot = "2026-04-01T10:00:00Z";
  two.shot = "2026-04-01T12:00:00Z";
  three.shot = "2026-04-01T12:00:00Z"; // a burst: the same second as IMG_0102
  four.shot = ""; // the scan read no timestamp
  app.setFolder(f);

  const order = () => visibleGroups().map((g) => g.stem.slice(-4)).join(" ");

  eq("sort · the default is shot time, newest first", gridSort.label, "shot ↓");
  eq("sort · newest first, bursts in capture order, no timestamp last", order(), "0102 0103 0101 0104");

  gridSort.reverse();
  eq("sort · reversed runs oldest first, no timestamp still last", order(), "0101 0102 0103 0104");

  gridSort.setField("name");
  eq("sort · name sorts A to Z", order(), "0101 0102 0103 0104");
  gridSort.reverse();
  eq("sort · name reversed runs Z to A", order(), "0104 0103 0102 0101");

  // The scan streams in batches: a late arrival must slot into sorted
  // position on the sheet while the source list keeps arrival order.
  gridSort.setField("shot");
  const late = folder("/Users/t/Pictures/2026-04", ["IMG_0100"]).groups![0];
  late.shot = "2026-04-01T14:00:00Z";
  app.appendFrames([late]);
  eq("sort · a streamed batch slots into sorted position", order(), "0100 0102 0103 0101 0104");
  eq("sort · the source list keeps arrival order", app.allGroups[app.allGroups.length - 1].stem, "IMG_0100");

  eq("sort · the choice is remembered across launches", localStorage.getItem("culler.gridSort"), "shot:desc");
}

// ---- search over the grid ----------------------------------------------------

async function search() {
  const host = stage(760, 40);
  opened.length = 0;

  // The folder standing behind the search, as CULL would have it open.
  app.setFolder(folder("/Users/t/Pictures/2026-04", ["IMG_0001", "IMG_0002"]));
  const behind = app.allGroups.length;

  library.openSearch();
  mount(SearchBar, { target: host });
  flushSync();

  const field = host.querySelector<HTMLInputElement>(".field")!;
  eq("search · the bar opens empty", field.value, "");
  check(
    "search · the banner says how to get back",
    text(host.querySelector(".banner")).includes("esc returns"),
    "the grid is showing the index, and has to say so",
  );

  // Typing runs the query and the results reach the grid as frames.
  field.value = "DSCF100";
  field.dispatchEvent(new Event("input", { bubbles: true }));
  await settleSearch();
  flushSync();
  eq("search · the query reaches the index", library.total, 9);

  showSearchResults(library.searchOpen, library.results);
  app.groups = visibleGroups();
  flushSync();
  eq("search · the results are what the grid holds", app.groups.length, 9);
  eq("search · a result keeps its own folder", app.groups[0].dir, `${ROOT}/2026-05`);
  // The grid sort applies to results too: DSCF1008 carries the newest shot
  // time of the nine, so the default — shot, newest first — puts it on top.
  eq("search · results follow the grid sort, newest first", app.groups[0].hash, "hash-DSCF1008");
  eq("search · a result maps onto a frame", frameToGroup(FRAMES[0]).jpegPath, FRAMES[0].jpegPath);
  eq("search · the banner counts what the index answered", text(host.querySelector(".found")), "9 in the index");

  // The cursor walks the results from the field, and return opens one.
  press(field, "ArrowDown");
  eq("search · down walks the results", app.focusIndex, 1);
  press(field, "ArrowUp");
  eq("search · up walks back", app.focusIndex, 0);

  // Index 3 of the sorted grid is DSCF1007; of the raw results it would be
  // DSCF1003 — so this also proves ⏎ opens the frame the cursor is on in the
  // order the user is looking at.
  app.setFocus(3);
  press(field, "Enter");
  eq("search · return opens the result's folder", opened[0]?.dir, `${ROOT}/2026-05`);
  eq("search · and lands on the frame itself", opened[0]?.hash, "hash-DSCF1007");

  // Opening a result leaves the search, which is what puts the folder back.
  showSearchResults(library.searchOpen, library.results);
  app.groups = visibleGroups();
  flushSync();
  eq("search · opening a result closes the search", library.searchOpen, false);
  eq("search · the open folder is back on the grid", app.groups.length, behind);
  eq("search · and it is the folder, not the index", app.groups[0].hash, "open-IMG_0001");

  // Esc is the other way out, from the bar itself.
  library.openSearch();
  showSearchResults(library.searchOpen, library.results);
  flushSync();
  field.value = "DSCF";
  field.dispatchEvent(new Event("input", { bubbles: true }));
  await settleSearch();
  showSearchResults(library.searchOpen, library.results);
  app.groups = visibleGroups();
  flushSync();
  eq("search · a second search fills the grid again", app.groups.length, 9);

  const escape = press(field, "Escape");
  showSearchResults(library.searchOpen, library.results);
  app.groups = visibleGroups();
  flushSync();
  eq("search · esc is consumed by the bar", escape.defaultPrevented, true);
  eq("search · esc closes the search", library.searchOpen, false);
  eq("search · esc restores the open folder", app.groups.length, behind);
  eq("search · the query does not survive the close", library.query, "");

  host.remove();
}

// ---- the sessions group ------------------------------------------------------

async function sessions() {
  const host = stage(240, 200);
  mount(Sessions, { target: host });
  await library.loadSessions();
  flushSync();

  const el = host.querySelector<HTMLElement>(".sessions")!;
  const rows = () => [...host.querySelectorAll<HTMLElement>(".row")];

  eq("sessions · every shoot has a row", rows().length, SESSIONS.length);
  eq("sessions · the newest is first", text(rows()[0].querySelector(".label")), "2026-06-01");
  eq("sessions · a shoot is named by its day", sessionLabel(SESSIONS[0]), "2026-06-01");
  eq("sessions · one that ran past midnight names both", sessionLabel(SESSIONS[1]), "2026-05-01 → 2026-05-02");
  eq("sessions · a row carries its frame count", text(rows()[0].querySelector(".count")), "8");
  eq("sessions · and how long it ran", text(rows()[0].querySelector(".span")), "1h");
  check("sessions · the group runs its own keyboard", el.dataset.keys === "local", "data-keys must stay local");

  library.focusSession(0);
  rows()[0].focus();
  press(el, "ArrowDown");
  eq("sessions · down moves one row", library.sessionIndex, 1);
  press(el, "k");
  eq("sessions · k moves back up", library.sessionIndex, 0);

  // Opening a session is a search over the time it ran, not an open of the
  // folder its first frame happens to sit in: a shoot that spans two cards is
  // one shoot, and opening a folder would silently drop the rest of it.
  library.closeSearch();
  opened.length = 0;
  press(el, "Enter");
  await settleSearch();
  check("sessions · return opens the shoot as a search", library.searchOpen);
  eq("sessions · scoped to when it started", library.facets.from, SESSIONS[0].start);
  // The backend's To is exclusive, so the window reaches a second past the
  // last frame or the last frame of the shoot falls outside its own session.
  eq(
    "sessions · and to a second past when it ended",
    library.facets.to,
    new Date(new Date(SESSIONS[0].end).getTime() + 1000).toISOString(),
  );
  // And it opens no folder at all: the results are the shoot, wherever its
  // frames are filed.
  eq("sessions · opening a shoot opens no folder", opened.length, 0);

  library.closeSearch();
  rows()[1].click();
  flushSync();
  await settleSearch();
  eq("sessions · a click scopes to that row's shoot", library.facets.from, SESSIONS[1].start);
  library.closeSearch();

  // The size floor is the backend's, and what it left out comes back with the
  // list: a filtered list that looked complete is how a shoot goes missing
  // without anybody noticing.
  // Same rule as the tree: .active follows the keyboard, so a :focus-visible
  // ring on top of it only ever drew a second border and then dropped it.
  eq("sessions · no focus ring is declared on a row", focusRingSelectors(".row").join(", "), "");

  eq("sessions · a list with nothing filtered hides nothing", library.sessionsHidden, 0);
  eq("sessions · and reports the floor it was drawn at", library.sessionFloor, 1);

  const wholeList = source.Sessions;
  source.Sessions = async () => ({ sessions: [SESSIONS[0]], hidden: 207, minFrames: 5 });
  await library.loadSessions();
  flushSync();
  eq("sessions · a floor drops the shoots under it", rows().length, 1);
  eq("sessions · and the store carries what was hidden", library.sessionsHidden, 207);
  eq("sessions · with the floor that hid them", library.sessionFloor, 5);
  source.Sessions = wholeList;
  await library.loadSessions();
  flushSync();

  host.remove();
}

// ---- what became of LIBRARY --------------------------------------------------

function modes() {
  eq("modes · there are three", MODES.length, 3);
  const ids = MODES.map((m) => m.id as string);
  check("modes · LIBRARY has left the mode bar", !ids.includes("library"), `still there: ${ids.join(" · ")}`);
  check("modes · EXIF has left the mode bar too", !ids.includes("exif"), `still there: ${ids.join(" · ")}`);
  eq("modes · the last slot is IMPORT", MODES[2].id, "import");
  eq("modes · and it is labelled so", MODES[2].label, "IMPORT");
  eq("modes · IMPORT has three sub-layouts", MODES[2].layouts.join(" · "), "review · route · verify");

  shell.setModeByIndex(2);
  eq("modes · ⌃3 lands on IMPORT", shell.mode, "import");
  eq("modes · which is where the ghost is drawn", shell.spec.label, "IMPORT");
  eq("modes · the switch is remembered across launches", localStorage.getItem("culler.mode"), "import");
  shell.setLayout(2);
  eq(
    "modes · the sub-layout record is remembered too, one index per mode",
    localStorage.getItem("culler.layouts"),
    `${shell.layouts.cull},${shell.layouts.map},2`,
  );
  shell.setLayout(0);
  shell.setModeByIndex(0);
  eq("modes · ⌃1 comes back to CULL", shell.mode, "cull");
  eq("modes · and that is what a relaunch would find", localStorage.getItem("culler.mode"), "cull");

  app.sidebar = false;
  eq("sidebar · hiding it is remembered across launches", localStorage.getItem("culler.sidebar"), "closed");
  app.sidebar = true;
  eq("sidebar · as is bringing it back", localStorage.getItem("culler.sidebar"), "open");

  const actions = new Set(ACTIONS.map((a) => a.id));
  check("actions · search is an action of its own", actions.has("search"), "the / binding needs a registry row");
  check("actions · storage is reachable from the palette", actions.has("storage"), "its IMPORT home is not built");
  check("actions · the mode action is IMPORT's", actions.has("mode-import"), "mode-library must be gone");

  library.storageOpen = false;
  ACTIONS.find((a) => a.id === "storage")!.run();
  eq("actions · running it opens the storage view", library.storageOpen, true);
  library.storageOpen = false;

  ACTIONS.find((a) => a.id === "search")!.run();
  eq("actions · running search opens the bar", library.searchOpen, true);
  ACTIONS.find((a) => a.id === "search")!.run();
  eq("actions · running it again closes it", library.searchOpen, false);
}

// ---- run ---------------------------------------------------------------------

async function run() {
  connectCatalog(source);
  onOpenFolder((dir: string, hash?: string) => {
    opened.push({ dir, hash });
    library.closeSearch();
  });
  watched = [root(ROOT)];

  await tree();
  await registerOnOpen();
  await sorting();
  await search();
  await sessions();
  modes();

  const failed = results.filter((r) => !r.pass);
  document.getElementById("results")!.textContent = JSON.stringify(
    { total: results.length, failed: failed.length, results },
    null,
    1,
  );
  document.title =
    failed.length === 0 ? `PASS ${results.length}/${results.length}` : `FAIL ${failed.length}`;
}

void run().catch((err: unknown) => {
  document.getElementById("results")!.textContent = `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
