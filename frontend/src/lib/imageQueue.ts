// Loading the grid's images, a few at a time.
//
// A folder of 2,000 frames used to hand the browser 2,000 <img src> at once.
// The webview would queue them all, saturate the connection pool, and the UI
// would sit frozen behind work nobody had asked for — the frames the user is
// actually looking at were stuck behind hundreds they had scrolled past.
//
// So tiles do not load themselves. They register here, and this decides what
// loads and when: at most a handful in flight, always the ones nearest the top
// of the viewport, and nothing at all for a tile that scrolled away before its
// turn came. The backend caps and cancels its side to match.

/**
 * How many images may be fetching at once. Low enough to leave the connection
 * pool room for the ones that matter, high enough to keep the grid filling.
 */
// Browsers cap ~6 connections per origin and service calls share that pool
// with previews — saturating it starves every button in the app.
const MAX_IN_FLIGHT = 3;

/**
 * How far outside the viewport a tile still counts as wanted, matching the
 * grid's own overscan so a small scroll does not start from nothing.
 */
const MEMBERSHIP_MARGIN = "50% 0px";

interface Entry {
  el: HTMLImageElement;
  url: string;
  /** Inside the viewport plus its margin, per the IntersectionObserver. */
  wanted: boolean;
  started: boolean;
  settled: boolean;
  settle: (() => void) | null;
}

const entries = new Map<HTMLImageElement, Entry>();
/**
 * URLs already fetched this session. The preview responses are immutable and
 * cached for a year, so a tile scrolled back to costs nothing and should not
 * wait behind the queue for it.
 */
const fetched = new Set<string>();

let inFlight = 0;
let observer: IntersectionObserver | null = null;

function membership(): IntersectionObserver {
  observer ??= new IntersectionObserver(
    (records) => {
      for (const record of records) {
        const entry = entries.get(record.target as HTMLImageElement);
        if (entry) entry.wanted = record.isIntersecting;
      }
      pump();
    },
    { rootMargin: MEMBERSHIP_MARGIN },
  );
  return observer;
}

/**
 * pump starts as many waiting tiles as there is room for, nearest the top of
 * the viewport first. The positions are read now rather than remembered:
 * reading them at the moment of choosing is what re-orders the queue as the
 * viewport moves, with no scroll listener to keep in step.
 */
function pump() {
  if (inFlight >= MAX_IN_FLIGHT) return;

  const waiting: Entry[] = [];
  for (const entry of entries.values()) {
    if (!entry.started && entry.wanted && entry.el.isConnected) waiting.push(entry);
  }
  if (waiting.length === 0) return;

  const byPosition = waiting
    .map((entry) => ({ entry, top: entry.el.getBoundingClientRect().top }))
    .sort((a, b) => a.top - b.top);

  for (const { entry } of byPosition) {
    if (inFlight >= MAX_IN_FLIGHT) break;
    start(entry);
  }
}

function start(entry: Entry) {
  entry.started = true;
  inFlight++;

  const settle = () => {
    if (entry.settled) return;
    entry.settled = true;
    fetched.add(entry.url);
    inFlight--;
    entry.el.removeEventListener("load", settle);
    entry.el.removeEventListener("error", settle);
    entry.settle = null;
    pump();
  };

  entry.settle = settle;
  entry.el.addEventListener("load", settle);
  entry.el.addEventListener("error", settle);
  entry.el.src = entry.url;
}

function add(el: HTMLImageElement, url: string) {
  if (url === "") return;
  if (fetched.has(url)) {
    // Served from the browser's cache; no request, so no slot needed.
    el.src = url;
    return;
  }
  entries.set(el, { el, url, wanted: false, started: false, settled: false, settle: null });
  membership().observe(el);
  pump();
}

function remove(el: HTMLImageElement) {
  const entry = entries.get(el);
  if (!entry) return;
  entries.delete(el);
  observer?.unobserve(el);
  if (entry.settle) {
    el.removeEventListener("load", entry.settle);
    el.removeEventListener("error", entry.settle);
  }
  // A tile pulled off screen mid-request gives its slot straight back: the
  // element is leaving the document, so the browser abandons the fetch and the
  // backend's own cancellation frees the read behind it.
  if (entry.started && !entry.settled) {
    inFlight--;
    pump();
  }
}

/**
 * queuedImage is the tile's end of all this: `use:queuedImage={url}` on an
 * <img> with no src of its own. The element is handed a src when the queue
 * decides its turn has come.
 */
export function queuedImage(el: HTMLImageElement, url: string) {
  add(el, url);
  return {
    update(next: string) {
      if (entries.get(el)?.url === next || (el.src !== "" && el.getAttribute("src") === next)) return;
      remove(el);
      el.removeAttribute("src");
      add(el, next);
    },
    destroy() {
      remove(el);
    },
  };
}
