// A headless bench for the metadata editor, which lives in the PHOTOS
// inspector.
//
// Not part of the application: nothing in src/main.ts reaches this, so it adds
// nothing to the bundle. It exists because the editor has to be verifiable
// without a backend — a field edited and committed, a dirty row showing what it
// replaces, a selection reading ⟨mixed⟩ until something replaces it, and a
// write plan that lists exactly what it will do — and none of that needs a
// photograph or a running Go process.
//
// The fake port stands in for ExifService. It returns the DTO shapes
// internal/app/exifservice.go returns, and its apply records what it was sent,
// so the payload can be checked against the Go side by eye.
//
// Run it with the dev server up, against whichever port it reports:
//   npx vite --port 9348
//   "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//     --headless --disable-gpu --window-size=1400,900 \
//     --virtual-time-budget=15000 --dump-dom \
//     http://127.0.0.1:9348/src/harness/exif.html
//
// Every assertion lands in #results as JSON, and the document title carries the
// tally so a check can read one line.

import { flushSync, mount, unmount } from "svelte";
import Inspector from "../components/Inspector.svelte";
import UnwrittenChip from "../components/exif/UnwrittenChip.svelte";
import WritePlanDialog from "../components/exif/WritePlanDialog.svelte";
import type { GroupDTO } from "../lib/bindings";
import { MIXED, exifState } from "../lib/exif.svelte";
import type { ExifEditDTO, ExifPlanDTO, ExifPort, FrameExifDTO } from "../lib/exif.svelte";
import { exifCache } from "../lib/exifcache.svelte";
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

function text(el: Element | null): string {
  return (el?.textContent ?? "").replace(/\s+/g, " ").trim();
}

// --- the fake frames --------------------------------------------------------

function jpegFrame(path: string, stem: string, artist: string, when: string): FrameExifDTO {
  return {
    path,
    stem,
    kind: "jpeg",
    sidecar: "",
    error: "",
    fields: [
      { tag: "DateTimeOriginal", label: "Capture time", section: "Capture", value: when, present: true, writable: true },
      { tag: "Artist", label: "Artist", section: "Rights", value: artist, present: artist !== "", writable: true },
      { tag: "Copyright", label: "Copyright", section: "Rights", value: "", present: false, writable: true },
      { tag: "Make", label: "Camera make", section: "Camera", value: "FUJIFILM", present: true, writable: false },
      { tag: "ISO", label: "ISO", section: "Exposure", value: "640", present: true, writable: false },
      {
        tag: "GPSPosition",
        label: "Position",
        section: "Location",
        value: "54.372158 N, 18.638306 E",
        present: true,
        writable: false,
      },
    ],
  };
}

function rawFrame(path: string, stem: string): FrameExifDTO {
  return {
    path,
    stem,
    kind: "raw",
    sidecar: `${path}.xmp`,
    error: "",
    fields: [
      {
        tag: "DateTimeOriginal",
        label: "Capture time",
        section: "Capture",
        value: "2026-08-03T19:44:10+02:00",
        present: true,
        writable: true,
      },
      { tag: "Artist", label: "Artist", section: "Rights", value: "", present: false, writable: true },
      { tag: "Orientation", label: "Orientation", section: "Image", value: "1", present: true, writable: false },
    ],
  };
}

const DIR = "/card";
const A = `${DIR}/DSCF0001.JPG`;
const B = `${DIR}/DSCF0002.JPG`;
const R = `${DIR}/DSCF0003.RAF`;

const metaLibrary: Record<string, FrameExifDTO> = {
  [A]: jpegFrame(A, "DSCF0001.JPG", "Old Artist", "2026-08-03T19:42:07+02:00"),
  [B]: jpegFrame(B, "DSCF0002.JPG", "Someone Else", "2026-08-03T19:43:01+02:00"),
  [R]: rawFrame(R, "DSCF0003.RAF"),
};

/** The grid's own view of the same three frames, for the inspector's header. */
function groupOf(stem: string, ext: "JPG" | "RAF"): GroupDTO {
  return {
    dir: DIR,
    stem,
    kind: ext === "JPG" ? "jpeg-only" : "raw-only",
    hasRaw: ext === "RAF",
    hasJpeg: ext === "JPG",
    rawPath: ext === "RAF" ? `${DIR}/${stem}.RAF` : "",
    jpegPath: ext === "JPG" ? `${DIR}/${stem}.JPG` : "",
    sidecars: 0,
    shot: "2026-08-03T19:42:07Z",
    warnings: null,
    verdict: "",
    mask: "rj",
    rating: 0,
    hash: `hash-${stem}`,
    destination: "",
    decision: "",
  } as GroupDTO;
}

const groups: GroupDTO[] = [groupOf("DSCF0001", "JPG"), groupOf("DSCF0002", "JPG"), groupOf("DSCF0003", "RAF")];

/** What apply was last sent, so the payload can be inspected. */
let lastApply: ExifEditDTO[] = [];
let lastPlan: ExifEditDTO[] = [];

