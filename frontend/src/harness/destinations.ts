// A headless bench for per-frame destinations: the move palette, the chip on
// a tile, and the way the apply summary groups by where frames are going.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because routing is a keyboard flow with a
// backend on the end of it — press a digit, a whole selection is bound for a
// folder — and the keyboard half has to be checkable without a card, a
// library, or a person to look at it. The destination service is answered
// through the port the palette store already goes through, so the assertions
// here are about the real component and the real store.
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

import { flushSync, mount } from "svelte";
import ApplyBar from "../components/ApplyBar.svelte";
import MovePalette from "../components/MovePalette.svelte";
import Tile from "../components/Tile.svelte";
import type { DestinationDTO, GroupDTO, PlanDTO } from "../lib/bindings";
import { destinationPort, destinations, leafOf, palette } from "../lib/palette.svelte";
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

/** press sends a key to the open palette the way the browser would. */
function press(key: string, over: KeyboardEventInit = {}) {
  const target = document.querySelector('[data-keys="local"]') ?? document.body;
  target.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...over }));
  flushSync();
}

function type(s: string) {
  for (const ch of s) press(ch);
}

/** settle lets the palette's awaits (load, use, pin) finish. */
async function settle() {
  for (let i = 0; i < 6; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  flushSync();
}

function openPalette(): HTMLElement {
  const host = stage(1000, 640);
  mount(MovePalette, { target: host });
  flushSync();
  return host;
}

async function run() {
  /* ---- the list the palette shows ---- */

  service.seed(["/library/2026/rejects", "/library/2026/portraits", "/library/2026/keepers"]);
  palette.show("move");
  loadFrames();
  const paletteHost = openPalette();
  await settle();

  const rowPaths = [...paletteHost.querySelectorAll(".row")].map((r) => r.getAttribute("data-destination"));
  eq("palette · rows come from the service, most recent first", rowPaths, [
    "/library/2026/keepers",
    "/library/2026/portraits",
    "/library/2026/rejects",
  ]);
  const digits = [...paletteHost.querySelectorAll(".row")].map((r) => text(r.querySelector(".cap.slot")));
  eq("palette · the first nine carry their digit", digits, ["1", "2", "3"]);

  /* ---- fuzzy search, and creating a path that is not on the list ---- */

  // "port" is both a search and a library-relative path nobody has used, so
  // the create row leads and the match follows it. Everything that does not
  // match is gone.
  type("port");
  await settle();
  const searched = [...paletteHost.querySelectorAll(".row")].map((r) => r.getAttribute("data-destination"));
  eq("palette · typing narrows to the match", searched, ["port", "/library/2026/portraits"]);

  for (let i = 0; i < 4; i++) press("Backspace");
  type("/library/2026/wildlife");
  await settle();
  const fresh = paletteHost.querySelector(".row");
  eq("palette · an unknown path leads the list", fresh?.getAttribute("data-destination"), "/library/2026/wildlife");
  eq("palette · and offers to create it", text(fresh?.querySelector(".rmeta")), "create and use");

  /* ---- Enter assigns, and advances focus like a verdict ---- */

  press("Enter");
  await settle();
  eq("assign · the focused frame is routed", app.groups[0].destination, "/library/2026/wildlife");
  eq("assign · a destination implies a keep", app.groups[0].verdict, "keep");
  eq("assign · focus advances, as a verdict does", app.focusIndex, 1);
  eq("assign · the palette closes", palette.open, false);
  check(
    "assign · the destination is remembered",
    service.calls.includes("use /library/2026/wildlife"),
    service.calls.join(", "),
  );
  eq("assign · nothing else was routed", app.groups[1].destination, "");

  /* ---- a digit routes straight there ---- */

  palette.show("move");
  const slotHost = openPalette();
  await settle();
  // The freshly used path is now the most recent, so it holds digit 1 and
  // the one the bench wants is behind it.
  const onTwo = destinations.forDigit(2);
  press("2");
  await settle();
  eq("slot · a digit routes the focused frame", app.groups[1].destination, onTwo?.path);
  eq("slot · and advances focus", app.focusIndex, 2);
  eq("slot · the palette closes", palette.open, false);

  /* ---- a digit routes a whole selection ---- */

  app.setFocus(2);
  app.toggleSelect();
  app.setFocus(3);
  app.toggleSelect();
  palette.show("move");
  const bulkHost = openPalette();
  await settle();
  const onOne = destinations.forDigit(1);
  press("1");
  await settle();
  eq("slot · the whole selection goes", [app.groups[2].destination, app.groups[3].destination], [
    onOne?.path,
    onOne?.path,
  ]);
  eq("slot · a selection does not move the focus", app.focusIndex, 3);
  bulkHost.remove();
  slotHost.remove();

  /* ---- 0 clears ---- */

  app.clearSelection();
  app.setFocus(0);
  palette.show("move");
  const clearHost = openPalette();
  await settle();
  press("0");
  await settle();
  eq("clear · 0 takes the routing off", app.groups[0].destination, "");
  eq("clear · but leaves the verdict", app.groups[0].verdict, "keep");
  eq("clear · the palette closes", palette.open, false);
  clearHost.remove();

  /* ---- a typed digit is part of a path, not a slot ---- */

  app.setFocus(0);
  palette.show("move");
  const typedHost = openPalette();
  await settle();
  type("/library/2026");
  await settle();
  eq("slot · digits inside a typed path stay in it", palette.query, "/library/2026");
  eq("slot · and route nothing on their own", app.groups[0].destination, "");
  press("Escape");
  await settle();
  typedHost.remove();
  paletteHost.remove();

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
  eq("tile · and the whole path on hover", chip?.getAttribute("title"), "/library/2026/{date:2006-01-02}");
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
