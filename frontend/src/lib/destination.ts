// What a typed destination means, worked out for the preview the palette
// shows before anything is routed.
//
// Every rule here is a mirror of the backend's — resolveDestination and
// standaloneDestination in internal/app/apply.go, ExpandTemplate in
// internal/ops/route.go — and the mirror is deliberate rather than lazy: the
// palette has to say where files would land while the user is still typing,
// which is one keystroke's notice, and a round trip per keystroke would be
// slower and no more true. The apply itself never reads this module. If the two
// ever disagree the backend is right, and the preview is the thing to fix.

/** What a template can name about the frame it is being previewed against. */
export interface Tokens {
  /** When the frame was taken, or null when nothing knows. */
  shot: Date | null;
  stem: string;
  /** The primary file's extension, lowercase and without the dot. */
  ext: string;
  camera: string;
  lens: string;
}

export const NO_TOKENS: Tokens = { shot: null, stem: "", ext: "", camera: "", lens: "" };

/** isTemplate reports whether a destination has anything to expand. */
export function isTemplate(dest: string): boolean {
  return dest.includes("{");
}

/**
 * standalone reports whether a destination names a place of its own rather than
 * hanging off the library root. It is the frontend's half of
 * standaloneDestination: a leading ~ or /, or a drive letter, since a Windows
 * path typed on Windows must not be taken for a library-relative folder.
 */
export function standalone(dest: string): boolean {
  const d = dest.trim();
  return d.startsWith("~") || d.startsWith("/") || d.startsWith("\\") || /^[A-Za-z]:[\\/]/.test(d);
}

/**
 * resolve is where a destination actually lands. A standalone one is taken at
 * its word; anything else hangs off the library root, which is what makes
 * "2026/portraits" mean the same folder on every machine.
 */
export function resolve(dest: string, libraryRoot: string): string {
  const trimmed = dest.trim();
  if (trimmed === "") return "";
  if (standalone(trimmed)) return trimmed;
  if (libraryRoot === "") return trimmed;
  return `${libraryRoot.replace(/\/+$/, "")}/${trimmed.replace(/^\/+/, "")}`;
}

/**
 * The layout pieces a date token understands, longest first so that "2006" is
 * a year rather than a year-2 followed by three digits. It is a subset of Go's
 * reference layout — the pieces a folder name is plausibly written from — and
 * anything else in the layout passes through as itself, which is what the
 * backend does with a literal too.
 */
const DATE_PIECES: [string, (d: Date) => string][] = [
  ["January", (d) => MONTHS[d.getMonth()]],
  ["Monday", (d) => DAYS[d.getDay()]],
  ["2006", (d) => String(d.getFullYear()).padStart(4, "0")],
  ["Jan", (d) => MONTHS[d.getMonth()].slice(0, 3)],
  ["Mon", (d) => DAYS[d.getDay()].slice(0, 3)],
  ["01", (d) => pad(d.getMonth() + 1)],
  ["02", (d) => pad(d.getDate())],
  ["03", (d) => pad(hour12(d))],
  ["04", (d) => pad(d.getMinutes())],
  ["05", (d) => pad(d.getSeconds())],
  ["06", (d) => pad(d.getFullYear() % 100)],
  ["15", (d) => pad(d.getHours())],
  ["PM", (d) => (d.getHours() < 12 ? "AM" : "PM")],
  ["pm", (d) => (d.getHours() < 12 ? "am" : "pm")],
  ["1", (d) => String(d.getMonth() + 1)],
  ["2", (d) => String(d.getDate())],
  ["3", (d) => String(hour12(d))],
  ["4", (d) => String(d.getMinutes())],
  ["5", (d) => String(d.getSeconds())],
];

const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];
const DAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function hour12(d: Date): number {
  const h = d.getHours() % 12;
  return h === 0 ? 12 : h;
}

