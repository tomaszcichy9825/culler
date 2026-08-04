// A headless bench for the empty-rejects dialog.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because this is the one dialog whose button
// destroys photographs, and every claim made about it — that nothing runs
// before the word is typed, that Esc gets out, that the keys it takes never
// reach the grid behind it — has to be a measured fact rather than a reading of
// the source. The stub backend counts calls, so "nothing ran" is an assertion
// rather than a hope.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9347
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1200,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9347/src/harness/rejects.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import RejectsDialog from "../components/RejectsDialog.svelte";
import { CONFIRM_WORD, connectRejects, rejects, summarise } from "../lib/rejects.svelte";
import type { RejectsResult, RejectsSurvey } from "../lib/rejects.svelte";
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

function stage(): HTMLDivElement {
  const el = document.createElement("div");
  el.style.cssText = "position:relative;width:1100px;height:820px";
  document.getElementById("mounts")!.appendChild(el);
  return el;
}

/** Keys go to the panel, which is where the app's own listener would see them. */
function press(target: EventTarget, key: string, init: KeyboardEventInit = {}) {
  target.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, key, ...init }));
  flushSync();
}

function type(target: EventTarget, word: string) {
  for (const ch of word) press(target, ch);
}

/** A card culled twice: one folder with a pair and a stray, one with a lone RAW. */
function survey(): RejectsSurvey {
  return {
    folder: "_Rejected",
    dirs: [
      {
        dir: "/card/DCIM/100FUJI",
        path: "/card/DCIM/100FUJI/_Rejected",
        raw: 2,
        jpeg: 1,
        pairs: 1,
        sidecars: 1,
        other: 1,
        files: 5,
        bytes: 64_000_000,
      },
      {
        dir: "/Volumes/work/shoot",
        path: "/Volumes/work/shoot/_Rejected",
        raw: 1,
        jpeg: 0,
        pairs: 0,
        sidecars: 0,
        other: 0,
        files: 1,
        bytes: 30_000_000,
      },
    ],
    raw: 3,
    jpeg: 1,
    pairs: 1,
    sidecars: 1,
    other: 1,
    files: 6,
    totalBytes: 94_000_000,
  };
}

/** What a folder that has never been culled comes back as. */
const NOTHING: RejectsSurvey = {
  folder: "_Rejected",
  dirs: [],
  raw: 0,
  jpeg: 0,
  pairs: 0,
  sidecars: 0,
  other: 0,
  files: 0,
  totalBytes: 0,
};

/** The backend, counting what it was asked to do. */
class Stub {
  surveys: string[][] = [];
  empties: string[][] = [];
  answer: RejectsSurvey = survey();
  result: RejectsResult = { batchId: "1-rejects", deleted: 6, failed: 0, bytes: 94_000_000, errors: [] };

  async Survey(dirs: string[]): Promise<RejectsSurvey> {
    this.surveys.push(dirs);
    return this.answer;
  }

  async Empty(dirs: string[]): Promise<RejectsResult> {
    this.empties.push(dirs);
    return this.result;
  }
}

const backend = new Stub();

