// A headless bench for the right pane, the table's EXIF columns, and the cache
// that feeds both.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because three things here are only true if
// you count — that a screenful of rows makes one read and not one per row, that
// a section stays collapsed across a remount, and that a value row actually
// puts its text on the clipboard — and none of them can be seen by looking at
// a screenshot.
//
// The fake reader stands in for ExifService.Read. It counts the paths it was
// given, so a request that fans out per row shows up as a number rather than as
// a slow folder someone notices six months later.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9349
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1600,1000 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9349/src/harness/inspector.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import Inspector from "../components/Inspector.svelte";
import TableView from "../components/TableView.svelte";
import type { GroupDTO } from "../lib/bindings";
import type { FrameExifDTO } from "../lib/exif.svelte";
import { exifCache } from "../lib/exifcache.svelte";
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

const settle = (ms = 0) => new Promise((r) => setTimeout(r, ms));

function text(el: Element | null): string {
  return (el?.textContent ?? "").replace(/\s+/g, " ").trim();
}

function texts(root: ParentNode, sel: string): string[] {
  return [...root.querySelectorAll(sel)].map((e) => text(e));
}

function click(el: Element) {
  (el as HTMLElement).click();
  flushSync();
}

const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";

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
    // Deliberately different from the EXIF below: the scan's timestamp and the
    // camera's are different facts, and both panes have to prefer the camera's.
    shot: "2026-07-18T18:42:07Z",
    warnings: null,
    verdict: "",
    mask: "rj",
    rating: 0,
    hash: `hash-${stem}`,
    destination: "",
    decision: "",
    ...over,
  };
}

/** Enough frames that a table window is a fraction of the folder. */
const groups: GroupDTO[] = [
  frame(0, { verdict: "keep", mask: "rj", rating: 3 }),
  ...Array.from({ length: 199 }, (_, i) => frame(i + 1)),
];

function exifFor(path: string): FrameExifDTO {
  return {
    path,
    stem: path.split("/").pop() ?? path,
    kind: "jpeg",
    sidecar: "",
    error: "",
    fields: [
      {
        tag: "DateTimeOriginal",
        label: "Capture time",
        section: "Capture",
        value: "2026-07-18T19:11:33+00:00",
        present: true,
        writable: true,
      },
      { tag: "Make", label: "Camera make", section: "Camera", value: "FUJIFILM", present: true, writable: false },
      { tag: "Model", label: "Camera model", section: "Camera", value: "X-T5", present: true, writable: false },
      {
        tag: "LensModel",
        label: "Lens",
        section: "Camera",
        value: "XF35mmF1.4 R",
        present: true,
        writable: false,
      },
      { tag: "ExposureTime", label: "Shutter", section: "Exposure", value: "1/250", present: true, writable: false },
      { tag: "FNumber", label: "Aperture", section: "Exposure", value: "ƒ2.8", present: true, writable: false },
      { tag: "ISO", label: "ISO", section: "Exposure", value: "640", present: true, writable: false },
      {
        tag: "FocalLength",
        label: "Focal length",
        section: "Exposure",
        value: "35 mm",
        present: true,
        writable: false,
      },
      // Carried but not present: an absent tag is not an empty one, and the
      // pane must not draw a row for it.
      { tag: "Copyright", label: "Copyright", section: "Rights", value: "", present: false, writable: true },
    ],
  };
}

/** Every path the fake reader was handed, in the order the batches arrived. */
let asked: string[][] = [];

exifCache.useReader((paths) => {
  asked.push([...paths]);
  const out: Record<string, FrameExifDTO> = {};
  for (const p of paths) out[p] = exifFor(p);
  return Promise.resolve(out);
});

/** What the fake clipboard was last given, and how many times. */
let copied: string[] = [];

Object.defineProperty(navigator, "clipboard", {
  configurable: true,
  value: {
    writeText: (s: string) => {
      copied.push(s);
      return Promise.resolve();
    },
  },
});