/** formatDate writes a date in a Go reference layout. */
export function formatDate(d: Date, layout: string): string {
  let out = "";
  let rest = layout;
  outer: while (rest !== "") {
    for (const [piece, write] of DATE_PIECES) {
      if (rest.startsWith(piece)) {
        out += write(d);
        rest = rest.slice(piece.length);
        continue outer;
      }
    }
    out += rest[0];
    rest = rest.slice(1);
  }
  return out;
}

/** sanitise makes a metadata value safe to be one folder name. */
function sanitise(v: string): string {
  const out = v.replace(/[/\\:]/g, "-").trim();
  return out === "." || out === ".." ? "" : out;
}

/**
 * resolveToken answers one token, or null when the frame has nothing to fill it
 * with — which includes tokens this build has never heard of, so an unknown one
 * collapses its folder level rather than becoming a literal.
 */
function resolveToken(name: string, tok: Tokens): string | null {
  if (name.startsWith("date:")) {
    const layout = name.slice("date:".length);
    if (tok.shot === null || layout === "") return null;
    return formatDate(tok.shot, layout);
  }
  let value = "";
  switch (name) {
    case "stem":
      value = tok.stem;
      break;
    case "ext":
      value = tok.ext;
      break;
    case "camera":
      value = tok.camera;
      break;
    case "lens":
      value = tok.lens;
      break;
  }
  value = sanitise(value);
  return value === "" ? null : value;
}

/** What an expansion came to, and what it could not answer. */
export interface Expansion {
  /** The folder the frames would go to, or "" when nothing survived. */
  path: string;
  /** The tokens the frame could not answer, in the order they were written. */
  unanswered: string[];
  /** Set when the template is malformed, in which case path is empty. */
  error: string;
}

/**
 * expandTemplate turns a destination template into a folder for one frame.
 *
 * A token the frame cannot answer takes its whole path segment with it, so
 * `/library/{camera}/{stem}` on a frame with no EXIF is `/library/DSCF0001`,
 * never `/library//DSCF0001` and never a folder called {camera}. The segment
 * goes rather than just the token because a segment written around a token —
 * `shot-on-{camera}` — is about that token and means nothing without it.
 */
export function expandTemplate(template: string, tok: Tokens): Expansion {
  const flat = template.replace(/\\/g, "/");
  const unc = flat.startsWith("//");
  const absolute = flat.startsWith("/");
  let rest = flat.replace(/^\/+/, "");

  const segments: string[] = [""];
  const dead: boolean[] = [false];
  const unanswered: string[] = [];

  const addLiteral = (s: string) => {
    const parts = s.split("/");
    segments[segments.length - 1] += parts[0];
    for (const p of parts.slice(1)) {
      segments.push(p);
      dead.push(false);
    }
  };

  while (rest !== "") {
    const open = rest.indexOf("{");
    if (open === -1) {
      addLiteral(rest);
      break;
    }
    addLiteral(rest.slice(0, open));
    const shut = rest.indexOf("}", open);
    if (shut === -1) {
      return { path: "", unanswered, error: `unclosed { in ${template}` };
    }
    const name = rest.slice(open + 1, shut);
    rest = rest.slice(shut + 1);

    const value = resolveToken(name, tok);
    if (value === null) {
      dead[dead.length - 1] = true;
      unanswered.push(name);
      continue;
    }
    addLiteral(value);
  }

  const kept = segments.filter((s, i) => !dead[i] && s !== "" && s !== "." && s !== "..");
  const joined = kept.join("/");
  if (joined === "") return { path: "", unanswered, error: "" };
  if (unc) return { path: `//${joined}`, unanswered, error: "" };
  if (absolute) return { path: `/${joined}`, unanswered, error: "" };
  return { path: joined, unanswered, error: "" };
}

/** basename is the last part of a path, for the name a file keeps as it moves. */
export function basename(path: string): string {
  const trimmed = path.replace(/[\\/]+$/, "");
  const cut = Math.max(trimmed.lastIndexOf("/"), trimmed.lastIndexOf("\\"));
  return cut === -1 ? trimmed : trimmed.slice(cut + 1);
}
