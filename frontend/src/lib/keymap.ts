// Keymap resolution: config chords in, action names out.
//
// Chords are written as "mod+a", "shift+mod+z", "ArrowLeft", "space", "?".
// "mod" is Cmd on macOS and Ctrl everywhere else — the config layer leaves
// that to us deliberately. Both a chord and a keyboard event are reduced to
// the same canonical signature string, and the lookup is one map read.

/** True on macOS, where "mod" means Cmd rather than Ctrl. */
export const isMac: boolean =
  typeof navigator !== "undefined" && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent);

interface Chord {
  key: string;
  mod: boolean;
  /**
   * Control held as a modifier in its own right. Only meaningful on macOS,
   * where "mod" is Cmd and Ctrl is therefore still free — the shell needs both
   * at once for ⌘1–3 panes and ⌃1–4 modes. Everywhere else Ctrl *is* mod and
   * this stays false.
   */
  ctrl: boolean;
  shift: boolean;
  alt: boolean;
}

/**
 * normaliseKey folds a key name to the form used in signatures. Space arrives
 * from the DOM as " " but is written "space" in the config, and everything is
 * compared case-insensitively so "Escape" and "escape" are one binding.
 */
function normaliseKey(key: string): string {
  if (key === " " || key === "spacebar") return "space";
  return key.toLowerCase();
}

/**
 * shiftMatters reports whether Shift should be part of a key's signature.
 * Shift is already baked into a punctuation character — "?" is Shift+/ on most
 * layouts — so including it there would stop "?" ever matching. For letters
 * and named keys it is a real distinction: "shift+mod+z" is not "mod+z".
 */
function shiftMatters(key: string): boolean {
  return key.length > 1 || /^[a-z0-9]$/.test(key);
}

function signature(c: Chord): string {
  const parts: string[] = [];
  if (c.mod) parts.push("mod");
  if (c.ctrl) parts.push("ctrl");
  if (c.shift && shiftMatters(c.key)) parts.push("shift");
  if (c.alt) parts.push("alt");
  parts.push(c.key);
  return parts.join("+");
}

/** parseChord turns one config chord into its canonical signature. */
export function parseChord(chord: string): string {
  const parts = chord.split("+");
  const key = normaliseKey(parts.pop() ?? "");
  const c: Chord = { key, mod: false, ctrl: false, shift: false, alt: false };
  for (const raw of parts) {
    switch (raw.toLowerCase()) {
      case "mod":
        c.mod = true;
        break;
      case "cmd":
      case "meta":
      case "super":
        // On macOS Cmd is the primary modifier; elsewhere it is a key of its
        // own that nothing in the stock keymap binds, so it folds onto mod.
        c.mod = true;
        break;
      case "ctrl":
      case "control":
        // Off macOS, Ctrl is the primary modifier, so "ctrl+n" and "mod+n" are
        // the same chord there and the first one bound wins.
        if (isMac) c.ctrl = true;
        else c.mod = true;
        break;
      case "shift":
        c.shift = true;
        break;
      case "alt":
      case "option":
        c.alt = true;
        break;
    }
  }
  return signature(c);
}

/**
 * eventKey reads the key a press means rather than the character it produced.
 * Option on macOS rewrites the digit row — ⌥1 arrives as "¡" — so digits are
 * taken from the physical key, which is what makes ⌥1–3 layouts and ⌃1–4 modes
 * resolve the same way whatever the keyboard layout.
 */
function eventKey(e: KeyboardEvent): string {
  const digit = /^Digit([0-9])$/.exec(e.code);
  return digit === null ? normaliseKey(e.key) : digit[1];
}

/** eventSignature reduces a keyboard event to the same form as parseChord. */
export function eventSignature(e: KeyboardEvent): string {
  return signatureOf(e, e.shiftKey);
}

/**
 * unshiftedSignature reads the press as though Shift were not held, so a chord
 * nothing binds with Shift can still be recognised as its plain binding. It is
 * how Shift+arrow reaches the focus actions as an "extend the selection"
 * variant: the four focus chords stay configurable in one place, rather than
 * gaining shifted twins that Go's DefaultKeymap and the settings catalogue
 * would both have to be kept in step with.
 */
export function unshiftedSignature(e: KeyboardEvent): string {
  return signatureOf(e, false);
}

function signatureOf(e: KeyboardEvent, shift: boolean): string {
  const primary = isMac ? e.metaKey : e.ctrlKey;
  // The Windows key binds nothing, so a chord held with it is not a binding
  // at all rather than a binding with the modifier ignored. Ctrl on macOS is a
  // modifier in its own right and is carried through instead.
  if (!isMac && e.metaKey) return " unbound";
  return signature({
    key: eventKey(e),
    mod: primary,
    ctrl: isMac && e.ctrlKey,
    shift,
    alt: e.altKey,
  });
}

/**
 * buildLookup maps every chord in the keymap to its action. A chord bound
 * twice is rejected by the backend's config validation, so first write wins
 * here without needing a tie-break.
 */
export function buildLookup(keymap: Record<string, string[] | null | undefined>): Map<string, string> {
  const lookup = new Map<string, string>();
  for (const [action, chords] of Object.entries(keymap)) {
    for (const chord of chords ?? []) {
      const sig = parseChord(chord);
      if (!lookup.has(sig)) lookup.set(sig, action);
    }
  }
  return lookup;
}

/** formatChord renders a chord for the keymap overlay. */
export function formatChord(chord: string): string {
  const parts = chord.split("+");
  const key = parts.pop() ?? "";
  const pretty = parts.map((p) => {
    switch (p.toLowerCase()) {
      case "mod":
        return isMac ? "⌘" : "Ctrl";
      case "ctrl":
      case "control":
        return isMac ? "⌃" : "Ctrl";
      case "shift":
        return isMac ? "⇧" : "Shift";
      case "alt":
      case "option":
        return isMac ? "⌥" : "Alt";
      default:
        return p;
    }
  });
  const names: Record<string, string> = {
    arrowleft: "←",
    arrowright: "→",
    arrowup: "↑",
    arrowdown: "↓",
    space: "Space",
    escape: "Esc",
    enter: "↩",
    tab: "Tab",
  };
  pretty.push(names[key.toLowerCase()] ?? (key.length === 1 ? key.toUpperCase() : key));
  return pretty.join(isMac ? "" : "+");
}

/**
 * ownsKeys reports whether the event landed somewhere that handles its own
 * keyboard, where the global keymap must stay out of the way: a field the user
 * is typing into, or a region that marks itself with data-keys="local" — the
 * tree, whose arrows mean something different from the grid's.
 *
 * Nothing here traps focus. Every such region gives the keyboard back on Esc.
 */
export function ownsKeys(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable) return true;
  return target.closest('[data-keys="local"]') !== null;
}

/**
 * inMapRegion reports whether the event landed inside MAP's wrap, which is
 * marked data-keys="map": a region whose own handler claims a handful of keys
 * (f, −/+, ⏎) and stops their propagation, deliberately letting everything
 * else bubble so the global chords keep working. It is distinct from
 * data-keys="local", which exists so typing in a field does not trigger the
 * keymap — the map is not a text input, and marking it local swallowed every
 * unclaimed key: ⌘Z, ⌃1, /, ?, Esc-in-one-press and the geotag dialog's ⏎
 * all died while the map held focus. The global listener processes whatever
 * bubbles out of this region, with the few browser-default exceptions it
 * names itself (Tab, which walks the pins).
 */
export function inMapRegion(target: EventTarget | null): boolean {
  return target instanceof HTMLElement && target.closest('[data-keys="map"]') !== null;
}
