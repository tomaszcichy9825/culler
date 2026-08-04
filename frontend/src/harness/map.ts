// A headless bench for MAP mode.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the pane that fetches map tiles is
// the one pane in the app that can reach the network, and "no tiles before
// consent" has to be a measured fact rather than a claim — so the bench reads
// the browser's own resource timings and asserts that nothing went to a tile
// host while the gate was up.
//
// The rest of it is the ordinary set: pins drawn from stubbed positions, the
// cluster maths on its own, the three sub-layouts, the keyboard walking the
// places rail, and the open-frame callback arriving with the folder and the
// frame's hash.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9347
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1600,1000 \
//     --virtual-time-budget=20000 --dump-dom \
//     http://127.0.0.1:9347/src/harness/map.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import type { Component as SvelteComponent } from "svelte";
import type { GroupDTO } from "../lib/bindings";
import MapCentre from "../components/map/MapCentre.svelte";
import MapLeft from "../components/map/MapLeft.svelte";
import MapRight from "../components/map/MapRight.svelte";
import {
  clusterPositions,
  connectMap,
  formatCoords,
  mapState,
  onOpenFrame,
  placeName,
  TILE_CONSENT_KEY,
} from "../lib/map.svelte";
import type { MapPosition, MapPositions } from "../lib/map.svelte";

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
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

/**
 * A mounted pane, and the way to take it down again.
 *
 * Taking it down properly matters here in a way it does not for a component
 * that owns all its own state: every pane on this screen reads and writes one
 * shared MapState, so a pane that was merely detached from the document would
 * go on answering changes to it and overwrite what the live pane had just
 * worked out. Detaching the node is not unmounting the component.
 */
interface Pane {
  host: HTMLDivElement;
  drop(): void;
}

function paneOf<P extends Record<string, unknown>>(
  Component: SvelteComponent<P>,
  props: P,
  width: number,
  height: number,
): Pane {
  const host = stage(width, height);
  const instance = mount(Component, { target: host, props });
  flushSync();
  return {
    host,
    drop() {
      void unmount(instance);
      flushSync();
      host.remove();
    },
  };
}

function text(el: Element | null): string {
  return (el?.textContent ?? "").trim();
}

function press(el: Element, key: string): KeyboardEvent {
  const e = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  el.dispatchEvent(e);
  flushSync();
  return e;
}

function click(el: Element) {
  (el as HTMLElement).click();
  flushSync();
}

const settle = (ms: number) => new Promise((r) => setTimeout(r, ms));

// --- the folder the bench works from ------------------------------------------

const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";

/** Kraków, tight enough that some of these share a pin and some do not. */
const PLACES: [number, number, number][] = [
  // latitude, longitude, how many frames were taken standing there
  [50.06168, 19.937, 4],
  [50.0544, 19.9354, 2],
  [50.0517, 19.9448, 1],
];

function position(n: number, lat: number, lon: number): MapPosition {
  const stem = `DSCF12${String(n).padStart(2, "0")}`;
  return {
    dir: DIR,
    stem,
    kind: "paired",
    hasRaw: true,
    hasJpeg: true,
    rawPath: `${DIR}/${stem}.RAF`,
    jpegPath: `${DIR}/${stem}.JPG`,
    shot: `2026-07-18T19:${String(40 + n).padStart(2, "0")}:07Z`,
    latitude: lat,
    longitude: lon,
    altitude: 219.4,
    hasAltitude: n % 2 === 0,
  };
}

const positions: MapPosition[] = [];
{
  let n = 0;
  for (const [lat, lon, count] of PLACES) {
    for (let i = 0; i < count; i++) {
      // A few metres apart, so they are distinct coordinates that still land in
      // one cell of the cluster grid at any sane zoom.
      positions.push(position(n, lat + i * 0.00004, lon + i * 0.00004));
      n++;
    }
  }
}

const FOLDER: MapPositions = {
  dir: DIR,
  frames: positions,
  total: positions.length + 5,
  positioned: positions.length,
  unpositioned: 4,
  unreadable: 1,
};

