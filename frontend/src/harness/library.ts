// A headless bench for LIBRARY's tree and its one-keystroke open.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the tree's keyboard and the open
// contract have to be verifiable without a catalogue, a backend or a person to
// look at it — in particular that ⏎ on a search result carries the frame's
// hash across, and ⏎ on a tree node or a session row does not.
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
import { ownsKeys } from "../lib/keymap";
import LibraryTree from "../components/library/LibraryTree.svelte";
import SearchResults from "../components/library/SearchResults.svelte";
import SessionsTable from "../components/library/SessionsTable.svelte";
import {
  connectCatalog,
  library,
  onOpenFolder,
  UNDECIDED_UNKNOWN,
  type CatalogFacets,
  type CatalogFrame,
  type CatalogSession,
  type CatalogSource,
  type CatalogTreeNode,
} from "../lib/library.svelte";

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

const settle = () => new Promise((r) => setTimeout(r, 0));

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
    start: "2026-05-01T09:00:00Z",
    end: "2026-05-01T12:00:00Z",
    spanMinutes: 180,
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
  {
    id: "2",
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
];

/** How many times each tree node was asked for its children. */
const fetched: Record<string, number> = {};

const source: CatalogSource = {
  Roots: async () => [],
  RegisterRoot: async () => [],
  RemoveRoot: async () => [],
  Reindex: async () => {},
  Search: async (_q: string, _f: CatalogFacets, limit: number, offset: number) => ({
    frames: FRAMES.slice(offset, limit > 0 ? offset + limit : undefined),
    total: FRAMES.length,
    offset,
    elapsed: 1,
  }),
  Counts: async () => ({ total: FRAMES.length, kinds: [], verdicts: [], ratings: [] }),
  Sessions: async () => SESSIONS,
  Storage: async () => ({ frames: 0, rawBytes: 0, jpegBytes: 0, bytes: 0, roots: [], volumes: [] }),
  TreeRoots: async () => [
    node(ROOT, { frames: 38, direct: 0, undecided: UNDECIDED_UNKNOWN, hasDirs: true, isRoot: true }),
  ],
  TreeChildren: async (dir: string) => {
    fetched[dir] = (fetched[dir] ?? 0) + 1;
    return TREE[dir] ?? [];
  },
};

/** Every open the components asked for, in order. */
const opened: { dir: string; hash?: string }[] = [];

// ---- the tree ----------------------------------------------------------------

async function tree() {
  const host = stage(240, 420);
  mount(LibraryTree, { target: host });
  await library.loadTree();
  flushSync();

  const el = host.querySelector<HTMLElement>(".tree")!;
  const rows = () => [...host.querySelectorAll<HTMLElement>(".name")];

  eq("tree · the roots are the top level", rows().length, 1);
  eq("tree · a root is named by its folder", text(rows()[0].querySelector(".label")), "Archive");
  eq("tree · the count badge is what is under it", text(rows()[0].querySelector(".count")), "38");
  check(
    "tree · an uncounted folder draws no undecided badge",
    rows()[0].querySelector(".undecided") === null,
    "a badge was drawn for a count that is unknown",
  );
  check(
    "tree · the pane keeps its keys to itself",
    el.dataset.keys === "local",
    `data-keys is ${String(el.dataset.keys)}`,
  );
  eq("tree · the container is a tree", el.getAttribute("role"), "tree");
  eq("tree · rows are tree items", host.querySelector(".row")?.getAttribute("role"), "treeitem");

  // Right expands, and does it through the backend exactly once.
  press(el, "ArrowRight");
  await settle();
  flushSync();
  eq("tree · right expands the root", rows().length, 3);
  eq("tree · and asked the catalogue for its children", fetched[ROOT], 1);
  eq("tree · children are indented one level", rows()[1].closest(".row")?.getAttribute("aria-level"), "2");
  eq("tree · a child's count badge", text(rows()[1].querySelector(".count")), "30");
  eq("tree · a child's undecided badge", text(rows()[1].querySelector(".undecided")), "7");
  check(
    "tree · a folder with nothing left to judge has no badge",
    rows()[2].querySelector(".undecided") === null,
    "a zero was drawn as a badge",
  );

  // Down walks the flattened list; right on an expanded node steps into it.
  press(el, "ArrowDown");
  eq("tree · down moves to the first child", library.treeIndex, 1);
  press(el, "ArrowRight");
  await settle();
  flushSync();
  eq("tree · right expands the child too", rows().length, 4);
  eq("tree · the grandchild is at level 3", rows()[2].closest(".row")?.getAttribute("aria-level"), "3");

  press(el, "ArrowRight");
  eq("tree · right again steps into the expanded node", library.treeIndex, 2);
  press(el, "ArrowLeft");
  eq("tree · left from a leaf goes to its parent", library.treeIndex, 1);
  press(el, "ArrowLeft");
  flushSync();
  eq("tree · left again collapses it", rows().length, 3);

  press(el, "End");
  eq("tree · End goes to the last row", library.treeIndex, 2);
  press(el, "Home");
  eq("tree · Home goes back to the first", library.treeIndex, 0);

  // Re-expanding does not go back to the backend: the children are held.
  press(el, "ArrowDown");
  press(el, "ArrowRight");
  await settle();
  flushSync();
  eq("tree · a second expansion is served from what was already fetched", fetched[`${ROOT}/2026-05`], 1);

  // The contract: ⏎ opens the folder, and names no frame.
  opened.length = 0;
  press(el, "Enter");
  eq("tree · ⏎ opens one folder", opened.length, 1);
  eq("tree · ⏎ opens the focused row's folder", opened[0]?.dir, `${ROOT}/2026-05`);
  eq("tree · a folder open names no frame", opened[0]?.hash, undefined);

  // A double click is the same thing for the mouse.
  opened.length = 0;
  rows()[0].dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
  flushSync();
  eq("tree · a double click opens it too", opened[0]?.dir, ROOT);

  // The focused row has to be visible as such whether or not the pane holds
  // the keyboard, or ⏎ acts on a row the user cannot see.
  library.focusNode(1);
  flushSync();
  check(
    "tree · the focused row is marked",
    rows()[1].className.includes("focused"),
    rows()[1].className,
  );
  eq("tree · and is the only one in the tab order", rows()[1].tabIndex, 0);
  eq("tree · the others are not", rows()[0].tabIndex, -1);

  // The mode keymap listens on the window and binds these arrows to the grid.
  // What keeps it out is the guard it consults, so that is what is asserted —
  // and the tree stops the browser scrolling the pane on top of it.
  check(
    "tree · the global keymap stays out of the tree",
    ownsKeys(rows()[1]),
    "the keymap would have acted on a key meant for the tree",
  );
  check(
    "tree · and out of the twisty too",
    ownsKeys(host.querySelector<HTMLElement>(".twisty")!),
    "a key on the twisty would reach the keymap",
  );
  check(
    "tree · but not out of the page around it",
    !ownsKeys(document.body),
    "the keymap is being kept out of the whole page",
  );
  check("tree · the tree consumes the arrows it acts on", press(el, "ArrowDown").defaultPrevented);

  host.remove();
}

