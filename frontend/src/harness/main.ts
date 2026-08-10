// A headless bench for the loupe components.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the three components have to be
// verifiable on their own — a verdict card in each of its states, a filmstrip
// actually being fed by the image queue, arrow keys reaching the callback and
// not the window as well — without a folder of photographs, a backend, or a
// person to look at it.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9346
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1600,1000 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9346/src/harness/index.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount } from "svelte";
import type { GroupDTO } from "../lib/bindings";
import { app, loupe } from "../lib/state.svelte";
import Filmstrip from "../components/Filmstrip.svelte";
import LoupeFirst from "../components/LoupeFirst.svelte";
import LoupeOverlay from "../components/LoupeOverlay.svelte";

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
    verb: "",
    decision: "",
    ...over,
  };
}

/** One of every state the components have to draw. */
const groups: GroupDTO[] = [
  frame(0, { verdict: "keep", mask: "rj", rating: 3 }),
  frame(1, { verdict: "keep", mask: "r", rating: 5 }),
  frame(2, { verdict: "cut", mask: "rj" }),
  frame(3),
  frame(4, { verdict: "keep", mask: "j" }),
  frame(5, { kind: "raw-only", hasJpeg: false, jpegPath: "", verdict: "keep", mask: "rj" }),
  frame(6, { kind: "jpeg-only", hasRaw: false, rawPath: "", verdict: "cut" }),
  frame(7, { warnings: ["sidecar could not be read"], sidecars: 2 }),
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

function press(el: Element, key: string): KeyboardEvent {
  const e = new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true });
  el.dispatchEvent(e);
  return e;
}

function click(el: Element) {
  (el as HTMLElement).click();
  flushSync();
}

const settle = (ms: number) => new Promise((r) => setTimeout(r, ms));

// ---- the filmstrip, at the loupe-first size ----------------------------------

function filmstripLarge() {
  const picked: number[] = [];
  const host = stage(1000, 140);
  mount(Filmstrip, {
    target: host,
    props: { groups, index: 1, onselect: (i: number) => picked.push(i) },
  });
  flushSync();

  const strip = host.querySelector<HTMLElement>(".strip")!;
  const frames = [...host.querySelectorAll<HTMLElement>(".frame")];

  eq("filmstrip · one frame per group", frames.length, groups.length);
  eq("filmstrip · strip height 104", Math.round(strip.getBoundingClientRect().height), 104);

  const thumb = frames[0].querySelector<HTMLElement>(".thumb")!.getBoundingClientRect();
  eq("filmstrip · thumb 112 wide", Math.round(thumb.width), 112);
  eq("filmstrip · thumb 72 tall", Math.round(thumb.height), 72);

  // Verdict stripes: hue by verdict, invisible when nothing is decided.
  const stripe = (i: number) => frames[i].querySelector<HTMLElement>(".stripe")!;
  eq("filmstrip · stripe 3px", Math.round(stripe(0).getBoundingClientRect().height), 3);
  eq("filmstrip · keep stripe is the keep hue", getComputedStyle(stripe(0)).backgroundColor, token("--keep"));
  eq("filmstrip · cut stripe is the cut hue", getComputedStyle(stripe(2)).backgroundColor, token("--cut"));
  eq("filmstrip · undecided stripe hidden", getComputedStyle(stripe(3)).opacity, "0");

  // R/J pair: kept, cut and absent are three different things.
  const halves = (i: number) => [...frames[i].querySelectorAll<HTMLElement>(".half")];
  eq("filmstrip · R/J pair drawn", halves(0).length, 2);
  check(
    "filmstrip · keep with a RAW-only mask cuts the J",
    halves(1)[0].className.includes("kept") && halves(1)[1].className.includes("cut"),
    halves(1).map((h) => h.className).join(" / "),
  );
  check(
    "filmstrip · a missing half reads as absent",
    halves(5)[1].className.includes("absent"),
    halves(5)[1].className,
  );
  check(
    "filmstrip · a cut J is struck through",
    getComputedStyle(halves(1)[1]).textDecorationLine === "line-through",
    getComputedStyle(halves(1)[1]).textDecorationLine,
  );

  eq("filmstrip · stem caption", text(frames[3].querySelector(".stem")), groups[3].stem);
  eq("filmstrip · focused frame is marked", frames[1].getAttribute("aria-selected"), "true");
  check(
    "filmstrip · focused frame carries the focus ring",
    getComputedStyle(frames[1].querySelector(".thumb")!).boxShadow !== "none",
    getComputedStyle(frames[1].querySelector(".thumb")!).boxShadow,
  );

  // Clicks and arrows both go out through the one callback.
  click(frames[4]);
  eq("filmstrip · click selects that frame", picked.at(-1), 4);

  press(strip, "ArrowRight");
  eq("filmstrip · right steps forward", picked.at(-1), 2);
  press(strip, "ArrowLeft");
  eq("filmstrip · left steps back", picked.at(-1), 0);
  press(strip, "Home");
  eq("filmstrip · Home goes to the first", picked.at(-1), 0);
  press(strip, "End");
  eq("filmstrip · End goes to the last", picked.at(-1), groups.length - 1);

  // The application's key listener sits on the window and binds the same
  // arrows, so the strip has to stop them — and must not stop anything else.
  let sawArrow = false;
  let sawLetter = false;
  const spy = (e: Event) => {
    if ((e as KeyboardEvent).key === "ArrowRight") sawArrow = true;
    if ((e as KeyboardEvent).key === "k") sawLetter = true;
  };
  window.addEventListener("keydown", spy);
  press(strip, "ArrowRight");
  press(strip, "k");
  window.removeEventListener("keydown", spy);
  check("filmstrip · arrows do not also reach the window", !sawArrow, "the window saw ArrowRight too");
  check("filmstrip · other keys still reach the window", sawLetter, "the window never saw k");

  return host;
}