const port: ExifPort = {
  read: (paths) => {
    const out: Record<string, FrameExifDTO> = {};
    for (const p of paths) {
      const frame = metaLibrary[p];
      // A real read returns a fresh object per call; sharing one would let the
      // harness mutate the library by accident and hide a bug.
      if (frame !== undefined) out[p] = JSON.parse(JSON.stringify(frame)) as FrameExifDTO;
    }
    return Promise.resolve(out);
  },
  plan: (edits) => {
    lastPlan = edits;
    const rows = edits.flatMap((e) => {
      const target = e.path.split("/").pop() ?? e.path;
      const method = e.path.endsWith(".RAF") ? "sidecar" : "in place";
      const lines: ExifPlanDTO["rows"] = [];
      if (e.dateTimeOriginal !== null) {
        lines.push({ sign: "+", target, tag: "DateTimeOriginal", value: e.dateTimeOriginal, method });
      }
      if (e.artist !== null) {
        lines.push({ sign: e.artist === "" ? "−" : "+", target, tag: "Artist", value: e.artist, method });
      }
      if (e.copyright !== null) {
        lines.push({ sign: e.copyright === "" ? "−" : "+", target, tag: "Copyright", value: e.copyright, method });
      }
      if (e.stripGps) lines.push({ sign: "−", target, tag: "GPSPosition", value: "removed", method });
      return lines;
    });
    return Promise.resolve({
      description: `Write metadata to ${edits.length} frames`,
      rows,
      writes: rows.length,
      frames: edits.length,
      files: edits.length,
      backupDir: "/data/culler/backup/2026-08-03",
      assurances: ["back up originals to /data/culler/backup/2026-08-03", "RAW via sidecar · JPEG in place"],
      warnings: [],
    });
  },
  apply: (edits) => {
    lastApply = edits;
    return Promise.resolve({ actions: edits.map(() => ({ outcome: "ok" })), description: "written" });
  },
};

// --- the bench --------------------------------------------------------------

const mounts = document.getElementById("mounts") as HTMLElement;

function fieldEl(tag: string): HTMLElement | null {
  return mounts.querySelector(`[data-testid="exif-editor"] .field[data-tag="${tag}"]`);
}

function stateOf(tag: string): string {
  return fieldEl(tag)?.dataset.state ?? "absent";
}

function valueOf(tag: string): string {
  const el = fieldEl(tag);
  const input = el?.querySelector("input");
  if (input instanceof HTMLInputElement) return input.value;
  return text(el?.querySelector("button.value") ?? null);
}

