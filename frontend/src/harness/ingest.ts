// A headless bench for the cold start and the scanning presentation.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because both screens are states you cannot
// reach on demand with a real backend — a card that happens to be plugged in,
// a folder slow enough to still be scanning while you look at it — and the
// behaviour they carry forward from the old loader is exactly the behaviour
// that is easy to lose in a restyle.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9348
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1400,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9348/src/harness/ingest.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount } from "svelte";
import ColdStart from "../components/ColdStart.svelte";
import Loader from "../components/Loader.svelte";
import { app, picker } from "../lib/state.svelte";

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
  check(
    name,
    same,
    `expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`,
  );
}

/** The computed form of a token, for comparing against a rendered colour. */
function token(name: string): string {
  const hex = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  return m === null
    ? hex
    : `rgb(${parseInt(m[1], 16)}, ${parseInt(m[2], 16)}, ${parseInt(m[3], 16)})`;
}

function text(el: Element | null | undefined): string {
  return (el?.textContent ?? "").replace(/\s+/g, " ").trim();
}

function texts(root: ParentNode, sel: string): string[] {
  return [...root.querySelectorAll(sel)].map((e) => text(e));
}

function click(el: Element | null | undefined) {
  (el as HTMLElement | undefined)?.click();
  flushSync();
}

function stage(width: number, height: number): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = `position:relative;display:flex;width:${width}px;height:${height}px;overflow:hidden;background:var(--bg-window)`;
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

// Both screens are drawn in whichever theme the URL asks for, so a screenshot
// of each can be taken without a second page: ?theme=light.
if (new URLSearchParams(location.search).get("theme") === "light") {
  document.documentElement.setAttribute("data-theme", "light");
}

const CARD = "/Volumes/FUJI_SD";
const DIR = "/Volumes/FUJI_SD/DCIM/103_FUJI";

/** The cold start with nothing plugged in and nothing remembered. */
function forget() {
  app.roots = [];
  app.network = {};
  app.folder = null;
  app.overlay = false;
  app.scanning = null;
  app.scanProgress = null;
  app.scanSlow = false;
  app.error = "";
  app.keymap = { "focus-path": ["o"], "keymap-overlay": ["?"] };
}

// ---- cold start, nothing known -----------------------------------------------

function coldEmpty() {
  forget();
  const host = stage(1000, 700);
  mount(ColdStart, { target: host });
  flushSync();

  eq("cold · no card, no eyebrow", host.querySelectorAll(".eyebrow").length, 0);
  eq(
    "cold · headline says nothing is open",
    text(host.querySelector(".headline")),
    "nothing open yet",
  );
  eq(
    "cold · and the meta line says what to do",
    text(host.querySelector(".meta")),
    "point culler at a card or a folder to start",
  );
  eq("cold · only the two steps that always work", texts(host, ".step .name"), [
    "open another folder",
    "keys",
  ]);
  eq(
    "cold · no session field without a handler for it",
    host.querySelectorAll(".field").length,
    0,
  );

  const headline = getComputedStyle(
    host.querySelector<HTMLElement>(".headline")!,
  );
  check(
    "cold · headline is Public Sans",
    headline.fontFamily.includes("Public Sans"),
    headline.fontFamily,
  );
  eq("cold · headline is 26px", headline.fontSize, "26px");
  eq("cold · headline is 600", headline.fontWeight, "600");
  eq("cold · headline tracking", headline.letterSpacing, "-0.26px");

  host.remove();
}

// ---- cold start, a card is in ------------------------------------------------

function coldCard() {
  forget();
  app.roots = [CARD, "/Users/t/Pictures"];
  const host = stage(1000, 700);
  mount(ColdStart, { target: host });
  flushSync();

  const eyebrow = host.querySelector<HTMLElement>(".eyebrow")!;
  eq("card · the eyebrow appears", text(eyebrow), "card detected");
  eq(
    "card · the eyebrow is brand cyan, never the accent",
    getComputedStyle(eyebrow).color,
    token("--brand"),
  );
  eq(
    "card · eyebrow tracking is the hero 0.2em",
    getComputedStyle(eyebrow).letterSpacing,
    "2.2px",
  );
  eq(
    "card · headline names the volume",
    text(host.querySelector(".headline")),
    "FUJI_SD",
  );
  eq(
    "card · meta is the path and its state",
    text(host.querySelector(".meta")),
    `${CARD} · not indexed yet`,
  );

  const steps = [...host.querySelectorAll<HTMLElement>(".step")];
  eq("card · three steps", steps.length, 3);
  eq(
    "card · the first is to cull it where it lies",
    texts(host, ".step .name"),
    ["cull in place", "open another folder", "keys"],
  );
  eq(
    "card · the primary step notes what it would open",
    text(steps[0].querySelector(".note")),
    CARD,
  );
  check(
    "card · and it is the only primary",
    steps.filter((s) => s.className.includes("primary")).length === 1,
  );
  eq(
    "card · step rows are 44px",
    Math.round(steps[0].getBoundingClientRect().height),
    44,
  );

  const keys = [...host.querySelectorAll<HTMLElement>(".step .key")];
  eq(
    "card · key squares are 22×22",
    `${Math.round(keys[0].getBoundingClientRect().width)}×${Math.round(keys[0].getBoundingClientRect().height)}`,
    "22×22",
  );
  eq(
    "card · the primary key square is filled with the accent",
    getComputedStyle(keys[0]).backgroundColor,
    token("--accent"),
  );
  eq(
    "card · the others are keycaps",
    getComputedStyle(keys[1]).backgroundColor,
    token("--bg-kbd"),
  );

  // The keys shown are the ones actually bound, not glyphs typed into a mock.
  eq("card · keys come from the keymap", texts(host, ".step .key"), [
    "↩",
    "O",
    "?",
  ]);
  app.keymap = { "focus-path": ["shift+p"], "keymap-overlay": ["f1"] };
  flushSync();
  check(
    "card · a rebound key redraws",
    texts(host, ".step .key").slice(1).join(",").includes("P"),
    texts(host, ".step .key").join(","),
  );

  host.remove();
}

