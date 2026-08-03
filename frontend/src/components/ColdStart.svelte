<script lang="ts">
  // The centre pane with no folder open. The design draws this less as an
  // empty state than as an offer: name what is plugged in, say how few keys it
  // takes to start, and get out of the way.
  //
  // What "card detected" can honestly claim today: the backend has no
  // removable-media signal — only IsNetwork, cached per root in app.network. So
  // the eyebrow appears for a root that sits directly under a mount point, or
  // that the network probe flagged, and it says nothing about capacity,
  // read-only state or when the card was last seen. A real signal — a watcher
  // that fires on insertion and carries those facts — is backend work and is
  // not built.

  import { tick } from "svelte";
  import { addRoot, openFolder } from "../lib/actions";
  import { formatChord } from "../lib/keymap";
  import { app, picker } from "../lib/state.svelte";

  interface Props {
    /** The path the sidebar's box holds, shared so the two never disagree. */
    path?: string;
    /**
     * The session-naming contract, unimplemented on purpose.
     *
     * Ingest — copying a card into a named session rather than culling it where
     * it lies — has no backend and no plan type, so nothing can consume a name
     * yet. The field is therefore drawn only when a caller passes a handler:
     * `onname` receives the name as typed and trimmed, and fires on every
     * change, so the caller holds the value. It is seeded from the detected
     * volume's name. Wiring it means giving it somewhere to go, not styling it.
     */
    onname?: (name: string) => void;
  }

  let { path = $bindable(""), onname }: Props = $props();

  /** Where removable volumes are mounted. A card root is one segment below. */
  const MOUNTS = ["/Volumes/", "/media/", "/mnt/"];

  function basename(p: string): string {
    const parts = p.replace(/\/+$/, "").split("/");
    return parts[parts.length - 1] || p;
  }

  /** True for a volume's own root — /Volumes/FUJI_SD, not a folder inside it. */
  function isVolumeRoot(p: string): boolean {
    const clean = p.replace(/\/+$/, "");
    for (const mount of MOUNTS) {
      if (!clean.startsWith(mount)) continue;
      return !clean.slice(mount.length).includes("/");
    }
    return false;
  }

  // The first candidate wins. Sources are listed in the order the user added
  // them, and with no better signal the earliest is as good a guess as any.
  let detected = $derived(app.roots.find((r) => isVolumeRoot(r) || app.network[r] === true) ?? null);

  // What ⏎ would open: whatever is in the path box, or the detected volume.
  let target = $derived(path.trim() !== "" ? path.trim() : detected);

  // The seed follows the detection rather than being taken once, because roots
  // arrive after the first paint. Null means the user has not typed, so the
  // seed still applies; an empty string is a name they deliberately cleared.
  let typed = $state<string | null>(null);
  let name = $derived(typed ?? (detected === null ? "" : basename(detected)));
  $effect(() => onname?.(name.trim()));

  /** The chord the user actually has bound, falling back to the stock one. */
  function key(action: string, fallback: string): string {
    const chord = app.keymap[action]?.[0];
    return chord === undefined ? fallback : formatChord(chord);
  }

  async function cull() {
    if (target === null) return;
    path = target;
    await openFolder(target);
    // A folder opened from here joins the tree the same way a typed one does,
    // so it can be got back to without retyping.
    if (app.error === "" && app.folder !== null) addRoot(app.folder.dir);
  }

  /** The path box only exists while the sidebar is open, so reveal it first. */
  async function reveal() {
    if (!app.sidebar) {
      app.sidebar = true;
      await tick();
    }
    picker.focus();
  }

  interface Step {
    key: string;
    name: string;
    note: string;
    primary: boolean;
    run: () => void;
  }

  let steps = $derived<Step[]>([
    // ⏎ reaches this through the path box, which takes focus on a cold start
    // and submits on Enter. The row is the same action for the mouse.
    ...(target === null
      ? []
      : [{ key: "↩", name: "cull in place", note: target, primary: true, run: () => void cull() }]),
    {
      key: key("focus-path", "o"),
      name: "open another folder",
      note: "type a path — ~ works",
      primary: false,
      run: () => void reveal(),
    },
    {
      key: key("keymap-overlay", "?"),
      name: "keys",
      note: "every binding, read from config.toml",
      primary: false,
      run: () => (app.overlay = true),
    },
  ]);