async function run() {
  exifState.usePort(port);
  exifCache.useReader((paths) => port.read(paths));
  shell.setMode("cull");

  app.allGroups = groups;
  app.groups = groups;
  app.focusIndex = 0;

  const pane = mount(Inspector, { target: mounts });
  const chip = mount(UnwrittenChip, { target: mounts });
  const dialog = mount(WritePlanDialog, { target: mounts });

  // ---- reading: one focused frame ------------------------------------------

  await exifState.load([A]);
  flushSync();

  check("the edit section is drawn once the editor is fed", mounts.querySelector('[data-testid="exif-editor"]') !== null);
  eq("only the writable tags become field rows", mounts.querySelectorAll('[data-testid="exif-editor"] .field').length, 3);
  check("a read-only tag is not an editable row", fieldEl("Make") === null, "Make got a field row");
  eq("a writable row is clean", stateOf("Artist"), "clean");
  eq("the value on disk is shown", valueOf("Artist"), "Old Artist");
  check(
    "a tag the file does not carry reads as absent rather than empty",
    valueOf("Copyright") === "—",
    `got ${valueOf("Copyright")}`,
  );
  eq("the frame's GPS reads as present", text(mounts.querySelector(".gps-line .gps-word")), "present");
  eq("nothing is unwritten yet", exifState.unwritten, 0);
  eq("no chip at zero", text(mounts.querySelector('[data-testid="exif-unwritten"] .chip')), "");

  // ---- editing one field ---------------------------------------------------

  exifState.beginEdit("Artist");
  flushSync();
  eq("the row takes the caret", stateOf("Artist"), "editing");
  check("the field holds the current value", valueOf("Artist") === "Old Artist", `got ${valueOf("Artist")}`);

  exifState.buffer = "Tomasz Cichy";
  exifState.commit();
  flushSync();
  eq("a committed change is dirty", stateOf("Artist"), "dirty");
  eq("the new value is shown", valueOf("Artist"), "Tomasz Cichy");
  eq("the value it replaces is struck through beside it", text(fieldEl("Artist")?.querySelector(".was") ?? null), "Old Artist");
  eq("one unwritten change", exifState.unwritten, 1);
  eq("the chip counts it", text(mounts.querySelector('[data-testid="exif-unwritten"] .chip')), "1 unwritten");
  eq(
    "the section header counts it too",
    text(mounts.querySelector('.section-label[data-section="edit"] .unwritten')),
    "1 unwritten",
  );

  // ---- a value equal to what is already there is not a change --------------

  exifState.beginEdit("Copyright");
  exifState.buffer = "";
  exifState.commit();
  flushSync();
  eq("committing an unchanged empty value drafts nothing", exifState.unwritten, 1);

  // ---- esc reverts the field, not the draft --------------------------------

  exifState.beginEdit("Artist");
  exifState.buffer = "half typed";
  exifState.revert();
  flushSync();
  eq("esc leaves the row", stateOf("Artist"), "dirty");
  eq("esc keeps the committed draft", valueOf("Artist"), "Tomasz Cichy");

  // ---- tab walks the editable rows only ------------------------------------

  exifState.beginEdit("DateTimeOriginal");
  exifState.nextField();
  flushSync();
  eq("⇥ moves to the next editable row", exifState.editingTag, "Artist");
  exifState.nextField();
  exifState.nextField();
  flushSync();
  eq("⇥ wraps back to the first editable row", exifState.editingTag, "DateTimeOriginal");
  exifState.revert();

  // ---- clearing a tag ------------------------------------------------------

  exifState.clearField("Artist");
  flushSync();
  eq("an emptied field is still a change", exifState.unwritten, 1);
  eq("clearing shows an empty value on a dirty row", stateOf("Artist"), "dirty");
  exifState.discard();
  flushSync();
  eq("discard clears every draft", exifState.unwritten, 0);

  // ---- a selection ----------------------------------------------------------

  await exifState.load([A, B, R]);
  flushSync();
  eq("a selection covers every loaded frame", exifState.targets.length, 3);
  check("the pane states the rule for a selection", text(mounts.querySelector(".edit-note")).includes("3 frames selected"), text(mounts.querySelector(".edit-note")));
  eq("frames that disagree read as mixed", valueOf("Artist"), MIXED);
  eq("a mixed row says so", stateOf("Artist"), "mixed");

  exifState.beginEdit("Artist");
  flushSync();
  check("editing a mixed row starts empty rather than picking a winner", valueOf("Artist") === "", `got ${valueOf("Artist")}`);
  exifState.buffer = "Tomasz Cichy";
  exifState.commit();
  flushSync();
  eq("one typed value drafts onto every frame", exifState.unwritten, 3);
  eq("the selection's row is dirty", stateOf("Artist"), "dirty");

  // ---- stripping GPS -------------------------------------------------------

  exifState.toggleStrip();
  flushSync();
  eq("stripping GPS is a drafted change per frame", exifState.unwritten, 6);
  eq("the GPS line says so", text(mounts.querySelector(".gps-line .gps-word")), "drafted for removal");
  exifState.toggleStrip();
  flushSync();
  eq("toggling it back removes those drafts", exifState.unwritten, 3);

  // ---- the write plan ------------------------------------------------------

  await exifState.requestWrite();
  flushSync();
  await settle();
  flushSync();

  const planEl = mounts.querySelector('[data-testid="exif-write-plan"]');
  check("the write plan is up", planEl !== null);
  eq("the plan was asked about every dirty frame", lastPlan.length, 3);
  eq("the plan lists one line per write", mounts.querySelectorAll('[data-testid="exif-write-plan"] .line').length, 3);
  eq(
    "the plan says where the originals go",
    text(mounts.querySelector('[data-testid="exif-write-plan"] .ok')),
    "✓back up originals to /data/culler/backup/2026-08-03",
  );
  check(
    "the plan states the method per file",
    text(mounts.querySelector('[data-testid="exif-write-plan"] .method')) === "in place",
    text(mounts.querySelector('[data-testid="exif-write-plan"] .method')),
  );
  eq("the edit payload carries the value", lastPlan[0]?.artist, "Tomasz Cichy");
  eq("an untouched tag is sent as null", lastPlan[0]?.copyright, null);

  // ---- writing -------------------------------------------------------------

  await exifState.confirmWrite();
  flushSync();
  await settle();
  flushSync();

  eq("apply was sent the same edits the plan described", lastApply.length, 3);
  eq("the dialog closed", mounts.querySelector('[data-testid="exif-write-plan"]'), null);
  eq("a clean write clears the drafts", exifState.unwritten, 0);
  eq("the chip is gone", text(mounts.querySelector('[data-testid="exif-unwritten"] .chip')), "");

  // ---- an unwired backend says so rather than exploding ---------------------

  exifState.usePort({
    read: () => Promise.reject(new Error("service is not connected")),
    plan: () => Promise.reject(new Error("service is not connected")),
    apply: () => Promise.reject(new Error("service is not connected")),
  });
  await exifState.load([A]);
  flushSync();
  eq("a failed read is reported, not thrown", exifState.error, "service is not connected");
  check("the section still draws the error", text(mounts.querySelector(".edit-error")) === "service is not connected", text(mounts.querySelector(".edit-error")));

  unmount(pane);
  unmount(chip);
  unmount(dialog);

  const failed = results.filter((r) => !r.pass);
  document.title = `exif harness: ${results.length - failed.length}/${results.length} passed`;
  const out = document.getElementById("results") as HTMLElement;
  out.textContent = JSON.stringify({ passed: results.length - failed.length, total: results.length, failed }, null, 2);
}

void run().catch((err: unknown) => {
  document.title = "exif harness: crashed";
  (document.getElementById("results") as HTMLElement).textContent = `crashed: ${String(err)}\n${
    err instanceof Error ? err.stack : ""
  }`;
});