/** A network volume that is not a mount-point child still counts. */
function coldNetwork() {
  forget();
  app.roots = ["/Users/t/nas/photos"];
  app.network = { "/Users/t/nas/photos": true };
  const host = stage(1000, 700);
  mount(ColdStart, { target: host });
  flushSync();

  eq(
    "network · the probe is enough for the eyebrow",
    text(host.querySelector(".eyebrow")),
    "card detected",
  );
  eq(
    "network · headline names it",
    text(host.querySelector(".headline")),
    "photos",
  );
  host.remove();

  // A plain local folder is not a card and must not claim to be one.
  forget();
  app.roots = ["/Users/t/Pictures/2026"];
  const plain = stage(1000, 700);
  mount(ColdStart, { target: plain });
  flushSync();
  eq(
    "network · a local folder gets no eyebrow",
    plain.querySelectorAll(".eyebrow").length,
    0,
  );
  eq("network · and no cull-in-place step", texts(plain, ".step .name"), [
    "open another folder",
    "keys",
  ]);
  plain.remove();
}

// ---- the session naming contract ---------------------------------------------

function session() {
  forget();
  app.roots = [CARD];
  const seen: string[] = [];
  const host = stage(1000, 700);
  mount(ColdStart, {
    target: host,
    props: { onname: (n: string) => seen.push(n) },
  });
  flushSync();

  const field = host.querySelector<HTMLInputElement>(".field")!;
  check("session · the field is drawn once a handler exists", field !== null);
  eq("session · seeded from the detected volume", field.value, "FUJI_SD");
  eq("session · and the seed is handed to the caller", seen.at(-1), "FUJI_SD");

  field.value = "  Iceland day 3  ";
  field.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
  eq(
    "session · what the caller receives is trimmed",
    seen.at(-1),
    "Iceland day 3",
  );
  eq("session · what the user sees is not", field.value, "  Iceland day 3  ");

  host.remove();
}

// ---- the steps do what they say ----------------------------------------------

function stepsRun() {
  forget();
  app.roots = [CARD];
  const host = stage(1000, 700);
  mount(ColdStart, { target: host });
  flushSync();

  const step = (name: string) =>
    [...host.querySelectorAll<HTMLElement>(".step")].find(
      (s) => text(s.querySelector(".name")) === name,
    );

  click(step("keys"));
  eq("steps · keys opens the overlay", app.overlay, true);
  app.overlay = false;

  let focused = 0;
  const previous = picker.focus;
  picker.focus = () => (focused += 1);
  click(step("open another folder"));
  // Revealing the sidebar is awaited, so the focus lands a microtask later.
  void Promise.resolve().then(() => {
    picker.focus = previous;
  });

  // cull in place goes through the same openFolder the path box uses, which
  // marks the scan synchronously before it reaches the backend.
  click(step("cull in place"));
  eq("steps · cull in place starts a scan of the card", app.scanning, CARD);
  app.scanning = null;

  return { host, focusedCount: () => focused };
}

// ---- the scanning presentation ------------------------------------------------