// ---- the filmstrip, at the overlay size --------------------------------------

function filmstripSmall() {
  const host = stage(1000, 100);
  mount(Filmstrip, {
    target: host,
    props: {
      groups,
      index: 0,
      onselect: () => {},
      height: 78,
      thumbWidth: 96,
      thumbHeight: 62,
      caption: false,
      badges: false,
      surface: false,
    },
  });
  flushSync();

  const strip = host.querySelector<HTMLElement>(".strip")!;
  eq("filmstrip · overlay height 78", Math.round(strip.getBoundingClientRect().height), 78);
  const thumb = host.querySelector<HTMLElement>(".thumb")!.getBoundingClientRect();
  eq("filmstrip · overlay thumb 96 wide", Math.round(thumb.width), 96);
  eq("filmstrip · overlay thumb 62 tall", Math.round(thumb.height), 62);
  eq("filmstrip · captions off", host.querySelectorAll(".stem").length, 0);
  eq("filmstrip · badges off", host.querySelectorAll(".half").length, 0);
  eq("filmstrip · stripes stay on", host.querySelectorAll(".stripe").length, groups.length);
}

// ---- the image queue ---------------------------------------------------------

// Runs before anything else is mounted, and deliberately so: the queue only
// loads what is near the viewport, and a strip pushed down the page by half a
// dozen other mounts is one it is right to leave alone.
async function queueFeeds() {
  const host = stage(1000, 140);
  mount(Filmstrip, { target: host, props: { groups, index: 0, onselect: () => {} } });
  flushSync();

  const imgs = () => [...host.querySelectorAll<HTMLImageElement>(".thumb img")];
  const withSrc = () => imgs().filter((i) => i.hasAttribute("src")).length;

  eq("queue · thumbs count", imgs().length, groups.length);
  // Nothing is handed a src by the template. If these were plain <img src> the
  // whole strip would be in flight before this line ran.
  eq("queue · no src until the queue says so", withSrc(), 0);

  await settle(2500);
  check("queue · the queue feeds them", withSrc() > 0, `${withSrc()} of ${imgs().length} were given a src`);
  check(
    "queue · srcs are preview URLs",
    imgs().every((i) => !i.hasAttribute("src") || i.getAttribute("src")!.startsWith("/preview?path=")),
    imgs()[0]?.getAttribute("src") ?? "",
  );
}

// ---- loupe-first (1b) --------------------------------------------------------

