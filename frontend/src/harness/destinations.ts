// A headless bench for per-frame destinations: the move palette, the chip on
// a tile, and the way the apply summary groups by where frames are going.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because routing is a keyboard flow with a
// backend on the end of it — press a digit, a whole selection is bound for a
// folder — and the keyboard half has to be checkable without a card, a
// library, or a person to look at it. Both services are answered through the
// ports the palette store already goes through, so the assertions here are
// about the real component and the real store.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9349
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1400,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9349/src/harness/destinations.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import ApplyBar from "../components/ApplyBar.svelte";
import MovePalette from "../components/MovePalette.svelte";
import Tile from "../components/Tile.svelte";
import type { DestinationDTO, GroupDTO, LibraryFolderDTO, PlanDTO } from "../lib/bindings";
import { expandTemplate, formatDate, resolve } from "../lib/destination";
import { destinationPort, destinations, leafOf, libraryFolderPort, palette } from "../lib/palette.svelte";
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
  const same = JSON.stringify(actual) === JSON.stringify(expected);
  check(name, same, `expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`);
}

function text(el: Element | null | undefined): string {
  return (el?.textContent ?? "").replace(/\s+/g, " ").trim();
}

function stage(width: number, height: number): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

/* ---- a fake destinations service ---- */

const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";
/** Where a library-relative destination lands, as the backend reports it. */
const LIBRARY = "/library";

/**
 * The service the palette store talks to, answered in memory. It is a real
 * round trip: the store reloads through it after every change, so the digits
 * the palette shows are the ones this list hands back rather than anything the
 * component worked out for itself.
 */
class FakeDestinations {
  rows: DestinationDTO[] = [];
  /** Every call made, so the bench can assert what the palette asked for. */
  calls: string[] = [];

  seed(paths: string[]) {
    // Newest last in, so the list comes back newest first.
    this.rows = paths.map((path, i) => ({
      path,
      label: "",
      lastUsedAt: `2026-05-0${i + 1}T09:00:00Z`,
      useCount: 1,
      pinned: false,
      slot: 0,
      digit: 0,
    }));
    this.order();
  }

  /** order sorts and re-derives digits the way the backend does. */
  order() {
    this.rows.sort((a, b) => {
      if (a.pinned !== b.pinned) return a.pinned ? -1 : 1;
      return b.lastUsedAt.localeCompare(a.lastUsedAt);
    });
    const taken = new Set<number>();
    for (const r of this.rows) {
      r.digit = 0;
      if (r.slot >= 1 && r.slot <= 9 && !taken.has(r.slot)) {
        taken.add(r.slot);
        r.digit = r.slot;
      }
    }
    let next = 1;
    for (const r of this.rows) {
      if (r.digit !== 0) continue;
      while (next <= 9 && taken.has(next)) next++;
      if (next > 9) break;
      taken.add(next);
      r.digit = next;
    }
  }

  list(): Promise<DestinationDTO[]> {
    return Promise.resolve(this.rows.map((r) => ({ ...r })));
  }

  use(path: string, label: string): Promise<void> {
    this.calls.push(`use ${path}`);
    const found = this.rows.find((r) => r.path === path);
    if (found) {
      found.useCount++;
      found.lastUsedAt = "2026-06-01T09:00:00Z";
      if (label !== "") found.label = label;
    } else {
      this.rows.push({
        path,
        label,
        lastUsedAt: "2026-06-01T09:00:00Z",
        useCount: 1,
        pinned: false,
        slot: 0,
        digit: 0,
      });
    }
    this.order();
    return Promise.resolve();
  }

  pin(path: string, pinned: boolean): Promise<void> {
    this.calls.push(`pin ${path} ${String(pinned)}`);
    const found = this.rows.find((r) => r.path === path);
    if (found) found.pinned = pinned;
    this.order();
    return Promise.resolve();
  }