function scanning() {
  forget();
  app.scanning = DIR;
  const host = stage(1000, 700);
  mount(Loader, { target: host });
  flushSync();

  eq(
    "scan · the eyebrow says what is happening",
    text(host.querySelector(".eyebrow")),
    "indexing",
  );
  eq(
    "scan · in the design's indexing hue, not the brand's",
    getComputedStyle(host.querySelector<HTMLElement>(".eyebrow")!).color,
    token("--amber"),
  );
  eq(
    "scan · headline is the folder being read",
    text(host.querySelector(".headline")),
    "103_FUJI",
  );
  eq(
    "scan · and the full path is under it",
    text(host.querySelector(".meta")),
    DIR,
  );
  eq(
    "scan · the count is honest before the first event",
    text(host.querySelector(".count")),
    "counting…",
  );

  const track = host.querySelector<HTMLElement>(".track")!;
  eq(
    "scan · track is 5px",
    Math.round(track.getBoundingClientRect().height),
    5,
  );
  eq(
    "scan · indeterminate until the backend says",
    host.querySelectorAll(".fill.sweep").length,
    1,
  );
  eq(
    "scan · and claims no value while it is",
    track.getAttribute("aria-valuenow"),
    null,
  );
  eq(
    "scan · the reassurance is a fact, not a promise",
    text(host.querySelector(".note")),
    "RAW and JPEG halves are paired as they are read — nothing is written to the card.",
  );
  eq("scan · no slow hint yet", host.querySelectorAll(".slow").length, 0);

  // The first progress event turns it determinate. This is the behaviour the
  // old loader had and the one most easily lost: the bar has to be driven by
  // app.scanProgress, and the total has to reach the headline.
  app.scanProgress = { done: 412, total: 1204 };
  flushSync();
  eq(
    "scan · the count arrives",
    text(host.querySelector(".count")),
    "412 / 1,204",
  );
  eq(
    "scan · in the accent",
    getComputedStyle(host.querySelector<HTMLElement>(".count")!).color,
    token("--accent"),
  );
  eq(
    "scan · the headline gains the total",
    text(host.querySelector(".headline")),
    "103_FUJI · 1,204 frames",
  );
  eq(
    "scan · the sweep gives way to a real bar",
    host.querySelectorAll(".fill.sweep").length,
    0,
  );
  eq("scan · which reports itself", track.getAttribute("aria-valuenow"), "412");
  eq(
    "scan · against the right maximum",
    track.getAttribute("aria-valuemax"),
    "1204",
  );

  const fill = host.querySelector<HTMLElement>(".fill")!;
  const ratio =
    fill.getBoundingClientRect().width / track.getBoundingClientRect().width;
  check(
    "scan · the bar is 34% along",
    Math.abs(ratio - 412 / 1204) < 0.01,
    String(ratio),
  );
  eq(
    "scan · filled with the accent",
    getComputedStyle(fill).backgroundColor,
    token("--accent"),
  );

  // A total of zero would divide by zero; it has to read as unknown instead.
  app.scanProgress = { done: 0, total: 0 };
  flushSync();
  eq(
    "scan · a zero total falls back to indeterminate",
    host.querySelectorAll(".fill.sweep").length,
    1,
  );
  eq(
    "scan · and back to the honest count",
    text(host.querySelector(".count")),
    "counting…",
  );

  app.scanProgress = { done: 412, total: 1204 };
  app.scanSlow = true;
  flushSync();
  const slow = host.querySelector<HTMLElement>(".slow")!;
  eq(
    "scan · the slow hint survives the restyle",
    text(slow),
    "large or slow folder — still scanning",
  );
  eq(
    "scan · shown as a warning",
    getComputedStyle(slow).color,
    token("--amber"),
  );

  return host;
}

// ---- both screens are the same column ----------------------------------------

function sameColumn(cold: HTMLElement, scan: HTMLElement) {
  const width = (host: HTMLElement) =>
    Math.round(
      host.querySelector<HTMLElement>(".column")!.getBoundingClientRect().width,
    );
  eq("shell · the cold start is the design's 560px column", width(cold), 560);
  eq(
    "shell · and so is the scan, so ⏎ does not move the screen",
    width(scan),
    560,
  );
}

// ---- run ---------------------------------------------------------------------

/**
 * Anything Svelte complains about is a failure in its own right — an effect
 * that reads and writes the same state takes the effect tree down with it and
 * leaves a screen that renders once and then quietly stops updating.
 */
const complaints: string[] = [];
window.addEventListener("error", (e) => complaints.push(`error: ${e.message}`));
window.addEventListener("unhandledrejection", (e) => {
  const reason = String(e.reason);
  // openFolder is called for real and there is no backend behind it here.
  if (
    reason.includes("wails") ||
    reason.includes("fetch") ||
    reason.includes("Failed")
  )
    return;
  complaints.push(`rejection: ${reason}`);
});

async function run() {
  coldEmpty();
  coldCard();
  coldNetwork();
  session();

  const steps = stepsRun();
  const scan = scanning();
  await new Promise((r) => setTimeout(r, 50));
  eq(
    "steps · open another folder reaches the path box",
    steps.focusedCount(),
    1,
  );
  sameColumn(steps.host, scan);

  eq("nothing complained", complaints, []);

  // The bench is also what a screenshot of this page shows, and the assertions
  // above deliberately leave both screens in whatever state they finished in —
  // including a real openFolder still unwinding against a backend that is not
  // there. Put both back into the state worth looking at.
  app.roots = [CARD];
  app.scanning = DIR;
  app.scanProgress = { done: 412, total: 1204 };
  app.scanSlow = true;
  flushSync();

  const failed = results.filter((r) => !r.pass);
  document.getElementById("results")!.textContent = JSON.stringify(
    { total: results.length, failed: failed.length, results },
    null,
    1,
  );
  document.title =
    failed.length === 0
      ? `PASS ${results.length}/${results.length}`
      : `FAIL ${failed.length}`;
}

void run().catch((err: unknown) => {
  document.getElementById("results")!.textContent =
    `THREW ${String(err)}\n${(err as Error)?.stack ?? ""}`;
  document.title = "FAIL threw";
});