function loupeFirst() {
  const verdicts: string[] = [];
  const ratings: number[] = [];
  const host = stage(1440, 820);
  mount(LoupeFirst, {
    target: host,
    props: {
      groups,
      index: 1,
      onfocus: () => {},
      onverdict: (v: "keep" | "cut") => verdicts.push(v),
      onrating: (n: number) => ratings.push(n),
    },
  });
  flushSync();

  const panel = host.querySelector<HTMLElement>(".panel")!;
  eq("loupe-first · rail is 250px", Math.round(panel.getBoundingClientRect().width), 250);
  eq("loupe-first · filename", text(panel.querySelector(".name")), groups[1].stem);
  check(
    "loupe-first · date and time",
    /^\d{4}-\d{2}-\d{2} · \d{2}:\d{2}:\d{2}$/.test(text(panel.querySelector(".when"))),
    text(panel.querySelector(".when")),
  );

  // Rating dots.
  const dots = [...panel.querySelectorAll<HTMLElement>(".dot")];
  eq("loupe-first · five rating dots", dots.length, 5);
  eq("loupe-first · dot is 10px", Math.round(dots[0].getBoundingClientRect().width), 10);
  eq("loupe-first · rating 5 fills five dots", dots.filter((d) => d.className.includes("on")).length, 5);
  eq("loupe-first · rate hint", text(panel.querySelector(".hint")), "1–5 to rate");
  click(dots[3]);
  eq("loupe-first · clicking the fourth dot rates 4", ratings.at(-1), 4);

  // Verdict cards.
  const cards = [...panel.querySelectorAll<HTMLElement>(".card")];
  eq("loupe-first · two verdict cards", cards.length, 2);
  eq("loupe-first · KEEP card", text(cards[0].querySelector(".vname")), "KEEP");
  eq("loupe-first · CUT card", text(cards[1].querySelector(".vname")), "CUT");
  eq("loupe-first · keys shown", texts(panel, ".vkey").join(""), "kx");
  eq("loupe-first · a kept frame marks KEEP active", cards[0].getAttribute("aria-pressed"), "true");
  eq("loupe-first · and CUT inactive", cards[1].getAttribute("aria-pressed"), "false");
  eq("loupe-first · active KEEP takes the keep border", getComputedStyle(cards[0]).borderTopColor, token("--keep"));
  eq(
    "loupe-first · inactive CUT takes the strong border",
    getComputedStyle(cards[1]).borderTopColor,
    token("--border-strong"),
  );
  check(
    "loupe-first · active KEEP is washed, not filled",
    getComputedStyle(cards[0]).backgroundColor.startsWith("rgba("),
    getComputedStyle(cards[0]).backgroundColor,
  );
  click(cards[1]);
  eq("loupe-first · clicking CUT asks for a cut", verdicts.at(-1), "cut");
  click(cards[0]);
  eq("loupe-first · clicking KEEP asks for a keep", verdicts.at(-1), "keep");

  // Files kept.
  const rows = [...panel.querySelectorAll<HTMLElement>(".file")];
  eq("loupe-first · a row per present half", rows.length, 2);
  eq("loupe-first · RAW row", text(rows[0].querySelector(".fname")), "DSCF1201.RAF");
  check("loupe-first · the kept half reads kept", rows[0].className.includes("kept"), rows[0].className);
  check("loupe-first · the masked-out half reads cut", rows[1].className.includes("cut"), rows[1].className);
  eq(
    "loupe-first · the cut file is struck through",
    getComputedStyle(rows[1].querySelector(".fname")!).textDecorationLine,
    "line-through",
  );
  eq("loupe-first · no size without bytes", rows[0].querySelectorAll(".fsize").length, 0);

  // Stacked facts, and only facts the scan actually has.
  eq("loupe-first · section titles", texts(panel, ".rule .label").join(","), "Frame,Files");
  const key = panel.querySelector<HTMLElement>(".fkey")!;
  const val = panel.querySelector<HTMLElement>(".fval")!;
  check(
    "loupe-first · facts stack key above value",
    key.getBoundingClientRect().bottom <= val.getBoundingClientRect().top + 1,
    `key bottom ${key.getBoundingClientRect().bottom}, value top ${val.getBoundingClientRect().top}`,
  );
  const invented = /\bISO\b|ƒ|f\/\d|1\/\d+s|\d+mm/.exec(panel.textContent ?? "");
  check("loupe-first · no invented EXIF", invented === null, `found ${invented?.[0] ?? ""}`);

  // The stage.
  eq("loupe-first · verdict badge", text(host.querySelector(".verdict-badge")), "KEEP · RAW ONLY");
  eq(
    "loupe-first · badge is glassy",
    getComputedStyle(host.querySelector<HTMLElement>(".verdict-badge")!).backgroundColor,
    "rgba(14, 16, 19, 0.86)",
  );
  eq("loupe-first · hint chips", texts(host, ".hints .chip").join(" · "), "Z 1:1 · C compare · Tab grid");
  check(
    "loupe-first · chips take no pointer events",
    getComputedStyle(host.querySelector<HTMLElement>(".float")!).pointerEvents === "none",
    getComputedStyle(host.querySelector<HTMLElement>(".float")!).pointerEvents,
  );
  eq(
    "loupe-first · filmstrip beneath the stage",
    Math.round(host.querySelector<HTMLElement>(".strip")!.getBoundingClientRect().height),
    104,
  );

  return { host, verdicts, ratings };
}