function flat(): string[] {
  return asked.flat();
}

function stage(width: number, height: number): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

// ---- the cache ---------------------------------------------------------------

async function cacheBatches() {
  exifCache.clear();
  asked = [];

  // Twelve frames, asked for one at a time the way twelve rows rendering would.
  for (const g of groups.slice(0, 12)) exifCache.request([g]);
  eq("cache · nothing goes out in the same tick", asked.length, 0);

  await settle(80);
  eq("cache · a tick's worth of requests is one call", asked.length, 1);
  eq("cache · every path asked for exactly once", flat().length, 12);
  eq("cache · deduped", new Set(flat()).size, 12);
  eq("cache · the JPEG is what gets read", flat()[0], groups[0].jpegPath);
  eq("cache · results are keyed by frame identity", exifCache.get("hash-DSCF1200")?.stem, "DSCF1200.JPG");

  // Re-asking is what a re-render does, and it must cost nothing.
  const calls = exifCache.calls;
  for (const g of groups.slice(0, 12)) exifCache.request([g]);
  exifCache.request(groups.slice(0, 12));
  await settle(80);
  eq("cache · a re-render makes no second read", exifCache.calls, calls);
  eq("cache · and asks for no further paths", flat().length, 12);

  // A RAW-only frame has no JPEG to prefer.
  exifCache.request([frame(90, { kind: "raw-only", hasJpeg: false, jpegPath: "" })]);
  await settle(80);
  check("cache · a RAW-only frame is read from the RAW", flat().includes(`${DIR}/DSCF1290.RAF`), flat().join(" "));

  // A frame with neither half is never sent to the backend at all.
  const before = flat().length;
  exifCache.request([frame(91, { hasRaw: false, hasJpeg: false, rawPath: "", jpegPath: "" })]);
  await settle(80);
  eq("cache · a frame with no file is not read", flat().length, before);
}

// ---- the table ---------------------------------------------------------------

async function tableColumns() {
  exifCache.clear();
  asked = [];

  const host = stage(1440, 700);
  const view = mount(TableView, {
    target: host,
    props: { groups, focusIndex: 0, onFocus: () => {}, preview: true },
  });
  flushSync();

  const cell = (sel: string) => text(host.querySelector(`.row ${sel}`));
  eq("table · shutter is a dash before the read lands", cell(".c-shutter"), "—");
  eq("table · lens is a dash before the read lands", cell(".c-lens"), "—");
  eq("table · the scan's clock is what shows meanwhile", cell(".c-shot"), "18:42:07");

  await settle(120);
  flushSync();

  eq("table · shutter arrives", cell(".c-shutter"), "1/250");
  eq("table · aperture arrives", cell(".c-aperture"), "ƒ2.8");
  eq("table · iso arrives", cell(".c-iso"), "640");
  eq("table · lens arrives", cell(".c-lens"), "XF35mmF1.4 R");
  eq("table · the shot column prefers the camera's own capture time", cell(".c-shot"), "19:11:33");
  check(
    "table · a filled cell is no longer drawn as absent",
    !host.querySelector(".row .c-iso")!.className.includes("absent"),
    host.querySelector(".row .c-iso")!.className,
  );

  // The point of the whole exercise: a folder of 200 frames is not 200 reads.
  const rows = host.querySelectorAll(".row").length;
  check(
    "table · only the rendered window is read",
    flat().length === rows && rows < groups.length,
    `${rows} rows rendered of ${groups.length}, ${flat().length} paths read`,
  );
  eq("table · no path is read twice", new Set(flat()).size, flat().length);
  check(
    "table · a screenful is a handful of calls, not one per row",
    exifCache.calls <= 3,
    `${exifCache.calls} calls for ${flat().length} paths`,
  );

  // The table's sort is a preference of its own, apart from the grid's: a
  // header click writes it down, a fresh mount starts from it, and a stored
  // value the table cannot sort by falls back to the default.
  localStorage.removeItem("culler.tableSort");
  host.querySelector<HTMLButtonElement>(".head .c-stem .sort")!.click();
  flushSync();
  eq("table · a header click is remembered across launches", localStorage.getItem("culler.tableSort"), "stem:asc");
  unmount(view);

  const again = mount(TableView, {
    target: host,
    props: { groups, focusIndex: 0, onFocus: () => {}, preview: true },
  });
  flushSync();
  eq(
    "table · a fresh mount starts from the remembered sort",
    host.querySelector(".head .c-stem")?.getAttribute("aria-sort"),
    "ascending",
  );
  unmount(again);

  localStorage.setItem("culler.tableSort", "banana:sideways");
  const corrupt = mount(TableView, {
    target: host,
    props: { groups, focusIndex: 0, onFocus: () => {}, preview: true },
  });
  flushSync();
  eq(
    "table · a corrupt stored sort falls back to the default",
    host.querySelector(".head .c-shot")?.getAttribute("aria-sort"),
    "descending",
  );
  unmount(corrupt);
  localStorage.removeItem("culler.tableSort");
  host.remove();
}