// ---- the search results ------------------------------------------------------

async function searchResults() {
  const host = stage(760, 520);
  mount(SearchResults, { target: host });
  await library.search();
  flushSync();

  const el = host.querySelector<HTMLElement>(".results")!;
  eq("results · the grid keeps its keys to itself", el.dataset.keys, "local");
  eq("results · every result is loaded", library.results.length, FRAMES.length);
  check(
    "results · the global keymap stays out of the grid",
    ownsKeys(host.querySelector<HTMLElement>(".tile")!),
    "the keymap would have acted on a key meant for the grid",
  );

  library.focus(0);
  flushSync();
  const tile = (i: number) => host.querySelector<HTMLElement>(`[data-row="${i}"]`);
  check("results · the focused tile is marked", tile(0)?.className.includes("focused") === true, "");
  eq("results · and is the only one in the tab order", tile(0)?.tabIndex, 0);

  press(el, "ArrowRight");
  eq("results · right steps one tile", library.focusIndex, 1);
  press(el, "ArrowLeft");
  eq("results · left steps back", library.focusIndex, 0);

  // A row is however many tiles the width fits, so the assertion is that down
  // moves by a whole row rather than by a tile.
  press(el, "ArrowDown");
  check(
    "results · down steps a whole row",
    library.focusIndex > 1,
    `moved to ${library.focusIndex}, which is a tile rather than a row`,
  );
  press(el, "End");
  eq("results · End goes to the last result", library.focusIndex, FRAMES.length - 1);
  press(el, "Home");
  eq("results · Home goes back to the first", library.focusIndex, 0);

  // The contract: ⏎ opens the folder AND names the frame, so cull lands on the
  // photograph the user was looking at rather than at the top of the folder.
  opened.length = 0;
  library.focus(4);
  flushSync();
  press(el, "Enter");
  eq("results · ⏎ opens one folder", opened.length, 1);
  eq("results · ⏎ opens the focused frame's folder", opened[0]?.dir, FRAMES[4].dir);
  eq("results · and names the frame itself", opened[0]?.hash, FRAMES[4].hash);

  opened.length = 0;
  tile(4)?.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
  flushSync();
  eq("results · a double click opens the same frame", opened[0]?.hash, FRAMES[4].hash);

  host.remove();
}

// ---- the sessions table ------------------------------------------------------

async function sessions() {
  const host = stage(900, 300);
  mount(SessionsTable, { target: host });
  await library.loadSessions();
  flushSync();

  const rows = [...host.querySelectorAll<HTMLElement>(".row")];
  eq("sessions · one row per shoot", rows.length, SESSIONS.length);
  eq(
    "sessions · the table keeps its keys to itself",
    host.querySelector<HTMLElement>(".body")?.dataset.keys,
    "local",
  );
  check(
    "sessions · the global keymap stays out of the table",
    ownsKeys(rows[0]),
    "the keymap would have acted on a key meant for the table",
  );
  eq("sessions · the selected row is in the tab order", rows[0].tabIndex, 0);
  eq("sessions · the others are not", rows[1].tabIndex, -1);

  press(rows[0], "ArrowDown");
  eq("sessions · down moves to the next shoot", library.sessionIndex, 1);
  eq("sessions · and selects it", library.selectedSession, SESSIONS[1].id);
  press(rows[0], "ArrowUp");
  eq("sessions · up moves back", library.sessionIndex, 0);

  // The contract: a session row opens its folder and names no frame.
  opened.length = 0;
  press(rows[0], "Enter");
  eq("sessions · ⏎ opens one folder", opened.length, 1);
  eq("sessions · ⏎ opens the session's folder", opened[0]?.dir, SESSIONS[0].dir);
  eq("sessions · a session names no frame", opened[0]?.hash, undefined);

  opened.length = 0;
  rows[1].dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
  flushSync();
  eq("sessions · a double click opens that row's folder", opened[0]?.dir, SESSIONS[1].dir);

  host.remove();
}

// ---- run ---------------------------------------------------------------------

async function run() {
  connectCatalog(source);
  onOpenFolder((dir: string, hash?: string) => opened.push({ dir, hash }));

  await tree();
  await searchResults();
  await sessions();

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