function loupeFirstStates() {
  // Undecided, cut, single-file and warned frames all have to draw.
  const cases: { index: number; badge: string; rows: number }[] = [
    { index: 3, badge: "UNDECIDED", rows: 2 },
    { index: 2, badge: "CUT", rows: 2 },
    { index: 5, badge: "KEEP", rows: 1 },
    { index: 0, badge: "KEEP · RAW + JPEG", rows: 2 },
    { index: 4, badge: "KEEP · JPEG ONLY", rows: 2 },
  ];
  for (const c of cases) {
    const host = stage(1000, 620);
    mount(LoupeFirst, {
      target: host,
      props: { groups, index: c.index, onfocus: () => {}, onverdict: () => {}, onrating: () => {} },
    });
    flushSync();
    eq(`loupe-first · badge for frame ${c.index}`, text(host.querySelector(".verdict-badge")), c.badge);
    eq(`loupe-first · file rows for frame ${c.index}`, host.querySelectorAll(".file").length, c.rows);
    host.remove();
  }

  const warned = stage(1000, 620);
  mount(LoupeFirst, {
    target: warned,
    props: { groups, index: 7, onfocus: () => {}, onverdict: () => {}, onrating: () => {} },
  });
  flushSync();
  check(
    "loupe-first · warnings get their own section",
    texts(warned, ".rule .label").includes("Warnings"),
    texts(warned, ".rule .label").join(","),
  );
  check(
    "loupe-first · the warning is shown in amber",
    getComputedStyle(warned.querySelector<HTMLElement>(".fval.warn")!).color === token("--amber"),
    getComputedStyle(warned.querySelector<HTMLElement>(".fval.warn")!).color,
  );
  eq("loupe-first · undecided frame fills no dots", warned.querySelectorAll(".dot.on").length, 0);
  warned.remove();

  // Sizes appear the moment a caller has them.
  const sized = stage(1000, 620);
  mount(LoupeFirst, {
    target: sized,
    props: {
      groups,
      index: 1,
      onfocus: () => {},
      onverdict: () => {},
      onrating: () => {},
      bytes: { raw: 56_819_712, jpeg: 12_373_196 },
    },
  });
  flushSync();
  // Sizes go through the application's own formatter, which drops the decimal
  // above 10 units — 54 MB where the design mock drew 54.2 MB.
  eq("loupe-first · kept size", text(sized.querySelectorAll(".file")[0].querySelector(".fsize")), "54 MB");
  eq("loupe-first · cut size is a deduction", text(sized.querySelectorAll(".file")[1].querySelector(".fsize")), "−12 MB");
  sized.remove();
}

// ---- the overlay (2b) --------------------------------------------------------