async function run() {
  connectRejects(backend);

  const host = stage();
  const view = mount(RejectsDialog, { target: host });
  flushSync();

  eq("closed · nothing is drawn until the command is run", host.querySelector(".panel"), null);

  // ---- the survey ----------------------------------------------------------

  await rejects.request(["/card/DCIM/100FUJI", "/Volumes/work/shoot"]);
  flushSync();

  eq("survey · the folders asked about are the ones handed in", backend.surveys, [
    ["/card/DCIM/100FUJI", "/Volumes/work/shoot"],
  ]);
  eq("survey · nothing has been destroyed by opening the dialog", backend.empties, []);

  const panel = host.querySelector<HTMLElement>(".panel")!;
  check("survey · the dialog is up", panel !== null);
  eq("survey · it is a modal dialog", panel.getAttribute("aria-modal"), "true");
  eq(
    "survey · the app's key layer is told to stand back",
    host.querySelector(".scrim")?.getAttribute("data-keys"),
    "local",
  );
  check("survey · the header totals the files and the bytes", text(panel.querySelector(".tally")) === "6 files · 89.6 MB", text(panel.querySelector(".tally")));
  eq("survey · the §R7 breakdown is drawn", texts(panel, ".totals .figure .k"), [
    "RAW",
    "JPEG",
    "pair",
    "sidecar",
    "other",
    "total",
  ]);
  eq("survey · the counts are the survey's", texts(panel, ".totals .figure .n"), ["3", "1", "1", "1", "1", "89.6 MB"]);
  eq("survey · one row per rejected folder", texts(panel, ".rows .line:not(.head) .path"), [
    "/card/DCIM/100FUJI/_Rejected",
    "/Volumes/work/shoot/_Rejected",
  ]);
  check(
    "survey · the row says what emptying that folder costs",
    text(panel.querySelector(".rows .line:not(.head)")).includes("61.0 MB"),
    text(panel.querySelector(".rows .line:not(.head)")),
  );
  check(
    "survey · the warning says the files do not go to the trash",
    text(panel.querySelector(".warn")).includes("permanently") && text(panel.querySelector(".warn")).includes("undo"),
    text(panel.querySelector(".warn")),
  );

  // ---- the friction --------------------------------------------------------

  const button = panel.querySelector<HTMLButtonElement>(".destroy")!;
  eq("friction · the button is dead before the word is typed", button.disabled, true);

  press(panel, "Enter");
  eq("friction · ⏎ on its own destroys nothing", backend.empties, []);

  type(panel, "DEL");
  eq("friction · the caps fill in as the word is typed", rejects.typed, "DEL");
  eq(
    "friction · three of six caps are lit",
    panel.querySelectorAll(".word .cap.on").length,
    3,
  );
  eq("friction · still dead half way through", button.disabled, true);

  press(panel, "x");
  eq("friction · a key that is not the next letter is ignored", rejects.typed, "DEL");
  press(panel, "Backspace");
  eq("friction · backspace takes a letter back", rejects.typed, "DE");

  type(panel, "lete");
  eq("friction · case is not part of it", rejects.typed, CONFIRM_WORD);
  flushSync();
  eq("friction · the word arms the button", button.disabled, false);

  // ---- containment ---------------------------------------------------------

  let leaked = 0;
  const listener = () => (leaked += 1);
  window.addEventListener("keydown", listener);
  // x is a letter, Tab would walk focus out, and k and 1 pass verdicts and
  // ratings in the grid behind this panel. None of them may get there.
  for (const key of ["x", "Tab", "k", "1"]) press(panel, key);
  window.removeEventListener("keydown", listener);
  eq("containment · nothing typed into the dialog reaches the window listener", leaked, 0);
  eq("containment · the dialog is still up", rejects.open, true);
  eq("containment · and the word survived the stray keys", rejects.typed, CONFIRM_WORD);

  // ---- running -------------------------------------------------------------

  press(panel, "Enter");
  await settle();
  flushSync();

  eq("run · exactly the surveyed folders are emptied", backend.empties, [
    ["/card/DCIM/100FUJI", "/Volumes/work/shoot"],
  ]);
  eq("run · the dialog closes on a clean run", rejects.open, false);
  check(
    "run · the toast says what was reclaimed",
    (app.toast?.message ?? "").includes("6") && (app.toast?.message ?? "").includes("89.6 MB"),
    app.toast?.message ?? "",
  );
  eq("run · nothing is drawn once it has closed", host.querySelector(".panel"), null);

  // ---- esc -----------------------------------------------------------------

  await rejects.request(["/card/DCIM/100FUJI"]);
  flushSync();
  const second = host.querySelector<HTMLElement>(".panel")!;
  type(second, "DELETE");
  let escaped = 0;
  const escListener = () => (escaped += 1);
  window.addEventListener("keydown", escListener);
  press(second, "Escape");
  window.removeEventListener("keydown", escListener);
  eq("esc · the press does not reach the window listener either", escaped, 0);
  eq("esc · one press closes it", rejects.open, false);
  eq("esc · the typed word does not survive the dialog", rejects.typed, "");
  eq("esc · nothing was destroyed on the way out", backend.empties.length, 1);

  // ---- a run that could not finish ------------------------------------------

  backend.result = {
    batchId: "2-rejects",
    deleted: 4,
    failed: 2,
    bytes: 40_000_000,
    errors: ["/card/DCIM/100FUJI/_Rejected/DSCF0100.RAF: permission denied"],
  };
  await rejects.request(["/card/DCIM/100FUJI"]);
  flushSync();
  const third = host.querySelector<HTMLElement>(".panel")!;
  type(third, "DELETE");
  press(third, "Enter");
  await settle();
  flushSync();

  eq("partial · the dialog stays up when something would not go", rejects.open, true);
  check(
    "partial · the failure is named",
    text(third.querySelector(".fail")).includes("permission denied"),
    text(third.querySelector(".fail")),
  );
  eq("partial · the word has to be typed again before a retry", rejects.typed, "");
  eq(
    "partial · the button is dead again",
    third.querySelector<HTMLButtonElement>(".destroy")!.disabled,
    true,
  );
  rejects.cancel();
  flushSync();

  // ---- nothing to empty ------------------------------------------------------

  backend.answer = NOTHING;
  await rejects.request(["/card/DCIM/100FUJI"]);
  flushSync();
  const fourth = host.querySelector<HTMLElement>(".panel")!;
  check(
    "empty · it says there is nothing to do",
    text(fourth.querySelector(".quiet")).includes("no rejected files"),
    text(fourth.querySelector(".quiet")),
  );
  type(fourth, "DELETE");
  flushSync();
  eq(
    "empty · the word does not arm a command with nothing to destroy",
    fourth.querySelector<HTMLButtonElement>(".destroy")!.disabled,
    true,
  );
  press(fourth, "Enter");
  await settle();
  eq("empty · and ⏎ does nothing either", backend.empties.length, 2);
  rejects.cancel();
  flushSync();

  eq("summary · a survey with nothing in it reads as nothing", summarise(NOTHING), "nothing to empty");
  eq(
    "summary · the palette line names the classes and the size",
    summarise(survey()),
    "3 RAW · 1 JPEG · 1 pair · 1 sidecar · 1 other · 89.6 MB",
  );

  unmount(view);

  // With #stay the dialog is left up afterwards, which is how it is
  // screenshotted: chrome --headless --screenshot against this file.
  if (location.hash === "#stay") {
    document.getElementById("results")!.style.display = "none";
    backend.answer = survey();
    mount(RejectsDialog, { target: stage() });
    await rejects.request(["/card/DCIM/100FUJI", "/Volumes/work/shoot"]);
    flushSync();
  }
}

void run()
  .catch((err: unknown) => {
    check("harness", false, `${String(err)}\n${(err as Error)?.stack ?? ""}`);
  })
  .finally(() => {
    const failed = results.filter((r) => !r.pass);
    document.getElementById("results")!.textContent = JSON.stringify(
      { total: results.length, failed: failed.length, results },
      null,
      1,
    );
    document.title = failed.length === 0 ? `PASS ${results.length}/${results.length}` : `FAIL ${failed.length}`;
  });