/** The open folder, as CULL holds it — this is where a hash comes from. */
const groups: GroupDTO[] = positions.map((p) => ({
  dir: p.dir,
  stem: p.stem,
  kind: p.kind,
  hasRaw: p.hasRaw,
  hasJpeg: p.hasJpeg,
  rawPath: p.rawPath,
  jpegPath: p.jpegPath,
  sidecars: 0,
  shot: p.shot,
  warnings: null,
  verdict: "",
  mask: "rj",
  rating: 0,
  hash: `hash-${p.stem}`,
  destination: "",
  decision: "",
}));

const opened: { dir: string; hash?: string }[] = [];

async function loadFolder() {
  connectMap({ Positions: async () => FOLDER });
  onOpenFrame((dir, hash) => opened.push({ dir, hash }));
  mapState.clear();
  await mapState.load(DIR, true);
  mapState.attach(groups);
}

/** Every resource the page has fetched that went to a raster tile host. */
function tileRequests(): string[] {
  return performance
    .getEntriesByType("resource")
    .map((e) => e.name)
    .filter((name) => /tile\.openstreetmap\.org|tile\.osm|basemaps?\./i.test(name));
}

// --- the cluster maths, on its own --------------------------------------------

// The projection is supplied, so this needs neither a map nor a tile: a cell is
// a square of projected pixels, and these are hand-placed inside and outside
// one.
function clusterMaths() {
  const at = new Map<string, { x: number; y: number }>([
    ["DSCF1200", { x: 10, y: 10 }],
    ["DSCF1201", { x: 40, y: 40 }], // same 64px cell as the first
    ["DSCF1202", { x: 70, y: 10 }], // next cell across
    ["DSCF1203", { x: 10, y: 70 }], // next cell down
  ]);
  const frames = [...at.keys()].map((stem, i) => ({
    ...position(i, 50 + i * 0.001, 19 + i * 0.001),
    stem,
  }));
  const project = (p: MapPosition) => at.get(p.stem)!;

  const clusters = clusterPositions(frames, project, 64);
  eq("cluster · three cells hold four frames", clusters.length, 3);
  eq("cluster · the shared cell holds two", clusters[0].count, 2);
  eq("cluster · every frame lands in exactly one", clusters.reduce((n, c) => n + c.count, 0), frames.length);
  check(
    "cluster · the pin sits at the mean of its members",
    Math.abs(clusters[0].latitude - (frames[0].latitude + frames[1].latitude) / 2) < 1e-9,
    `${clusters[0].latitude} vs ${(frames[0].latitude + frames[1].latitude) / 2}`,
  );
  eq("cluster · order is the order the frames came in", clusters[0].frames[0].stem, "DSCF1200");

  // A smaller cell has to separate what a larger one merged.
  eq("cluster · a smaller cell splits them", clusterPositions(frames, project, 16).length, 4);
  // And a larger one has to merge what a smaller one split.
  eq("cluster · a larger cell merges them", clusterPositions(frames, project, 512).length, 1);

  eq("cluster · nothing in, nothing out", clusterPositions([], project, 64).length, 0);

  // A projection that cannot place a frame drops it rather than producing a
  // cluster at NaN, which Leaflet would refuse to draw and the rail would list.
  const broken = clusterPositions(frames, (p) => (p.stem === "DSCF1202" ? { x: NaN, y: 0 } : project(p)), 64);
  eq("cluster · an unprojectable frame is dropped", broken.reduce((n, c) => n + c.count, 0), 3);

  eq("cluster · a place is named by its coordinates", placeName(clusters[0]), `${clusters[0].latitude.toFixed(4)}, ${clusters[0].longitude.toFixed(4)}`);
  eq("cluster · coordinates print at six decimals", formatCoords(50.06168, 19.937), "50.061680, 19.937000");
}

// --- the consent gate ----------------------------------------------------------

