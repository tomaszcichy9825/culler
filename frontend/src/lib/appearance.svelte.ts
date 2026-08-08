// Colour scheme, accent and monospace face.
//
// These three are the settings that are not in config.json: the file has no
// fields for them, so they are remembered by the app itself and the settings
// page says so rather than pretending they were written. Everything else on
// every settings page goes through settings.svelte.ts and the config file.
//
// The scheme half is not a new mechanism. index.html resolves data-theme on
// <html> before the first paint and keeps following the OS; this module writes
// the same localStorage key and applies the same attribute, so a change here
// and a relaunch agree.

import { remember, stored } from "./persist";

/** Where the resolved scheme is remembered — the key index.html reads. */
const THEME_KEY = "culler.theme";
const ACCENT_KEY = "culler.accent";
const MONO_KEY = "culler.mono";

export type Scheme = "system" | "dark" | "light";
export type AccentName = "blue" | "cyan" | "purple";

export const SCHEMES: Scheme[] = ["system", "dark", "light"];
export const ACCENTS: AccentName[] = ["blue", "cyan", "purple"];

type Rgb = [number, number, number];

/**
 * The accent hues offered by 10d. Blue is the value already in style.css for
 * each theme, so choosing it clears the overrides rather than restating them —
 * the default stays pixel-exact with what the design authored, including the
 * derived tokens that are hand-tuned there.
 */
const ACCENT_RGB: Record<AccentName, { dark: Rgb; light: Rgb }> = {
  blue: { dark: [97, 175, 239], light: [47, 111, 224] },
  cyan: { dark: [86, 182, 194], light: [14, 135, 146] },
  purple: { dark: [198, 120, 221], light: [141, 63, 176] },
};

/** The surfaces the derived accent tokens are mixed against, per theme. */
const MIX_SURFACE: Record<"dark" | "light", Rgb> = { dark: [20, 22, 26], light: [255, 255, 255] };
const MIX_MUTED: Record<"dark" | "light", Rgb> = { dark: [139, 145, 158], light: [90, 97, 111] };

function mix(a: Rgb, b: Rgb, t: number): Rgb {
  return [
    Math.round(a[0] * t + b[0] * (1 - t)),
    Math.round(a[1] * t + b[1] * (1 - t)),
    Math.round(a[2] * t + b[2] * (1 - t)),
  ];
}

function rgb(c: Rgb): string {
  return `rgb(${c[0]}, ${c[1]}, ${c[2]})`;
}

function rgba(c: Rgb, alpha: number): string {
  return `rgba(${c[0]}, ${c[1]}, ${c[2]}, ${alpha})`;
}

/** Every token that carries the accent hue, so one swap moves all of them. */
function accentTokens(base: Rgb, theme: "dark" | "light"): Record<string, string> {
  const surface = MIX_SURFACE[theme];
  const muted = MIX_MUTED[theme];
  // The ratios reproduce the authored blue values from #61afef; they are only
  // ever used for the other two hues.
  const selected = theme === "dark" ? mix(base, surface, 0.32) : mix(base, surface, 0.45);
  const paneFocus = theme === "dark" ? mix(base, surface, 0.42) : mix(base, surface, 0.62);
  return {
    "--accent": rgb(base),
    "--accent-wash-10": rgba(base, 0.1),
    "--accent-wash-14": rgba(base, 0.14),
    "--accent-wash-16": rgba(base, 0.16),
    "--accent-wash-18": rgba(base, 0.18),
    "--focus-ring": `0 0 0 2px ${rgba(base, 0.4)}`,
    "--focus-ring-soft": `0 0 0 2px ${rgba(base, 0.35)}`,
    "--focus-inset": `inset 0 0 0 1px ${rgba(base, 0.3)}`,
    "--focus-inset-2": `inset 0 0 0 1px ${rgba(base, 0.35)}`,
    "--focus-edge": `inset 2px 0 0 ${rgb(base)}`,
    "--border-focus": rgb(base),
    "--border-selected": rgb(selected),
    "--border-pane-focus": rgb(paneFocus),
    "--text-on-focus-hint": rgb(mix(base, muted, 0.5)),
  };
}

const ACCENT_TOKEN_NAMES = Object.keys(accentTokens([0, 0, 0], "dark"));

function isScheme(v: string): v is Scheme {
  return v === "system" || v === "dark" || v === "light";
}

function isAccent(v: string): v is AccentName {
  return v === "blue" || v === "cyan" || v === "purple";
}

/** The face that ships with the app, and the fallbacks behind whatever is set. */
export const DEFAULT_MONO = "JetBrains Mono";
const MONO_FALLBACK = `"${DEFAULT_MONO}", ui-monospace, monospace`;

class AppearanceState {
  scheme = $state<Scheme>("system");
  accent = $state<AccentName>("blue");
  /** The face name as typed. Empty means the bundled default. */
  mono = $state<string>("");

  /** Whether the system currently asks for light, tracked while scheme is system. */
  private systemLight = $state(false);
  private started = false;

  /** dark or light, with system resolved. */
  get theme(): "dark" | "light" {
    if (this.scheme === "system") return this.systemLight ? "light" : "dark";
    return this.scheme;
  }

  /**
   * start reads what was remembered and applies it. Safe to call more than
   * once; the second call is a no-op.
   */
  start() {
    if (this.started || typeof document === "undefined") return;
    this.started = true;

    this.scheme = stored(THEME_KEY, (raw) => (isScheme(raw) ? raw : null), "system");
    this.accent = stored(ACCENT_KEY, (raw) => (isAccent(raw) ? raw : null), "blue");
    this.mono = stored(MONO_KEY, (raw) => raw, "");

    const media = window.matchMedia("(prefers-color-scheme: light)");
    this.systemLight = media.matches;
    media.addEventListener("change", (e) => {
      this.systemLight = e.matches;
      // The accent's derived values are mixed against the theme's surface, so
      // the OS flipping under a system scheme has to redraw them.
      this.apply();
    });

    this.apply();
  }

  setScheme(scheme: Scheme) {
    this.scheme = scheme;
    remember(THEME_KEY, scheme);
    this.apply();
  }

  setAccent(accent: AccentName) {
    this.accent = accent;
    remember(ACCENT_KEY, accent);
    this.apply();
  }

  /** setMono takes a face name; empty restores the bundled one. */
  setMono(face: string) {
    this.mono = face.trim();
    remember(MONO_KEY, this.mono);
    this.apply();
  }

  /** apply writes the resolved values onto <html>, where every token lives. */
  apply() {
    if (typeof document === "undefined") return;
    const root = document.documentElement;
    root.setAttribute("data-theme", this.theme);

    for (const name of ACCENT_TOKEN_NAMES) root.style.removeProperty(name);
    if (this.accent !== "blue") {
      const tokens = accentTokens(ACCENT_RGB[this.accent][this.theme], this.theme);
      for (const [name, value] of Object.entries(tokens)) root.style.setProperty(name, value);
    }

    if (this.mono === "" || this.mono === DEFAULT_MONO) root.style.removeProperty("--font-mono");
    else root.style.setProperty("--font-mono", `"${this.mono}", ${MONO_FALLBACK}`);
  }

  /** The swatch drawn beside the accent chips, as a CSS colour. */
  swatch(accent: AccentName): string {
    return rgb(ACCENT_RGB[accent][this.theme]);
  }

  /** The same colour written the way the token sheet writes it. */
  swatchHex(accent: AccentName): string {
    const c = ACCENT_RGB[accent][this.theme];
    return `#${c.map((n) => n.toString(16).padStart(2, "0")).join("")}`;
  }
}

export const appearance = new AppearanceState();