  bind(path: string, slot: number): Promise<void> {
    this.calls.push(`bind ${path} ${slot}`);
    for (const r of this.rows) if (r.slot === slot) r.slot = 0;
    const found = this.rows.find((r) => r.path === path);
    if (found) found.slot = slot;
    this.order();
    return Promise.resolve();
  }

  forget(path: string): Promise<void> {
    this.calls.push(`forget ${path}`);
    this.rows = this.rows.filter((r) => r.path !== path);
    this.order();
    return Promise.resolve();
  }
}

const service = new FakeDestinations();
destinationPort.list = () => service.list();
destinationPort.use = (p, l) => service.use(p, l);
destinationPort.pin = (p, v) => service.pin(p, v);
destinationPort.bind = (p, s) => service.bind(p, s);
destinationPort.forget = (p) => service.forget(p);

/* ---- a fake catalogue ---- */

/** folder builds one catalogued folder the way LibraryIndexService reports it. */
function folder(path: string, frames: number): LibraryFolderDTO {
  const rel = path.startsWith(`${LIBRARY}/`) ? path.slice(LIBRARY.length + 1) : "";
  return { path, rel, name: path.slice(path.lastIndexOf("/") + 1), frames };
}

/** Every folder the library holds, busiest first as the catalogue returns it. */
const catalogue: LibraryFolderDTO[] = [
  folder(`${LIBRARY}/2026/portraits`, 412),
  folder(`${LIBRARY}/2026/keepers`, 180),
  folder(`${LIBRARY}/2026`, 640),
  folder("/archive/2019/portfolio", 55),
];

let folderCalls = 0;
libraryFolderPort.list = (limit: number) => {
  folderCalls++;
  return Promise.resolve({ root: LIBRARY, folders: catalogue.slice(0, limit) });
};

/* ---- frames ---- */

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
    sidecars: 1,
    shot: "2026-05-17T09:30:00Z",
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

function loadFrames() {
  const groups = [frame(1), frame(2), frame(3), frame(4)];
  app.allGroups = groups;
  app.groups = groups;
  app.focusIndex = 0;
  app.clearSelection();
  app.scanning = null;
  app.plan = null;
}

/* ---- driving the palette ---- */

let host: HTMLElement | null = null;
let component: Record<string, unknown> | null = null;

function openPalette(kind: "move" | "copy" = "move"): HTMLElement {
  closePalette();
  palette.show(kind);
  host = stage(1000, 640);
  component = mount(MovePalette, { target: host }) as Record<string, unknown>;
  flushSync();
  return host;
}

function closePalette() {
  if (component !== null) void unmount(component);
  host?.remove();
  component = null;
  host = null;
}

/** The palette's text field, which is a real input and holds its own caret. */
function field(): HTMLInputElement | null {
  return host?.querySelector<HTMLInputElement>("[data-palette-field]") ?? null;
}

/**
 * press sends a key the way the browser would: at the field when there is one,
 * so the routing sees the same target the real keyboard produces. It hands the
 * event back, because whether the palette let the press through to the field is
 * exactly what the cursor-key assertions are about.
 */
function press(key: string, over: KeyboardEventInit = {}): KeyboardEvent {
  const target = field() ?? host?.querySelector('[data-keys="local"]') ?? document.body;
  const ev = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...over });
  target.dispatchEvent(ev);
  flushSync();
  return ev;
}

/**
 * type presses each key and then does what the browser's default action would
 * have done with it — a synthetic keydown inserts nothing on its own. A press
 * the palette consumed inserts nothing here either, which is the point.
 */
function type(s: string) {
  for (const ch of s) {
    const ev = press(ch);
    const el = field();
    if (el === null || ev.defaultPrevented) continue;
    const at = el.selectionStart ?? el.value.length;
    const to = el.selectionEnd ?? at;
    el.value = el.value.slice(0, at) + ch + el.value.slice(to);
    el.setSelectionRange(at + 1, at + 1);
    el.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
  }
}