function loupeOverlay() {
  const ratings: number[] = [];
  const picked: number[] = [];
  const host = stage(1440, 800);
  mount(LoupeOverlay, {
    target: host,
    props: {
      groups,
      index: 0,
      onfocus: (i: number) => picked.push(i),
      onrating: (n: number) => ratings.push(n),
    },
  });
  flushSync();

  const overlay = host.querySelector<HTMLElement>(".overlay")!;
  const style = getComputedStyle(overlay);
  eq("overlay · sits on the loupe scrim", style.backgroundColor, "rgba(9, 10, 12, 0.82)");
  eq("overlay · padding", `${style.paddingTop} ${style.paddingRight} ${style.paddingBottom}`, "26px 30px 20px");
  check("overlay · covers its parent", style.position === "absolute", style.position);

  const card = host.querySelector<HTMLElement>(".card")!;
  eq("overlay · info card is 268px", Math.round(card.getBoundingClientRect().width), 268);
  eq("overlay · filename", text(card.querySelector(".name")), groups[0].stem);
  eq("overlay · five dots", card.querySelectorAll(".dot").length, 5);
  eq("overlay · rating 3 fills three", card.querySelectorAll(".dot.on").length, 3);
  click(card.querySelectorAll(".dot")[1]);
  eq("overlay · clicking the second dot rates 2", ratings.at(-1), 2);

  eq("overlay · file rows", card.querySelectorAll(".file").length, 2);
  check(
    "overlay · both halves kept under an unmasked keep",
    [...card.querySelectorAll(".file")].every((r) => r.className.includes("kept")),
    [...card.querySelectorAll(".file")].map((r) => r.className).join(" / "),
  );

  // Here the key sits beside its value, not above it.
  const key = card.querySelector<HTMLElement>(".fkey")!;
  const val = card.querySelector<HTMLElement>(".fval")!;
  eq("overlay · key column is 78px", Math.round(key.getBoundingClientRect().width), 78);
  check(
    "overlay · facts run key beside value",
    Math.abs(key.getBoundingClientRect().top - val.getBoundingClientRect().top) < 2,
    `key ${key.getBoundingClientRect().top}, value ${val.getBoundingClientRect().top}`,
  );
  eq("overlay · fact row height", Math.round(key.parentElement!.getBoundingClientRect().height), 19);

  eq("overlay · verdict badge", text(host.querySelector(".verdict-badge")), "KEEP · RAW + JPEG");
  eq("overlay · hint chips", texts(host, ".hints .chip").join(" · "), "z 1:1 · c compare · space close");

  const strip = host.querySelector<HTMLElement>(".strip")!;
  eq("overlay · filmstrip is 78px", Math.round(strip.getBoundingClientRect().height), 78);
  eq("overlay · filmstrip has no captions", strip.querySelectorAll(".stem").length, 0);
  const thumb = strip.querySelector<HTMLElement>(".thumb")!.getBoundingClientRect();
  eq("overlay · filmstrip thumb 96×62", `${Math.round(thumb.width)}×${Math.round(thumb.height)}`, "96×62");
  eq("overlay · filmstrip keeps its verdict stripes", strip.querySelectorAll(".stripe").length, groups.length);
  click(strip.querySelectorAll(".frame")[6]);
  eq("overlay · the strip moves the app's focus", picked.at(-1), 6);
}

// ---- the stage frames the photograph -----------------------------------------

// The overlay used to hang the image off the bottom of its stage: a grid item's
// `max-height: 100%` resolves against its track, and the single implicit row was
// auto-sized to the photograph, so the ceiling never bit and a 3:2 frame ran 68px
// past the clip. The image has to sit inside the stage, centred, at any window
// the app can be dragged to — which is the point of checking three of them.
//
// The preview endpoint is not up under the bench, so the stage is given an SVG
// with real intrinsic dimensions. Layout does not care where the pixels came
// from; it cares that the element reports 3000×2000.
/**
 * inner is the box an image actually has to fit inside: the element's rectangle
 * less its border and its padding. The overlay's stage is framed and 1b's is
 * padded, and measuring either against `getBoundingClientRect` would call a
 * correctly sized image two pixels short.
 */
function inner(el: HTMLElement): DOMRect {
  const r = el.getBoundingClientRect();
  const s = getComputedStyle(el);
  const px = (v: string) => parseFloat(v) || 0;
  const left = r.left + px(s.borderLeftWidth) + px(s.paddingLeft);
  const top = r.top + px(s.borderTopWidth) + px(s.paddingTop);
  const right = r.right - px(s.borderRightWidth) - px(s.paddingRight);
  const bottom = r.bottom - px(s.borderBottomWidth) - px(s.paddingBottom);
  return new DOMRect(left, top, right - left, bottom - top);
}

