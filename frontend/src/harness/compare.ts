// A headless bench for the compare screen.
//
// Its own entry rather than a section of the loupe harness: the two are checked
// independently, and a screen that cannot be verified without the other's
// fixtures is a screen nobody will re-run the checks on.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9347
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1600,1000 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9347/src/harness/compare.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount } from "svelte";
import type { GroupDTO } from "../lib/bindings";
import { setVerdict } from "../lib/decisions";
import { previewURL } from "../lib/preview";
import { app, groupKey } from "../lib/state.svelte";
import CompareView from "../components/CompareView.svelte";
import {
  compare,
  compareChip,
  compareKeys,
  compareSet,
  compareSummary,
  compareTitle,
  cropInset,
  railNote,
} from "../components/CompareView.svelte";
import { metricRows } from "../components/CompareMetrics.svelte";

interface Result {
  name: string;
  pass: boolean;
  detail: string;
  skipped?: boolean;
}

const results: Result[] = [];

function check(name: string, pass: boolean, detail = "") {
  results.push({ name, pass, detail: pass ? "" : detail });
}

/**
 * A check that could not be run here. The crop inset needs a preview to have
 * decoded, and the dev server has no asset route behind it, so those checks are
 * named and counted rather than quietly left out — run the harness behind
 * something serving /preview and they run for real.
 */
function skip(name: string, why: string) {
  results.push({ name, pass: true, detail: `skipped: ${why}`, skipped: true });
}

function eq(name: string, actual: unknown, expected: unknown) {
  check(name, Object.is(actual, expected), `expected ${String(expected)}, got ${String(actual)}`);
}

