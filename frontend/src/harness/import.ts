// A headless bench for IMPORT mode's three panes.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because every state worth checking here
// needs hardware — a card in the reader, a library that has already seen it, a
// copy halfway through — and none of those can be produced on demand against a
// real backend. The store takes its backend by injection for exactly this
// reason, so the screens are driven by a stub that answers instantly.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9348
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1500,1000 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9348/src/harness/import.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount } from "svelte";
import ImportCentre from "../components/import/ImportCentre.svelte";
import ImportLeft from "../components/import/ImportLeft.svelte";
import ImportRight from "../components/import/ImportRight.svelte";
import {
  connectImport,
  importState,
  onOpenFolder,
  onReveal,
  PHASE_BACKUP,
  PHASE_COPY,
  PHASE_SCAN,
  type ImportBatch,
  type ImportCard,
  type ImportCardSummary,
  type ImportPlan,
  type ImportSource,
  type ImportSpace,
} from "../lib/import.svelte";

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

function texts(root: ParentNode, sel: string): string[] {
  return [...root.querySelectorAll(sel)].map((e) => text(e));
}

function token(name: string): string {
  const hex = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  return m === null
    ? hex
    : `rgb(${parseInt(m[1], 16)}, ${parseInt(m[2], 16)}, ${parseInt(m[3], 16)})`;
}