async function consentGate() {
  localStorage.removeItem(TILE_CONSENT_KEY);
  mapState.forgetConsent();

  const before = tileRequests().length;
  const pane = paneOf(MapCentre, { layout: 0 }, 560, 360);
  const host = pane.host;
  await settle(1200);

  check("consent · the panel is up before anything is fetched", host.querySelector('[role="dialog"]') !== null);
  eq("consent · nothing has been asked yet", mapState.asked, false);
  eq("consent · and so tiles are off", mapState.tilesEnabled, false);
  eq("consent · no tile layer exists", host.querySelectorAll("img.leaflet-tile").length, 0);
  eq("consent · and no request went to a tile host", tileRequests().length - before, 0);
  check(
    "consent · the map itself is up behind the gate",
    host.querySelector(".leaflet-container") !== null,
    "no leaflet container",
  );

  // Declining leaves a working map with no basemap under it, and still no
  // request. This is the case that has to keep working offline.
  click(host.querySelector('[role="dialog"] .later')!);
  await settle(600);
  eq("consent · declining is remembered", localStorage.getItem(TILE_CONSENT_KEY), "declined");
  eq("consent · the panel goes away", host.querySelector('[role="dialog"]'), null);
  eq("consent · declined still fetches nothing", tileRequests().length - before, 0);
  eq("consent · and still no tiles in the document", host.querySelectorAll("img.leaflet-tile").length, 0);
  check(
    "consent · the pane says why there is no basemap",
    text(host.querySelector(".float")).includes("tiles are off"),
    text(host.querySelector(".float")),
  );

  // Enabling is the only thing that builds a tile layer.
  mapState.grantTiles();
  flushSync();
  await settle(900);
  eq("consent · enabling is remembered", localStorage.getItem(TILE_CONSENT_KEY), "granted");
  const tiles = [...host.querySelectorAll<HTMLImageElement>("img.leaflet-tile")];
  check("consent · enabling builds the tile layer", tiles.length > 0, `${tiles.length} tiles`);
  check(
    "consent · and the tiles are the OSM endpoint",
    tiles.every((t) => (t.getAttribute("src") ?? "").startsWith("https://tile.openstreetmap.org/")),
    tiles[0]?.getAttribute("src") ?? "",
  );
  check(
    "consent · attribution is shown, as the licence requires",
    text(host.querySelector(".leaflet-control-attribution")).includes("OpenStreetMap"),
    text(host.querySelector(".leaflet-control-attribution")),
  );
  // The positive control for the three assertions above. Without it, "no
  // request went to a tile host" would pass just as happily if the detector
  // could never see one — the point is that requests are visible here, and
  // were absent while the gate was up.
  check(
    "consent · and the requests the gate was holding back are now visible",
    tileRequests().length > before,
    `${tileRequests().length - before} tile requests after enabling`,
  );

  // Revoking takes the basemap away again without disturbing anything else.
  mapState.declineTiles();
  flushSync();
  await settle(300);
  eq("consent · revoking removes the tile layer", host.querySelectorAll("img.leaflet-tile").length, 0);

  pane.drop();
}

// --- nothing is loaded from a CDN ----------------------------------------------