async function loupeFraming() {
  const shapes: [number, number][] = [
    [3000, 2000], // landscape, wider than the stage
    [2000, 3000], // portrait, taller than the stage
  ];
  const windows: [number, number][] = [
    [1440, 820],
    [1024, 640],
    [1920, 1200],
  ];

  for (const [w, h] of windows) {
    for (const [nw, nh] of shapes) {
      const host = stage(w, h);
      mount(LoupeOverlay, { target: host, props: { groups, index: 0, onfocus: () => {}, onrating: () => {} } });
      flushSync();

      const box = host.querySelector<HTMLElement>(".stage")!;
      const img = host.querySelector<HTMLImageElement>(".stage img")!;
      await new Promise<void>((done) => {
        img.onload = () => done();
        img.onerror = () => done();
        img.src =
          "data:image/svg+xml;utf8," +
          encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="${nw}" height="${nh}"></svg>`);
      });
      flushSync();

      const s = inner(box);
      const i = img.getBoundingClientRect();
      const shape = `${nw}×${nh} at ${w}×${h}`;
      const seen = `stage ${Math.round(s.width)}×${Math.round(s.height)}, image ${Math.round(i.width)}×${Math.round(i.height)} at ${Math.round(i.left - s.left)},${Math.round(i.top - s.top)}`;
      check(
        `loupe · ${shape} · the whole image is inside the stage`,
        i.left >= s.left - 1 && i.right <= s.right + 1 && i.top >= s.top - 1 && i.bottom <= s.bottom + 1,
        seen,
      );
      check(
        `loupe · ${shape} · and centred in it`,
        Math.abs(i.left - s.left - (s.right - i.right)) <= 1 && Math.abs(i.top - s.top - (s.bottom - i.bottom)) <= 1,
        seen,
      );
      check(
        `loupe · ${shape} · at the largest size that fits`,
        Math.abs(s.width - i.width) <= 1 || Math.abs(s.height - i.height) <= 1,
        seen,
      );
      check(`loupe · ${shape} · aspect ratio kept`, Math.abs(i.width / i.height - nw / nh) < 0.01, seen);

      host.remove();
    }
  }

  // 1b draws the same stage with 22px of breathing room, which the image has to
  // stay inside rather than merely inside the clip.
  const host = stage(1280, 760);
  mount(LoupeFirst, {
    target: host,
    props: { groups, index: 0, onfocus: () => {}, onverdict: () => {}, onrating: () => {} },
  });
  flushSync();
  const box = host.querySelector<HTMLElement>(".stage")!;
  const img = host.querySelector<HTMLImageElement>(".stage img")!;
  await new Promise<void>((done) => {
    img.onload = () => done();
    img.onerror = () => done();
    img.src =
      "data:image/svg+xml;utf8," +
      encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="3000" height="2000"></svg>');
  });
  flushSync();
  const s = inner(box);
  const i = img.getBoundingClientRect();
  const outer = box.getBoundingClientRect();
  check(
    "loupe-first · the image stays inside the stage's 22px padding",
    i.left >= outer.left + 21 && i.right <= outer.right - 21 && i.top >= outer.top + 21 && i.bottom <= outer.bottom - 21,
    `stage ${Math.round(outer.width)}×${Math.round(outer.height)}, image ${Math.round(i.width)}×${Math.round(i.height)} at ${Math.round(i.left - outer.left)},${Math.round(i.top - outer.top)}`,
  );
  check(
    "loupe-first · and fills what the padding leaves",
    Math.abs(s.width - i.width) <= 1 || Math.abs(s.height - i.height) <= 1,
    `available ${Math.round(s.width)}×${Math.round(s.height)}, image ${Math.round(i.width)}×${Math.round(i.height)}`,
  );

  // The zoom factor is read off the laid-out width, so a change to how the
  // image is sized is a change to what 1:1 means. z has to still put one image
  // pixel on one screen pixel, and the arrows have to still pan within it.
  const fitted = i.width;
  app.zoom = true;
  flushSync();
  const zoomed = img.getBoundingClientRect();
  eq("loupe · z renders the image at its own pixel count", Math.round(zoomed.width), 3000);
  check(
    "loupe · and that is a real magnification of the fitted size",
    zoomed.width > fitted * 2,
    `fitted ${Math.round(fitted)}, zoomed ${Math.round(zoomed.width)}`,
  );

  app.panX = 0;
  app.panY = 0;
  loupe.pan(-400, 0);
  flushSync();
  check("loupe · arrows pan while zoomed", app.panX !== 0, `panX ${app.panX}`);

  // Far enough to be against the stop, then further: the second press must not
  // move it, or the photograph slides off and leaves the background showing.
  loupe.pan(-40000, 0);
  flushSync();
  const limit = app.panX;
  loupe.pan(-40000, 0);
  flushSync();
  eq("loupe · and a pan is clamped to the part that is off stage", app.panX, limit);
  const scale = 3000 / fitted;
  check(
    "loupe · the stop is exactly the overhang, so no background shows",
    Math.abs(Math.abs(limit) * scale - (3000 - box.clientWidth) / 2) <= 1,
    `stopped at ${Math.round(Math.abs(limit) * scale)} of ${Math.round((3000 - box.clientWidth) / 2)} available`,
  );

  app.zoom = false;
  app.panX = 0;
  app.panY = 0;
  host.remove();
}

// ---- run ---------------------------------------------------------------------

async function run() {
  // The default cut scope, as the config layer would have set it.
  app.cutRemoves = "both";

  await queueFeeds();
  filmstripLarge();
  filmstripSmall();
  loupeFirst();
  loupeFirstStates();
  loupeOverlay();
  await loupeFraming();

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
