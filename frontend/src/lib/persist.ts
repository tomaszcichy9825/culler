// The one place viewing preferences touch localStorage.
//
// Every durable preference — the mode, the sort, the tile size, a pane split —
// goes through these three functions under a culler.* key, so the try/catch
// for a webview with storage disabled is written once: such a webview still
// works in full, it just forgets between launches. Values are plain strings,
// no schema; each caller parses and validates its own value on the way in, so
// a stale or hand-edited entry falls back to the default silently rather than
// wedging anything.

/**
 * stored reads a key and parses it. The fallback answers when the key is
 * absent, storage is unavailable, parse throws, or parse returns null —
 * returning null is how a parser says the stored value is invalid.
 */
export function stored<T>(key: string, parse: (raw: string) => T | null, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    if (raw === null) return fallback;
    return parse(raw) ?? fallback;
  } catch {
    return fallback;
  }
}

/** remember writes a value. A failure keeps the session value and moves on. */
export function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Session-only is the acceptable fallback; see the module comment.
  }
}

/** forget removes a key, for values whose absence is itself the stored state. */
export function forget(key: string) {
  try {
    localStorage.removeItem(key);
  } catch {
    // As above.
  }
}