</script>

<div class="cold">
  <div class="column">
    <div class="hero">
      {#if detected !== null}
        <span class="eyebrow">card detected</span>
      {/if}
      <h1 class="headline">{target === null ? "nothing open yet" : basename(target)}</h1>
      <p class="meta" title={target ?? ""}>
        {target === null ? "point culler at a card or a folder to start" : `${target} · not indexed yet`}
      </p>
    </div>

    {#if onname !== undefined}
      <div class="session">
        <div class="rule">
          <span class="label">Session</span>
          <span class="hair"></span>
        </div>
        <input
          class="field"
          type="text"
          spellcheck="false"
          autocapitalize="off"
          autocorrect="off"
          value={name}
          oninput={(e) => (typed = e.currentTarget.value)}
          placeholder="name this session"
          aria-label="Session name"
        />
      </div>
    {/if}

    <div class="steps">
      {#each steps as step (step.name)}
        <button class="step" class:primary={step.primary} onclick={step.run}>
          <span class="key">{step.key}</span>
          <span class="name">{step.name}</span>
          <span class="note" title={step.note}>{step.note}</span>
        </button>
      {/each}
    </div>
  </div>
</div>

<style>
  .cold {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: grid;
    place-items: center;
    padding: 40px;
    overflow: auto;
  }

  .column {
    width: 560px;
    max-width: 100%;
    display: flex;
    flex-direction: column;
    gap: 22px;
    min-width: 0;
  }

  .hero {
    display: flex;
    flex-direction: column;
    gap: 7px;
    min-width: 0;
  }

  /* One of the design's two hero eyebrows, and the only place brand cyan
     appears in the cull screens. Never the UI accent — this marks the product,
     not something you can interact with. */
  .eyebrow {
    font-size: 11px;
    letter-spacing: 0.2em;
    text-transform: uppercase;
    color: var(--brand);
  }

  /* Public Sans appears in the application exactly once, here. */
  .headline {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 26px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text-hi);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta {
    margin: 0;
    font-size: 12px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .session {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-width: 0;
  }

  .rule {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .label {
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .hair {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  .field {
    font: inherit;
    font-size: 11.5px;
    height: 26px;
    padding: 0 10px;
    border-radius: 6px;
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text);
    outline: none;
    min-width: 0;
    -webkit-user-select: text;
    user-select: text;
  }

  .field:focus {
    border-color: var(--border-focus);
  }

  .field::placeholder {
    color: var(--text-dim);
  }

  .steps {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .step {
    display: flex;
    align-items: center;
    gap: 13px;
    height: 44px;
    padding: 0 14px;
    border-radius: 6px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    text-align: left;
    cursor: pointer;
    min-width: 0;
  }

  /* The one step that starts work takes the UI accent, the way every other
     primary in the design does; the rest stay on the secondary button box. */
  .step.primary {
    background: var(--accent-wash-10);
    border-color: var(--accent-wash-18);
  }

  .step:hover {
    border-color: var(--border-focus);
  }

  .step .key {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 5px;
    background: var(--bg-kbd);
    color: var(--text-2);
    font-size: 11px;
    font-weight: 700;
  }

  .step.primary .key {
    background: var(--accent);
    color: var(--on-accent);
  }

  .step .name {
    flex: 0 0 auto;
    font-size: 13px;
    color: var(--text);
    white-space: nowrap;
  }

  .step.primary .name {
    color: var(--text-hi);
  }

  .step .note {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
