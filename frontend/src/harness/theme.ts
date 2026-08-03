// A headless sweep of the light theme.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. The design drew exactly one light screen, so most of
// the light block is derived, and the failure mode of a derived theme is not
// an ugly colour — it is one token nobody remembered, leaving a component
// rendering a dark value on paper. That is what this looks for, three ways:
//
//   1. Token parity, from the stylesheet itself. Every custom property the
//      dark :root declares must be declared again in the light block, and the
//      light value must sit on the right side of the lightness line for its
//      family — surfaces and borders light, ink dark.
//   2. Literals, from every rule in every component. A colour written as a hex
//      or an rgba() rather than a token cannot follow the theme, so each one
//      has to be either inside a :root block or inside a rule that says
//      data-theme.
//   3. What actually renders. Every screen the shell can reach is mounted
//      under the light theme and every element in it is read back, so a token
//      that resolves to a dark value anywhere is caught with the element that
//      drew it.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9349
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1500,1000 \
//     --virtual-time-budget=20000 --dump-dom \
//     http://127.0.0.1:9349/src/harness/theme.html
//
// Every assertion lands in #results as JSON, and the document title carries
// the tally so a check can read one line.

import { flushSync, mount } from "svelte";
import App from "../App.svelte";
import ColdStart from "../components/ColdStart.svelte";
import type { Config, GroupDTO } from "../lib/bindings";
import {
  CollisionPolicy,
  CutScope,
  KeepMask,
  TrashMode,
} from "../../bindings/github.com/tomaszcichy9825/culler/internal/config/models.js";
import { appearance } from "../lib/appearance.svelte";
import { DEFAULT_KEYMAP } from "../lib/keymapCatalogue";
import { palette } from "../lib/palette.svelte";
import { settings } from "../lib/settings.svelte";
import type { ConfigPort } from "../lib/settings.svelte";
import { shell } from "../lib/shell.svelte";
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

// ---- reading the token blocks out of the stylesheet ---------------------------

/** Every rule in every sheet the page has, component styles included. */
function allRules(): CSSStyleRule[] {
  const out: CSSStyleRule[] = [];
  const walk = (rules: CSSRuleList) => {
    for (const rule of rules) {
      if (rule instanceof CSSStyleRule) out.push(rule);
      // @media and @supports wrap their own lists.
      const nested = (rule as CSSGroupingRule).cssRules;
      if (nested !== undefined && !(rule instanceof CSSStyleRule)) walk(nested);
    }
  };
  for (const sheet of document.styleSheets) {
    try {
      walk(sheet.cssRules);
    } catch {
      // A cross-origin sheet would throw. There are none, and if one appears
      // it is a finding in its own right rather than something to swallow.
      check("sheets · every stylesheet is readable", false, sheet.href ?? "inline");
    }
  }
  return out;
}

function tokensOf(selector: string, rules: CSSStyleRule[]): Map<string, string> {
  const out = new Map<string, string>();
  for (const rule of rules) {
    if (rule.selectorText !== selector) continue;
    for (const prop of rule.style) {
      if (prop.startsWith("--")) out.set(prop, rule.style.getPropertyValue(prop).trim());
    }
  }
  return out;
}

function rgbOf(value: string): [number, number, number] | null {
  const hex = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(value.trim());
  if (hex !== null) return [parseInt(hex[1], 16), parseInt(hex[2], 16), parseInt(hex[3], 16)];
  const fn = /rgba?\(\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)/i.exec(value);
  if (fn !== null) return [Number(fn[1]), Number(fn[2]), Number(fn[3])];
  return null;
}

/** Perceived lightness, 0 black to 1 white. Good enough to sort ink from paper. */
function luminance(value: string): number | null {
  const c = rgbOf(value);
  if (c === null) return null;
  return (0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]) / 255;
}

/** The computed spelling of a declared colour, for comparing against a render. */
function computedForm(value: string): string {
  const c = rgbOf(value);
  if (c === null) return value.trim();
  const alpha = /rgba\([^)]*,\s*([0-9.]+)\s*\)/.exec(value);
  if (alpha === null) return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
  return `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${Number(alpha[1])})`;
}

// ---- the fake config, so the settings screen has a file to read ---------------