function stage(width: number, height: number, label: string): HTMLDivElement {
  const wrap = document.createElement("div");
  wrap.style.cssText = "margin:0 0 10px";
  const caption = document.createElement("div");
  caption.textContent = label;
  caption.style.cssText =
    "font:10px/1.6 var(--font-mono);letter-spacing:.1em;text-transform:uppercase;color:var(--text-faint);padding:4px 2px";
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window);border:1px solid var(--border-window)`;
  wrap.append(caption, el);
  document.getElementById("mounts")!.appendChild(wrap);
  return el;
}

if (new URLSearchParams(location.search).get("theme") === "light") {
  document.documentElement.setAttribute("data-theme", "light");
}

// ---- the stubbed backend ------------------------------------------------

const CARD = "/Volumes/FUJI_SD";
const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";
const LIBRARY = "/Volumes/Photos";

const CARDS: ImportCard[] = [
  {
    path: CARD,
    name: "FUJI_SD",
    total: 128_000_000_000,
    free: 41_000_000_000,
    network: false,
    hasDcim: true,
    dir: DIR,
    folders: 3,
    frames: 2400,
    estimated: true,
    error: "",
  },
  {
    path: "/Volumes/NIKON",
    name: "NIKON",
    total: 64_000_000_000,
    free: 60_000_000_000,
    network: false,
    hasDcim: true,
    dir: "/Volumes/NIKON/DCIM/100NCD90",
    folders: 1,
    frames: 480,
    estimated: false,
    error: "",
  },
];

const SUMMARY: ImportCardSummary = {
  path: CARD,
  name: "FUJI_SD",
  network: false,
  hasDcim: true,
  dirs: [
    {
      path: DIR,
      name: "103_FUJI",
      frames: 812,
      files: 1624,
      bytes: 29_400_000_000,
      first: "2026-05-01T09:12:00Z",
      last: "2026-05-01T18:40:00Z",
    },
    {
      path: "/Volumes/FUJI_SD/DCIM/104_FUJI",
      name: "104_FUJI",
      frames: 796,
      files: 1592,
      bytes: 28_100_000_000,
      first: "2026-05-02T08:02:00Z",
      last: "2026-05-02T17:31:00Z",
    },
    {
      path: "/Volumes/FUJI_SD/DCIM/105_FUJI",
      name: "105_FUJI",
      frames: 802,
      files: 1604,
      bytes: 28_900_000_000,
      first: "2026-05-03T07:44:00Z",
      last: "2026-05-03T19:02:00Z",
    },
  ],
  frames: 2410,
  files: 4820,
  bytes: 86_400_000_000,
  sessions: 3,
  first: "2026-05-01T09:12:00Z",
  last: "2026-05-03T19:02:00Z",
  imported: 40,
  sampled: 200,
};

const PLAN: ImportPlan = {
  dir: DIR,
  frames: 48,
  routed: 31,
  cut: 9,
  unrouted: 8,
  undecided: 5,
  routes: [
    {
      destination: "2026/portraits",
      path: `${LIBRARY}/2026/portraits`,
      frames: 24,
      files: 48,
      bytes: 900_000_000,
    },
    {
      destination: "/Users/t/Desktop/deliver",
      path: "/Users/t/Desktop/deliver",
      frames: 7,
      files: 14,
      bytes: 260_000_000,
    },
  ],
  files: 62,
  bytes: 1_160_000_000,
  verb: "copy",
  libraryRoot: LIBRARY,
  network: false,
  space: [],
};

const SPACE: ImportSpace[] = [
  {
    destination: "2026/portraits",
    path: `${LIBRARY}/2026/portraits`,
    frames: 24,
    bytes: 900_000_000,
    volume: LIBRARY,
    volumeName: "Photos",
    free: 12_000_000_000,
    total: 500_000_000_000,
    network: false,
    removable: false,
    fits: true,
  },
  {
    destination: "/Users/t/Desktop/deliver",
    path: "/Users/t/Desktop/deliver",
    frames: 7,
    bytes: 260_000_000,
    volume: "/",
    volumeName: "Macintosh HD",
    free: 100_000_000,
    total: 1_000_000_000_000,
    network: false,
    removable: false,
    fits: false,
  },
];

const BATCH: ImportBatch = {
  id: "1-1",
  time: "2026-08-03T11:04:00Z",
  description: "Copy to 2026/portraits (24 frames), Copy to /Users/t/Desktop/deliver (7 frames)",
  actions: [{ verb: "copy", src: `${DIR}/DSCF0001.RAF`, dst: "", outcome: "ok", err: "" }],
};

const calls: string[] = [];

const stub: ImportSource = {
  DetectCards: async () => {
    calls.push("DetectCards");
    return CARDS;
  },
  CardSummary: async (path) => {
    calls.push(`CardSummary ${path}`);
    return SUMMARY;
  },
  ImportPlan: async (dir) => {
    calls.push(`ImportPlan ${dir}`);
    return { ...PLAN, dir, space: SPACE };
  },
  Execute: async (dir, backupDest) => {
    calls.push(`Execute ${dir} ${backupDest}`);
    return BATCH;
  },
};

connectImport(stub);

const opened: string[] = [];
onOpenFolder((dir) => opened.push(dir));
const revealed: string[] = [];
onReveal((path) => revealed.push(path));

/** settle lets the stub's promises land and the effects that follow them run. */
async function settle() {
  for (let i = 0; i < 6; i++) await Promise.resolve();
  flushSync();
}

// ---- the left pane ------------------------------------------------------

async function left() {
  const host = stage(240, 560, "left · cards");
  mount(ImportLeft, { target: host });
  flushSync();
  await settle();

  eq("left · a card row per removable volume", host.querySelectorAll(".card").length, 2);
  eq("left · named by the volume", texts(host, ".card .name"), ["FUJI_SD", "NIKON"]);
  eq("left · an extrapolated count says so", texts(host, ".card .frames"), [
    "≈2,400 frames",
    "480 frames",
  ]);
  check(
    "left · a dcim card is tagged",
    texts(host, ".card .pill").every((t) => t === "dcim"),
    texts(host, ".card .pill").join(","),
  );
  eq(
    "left · the first card is selected on arrival",
    host.querySelector(".card")?.classList.contains("selected"),
    true,
  );
  eq(
    "left · which is what reads the card",
    calls.filter((c) => c.startsWith("CardSummary")),
    [`CardSummary ${CARD}`],
  );
  eq(
    "left · and the already-imported share is drawn from the sample",
    text(host.querySelector(".card .note")),
    "20% already in the library",
  );

  const list = host.querySelector<HTMLElement>(".cards")!;
  eq("left · the list runs its own keyboard", list.dataset.keys, "local");

  // j and k walk the rows without selecting: reading a card is work, and it
  // happens on ⏎ rather than on every keypress.
  list.dispatchEvent(new KeyboardEvent("keydown", { key: "j", bubbles: true }));
  flushSync();
  eq("left · j moves the cursor", importState.cardIndex, 1);
  eq("left · without reading the card under it", importState.selectedPath, CARD);
  list.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await settle();
  eq("left · ⏎ selects it", importState.selectedPath, "/Volumes/NIKON");

  // Back to the card the rest of the bench is about.
  await importState.selectCard(0);
  await settle();

  const dot = getComputedStyle(host.querySelector<HTMLElement>(".card .dot")!);
  eq("left · a card is amber, the colour the app uses for removable media", dot.backgroundColor, token("--amber"));

  // The rail is narrow and fixed. A long note or a long volume name has to be
  // cut, never widen the column — anything past the right edge is invisible.
  const pane = host.querySelector<HTMLElement>(".pane")!;
  eq("left · the pane fits the rail it is given", pane.scrollWidth <= pane.clientWidth, true);
  const free = [...host.querySelectorAll<HTMLElement>(".card .free")];
  // Binary units throughout, as the rest of the app prints them: 41 GB off a
  // card's own label is 38.2 GB of the gibibytes a file manager counts in.
  eq("left · free space is drawn", free.map((e) => text(e)), ["38.2 GB free", "55.9 GB free"]);
  check(
    "left · and lands inside the rail rather than past its edge",
    free.every((e) => e.getBoundingClientRect().right <= host.getBoundingClientRect().right + 0.5),
    free.map((e) => `${e.getBoundingClientRect().right} > ${host.getBoundingClientRect().right}`).join(", "),
  );

  return host;
}

// ---- review -------------------------------------------------------------

async function review() {
  const host = stage(760, 560, "centre · review (⌥1)");
  const seen: string[] = [];
  mount(ImportCentre, { target: host, props: { layout: 0, onreview: (d: string) => seen.push(d) } });
  flushSync();
  await settle();

  eq("review · headed by the card", text(host.querySelector("h2")), "FUJI_SD");
  eq("review · four figures", host.querySelectorAll(".figure").length, 4);
  eq("review · what is on the card", texts(host, ".figure .value"), [
    "2,410",
    "80.5 GB",
    "3",
    "20%",
  ]);
  eq("review · labelled", texts(host, ".figure .key"), [
    "frames",
    "on the card",
    "shoots",
    "already imported",
  ]);
  eq(
    "review · and the sample is not passed off as a count",
    text(host.querySelector(".caveat")),
    "measured on 200 frames spread across the card, not all 2,410",
  );

  eq(
    "review · one affordance, on ⏎",
    text(host.querySelector(".hero-key")),
    "⏎",
  );
  eq(
    "review · naming the folder it opens",
    text(host.querySelector(".hero-name")),
    "review in cull",
  );
  eq("review · which is the card's first folder", text(host.querySelector(".hero-note")), "103_FUJI");
  host.querySelector<HTMLElement>(".hero")!.click();
  flushSync();
  eq("review · and pressing it hands that folder over", seen, [DIR]);

  eq("review · every folder on the card is listed", host.querySelectorAll(".row:not(.head)").length, 3);
  eq("review · by name", texts(host, ".row:not(.head) .c-name"), [
    "103_FUJI",
    "104_FUJI",
    "105_FUJI",
  ]);
  eq(
    "review · with the span of the whole card in the header",
    text(host.querySelector(".label .hint")),
    "2026-05-01 → 2026-05-03",
  );

  return host;
}

// ---- route --------------------------------------------------------------

async function route() {
  const host = stage(760, 560, "centre · route (⌥2)");
  const seen: string[] = [];
  mount(ImportCentre, { target: host, props: { layout: 1, onreview: (d: string) => seen.push(d) } });
  flushSync();
  await settle();

  eq("route · headed by the folder", text(host.querySelector("h2")), "103_FUJI");
  eq("route · the split is drawn three ways", host.querySelectorAll(".split .part").length, 3);
  eq("route · and labelled", texts(host, ".legend .tag"), [
    "31 routed",
    "9 cut",
    "8 staying on the card",
  ]);

  const bar = host.querySelector<HTMLElement>(".part.routed")!;
  const track = host.querySelector<HTMLElement>(".split")!;
  const share = bar.getBoundingClientRect().width / track.getBoundingClientRect().width;
  check("route · the routed part is 31 of 48 wide", Math.abs(share - 31 / 48) < 0.01, String(share));

  eq(
    "route · the unrouted frames are a warning",
    text(host.querySelector(".warning-name")),
    "8 frames are routed nowhere",
  );
  eq(
    "route · which says how many nobody looked at, and where to go",
    text(host.querySelector(".warning-note")),
    "5 of them nobody has looked at — open the folder in cull and filter to undecided",
  );
  eq(
    "route · in amber, not red: this is an oversight, not a failure",
    getComputedStyle(host.querySelector<HTMLElement>(".warning-name")!).color,
    token("--amber"),
  );
  host.querySelector<HTMLElement>(".warning")!.click();
  flushSync();
  eq("route · and it jumps to the folder in cull", seen, [DIR]);

  eq("route · one row per destination", host.querySelectorAll(".row:not(.head)").length, 2);
  eq("route · named as the user recorded them", texts(host, ".row:not(.head) .c-dest"), [
    "2026/portraits",
    "/Users/t/Desktop/deliver",
  ]);
  eq("route · with their share of what is routed", texts(host, ".row:not(.head) .c-share"), [
    "77%",
    "23%",
  ]);
  eq(
    "route · and the library root the relative ones hang off",
    text(host.querySelector(".label .hint")),
    LIBRARY,
  );

  return host;
}

// ---- verify -------------------------------------------------------------

async function verify() {
  const host = stage(760, 700, "centre · verify (⌥3)");
  mount(ImportCentre, { target: host, props: { layout: 2 } });
  flushSync();
  await settle();

  eq(
    "verify · headed by what is about to happen",
    text(host.querySelector("h2")),
    "31 frames into the library",
  );
  eq(
    "verify · and what it costs",
    text(host.querySelector(".path")),
    "62 files · 1.1 GB",
  );

  const field = host.querySelector<HTMLInputElement>(".field")!;
  eq("verify · the backup field is off until the toggle is on", field.disabled, true);
  eq("verify · and it runs its own keyboard", field.dataset.keys, "local");
  const toggle = host.querySelector<HTMLInputElement>(".toggle input")!;
  toggle.checked = true;
  toggle.dispatchEvent(new Event("change", { bubbles: true }));
  flushSync();
  eq("verify · turning it on opens the field", field.disabled, false);
  eq("verify · and the header says the frames are written twice", text(host.querySelector(".path")), "62 files · 1.1 GB · written twice");

  eq("verify · a landing row per destination", host.querySelectorAll(".row").length, 2);
  eq("verify · each naming its volume", texts(host, ".row .c-vol"), ["Photos", "Macintosh HD"]);
  eq(
    "verify · one of which cannot hold it",
    text(host.querySelector(".warn")),
    "1 destination has less room than this import needs",
  );

  eq(
    "verify · the button says what it will do",
    text(host.querySelector(".run-name")),
    "copy 31 frames into the library",
  );

  // A backup with nowhere to go is refused before anything moves.
  await importState.execute();
  await settle();
  eq(
    "verify · a second copy with no folder is refused",
    text(host.querySelector(".error")),
    "the second copy has nowhere to go — name a backup folder",
  );
  eq("verify · and nothing was executed", calls.filter((c) => c.startsWith("Execute")), []);

  // The bar is indeterminate while the card is being read, because there is no
  // honest fraction until the plan exists.
  importState.running = true;
  importState.error = null;
  importState.applyProgress({
    dir: DIR,
    phase: PHASE_SCAN,
    files: 0,
    total: 48,
    bytes: 0,
    complete: false,
    error: "",
  });
  flushSync();
  eq("verify · reading the card is indeterminate", host.querySelectorAll(".fill.sweep").length, 1);
  eq(
    "verify · and named",
    text(host.querySelector(".progress .label span")),
    "reading the card",
  );

  importState.applyProgress({
    dir: DIR,
    phase: PHASE_COPY,
    files: 24,
    total: 124,
    bytes: 450_000_000,
    complete: false,
    error: "",
  });
  flushSync();
  eq("verify · copying is a real bar", host.querySelectorAll(".fill.sweep").length, 0);
  eq("verify · which reports itself", text(host.querySelector(".progress .hint")), "24 / 124");
  const fill = host.querySelector<HTMLElement>(".fill")!;
  const bar = host.querySelector<HTMLElement>(".track")!;
  const ratio = fill.getBoundingClientRect().width / bar.getBoundingClientRect().width;
  check("verify · at 24 of 124", Math.abs(ratio - 24 / 124) < 0.01, String(ratio));

  importState.applyProgress({
    dir: DIR,
    phase: PHASE_BACKUP,
    files: 100,
    total: 124,
    bytes: 900_000_000,
    complete: false,
    error: "",
  });
  flushSync();
  eq(
    "verify · the second copy is its own phase",
    text(host.querySelector(".progress .label span")),
    "writing the second copy",
  );

  // The done state.
  importState.running = false;
  importState.batch = BATCH;
  importState.progress = null;
  flushSync();
  eq("verify · the batch describes itself", text(host.querySelector(".done .summary")), BATCH.description);
  eq(
    "verify · in the keep green, because nothing failed",
    getComputedStyle(host.querySelector<HTMLElement>(".done .summary")!).color,
    token("--keep-text"),
  );
  eq("verify · one open button per destination", host.querySelectorAll(".open").length, 2);
  host.querySelector<HTMLElement>(".open")!.click();
  flushSync();
  eq("verify · which reveals the folder it landed in", revealed, [`${LIBRARY}/2026/portraits`]);

  // A partial import must not read as a success.
  importState.batch = {
    ...BATCH,
    actions: [
      ...BATCH.actions,
      { verb: "copy", src: `${DIR}/DSCF0002.RAF`, dst: "", outcome: "error", err: "input/output error" },
    ],
  };
  flushSync();
  eq(
    "verify · a partial import says which files did not land",
    text(host.querySelector(".done .warn")),
    "1 of 2 files did not land — the frames they belong to kept their routing, so the import can be run again",
  );
  eq(
    "verify · and stops calling itself done",
    text(host.querySelector(".done .label span")),
    "partly done",
  );

  importState.batch = BATCH;
  flushSync();
  return host;
}

// ---- the right pane -----------------------------------------------------

async function right() {
  const host = stage(330, 560, "right · landing");
  mount(ImportRight, { target: host });
  flushSync();
  await settle();

  eq("right · one card per volume, not per destination", host.querySelectorAll(".card").length, 2);
  eq("right · named by the volume", texts(host, ".card .name"), ["Photos", "Macintosh HD"]);
  eq("right · biggest first", texts(host, ".card .total"), ["858.3 MB", "248.0 MB"]);
  eq(
    "right · the volume with no room is marked",
    [...host.querySelectorAll(".card")].map((c) => c.classList.contains("tight")),
    [false, true],
  );
  eq(
    "right · and says why",
    texts(host, ".card.tight .warn"),
    ["not enough room for what is routed here"],
  );
  eq(
    "right · every destination on the volume is listed under it",
    texts(host, ".card .c-name"),
    ["2026/portraits", "/Users/t/Desktop/deliver"],
  );
  eq(
    "right · with a capacity bar apiece",
    host.querySelectorAll(".card .track").length,
    2,
  );

  return host;
}

// ---- run ----------------------------------------------------------------

/**
 * Anything Svelte complains about is a failure in its own right — an effect
 * that reads and writes the same state takes the effect tree down with it and
 * leaves a screen that renders once and then quietly stops updating.
 */
const complaints: string[] = [];
window.addEventListener("error", (e) => complaints.push(`error: ${e.message}`));
window.addEventListener("unhandledrejection", (e) => {
  complaints.push(`rejection: ${String(e.reason)}`);
});

async function run() {
  await left();
  await review();
  await route();
  await verify();
  await right();

  eq("nothing complained", complaints, []);

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
  document.getElementById("results")!.textContent =
    `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