/** The computed form of a token, for comparing against a rendered colour. */
function token(name: string): string {
  const hex = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  return m === null ? hex : `rgb(${parseInt(m[1], 16)}, ${parseInt(m[2], 16)}, ${parseInt(m[3], 16)})`;
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
    shot: "2026-07-18T19:42:07Z",
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

/** A burst of four: the shape compare was drawn for. */
const seed: GroupDTO[] = [
  frame(1, { rating: 5, shot: "2026-07-18T19:42:07Z" }),
  frame(2, { rating: 2, shot: "2026-07-18T19:42:09Z" }),
  frame(3, { rating: 3, shot: "2026-07-18T19:42:11Z", kind: "raw-only", hasJpeg: false, jpegPath: "" }),
  frame(4, { shot: "2026-07-18T19:42:14Z" }),
];

function stage(width: number, height: number): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

function text(el: Element | null): string {
  return (el?.textContent ?? "").trim();
}

function texts(root: ParentNode, sel: string): string[] {
  return [...root.querySelectorAll(sel)].map((e) => text(e));
}

function click(el: Element) {
  (el as HTMLElement).click();
  flushSync();
}

const settle = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---- the pure pieces ---------------------------------------------------------

function pure() {
  // Entry model: a selection of two or more is the comparison; otherwise the
  // focused frame and the one after it.
  eq("set · a selection of three is the comparison", compareSet(seed, seed.slice(0, 3), 0).length, 3);
  eq("set · no selection takes the focused frame and the next", compareSet(seed, [], 1)[0].stem, seed[1].stem);
  eq("set · and the one after it", compareSet(seed, [], 1)[1].stem, seed[2].stem);
  eq("set · on the last frame it reaches back", compareSet(seed, [], 3)[1].stem, seed[2].stem);
  eq("set · one frame alone cannot be compared", compareSet([seed[0]], [], 0).length, 0);

  eq("title · summary line", compareSummary(4), "comparing 2 of 4 selected");
  eq("title · two frames compare as two of two", compareSummary(2), "comparing 2 of 2 selected");
  eq("title · unzoomed reads fit", compareTitle(false, true).zoom, "fit");
  eq("title · zoomed reads 1:1", compareTitle(true, true).zoom, "1:1");
  eq("title · locked panning", compareTitle(true, true).lock, "panning locked");
  eq("title · unlocked panning", compareTitle(true, false).lock, "panning free");
  eq("status · side chip", compareChip("b"), "COMPARE · side B");
  eq("status · l names the state it moves to", compareKeys(true)[3].hint, "unlock panning");
  eq("status · and back again", compareKeys(false)[3].hint, "lock panning");

  // The clock is the camera's, rendered in the machine's own zone, so the note
  // is asserted by shape rather than by the hour the design happened to draw.
  check("rail · a run inside a minute is a burst", /^burst \d{2}:\d{2}$/.test(railNote(seed)), railNote(seed));
  eq(
    "rail · a wider span is written out",
    railNote([seed[0], frame(9, { shot: "2026-07-18T20:10:00Z" })]).includes("→"),
    true,
  );
  eq("rail · no note without timestamps", railNote([frame(9, { shot: "" })]), "");

  // The crop inset: the middle of the inset is the middle of the pane's view.
  const centred = cropInset({ w: 4000, h: 3000 }, { x: 0, y: 0 }, 2);
  eq("crop · 1:1 background size", centred.size, "4000px 3000px");
  eq("crop · centred on the frame", centred.position, "-1925px -1425px");
  const panned = cropInset({ w: 4000, h: 3000 }, { x: 100, y: 0 }, 2);
  eq("crop · a pan moves it by fit-scaled pixels", panned.position, "-1725px -1425px");

  // Metrics: only rating and pixels take a side, and nothing is invented.
  const rows = metricRows(
    { group: seed[0], bytes: { raw: 56_819_712, jpeg: 12_373_196 } },
    { group: seed[1], bytes: { raw: 54_000_000, jpeg: 12_000_000 } },
  );
  eq("metrics · five rows", rows.a.length, 5);
  eq("metrics · keys", rows.a.map((r) => r.key).join(","), "rating,pixels,on disk,files,shot");
  eq("metrics · the better rating wins", rows.a[0].tone, "win");
  eq("metrics · and the other loses", rows.b[0].tone, "lose");
  eq("metrics · winning delta", rows.a[0].delta, "+3");
  eq("metrics · losing delta", rows.b[0].delta, "−3");
  eq("metrics · no dimensions without a decoded image", rows.a[1].value, "—");
  eq("metrics · and no delta for them either", rows.a[1].delta, "—");
  eq("metrics · a file size takes no side", rows.a[2].tone, "none");
  check("metrics · but is still compared", rows.a[2].delta.startsWith("+"), rows.a[2].delta);
  eq("metrics · shot time takes no side", rows.a[4].tone, "none");
  eq("metrics · and has no meter to fill", rows.a[4].ratio, null);
  eq("metrics · equal values read as level", metricRows({ group: seed[0] }, { group: seed[0] }).a[0].delta, "same");

  const invented = /\bISO\b|ƒ|f\/\d|1\/\d+s|\d+mm|sharp/i.exec(
    rows.a.map((r) => `${r.key} ${r.value}`).join(" "),
  );
  check("metrics · nothing invented", invented === null, `found ${invented?.[0] ?? ""}`);
}

// ---- the screen --------------------------------------------------------------

interface Ask {
  stems: string[];
  verdict: string;
}

function screen() {
  const asks: Ask[] = [];
  let exits = 0;

  // The frames go through the store so a verdict recorded by the application's
  // own helper lands on the very objects the panes are drawing.
  app.groups = seed.map((g) => ({ ...g }));
  app.focusIndex = 0;
  app.selection = new Set();
  const groups = app.groups;

  const bytes: Record<string, { raw: number; jpeg: number }> = {
    [groupKey(groups[0])]: { raw: 56_819_712, jpeg: 12_373_196 },
    [groupKey(groups[1])]: { raw: 54_000_000, jpeg: 12_000_000 },
  };

  const host = stage(1440, 820);
  mount(CompareView, {
    target: host,
    props: {
      groups,
      bytes,
      onverdict: (frames: GroupDTO[], verdict: "keep" | "cut") =>
        asks.push({ stems: frames.map((f) => f.stem), verdict }),
      onexit: () => exits++,
    },
  });
  flushSync();

  // ---- pane headers ----
  const panes = [...host.querySelectorAll<HTMLElement>(".pane")];
  eq("compare · two panes", panes.length, 2);
  eq("compare · A and B", texts(host, ".phead .tag").join(""), "AB");
  eq("compare · header is 34px", Math.round(panes[0].querySelector(".phead")!.getBoundingClientRect().height), 34);
  const tag = panes[0].querySelector<HTMLElement>(".tag")!.getBoundingClientRect();
  eq("compare · tag square is 19×19", `${Math.round(tag.width)}×${Math.round(tag.height)}`, "19×19");
  eq("compare · pane A holds the first frame", text(panes[0].querySelector(".stem")), groups[0].stem);
  eq("compare · pane B holds the second", text(panes[1].querySelector(".stem")), groups[1].stem);
  check(
    "compare · headers carry the timestamp",
    /^\d{4}-\d{2}-\d{2} · \d{2}:\d{2}:\d{2}$/.test(text(panes[0].querySelector(".when"))),
    text(panes[0].querySelector(".when")),
  );
  eq("compare · undecided pill", text(panes[0].querySelector(".pill")), "UNDECIDED");
  eq(
    "compare · the active side's tag is filled",
    getComputedStyle(panes[0].querySelector<HTMLElement>(".tag")!).backgroundColor,
    token("--accent"),
  );
  check(
    "compare · the active side's image is ringed",
    panes[0].querySelector(".shot.ringed") !== null && panes[1].querySelector(".shot.ringed") === null,
    "both or neither carried the ring",
  );

  // ---- pills react to the application's own verdict helper ----
  app.focusIndex = 0;
  setVerdict("keep");
  flushSync();
  eq("compare · a keep reaches pane A's pill", text(panes[0].querySelector(".pill")), "KEEP · RAW + JPEG");
  eq(
    "compare · in the keep hue",
    getComputedStyle(panes[0].querySelector<HTMLElement>(".pill")!).color,
    token("--keep-text"),
  );
  app.focusIndex = 1;
  setVerdict("cut");
  flushSync();
  eq("compare · a cut reaches pane B's pill", text(panes[1].querySelector(".pill")), "CUT");
  eq(
    "compare · in the cut hue",
    getComputedStyle(panes[1].querySelector<HTMLElement>(".pill")!).color,
    token("--cut-text"),
  );

  // ---- metric rows ----
  const rowsOf = (pane: HTMLElement) => [...pane.querySelectorAll<HTMLElement>(".metric")];
  eq("metrics · block says where the numbers came from", text(host.querySelector(".metrics .label")), "Measured");
  eq("metrics · and how far they go", text(host.querySelector(".metrics .note")), "what the scan knows");
  eq("metrics · row height 26", Math.round(rowsOf(panes[0])[0].getBoundingClientRect().height), 26);
  eq(
    "metrics · key column is 96px",
    Math.round(rowsOf(panes[0])[0].querySelector<HTMLElement>(".mkey")!.getBoundingClientRect().width),
    96,
  );
  eq(
    "metrics · meter is 4px",
    Math.round(rowsOf(panes[0])[0].querySelector<HTMLElement>(".meter")!.getBoundingClientRect().height),
    4,
  );

  const ratingA = rowsOf(panes[0])[0];
  const ratingB = rowsOf(panes[1])[0];
  eq("metrics · winning value takes the keep hue", getComputedStyle(ratingA.querySelector<HTMLElement>(".mval")!).color, token("--keep"));
  eq("metrics · winning bar too", getComputedStyle(ratingA.querySelector<HTMLElement>(".fill")!).backgroundColor, token("--keep"));
  eq("metrics · winning delta", getComputedStyle(ratingA.querySelector<HTMLElement>(".delta")!).color, token("--keep"));
  eq("metrics · losing delta takes the cut hue", getComputedStyle(ratingB.querySelector<HTMLElement>(".delta")!).color, token("--cut"));
  eq("metrics · the loser's bar stays neutral", getComputedStyle(ratingB.querySelector<HTMLElement>(".fill")!).backgroundColor, token("--neutral-bar"));

  const sizeRow = rowsOf(panes[0])[2];
  eq("metrics · a directionless bar is neutral", getComputedStyle(sizeRow.querySelector<HTMLElement>(".fill")!).backgroundColor, token("--neutral-bar"));
  eq("metrics · and its delta is dim", getComputedStyle(sizeRow.querySelector<HTMLElement>(".delta")!).color, token("--text-dim"));
  eq("metrics · the unmeasured row has no meter fill", rowsOf(panes[0])[1].querySelectorAll(".fill").length, 0);
  eq("metrics · pane B knows its own files", text(rowsOf(panes[1])[3].querySelector(".mval")), "RAW + JPEG");

  // Nothing has decoded in a headless run, so the crop inset stays away rather
  // than drawing a box with no photograph in it.
  eq("crop · no inset until an image has decoded", host.querySelectorAll(".inset").length, 0);

  // ---- the rail ----
  const rail = host.querySelector<HTMLElement>(".rail")!;
  eq("rail · 78px", Math.round(rail.getBoundingClientRect().height), 78);
  eq("rail · label", text(rail.querySelector(".rtitle")), "Comparing");
  check(
    "rail · note counts the set and names the burst",
    /^4 frames · burst \d{2}:\d{2}$/.test(text(rail.querySelector(".rnote"))),
    text(rail.querySelector(".rnote")),
  );
  eq("rail · note takes the accent", getComputedStyle(rail.querySelector<HTMLElement>(".rnote")!).color, token("--accent"));
  const rframes = [...rail.querySelectorAll<HTMLElement>(".rframe")];
  eq("rail · one thumbnail per frame", rframes.length, groups.length);
  const rthumb = rframes[0].querySelector<HTMLElement>(".rthumb")!.getBoundingClientRect();
  eq("rail · thumbs are 82×54", `${Math.round(rthumb.width)}×${Math.round(rthumb.height)}`, "82×54");
  eq("rail · the two in the panes are tagged", texts(rail, ".rtag").join(""), "AB");
  eq("rail · and marked selected", rframes.filter((f) => f.getAttribute("aria-selected") === "true").length, 2);

  // A rail click loads that frame into the side holding the keys.
  click(rframes[3]);
  eq("rail · click loads into the active side", text(panes[0].querySelector(".stem")), groups[3].stem);
  click(rframes[0]);

  // ---- the key API ----
  eq("keys · A starts with them", compare.side(), "a");
  compare.switchSide();
  flushSync();
  eq("keys · ⇥ switches side", compare.side(), "b");
  eq(
    "keys · and the tag follows",
    getComputedStyle(panes[1].querySelector<HTMLElement>(".tag")!).backgroundColor,
    token("--accent"),
  );

  compare.verdict("cut");
  eq("keys · x judges the active side alone", JSON.stringify(asks.at(-1)), JSON.stringify({ stems: [groups[1].stem], verdict: "cut" }));

  asks.length = 0;
  compare.wins();
  eq("keys · w keeps the winner", JSON.stringify(asks[0]), JSON.stringify({ stems: [groups[1].stem], verdict: "keep" }));
  eq(
    "keys · and cuts the rest of the comparison",
    JSON.stringify(asks[1]),
    JSON.stringify({ stems: [groups[0].stem, groups[2].stem, groups[3].stem], verdict: "cut" }),
  );

  eq("keys · panning starts locked", compare.title().lock, "panning locked");
  compare.togglePanLock();
  flushSync();
  eq("keys · l unlocks it", compare.title().lock, "panning free");
  eq("keys · and the title says which", compare.title().locked, false);
  compare.togglePanLock();

  eq("keys · the zoom starts fitted", compare.title().zoom, "fit");
  compare.toggleZoom();
  flushSync();
  eq("keys · z takes it to 1:1", compare.title().zoom, "1:1");
  compare.toggleZoom();

  eq("keys · the summary counts the whole set", compare.summary(), "comparing 2 of 4 selected");
  eq("keys · frames() names both panes", compare.frames().map((g) => g.stem).join(","), `${groups[0].stem},${groups[1].stem}`);

  compare.exit();
  eq("keys · c leaves compare", exits, 1);

  return host;
}

// ---- the crop inset, the zoom and the pan lock -------------------------------

/** Whether anything is answering the asset server's preview route. */
async function previewsServed(): Promise<boolean> {
  try {
    const r = await fetch(previewURL(seed[0]));
    return r.ok && (r.headers.get("content-type") ?? "").startsWith("image/");
  } catch {
    return false;
  }
}

/** The scale out of a computed `matrix(...)`, or 1 when there is no transform. */
function scaleOf(el: Element): number {
  const m = /matrix\(([-\d.]+)/.exec(getComputedStyle(el).transform);
  return m === null ? 1 : Number(m[1]);
}

const CROP_CHECKS = [
  "crop · an inset per decoded pane",
  "crop · the inset is 150×150",
  "crop · sits at the drawn corner",
  "crop · opens on the middle of the frame",
  "crop · says what it is showing",
  "zoom · 1:1 scales the image up",
  "pan · a locked pan moves both panes together",
  "pan · unlocked, only the active pane moves",
];

async function cropAndPan(host: HTMLElement) {
  if (!(await previewsServed())) {
    for (const name of CROP_CHECKS) skip(name, "nothing is serving /preview");
    return;
  }
  await settle(800);

  const insets = () => [...host.querySelectorAll<HTMLElement>(".inset")];
  const at = (el: HTMLElement) => getComputedStyle(el).backgroundPosition;
  eq(CROP_CHECKS[0], insets().length, 2);
  if (insets().length < 2) return;

  const box = insets()[0].getBoundingClientRect();
  const stage = insets()[0].parentElement!.getBoundingClientRect();
  eq(CROP_CHECKS[1], `${Math.round(box.width)}×${Math.round(box.height)}`, "150×150");
  eq(
    CROP_CHECKS[2],
    `${Math.round(stage.bottom - box.bottom)},${Math.round(stage.right - box.right)}`,
    "26,26",
  );

  const img = host.querySelector<HTMLImageElement>(".shot")!;
  eq(CROP_CHECKS[3], at(insets()[0]), `${75 - img.naturalWidth / 2}px ${75 - img.naturalHeight / 2}px`);
  eq(CROP_CHECKS[4], text(host.querySelector(".chip")), "preview 1:1 · centre");

  compare.toggleZoom();
  flushSync();
  check(CROP_CHECKS[5], scaleOf(img) > 1, `image drew at ${scaleOf(img)}×`);

  const before = insets().map(at);
  compare.pan(-140, 0);
  flushSync();
  const locked = insets().map(at);
  check(
    CROP_CHECKS[6],
    locked[0] !== before[0] && locked[0] === locked[1],
    `${before.join(" / ")} → ${locked.join(" / ")}`,
  );

  compare.togglePanLock();
  compare.pan(-140, 0);
  flushSync();
  const free = insets().map(at);
  const moved = compare.side() === "a" ? 0 : 1;
  check(
    CROP_CHECKS[7],
    free[moved] !== locked[moved] && free[1 - moved] === locked[1 - moved],
    `${locked.join(" / ")} → ${free.join(" / ")}`,
  );

  compare.togglePanLock();
  compare.toggleZoom();
}

// ---- the image queue ---------------------------------------------------------

// Mounted on its own and checked first: the queue only loads what is near the
// viewport, and a rail pushed down the page by another mount is one it is right
// to leave alone.
async function railFeeds() {
  const host = stage(1440, 200);
  mount(CompareView, {
    target: host,
    props: { groups: seed, onverdict: () => {}, onexit: () => {} },
  });
  flushSync();

  const imgs = () => [...host.querySelectorAll<HTMLImageElement>(".rthumb img")];
  const withSrc = () => imgs().filter((i) => i.hasAttribute("src")).length;

  eq("queue · a rail thumb per frame", imgs().length, seed.length);
  eq("queue · no src until the queue says so", withSrc(), 0);

  await settle(2500);
  check("queue · the queue feeds the rail", withSrc() > 0, `${withSrc()} of ${imgs().length} were given a src`);
  check(
    "queue · srcs are preview URLs",
    imgs().every((i) => !i.hasAttribute("src") || i.getAttribute("src")!.startsWith("/preview?path=")),
    imgs()[0]?.getAttribute("src") ?? "",
  );
  host.remove();
}

// ---- run ---------------------------------------------------------------------

async function run() {
  app.cutRemoves = "both";

  await railFeeds();
  pure();
  await cropAndPan(screen());

  const failed = results.filter((r) => !r.pass);
  const skipped = results.filter((r) => r.skipped).length;
  document.getElementById("results")!.textContent = JSON.stringify(
    { total: results.length, failed: failed.length, skipped, results },
    null,
    1,
  );
  const tally = skipped === 0 ? "" : ` (${skipped} skipped)`;
  document.title = failed.length === 0 ? `PASS ${results.length}/${results.length}${tally}` : `FAIL ${failed.length}`;
}

void run().catch((err: unknown) => {
  document.getElementById("results")!.textContent = `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