/** erase is Backspace, default action and all. */
function erase(n = 1) {
  for (let i = 0; i < n; i++) {
    const ev = press("Backspace");
    const el = field();
    if (el === null || ev.defaultPrevented) continue;
    const at = el.selectionStart ?? el.value.length;
    if (at === 0) continue;
    el.value = el.value.slice(0, at - 1) + el.value.slice(el.selectionEnd ?? at);
    el.setSelectionRange(at - 1, at - 1);
    el.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
  }
}

/** settle lets the palette's awaits (load, use, pin) finish. */
async function settle() {
  for (let i = 0; i < 6; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  flushSync();
}

function rowPaths(): (string | null)[] {
  return [...(host?.querySelectorAll(".row") ?? [])].map((r) => r.getAttribute("data-destination"));
}

function rowKinds(): (string | null)[] {
  return [...(host?.querySelectorAll(".row") ?? [])].map((r) => r.getAttribute("data-kind"));
}

async function run() {
  /* ---- the pure rules the preview is drawn from ---- */

  eq("template · a date token takes a Go layout", formatDate(new Date(2026, 4, 17, 14, 5, 9), "2006-01-02"), "2026-05-17");
  eq("template · and the pieces a folder name is written from", formatDate(new Date(2026, 4, 17, 14, 5, 9), "Jan 2 15:04"), "May 17 14:05");
  eq(
    "template · a token the frame cannot answer takes its own segment",
    expandTemplate("/library/{camera}/{stem}", {
      shot: null,
      stem: "DSCF0001",
      ext: "raf",
      camera: "",
      lens: "",
    }).path,
    "/library/DSCF0001",
  );
  eq(
    "template · a separator inside a value is not a new folder level",
    expandTemplate("{camera}", { shot: null, stem: "", ext: "", camera: "Nikon/Z8", lens: "" }).path,
    "Nikon-Z8",
  );
  eq("resolve · a relative destination hangs off the library root", resolve("2026/portraits", LIBRARY), "/library/2026/portraits");
  eq("resolve · an absolute one is taken at its word", resolve("/elsewhere/keep", LIBRARY), "/elsewhere/keep");

  /* ---- the list the palette shows ---- */

  // The remembered list deliberately does not hold the portraits folder: it is
  // the catalogue's, and finding it is what the suggestions are for.
  service.seed([`${LIBRARY}/2026/rejects`, `${LIBRARY}/2025/weddings`, `${LIBRARY}/2026/keepers`]);
  loadFrames();
  openPalette();
  await settle();

  eq("palette · rows come from the service, most recent first", rowPaths().slice(0, 3), [
    `${LIBRARY}/2026/keepers`,
    `${LIBRARY}/2025/weddings`,
    `${LIBRARY}/2026/rejects`,
  ]);
  const digits = [...host!.querySelectorAll('.row[data-kind="remembered"]')].map((r) => text(r.querySelector(".cap.slot")));
  eq("palette · the first nine carry their digit", digits, ["1", "2", "3"]);
  check("palette · the catalogue was asked for its folders", folderCalls > 0, `${folderCalls} calls`);

  // A folder that is also a remembered destination is one place, however the
  // two lists write it: the relative form resolves to the same folder.
  const keepers = rowPaths().filter((p) => resolve(p ?? "", LIBRARY) === `${LIBRARY}/2026/keepers`);
  eq("palette · a suggestion that is already a destination is not offered twice", keepers.length, 1);
  check(
    "palette · the folders the library holds are offered under the remembered ones",
    rowKinds().includes("folder"),
    rowKinds().join(","),
  );

  /* ---- fuzzy search against the catalogue ---- */

  // "port" is a word, not a path, so it is a search: the folder the library
  // already has leads, and creating a folder called "port" is the deliberate
  // choice at the bottom.
  type("port");
  await settle();
  // A folder outside the library root matches on the same terms and is offered
  // as the absolute path it is; the best match still leads.
  eq("search · a typed word finds the library folder", rowPaths(), [
    "2026/portraits",
    "/archive/2019/portfolio",
    "port",
  ]);
  eq("search · the matches lead and the create row follows them", rowKinds(), ["folder", "folder", "create"]);
  eq(
    "search · the suggestion says what the library has filed there",
    text(host!.querySelector('.row[data-kind="folder"] .rmeta')),
    "412 frames filed here",
  );
  eq(
    "search · the preview resolves it against the library root",
    text(host!.querySelector(".dest .folder")),
    `${LIBRARY}/2026/portraits`,
  );
  check(
    "search · and says which kind of destination it is",
    text(host!.querySelector(".dest .shape")).startsWith("library-relative"),
    text(host!.querySelector(".dest .shape")),
  );

  /* ---- the cursor keys belong to the text, not to the list ---- */

  const before = palette.index;
  const left = press("ArrowLeft");
  eq("keys · ← is the field's, so the row cursor stays put", palette.index, before);
  eq("keys · and the palette does not swallow it", left.defaultPrevented, false);
  const wordLeft = press("ArrowLeft", { altKey: true });
  eq("keys · ⌥← is the field's too", wordLeft.defaultPrevented, false);
  const lineStart = press("Home");
  eq("keys · Home goes to the start of the text, not the top of the list", lineStart.defaultPrevented, false);
  eq("keys · which leaves the row cursor alone", palette.index, before);
  const down = press("ArrowDown");
  eq("keys · ↓ walks the rows", palette.index, before + 1);
  eq("keys · and is the palette's, so the caret does not move", down.defaultPrevented, true);
  press("ArrowUp");
  eq("keys · ↑ walks back", palette.index, before);

  // Editing the middle of a typed path is the thing the old routing made
  // impossible: the caret goes back, a character goes in, and the text either
  // side of it survives.
  const el = field()!;
  el.setSelectionRange(0, 0);
  type("s");
  await settle();
  eq("keys · a character typed at the caret lands there", palette.query, "sport");
  erase();
  await settle();
  eq("keys · and backspace takes it back", palette.query, "port");

  /* ---- ⏎ takes the suggestion ---- */

  press("Enter");
  await settle();
  eq("assign · the focused frame is routed to the library folder", app.groups[0].destination, "2026/portraits");
  eq("assign · a destination implies a keep", app.groups[0].verdict, "keep");
  eq("assign · focus advances, as a verdict does", app.focusIndex, 1);
  eq("assign · the palette closes", palette.open, false);
  check(
    "assign · the destination is remembered",
    service.calls.includes("use 2026/portraits"),
    service.calls.join(", "),
  );
  eq("assign · nothing else was routed", app.groups[1].destination, "");
  closePalette();

  /* ---- a path that is not a folder anybody has is still a destination ---- */

  app.setFocus(0);
  openPalette();
  await settle();
  type("/library/2026/wildlife");
  await settle();
  eq("create · a typed path leads, ahead of anything it happens to match", rowKinds()[0], "create");
  eq("create · and is exactly what was typed", rowPaths()[0], "/library/2026/wildlife");
  eq("create · offered as a create rather than as a match", text(host!.querySelector(".row .rmeta")), "create and use");
  eq(
    "create · the preview takes an absolute path at its word",
    text(host!.querySelector(".dest .shape")),
    "absolute path",
  );
  press("Enter");
  await settle();
  eq("create · ⏎ routes exactly there", app.groups[0].destination, "/library/2026/wildlife");
  closePalette();

  /* ---- templates ---- */

  app.setFocus(0);
  openPalette();
  await settle();
  type("{date:2006}/portraits");
  await settle();
  check("template · the token hint is shown", host!.querySelector(".tokens") !== null);
  eq("template · a template is a path being written, so it leads", rowKinds()[0], "create");
  eq("template · and says it makes a folder per frame", text(host!.querySelector(".row .rmeta")), "create per frame");
  eq(
    "template · the preview expands it against the focused frame",
    text(host!.querySelector(".dest .folder")),
    `${LIBRARY}/2026/portraits`,
  );
  press("Escape");
  await settle();
  closePalette();

  /* ---- the verb the palette was opened with ---- */

  app.setFocus(0);
  app.toggleSelect();
  app.setFocus(1);
  app.toggleSelect();
  openPalette("copy");
  await settle();
  eq("verb · the title states the verb and the count", text(host!.querySelector(".title")), "copy 2 frames to…");
  eq("verb · and so does the confirm", text(host!.querySelector(".primary")), "copy ⏎");
  closePalette();
  app.clearSelection();

  openPalette("move");
  await settle();
  eq("verb · a move palette says move", text(host!.querySelector(".title")), "move 1 frame to…");
  eq("verb · on the confirm too", text(host!.querySelector(".primary")), "move ⏎");
  closePalette();

  /* ---- and the verb is recorded, not just displayed ---- */

  // The key that opened the palette is the decision: c records a copy and m
  // records a move, per frame, so one folder can hold frames arriving both
  // ways and the apply does what was asked on each.
  loadFrames();
  app.setFocus(0);
  openPalette("copy");
  await settle();
  press("Enter");
  await settle();
  eq("verb · a copy palette records a copy", app.groups[0].verb, "copy");
  closePalette();

  app.setFocus(1);
  openPalette("move");
  await settle();
  press("Enter");
  await settle();
  eq("verb · a move palette records a move", app.groups[1].verb, "move");
  eq("verb · the frame beside it keeps its own", app.groups[0].verb, "copy");
  closePalette();

  // Clearing the routing clears the verb with it: a frame going nowhere is
  // neither being moved nor copied.
  app.setFocus(1);
  openPalette("move");
  await settle();
  press("0");
  await settle();
  eq("verb · clearing the routing clears the verb", app.groups[1].verb, "");
  eq("verb · and the destination", app.groups[1].destination, "");
  closePalette();

  /* ---- a digit routes straight there ---- */

  loadFrames();
  app.setFocus(1);
  openPalette();
  await settle();
  const onTwo = destinations.forDigit(2);
  press("2");
  await settle();
  eq("slot · a digit routes the focused frame", app.groups[1].destination, onTwo?.path);
  eq("slot · and advances focus", app.focusIndex, 2);
  eq("slot · the palette closes", palette.open, false);
  closePalette();

  /* ---- a digit routes a whole selection ---- */

  app.setFocus(2);
  app.toggleSelect();
  app.setFocus(3);
  app.toggleSelect();
  openPalette();
  await settle();
  const onOne = destinations.forDigit(1);
  press("1");
  await settle();
  eq("slot · the whole selection goes", [app.groups[2].destination, app.groups[3].destination], [
    onOne?.path,
    onOne?.path,
  ]);
  eq("slot · a selection does not move the focus", app.focusIndex, 3);
  closePalette();

  /* ---- 0 clears ---- */

  app.clearSelection();
  app.setFocus(2);
  openPalette();
  await settle();
  press("0");
  await settle();
  eq("clear · 0 takes the routing off", app.groups[2].destination, "");
  eq("clear · but leaves the verdict", app.groups[2].verdict, "keep");
  eq("clear · the palette closes", palette.open, false);
  closePalette();

  /* ---- a typed digit is part of a path, not a slot ---- */

  app.setFocus(0);
  openPalette();
  await settle();
  type("/library/2026");
  await settle();
  eq("slot · digits inside a typed path stay in it", palette.query, "/library/2026");
  eq("slot · and route nothing on their own", app.groups[0].destination, "");
  press("Escape");
  await settle();
  closePalette();

  /* ---- the chip on a tile ---- */

  const tileHost = stage(260, 220);
  mount(Tile, {
    target: tileHost,
    props: {
      group: frame(9, { destination: "/library/2026/{date:2006-01-02}", verdict: "keep" }),
      index: 0,
      focused: true,
      selected: false,
      width: 240,
      height: 190,
      x: 0,
      y: 0,
      onfocus: () => {},
      onopen: () => {},
    },
  });
  flushSync();
  const chip = tileHost.querySelector(".dest");
  eq("tile · the chip shows the leaf", text(chip), "→ {date:2006-01-02}");
  eq(
    "tile · and the verb with the whole path on hover",
    chip?.getAttribute("title"),
    "copy to /library/2026/{date:2006-01-02}",
  );

  // A frame routed to move reads differently from one routed to copy: it is
  // the only routing that empties the place the photograph is now.
  const moveHost = stage(260, 220);
  mount(Tile, {
    target: moveHost,
    props: {
      group: frame(10, { destination: "/library/keepers", verdict: "keep", verb: "move" }),
      index: 0,
      focused: true,
      selected: false,
      width: 240,
      height: 190,
      x: 0,
      y: 0,
      onfocus: () => {},
      onopen: () => {},
    },
  });
  flushSync();
  const moveChip = moveHost.querySelector(".dest");
  eq("tile · a move chip carries its own glyph", text(moveChip), "⇥ keepers");
  eq("tile · and says so on hover", moveChip?.getAttribute("title"), "move to /library/keepers");
  check("tile · and is styled apart from a copy", moveChip?.classList.contains("moving") === true);

  eq("leafOf · a trailing separator is not a level", leafOf("/library/keepers/"), "keepers");

  const bare = stage(260, 220);
  mount(Tile, {
    target: bare,
    props: {
      group: frame(8),
      index: 0,
      focused: false,
      selected: false,
      width: 240,
      height: 190,
      x: 0,
      y: 0,
      onfocus: () => {},
      onopen: () => {},
    },
  });
  flushSync();
  check("tile · an unrouted frame has no chip", bare.querySelector(".dest") === null);

  /* ---- the apply summary ---- */

  const plan: PlanDTO = {
    description: "Copy to /library/2026/keepers (2 frames), Drop both (1 frame)",
    actions: [
      { verb: "copy", src: `${DIR}/DSCF1201.RAF`, dst: "/library/2026/keepers/DSCF1201.RAF" },
      { verb: "trash", src: `${DIR}/DSCF1204.RAF`, dst: "" },
    ],
    counts: { drop_all: 1 },
    destinations: [
      { path: "/library/2026/keepers", verb: "copy", frames: 2, files: 6, bytes: 72_004_000 },
      { path: "{date:2006}/portraits", verb: "move", frames: 1, files: 3, bytes: 36_002_000 },
    ],
    totalBytes: 108_006_000,
  };
  loadFrames();
  app.groups[0].destination = "/library/2026/keepers";
  app.groups[0].verdict = "keep";
  app.groups[1].destination = "{date:2006}/portraits";
  app.groups[1].verdict = "keep";
  app.groups[3].verdict = "cut";
  app.plan = plan;

  const barHost = stage(1000, 560);
  mount(ApplyBar, { target: barHost });
  flushSync();

  eq("apply bar · the pending strip counts the routed frames", text(barHost.querySelector(".chip.routed")), "→ routed · 2");
  const routeRows = [...barHost.querySelectorAll(".routes .row")].map((r) => [
    text(r.querySelector("dt")),
    text(r.querySelector("dd")),
  ]);
  eq("apply summary · one row per destination, verb and all", routeRows, [
    ["copy → /library/2026/keepers", "2 frames · 6 files · 69 MB"],
    ["move → {date:2006}/portraits", "1 frames · 3 files · 34 MB"],
  ]);
  check(
    "apply summary · the verdict counts survive beside them",
    [...barHost.querySelectorAll("dl:not(.routes) .row")].length > 0,
  );

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