// ---- the inspector -----------------------------------------------------------

async function inspector() {
  exifCache.clear();
  asked = [];
  copied = [];
  localStorage.clear();

  app.groups = groups;
  app.focusIndex = 0;

  const host = stage(296, 900);
  let view = mount(Inspector, { target: host, props: {} });
  flushSync();
  await settle(120);
  flushSync();

  eq("inspector · sections are named as the spec has them", texts(host, ".section-label"), [
    "Histogram",
    "Metadata",
    "Files",
  ]);
  eq("inspector · one read for the focused frame", flat().length, 1);

  const rows = () => [...host.querySelectorAll<HTMLElement>(".row")];
  const rowFor = (k: string) => rows().find((r) => text(r.querySelector(".rkey")) === k) ?? null;
  const valueFor = (k: string) => text(rowFor(k)?.querySelector(".rvalue") ?? null);

  eq("inspector · camera make", valueFor("make"), "FUJIFILM");
  eq("inspector · camera model", valueFor("model"), "X-T5");
  eq("inspector · lens", valueFor("lens"), "XF35mmF1.4 R");
  eq("inspector · shutter", valueFor("shutter"), "1/250");
  eq("inspector · aperture", valueFor("ƒ"), "ƒ2.8");
  eq("inspector · iso", valueFor("iso"), "640");
  eq("inspector · focal length", valueFor("focal"), "35 mm");
  // The clock is rendered in the viewer's own zone, so the hour is whatever
  // the machine running this is; the minute and second are the camera's.
  check(
    "inspector · capture time comes from the camera, not the scan",
    valueFor("shot").endsWith(":11:33"),
    valueFor("shot"),
  );
  check("inspector · an absent tag gets no row", rowFor("copyright") === null, "a copyright row was drawn");

  // ---- click to copy -------------------------------------------------------

  const values = [...host.querySelectorAll<HTMLElement>(".rvalue")];
  check(
    "inspector · every value is a real button",
    values.length > 0 && values.every((v) => v.tagName === "BUTTON"),
    values.map((v) => v.tagName).join(","),
  );
  check(
    "inspector · every value carries its full text as a title",
    values.every((v) => (v.title ?? "").includes("Click to copy")),
    values.find((v) => !(v.title ?? "").includes("Click to copy"))?.title ?? "",
  );
  check(
    "inspector · every value is truncated rather than wrapped",
    values.every((v) => getComputedStyle(v).textOverflow === "ellipsis"),
    getComputedStyle(values[0]).textOverflow,
  );

  click(rowFor("iso")!.querySelector(".rvalue")!);
  await settle();
  eq("inspector · clicking a value copies it", copied.at(-1), "640");
  eq("inspector · and says so", app.toast?.message, "copied");

  const folder = rowFor("folder")!.querySelector<HTMLElement>(".rvalue")!;
  click(folder);
  await settle();
  eq("inspector · a path copies the path, not the mark it is drawn with", copied.at(-1), DIR);
  check("inspector · the path row is a button too", folder.tagName === "BUTTON", folder.tagName);

  // A row with nothing in it must not offer to copy nothing.
  app.groups = [frame(8, { kind: "raw-only", hasJpeg: false, jpegPath: "" })];
  app.focusIndex = 0;
  flushSync();
  const empty = rowFor("jpeg")!.querySelector<HTMLElement>(".rvalue")!;
  eq("inspector · an absent value reads as a dash", text(empty), "—");
  check("inspector · and is not a button", empty.tagName === "SPAN", empty.tagName);
  app.groups = groups;
  app.focusIndex = 0;
  flushSync();
  await settle(40);
  flushSync();

  // ---- collapsing ----------------------------------------------------------

  const header = (title: string) =>
    [...host.querySelectorAll<HTMLElement>(".section-label")].find((h) => text(h).includes(title))!;

  const metadata = header("Metadata");
  check("inspector · a section header is a button", metadata.tagName === "BUTTON", metadata.tagName);
  eq("inspector · an open section says so", metadata.getAttribute("aria-expanded"), "true");
  const open = rows().length;
  const inMetadata = rowsIn(host, "metadata");
  check("inspector · the metadata section is the one carrying the camera rows", inMetadata >= 8, String(inMetadata));

  click(metadata);
  eq("inspector · collapsing hides its rows", rows().length, open - inMetadata);
  eq("inspector · and says so", header("Metadata").getAttribute("aria-expanded"), "false");
  eq(
    "inspector · the collapsed state is written down",
    localStorage.getItem("culler.inspector.section.metadata"),
    "collapsed",
  );
  check("inspector · the other sections are untouched", rows().length > 0, "everything collapsed");

  // A remount is the next launch, as far as the pane is concerned.
  unmount(view);
  view = mount(Inspector, { target: host, props: {} });
  flushSync();
  eq("inspector · a collapsed section stays collapsed across a remount", header("Metadata").getAttribute("aria-expanded"), "false");
  eq("inspector · and an open one stays open", header("Files").getAttribute("aria-expanded"), "true");

  click(header("Metadata"));
  eq("inspector · re-opening is written down too", localStorage.getItem("culler.inspector.section.metadata"), "open");
  eq("inspector · and the rows come back", header("Metadata").getAttribute("aria-expanded"), "true");

  // ---- warnings ------------------------------------------------------------

  app.groups = [frame(7, { warnings: ["sidecar could not be read"], sidecars: 2 })];
  app.focusIndex = 0;
  flushSync();
  check(
    "inspector · a warned frame gets a Warnings section",
    texts(host, ".section-label").includes("Warnings"),
    texts(host, ".section-label").join(","),
  );
  click(host.querySelector(".warning")!);
  await settle();
  eq("inspector · a warning copies too", copied.at(-1), "sidecar could not be read");

  unmount(view);
  host.remove();
  app.groups = [];
}

/** How many rows the metadata section is currently drawing. */
function rowsIn(host: HTMLElement, id: string): number {
  const section = [...host.querySelectorAll(".block")].find(
    (b) => b.querySelector(`.section-label[data-section="${id}"]`) !== null,
  );
  return section?.querySelectorAll(".row").length ?? 0;
}

// ---- run ---------------------------------------------------------------------

async function run() {
  app.cutRemoves = "both";

  await cacheBatches();
  await tableColumns();
  await inspector();

  const failed = results.filter((r) => !r.pass);
  document.getElementById("results")!.textContent = JSON.stringify(
    { total: results.length, failed: failed.length, failures: failed },
    null,
    1,
  );
  document.title = failed.length === 0 ? `PASS ${results.length}/${results.length}` : `FAIL ${failed.length}`;
}

void run().catch((err: unknown) => {
  document.getElementById("results")!.textContent = `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