function bundledNotFetched() {
  const external = performance
    .getEntriesByType("resource")
    .map((e) => e.name)
    .filter((name) => /^https?:\/\//i.test(name) && !name.startsWith(location.origin))
    // The tile endpoint is the accepted exception and is gated above; every
    // other outbound fetch is a bundling failure.
    .filter((name) => !/tile\.openstreetmap\.org/i.test(name));
  check("bundle · leaflet is bundled, not fetched from a CDN", external.length === 0, external.join(", "));
}

// --- pins ----------------------------------------------------------------------

async function pinsLayout() {
  await loadFolder();
  mapState.grantTiles();

  const pane = paneOf(MapCentre, { layout: 0 }, 900, 560);
  const host = pane.host;
  await settle(1400);

  // A dropped pane must really be gone: every live MapCentre writes the shared
  // clusters, so a leaked one would keep recutting them behind the live map.
  eq("pins · only one map is alive", document.querySelectorAll(".leaflet-container").length, 1);

  const pins = [...host.querySelectorAll<HTMLElement>(".pin")];
  eq("pins · one pin per cluster", pins.length, mapState.clusters.length);
  // The three places are hundreds of metres apart, so a map framed on them has
  // to separate them — a single pin would mean the fit never happened and the
  // clusters were cut at the world zoom.
  eq("pins · the three places are three pins", pins.length, PLACES.length);
  check("pins · the folder clustered into fewer pins than frames", pins.length < positions.length, `${pins.length} pins for ${positions.length} frames`);
  eq(
    "pins · the counts on the pins add up to the positioned frames",
    pins.reduce((n, p) => n + Number(text(p.querySelector(".ct"))), 0),
    positions.length,
  );
  eq("pins · exactly one pin is selected", host.querySelectorAll(".pin.sel").length, 1);

  const ring = getComputedStyle(host.querySelector<HTMLElement>(".pin.sel")!).boxShadow;
  check("pins · the selected pin takes the focus ring", ring !== "none" && ring !== "", ring);
  const swatch = host.querySelector<HTMLElement>(".pin .sw")!.getBoundingClientRect();
  eq("pins · the swatch is 26×18", `${Math.round(swatch.width)}×${Math.round(swatch.height)}`, "26×18");

  // The status chip counts what was placed and what was not, from the numbers
  // the backend gave rather than from the pins on screen.
  const chip = text(host.querySelector(".float"));
  check("pins · the chip says how many were placed", chip.includes(`${positions.length} of ${FOLDER.total} frames placed`), chip);
  check("pins · and how many have no GPS", chip.includes("5 frames have no GPS"), chip);

  // Clicking a pin moves the focus; the pin under the pointer becomes the
  // selected one and the rail and inspector follow it.
  // The map is built inside an effect and the clusters are cut inside the same
  // one, so a map that re-created itself would quietly reset the view and the
  // focus with it. Settling first, then asserting, is what catches that.
  const second = mapState.clusterIndex === 0 ? 1 : 0;
  mapState.focusCluster(second);
  flushSync();
  await settle(400);
  check(
    "pins · focusing another cluster moves the selection",
    mapState.clusterIndex === second,
    `asked for ${second}, landed on ${mapState.clusterIndex}, with ${mapState.clusters.length} clusters`,
  );
  eq("pins · the map was not rebuilt underneath it", mapState.clusters.length, PLACES.length);
  eq("pins · and the pin under it becomes the selected one", host.querySelectorAll(".pin.sel").length, 1);

  pane.drop();
}

// --- heat and track ------------------------------------------------------------

async function heatLayout() {
  const pane = paneOf(MapCentre, { layout: 1 }, 700, 460);
  const host = pane.host;
  await settle(1200);

  const canvas = host.querySelector<HTMLCanvasElement>("canvas.heat")!;
  eq("heat · the density canvas is showing", getComputedStyle(canvas).display, "block");
  eq("heat · and no pins are drawn under it", host.querySelectorAll(".pin").length, 0);
  eq("heat · the canvas takes no pointer events", getComputedStyle(canvas).pointerEvents, "none");

  const ctx = canvas.getContext("2d")!;
  const pixels = ctx.getImageData(0, 0, canvas.width, canvas.height).data;
  let painted = 0;
  for (let i = 3; i < pixels.length; i += 4) if (pixels[i] > 0) painted++;
  check("heat · the blobs are actually painted", painted > 0, `${painted} pixels have alpha`);

  pane.drop();
}

async function trackLayout() {
  const pane = paneOf(MapCentre, { layout: 2 }, 700, 460);
  const host = pane.host;
  await settle(1200);

  const lines = [...host.querySelectorAll<SVGPathElement>("path.leaflet-interactive")];
  check("track · a line is drawn through the positions", lines.length > 0, `${lines.length} paths`);
  const polyline = lines.find((p) => (p.getAttribute("stroke") ?? "").toLowerCase() === "#c678dd");
  check("track · the line takes the violet treatment", polyline !== undefined, lines.map((p) => p.getAttribute("stroke")).join(","));
  eq("track · no pins in the track layout", host.querySelectorAll(".pin").length, 0);

  pane.drop();
}

// --- the places rail ------------------------------------------------------------

function placesRail() {
  mapState.focusCluster(0);
  const pane = paneOf(MapLeft, { layout: 0 }, 208, 560);
  const host = pane.host;

  const rows = () => [...host.querySelectorAll<HTMLElement>(".place")];
  eq("places · one row per pin", rows().length, mapState.clusters.length);
  eq("places · rows are 26px", Math.round(rows()[0].getBoundingClientRect().height), 26);
  eq("places · the header names the pane", text(host.querySelector(".head")).toLowerCase().startsWith("places"), true);
  eq(
    "places · a row is named by its coordinates",
    text(rows()[0].querySelector(".name")),
    placeName(mapState.clusters[0]),
  );
  eq("places · and carries its count", text(rows()[0].querySelector(".count")), String(mapState.clusters[0].count));
  eq("places · the active row is marked", rows()[0].getAttribute("aria-selected"), "true");
  check(
    "places · the active row takes the accent mark",
    getComputedStyle(rows()[0]).borderLeftColor === "rgb(97, 175, 239)",
    getComputedStyle(rows()[0]).borderLeftColor,
  );

  // The frames that carry no position are accounted for at the foot of the
  // rail rather than left out of the tally.
  const none = host.querySelector<HTMLElement>(".none");
  check("places · the frames with no position are listed", none !== null, "no row");
  eq("places · counted together", text(none!.querySelector(".count")), "5");

  // Arrows walk the rail; Enter opens what the arrows landed on.
  const list = host.querySelector<HTMLElement>(".places")!;
  press(list, "ArrowDown");
  eq("places · down steps to the next place", mapState.clusterIndex, 1);
  press(list, "ArrowUp");
  eq("places · up steps back", mapState.clusterIndex, 0);
  press(list, "End");
  eq("places · End goes to the last", mapState.clusterIndex, mapState.clusters.length - 1);
  press(list, "Home");
  eq("places · Home goes to the first", mapState.clusterIndex, 0);

  // The application's own key listener sits on the window; a region marked
  // data-keys="local" is how it is told to keep out.
  check("places · the rail claims its keys", list.closest('[data-keys="local"]') !== null, "not marked local");

  opened.length = 0;
  press(list, "Enter");
  eq("places · Enter opens one frame", opened.length, 1);
  eq("places · with the folder", opened[0]?.dir, DIR);
  eq("places · and the frame's hash", opened[0]?.hash, `hash-${mapState.frame!.stem}`);

  // The heat rail is ranked, so the busiest place is at the top of it.
  pane.drop();
  const ranked = paneOf(MapLeft, { layout: 1 }, 208, 560);
  const counts = [...ranked.host.querySelectorAll(".place .count")].map((c) => Number(text(c)));
  eq("density · the rail is ranked by frames", JSON.stringify(counts), JSON.stringify([...counts].sort((a, b) => b - a)));
  check("density · and meters the ranking", ranked.host.querySelectorAll(".place .meter").length > 0, "no meters");
  ranked.drop();

  // The track rail explains what the line is, and is careful not to claim a
  // GPX match that never happened.
  const tracks = paneOf(MapLeft, { layout: 2 }, 208, 560);
  check(
    "track · the rail says the line is the folder's own positions",
    text(tracks.host.querySelector(".foot")).includes("capture order"),
    text(tracks.host.querySelector(".foot")),
  );
  check(
    "track · and that nothing is written",
    text(tracks.host.querySelector(".foot")).includes("nothing is written"),
    text(tracks.host.querySelector(".foot")),
  );
  tracks.drop();
}

// --- the inspector ---------------------------------------------------------------

function inspector() {
  mapState.focusCluster(0);
  mapState.focusFrame(0);
  const pane = paneOf(MapRight, {}, 296, 700);
  const host = pane.host;

  const frame = mapState.frame!;
  const cluster = mapState.cluster!;
  eq("inspector · names the frame", text(host.querySelector(".stem")), frame.stem);
  eq("inspector · counts what is at the pin", text(host.querySelector(".here")), `${cluster.count} frames here`);

  const rows = [...host.querySelectorAll<HTMLElement>(".row")];
  const value = (key: string) => text(rows.find((r) => text(r.querySelector(".key")) === key)?.querySelector(".val") ?? null);
  eq("inspector · latitude at six decimals", value("latitude"), frame.latitude.toFixed(6));
  eq("inspector · longitude at six decimals", value("longitude"), frame.longitude.toFixed(6));
  eq("inspector · the time of day", value("time"), "19:40:07");
  eq("inspector · the day", value("date"), "2026-07-18");
  check(
    "inspector · the position is marked as the camera's",
    text(host.querySelector(".source")) === "from camera",
    text(host.querySelector(".source")),
  );
  check(
    "inspector · the coordinates are a copy control",
    host.querySelector("button.coords") !== null && text(host.querySelector(".copy")).includes("copy"),
    "no copy control",
  );

  // No reverse geocoding anywhere on the pane: a place name would have to come
  // from a network call this mode does not make.
  const invented = /\b(city|region|country|place)\b/i.exec(host.textContent ?? "");
  check("inspector · no reverse-geocoded place names", invented === null, `found ${invented?.[0] ?? ""}`);

  // The grid under "at this pin" is every frame standing there, and picking
  // one moves the inspector onto it.
  const cells = [...host.querySelectorAll<HTMLElement>(".cell")];
  eq("inspector · a cell per frame at the pin", cells.length, cluster.count);
  eq("inspector · the focused frame is marked", host.querySelectorAll(".cell.on").length, 1);
  click(cells[1]);
  eq("inspector · picking a cell moves the inspector", mapState.frameIndex, 1);
  eq("inspector · and the name follows", text(host.querySelector(".stem")), cluster.frames[1].stem);

  opened.length = 0;
  mapState.open();
  eq("inspector · opening hands over the folder", opened[0]?.dir, DIR);
  eq("inspector · and the hash of the frame the inspector is on", opened[0]?.hash, `hash-${cluster.frames[1].stem}`);

  // A frame the map was never given a hash for opens its folder and nothing
  // more, rather than inventing an identity.
  mapState.attach([]);
  opened.length = 0;
  mapState.open();
  eq("inspector · without a hash, only the folder is handed over", opened[0]?.hash, undefined);
  mapState.attach(groups);

  pane.drop();
}

// --- the empty folder --------------------------------------------------------------

async function nothingPositioned() {
  connectMap({
    Positions: async () => ({ dir: DIR, frames: [], total: 12, positioned: 0, unpositioned: 12, unreadable: 0 }),
  });
  await mapState.load(DIR, true);

  eq("empty · nothing is positioned", mapState.positions.length, 0);
  eq("empty · and the count says so", mapState.withoutGPS, "12 frames have no GPS");

  const pane = paneOf(MapRight, {}, 296, 400);
  check(
    "empty · the inspector says there is nothing to show",
    text(pane.host.querySelector(".empty")).includes("carries a position"),
    text(pane.host.querySelector(".empty")),
  );
  pane.drop();

  const rail = paneOf(MapLeft, { layout: 0 }, 208, 400);
  eq("empty · the rail lists no places", rail.host.querySelectorAll(".place").length, 0);
  check(
    "empty · but still accounts for the frames",
    text(rail.host.querySelector(".none .count")) === "12",
    text(rail.host.querySelector(".none")),
  );
  rail.drop();

  // One frame is a real folder too: a single position must not crash the fit.
  connectMap({
    Positions: async () => ({
      dir: DIR,
      frames: [position(0, 50.06168, 19.937)],
      total: 1,
      positioned: 1,
      unpositioned: 0,
      unreadable: 0,
    }),
  });
  await mapState.load(DIR, true);
  const single = paneOf(MapCentre, { layout: 0 }, 500, 340);
  await settle(1000);
  eq("single · one frame draws one pin", single.host.querySelectorAll(".pin").length, 1);
  eq("single · and nothing says it has no GPS", mapState.withoutGPS, "");
  single.drop();
}

// --- run ------------------------------------------------------------------------

async function run() {
  clusterMaths();
  await consentGate();
  await pinsLayout();
  await heatLayout();
  await trackLayout();
  placesRail();
  inspector();
  await nothingPositioned();
  bundledNotFetched();

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