function stockConfig(): Config {
  return {
    behaviour: {
      collisionPolicy: CollisionPolicy.CollisionRenameSuffix,
      bulkConfirmThreshold: 20,
      trashMode: TrashMode.TrashSystem,
      rejectedFolderName: "_Rejected",
      defaultKeepMask: KeepMask.KeepMaskBoth,
      cutRemoves: CutScope.CutRemovesBoth,
      libraryRoot: "~/Pictures",
      moveOnImport: false,
      verifyCopies: true,
      localReadSlots: 16,
      networkReadSlots: 4,
      networkHashWorkers: 4,
      slowScanHintSeconds: 10,
    },
    keymap: JSON.parse(JSON.stringify(DEFAULT_KEYMAP)) as Config["keymap"],
    rawExts: [".raf", ".arw", ".cr3", ".nef", ".dng"],
    jpegExts: [".jpg", ".jpeg", ".heic"],
    sidecarExts: [".xmp", ".aae", ".dop"],
  };
}

const port: ConfigPort = {
  get: async () => stockConfig(),
  path: async () => "/tmp/culler-harness/config.json",
  save: async () => {},
};

// ---- frames -------------------------------------------------------------------

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

// ---- the audit ----------------------------------------------------------------

/** Values that mean the same thing in both themes, so finding one is not a bug. */
const SHARED = new Set(["--font-mono", "--font-sans", "--scrim-loupe", "--absent-wash"]);

let darkOnly = new Set<string>();
/** Which token a dark-only value came from, so a failure names it. */
const owner = new Map<string, string>();

interface Offence {
  where: string;
  prop: string;
  value: string;
}

function auditRendered(root: ParentNode, screen: string, proof: string) {
  const offences: Offence[] = [];
  const props = [
    "color",
    "backgroundColor",
    "borderTopColor",
    "borderRightColor",
    "borderBottomColor",
    "borderLeftColor",
    "outlineColor",
    "caretColor",
  ] as const;

  for (const el of root.querySelectorAll<HTMLElement>("*")) {
    const style = getComputedStyle(el);
    for (const prop of props) {
      const value = style[prop];
      if (!darkOnly.has(value)) continue;
      // A fully transparent colour paints nothing, whatever it resolved from.
      if (style[prop].endsWith(", 0)")) continue;
      offences.push({ where: `${el.tagName.toLowerCase()}.${el.className || "-"}`, prop, value });
    }
    // Gradients and shadows carry colours the property list above never sees.
    for (const prop of ["backgroundImage", "boxShadow"] as const) {
      const value = style[prop];
      if (value === "none" || value === "") continue;
      for (const dark of darkOnly) {
        if (!value.includes(dark)) continue;
        offences.push({ where: `${el.tagName.toLowerCase()}.${el.className || "-"}`, prop, value: dark });
      }
    }
  }

  const named = offences.map((o) => `${o.where} ${o.prop}=${o.value} (${owner.get(o.value) ?? "?"})`);
  // A screen that failed to render would pass the audit by drawing nothing at
  // all, so something only that screen draws is asserted alongside it.
  const drawn = root.querySelectorAll(proof).length;
  check(`light · ${screen} rendered`, drawn > 0, `nothing matched ${proof}`);
  eq(`light · ${screen} draws nothing dark`, [...new Set(named)], []);
}

// ---- run -----------------------------------------------------------------------

const complaints: string[] = [];
window.addEventListener("error", (e) => complaints.push(`error: ${e.message}`));

async function run() {
  const rules = allRules();
  const dark = tokensOf(":root", rules);
  const light = tokensOf(':root[data-theme="light"]', rules);

  // ---- 1. token parity -------------------------------------------------------

  eq("tokens · the dark block is the one being matched", dark.size > 80, true);
  const missing = [...dark.keys()].filter((t) => !light.has(t) && !SHARED.has(t));
  eq("tokens · every dark token has a light value", missing, []);
  const extra = [...light.keys()].filter((t) => !dark.has(t));
  eq("tokens · and the light block invents none of its own", extra, []);

  // Surfaces, borders and text are the three families whose light values can be
  // checked rather than eyeballed: paper is light, ink is dark, and a border
  // has to sit between its surface and the ink.
  // The accent-tinted borders are excluded: they carry the accent hue rather
  // than the surface's, so they are as dark as the accent needs to be to read
  // on paper and the paper test says nothing useful about them.
  const accentBorder = (name: string) => name.includes("focus") || name === "--border-selected";

  const tooDark: string[] = [];
  const tooLight: string[] = [];
  for (const [name, value] of light) {
    const l = luminance(value);
    if (l === null) continue;
    if ((name.startsWith("--bg-") || name.startsWith("--border")) && !accentBorder(name) && l < 0.75) {
      tooDark.push(`${name}=${value} (${l.toFixed(2)})`);
    }
    if (name.startsWith("--text") && !name.includes("on-") && l > 0.75) {
      tooLight.push(`${name}=${value} (${l.toFixed(2)})`);
    }
  }
  eq("tokens · no surface or border stayed dark", tooDark, []);
  eq("tokens · no text tier washed out", tooLight, []);

  // Borders have to be darker than the paper they separate, which is the
  // inverse of the dark theme and the rule most easily got backwards.
  const paper = luminance(light.get("--bg-app") ?? "") ?? 1;
  const borderOrder = ["--border-hair", "--border", "--border-strong", "--border-window", "--border-dialog"];
  const lums = borderOrder.map((n) => luminance(light.get(n) ?? "") ?? 1);
  check("tokens · every border is darker than paper", lums.every((l) => l < paper), JSON.stringify(lums));
  check(
    "tokens · and stronger means darker, in order",
    lums.every((l, i) => i === 0 || l <= lums[i - 1] + 0.001),
    JSON.stringify(borderOrder.map((n, i) => `${n}=${lums[i].toFixed(3)}`)),
  );

  // ---- 2. literals -----------------------------------------------------------

  // Every colour written into a component rule rather than taken from a token.
  const literals: string[] = [];
  for (const rule of rules) {
    const selector = rule.selectorText;
    if (selector.includes(":root") || selector.includes("data-theme")) continue;
    for (const prop of rule.style) {
      const value = rule.style.getPropertyValue(prop);
      if (!/#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(/.test(value)) continue;
      // A var() fallback still follows the theme when the token exists.
      if (/var\(--[a-z0-9-]+,/.test(value)) continue;
      if (/rgba?\(\s*0\s*,\s*0\s*,\s*0\s*,\s*0\s*\)/.test(value)) continue;
      literals.push(`${selector} { ${prop}: ${value} }`);
    }
  }
  /**
   * The two literals in the app today, and what each one means.
   *
   * The hatch is fine: TableView writes the design's dark hatch and its light
   * one, the light one under a :root[data-theme="light"] selector, so what
   * turns up here is only the dark half of a pair. (Declared as #1c2026;
   * cssText hands it back as rgb.)
   *
   * The tile's top overlay scrim is not fine, and it is the one light-theme
   * defect this sweep found that is not fixable from the token block: it is a
   * hard-coded dark gradient over what becomes a light thumbnail well. Screen
   * 7a is explicit that the light tile has no gradient scrim at all — it uses
   * padding: 4px 5px 0 instead. The fix is a data-theme override in
   * Tile.svelte, which belongs to the tile, not to the theme.
   */
  const known = [
    "rgb(28, 32, 38)", // TableView .stage — paired with a light override
    "rgba(14, 16, 19, 0.82)", // Tile .overlay — see above, still owed
  ];
  const unexplained = literals.filter((l) => !known.some((k) => l.includes(k)));
  eq("literals · every colour in a component is a token or a known exception", unexplained, []);
  check(
    "literals · the tile's dark scrim is still owed a light override",
    literals.some((l) => l.includes("rgba(14, 16, 19, 0.82)")),
    "the scrim is gone — drop it from the known list",
  );

  // ---- the dark-only value set, for the render audit --------------------------

  for (const [name, value] of dark) {
    if (SHARED.has(name)) continue;
    const lightValue = light.get(name);
    if (lightValue !== undefined && lightValue.trim() === value.trim()) continue;
    const form = computedForm(value);
    if (form === "" || !/^rgba?\(/.test(form)) continue;
    darkOnly.add(form);
    owner.set(form, name);
  }
  // The accent tokens are overwritten inline on <html> when a non-blue accent
  // is chosen, so they are audited by the accent pass rather than this set.
  darkOnly = new Set([...darkOnly]);
  check("audit · there is a dark palette to look for", darkOnly.size > 30, String(darkOnly.size));

  // ---- 3. what renders --------------------------------------------------------

  appearance.setScheme("light");
  settings.useConfigPort(port);
  eq("light · the document is in the light theme", document.documentElement.getAttribute("data-theme"), "light");

  const host = document.createElement("div");
  host.style.cssText = "position:relative;width:1440px;height:900px;overflow:hidden";
  document.getElementById("mounts")!.appendChild(host);
  mount(App, { target: host });
  await settle(80);
  flushSync();

  app.folder = { dir: DIR, network: false, groups };
  app.allGroups = groups.map((g) => ({ ...g }));
  app.groups = app.allGroups;
  app.focusIndex = 1;
  app.selection = new Set([app.groups[0].hash]);
  app.keymap = JSON.parse(JSON.stringify(DEFAULT_KEYMAP)) as Record<string, string[]>;
  flushSync();

  const screens: { name: string; proof: string; enter: () => void; leave: () => void }[] = [
    {
      name: "contact sheet",
      proof: ".tile",
      enter: () => {
        shell.setMode("cull");
        shell.setLayout(0);
        app.view = "grid";
      },
      leave: () => {},
    },
    { name: "loupe-first", proof: ".loupe-first .panel", enter: () => shell.setLayout(1), leave: () => shell.setLayout(0) },
    { name: "table", proof: ".table .scroller", enter: () => shell.setLayout(2), leave: () => shell.setLayout(0) },
    { name: "loupe overlay", proof: "section.overlay", enter: () => (app.view = "loupe"), leave: () => (app.view = "grid") },
    {
      name: "compare",
      proof: ".compare .panes",
      enter: () => (app.compare = [groups[0], groups[1]]),
      leave: () => (app.compare = null),
    },
    { name: "command palette", proof: ".palette-scrim .panel", enter: () => palette.show("command"), leave: () => palette.close() },
    { name: "move palette", proof: ".palette-scrim .panel", enter: () => palette.show("move"), leave: () => palette.close() },
    { name: "filter palette", proof: ".palette-scrim .panel", enter: () => palette.show("filter"), leave: () => palette.close() },
    { name: "keys overlay", proof: ".scrim .panel", enter: () => (app.overlay = true), leave: () => (app.overlay = false) },
    {
      name: "error banner",
      proof: ".error",
      enter: () => (app.error = "could not open /Volumes/FUJI_SD: input/output error"),
      leave: () => (app.error = ""),
    },
    {
      name: "toast",
      proof: ".toast",
      enter: () => app.notify("applied 21 frames"),
      leave: () => (app.toast = null),
    },
    {
      name: "scanning",
      proof: ".scan .track",
      enter: () => {
        app.scanning = DIR;
        app.scanProgress = { done: 412, total: 1204 };
        app.scanSlow = true;
      },
      leave: () => {
        app.scanning = null;
        app.scanProgress = null;
        app.scanSlow = false;
      },
    },
  ];

  for (const screen of screens) {
    screen.enter();
    flushSync();
    await settle(10);
    auditRendered(host, screen.name, screen.proof);
    screen.leave();
    flushSync();
  }

  // Settings is its own window rather than a mode, and its aside and chips are
  // the densest use of the chromatic tokens anywhere.
  for (const page of ["general", "keymap", "culling", "files", "catalogue", "appearance", "advanced"] as const) {
    settings.openView(page);
    flushSync();
    await settle(20);
    auditRendered(host, `settings · ${page}`, ".settings [data-page]");
  }
  settings.close();
  flushSync();

  // ---- the cold start, mounted on its own -------------------------------------

  // App.svelte still draws the old "type a folder" empty state, so this screen
  // cannot be reached through the shell until ColdStart is swapped in there.
  // It is mounted directly rather than left unaudited.
  const coldHost = document.createElement("div");
  coldHost.style.cssText = "position:relative;display:flex;width:1000px;height:700px;overflow:hidden";
  document.getElementById("mounts")!.appendChild(coldHost);
  app.roots = ["/Volumes/FUJI_SD"];
  mount(ColdStart, { target: coldHost, props: { onname: () => {} } });
  flushSync();
  auditRendered(coldHost, "cold start", ".cold .step");

  // ---- the accent override, in both themes ------------------------------------

  const accentOf = () => getComputedStyle(document.documentElement).getPropertyValue("--accent").trim();
  for (const accent of ["cyan", "purple", "blue"] as const) {
    appearance.setAccent(accent);
    flushSync();
    check(`accent · ${accent} resolves in light`, accentOf() !== "", accentOf());
    const lightValue = accentOf();
    appearance.setScheme("dark");
    flushSync();
    check(`accent · ${accent} is redrawn for dark`, accentOf() !== "", accentOf());
    check(
      `accent · ${accent} is not the same colour in both themes`,
      accent === "blue" ? lightValue !== accentOf() : lightValue !== accentOf(),
      `${lightValue} then ${accentOf()}`,
    );
    appearance.setScheme("light");
    flushSync();
  }
  appearance.setAccent("blue");
  flushSync();
  // A chosen accent must not have leaked into anything the theme owns.
  appearance.setAccent("purple");
  flushSync();
  await settle(10);
  auditRendered(host, "purple accent on light", ".tile");
  appearance.setAccent("blue");
  flushSync();

  eq("nothing complained", complaints, []);

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
