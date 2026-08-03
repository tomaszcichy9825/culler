<script lang="ts">
  // m and c. Where the frames go.
  //
  // The palette assigns a destination; it moves nothing. That separation is
  // the whole design: routing is a decision like a verdict, reversible until
  // the apply, and the apply is the one place files are touched. Whether the
  // apply copies or moves is a setting, not a key — a card is the only copy of
  // a photograph until an import has finished.
  //
  // Digits are slots. With the palette open, 1-9 route straight to the
  // destination bound to that digit and 0 clears the routing, which is the
  // two-keystroke path R8 is about. A digit is only a slot while nothing has
  // been typed: every real destination starts with /, ~ or a letter, so a
  // typed path is never ambiguous with a slot.

  import { clearDestination, message, setDestination } from "../lib/decisions";
  import { destinations, leafOf, palette, rank } from "../lib/palette.svelte";
  import type { DestinationDTO } from "../lib/bindings";
  import { app } from "../lib/state.svelte";
  import PaletteFrame from "./PaletteFrame.svelte";

  interface Row {
    path: string;
    /** Why the row is here, or what it is. */
    meta: string;
    /** The digit that reaches it, 0 for none. */
    digit: number;
    pinned: boolean;
    /** True for a path that has been typed and is not remembered yet. */
    fresh: boolean;
  }

  /** How many per-file lines the result block shows before it summarises. */
  const RESULT_LINES = 3;

  let copying = $derived(palette.kind === "copy");
  let typed = $derived(palette.query.trim());

  // The list is the backend's, so it is fetched when the palette opens rather
  // than held across the session: another window, or an apply, can change it.
  $effect(() => {
    void destinations.load();
  });

  let rows = $derived.by(() => {
    const known: Row[] = rank(palette.query, destinations.rows, (d: DestinationDTO) => [d.path, d.label]).map(
      (d) => ({
        path: d.path,
        meta: d.pinned ? "pinned" : d.useCount > 0 ? `used ${d.useCount}×` : "remembered",
        digit: d.digit,
        pinned: d.pinned,
        fresh: false,
      }),
    );
    // A path nobody has used before is still a destination; it leads, because
    // typing one is the whole point of the field.
    if (typed !== "" && !destinations.has(typed)) {
      known.unshift({ path: typed, meta: "create and use", digit: 0, pinned: false, fresh: true });
    }
    return known;
  });

  let cursor = $derived(Math.min(palette.index, Math.max(0, rows.length - 1)));
  let chosen = $derived(rows[cursor] ?? null);

  let targets = $derived(app.targets);

  /** Every file the destination would take, RAW and JPEG halves alike. */
  let files = $derived.by(() => {
    const out: string[] = [];
    for (const g of targets) {
      if (g.rawPath !== "") out.push(g.rawPath);
      if (g.jpegPath !== "") out.push(g.jpegPath);
    }
    return out;
  });

  let sidecars = $derived(targets.reduce((n, g) => n + g.sidecars, 0));

  function basename(path: string): string {
    const cut = path.lastIndexOf("/");
    return cut === -1 ? path : path.slice(cut + 1);
  }

  /** assign routes the target frames and remembers where they went. */
  async function assign(path: string, pin: boolean) {
    if (targets.length === 0) {
      app.notify("no frames to route");
      return;
    }
    const routed = setDestination(path);
    if (routed === 0) return;
    palette.close();
    try {
      await destinations.use(path);
      if (pin) await destinations.pin(path, true);
    } catch (err) {
      // The frames are routed either way; only the palette's memory of the
      // place failed, and saying so beats a list that silently never grows.
      app.notify(`routed, but could not remember ${path}: ${message(err)}`, "error");
      return;
    }
    app.notify(`${routed} frame(s) → ${path}${pin ? " · pinned" : ""}`);
  }

  function run(alt: boolean) {
    if (chosen === null) {
      app.notify("type a destination first");
      return;
    }
    // ⌥⏎ is the same assignment with the destination pinned, which is the only
    // keyboard-only way to promote one above the recent list.
    void assign(chosen.path, alt);
  }

  /**
   * slot routes straight to the destination on a digit, or clears the routing
   * on 0. Returns false when the key means nothing, so the press falls through
   * to the palette's ordinary typing.
   */
  function slot(digit: number): boolean {
    if (digit === 0) {
      const cleared = clearDestination();
      if (cleared > 0) {
        palette.close();
        app.notify(`${cleared} frame(s) staying put`);
      }
      return true;
    }
    const found = destinations.forDigit(digit);
    if (found === null) {
      app.notify(`nothing on ${digit} yet`);
      return true;
    }
    void assign(found.path, false);
    return true;
  }

  /**
   * Digits are only slots while the field is empty. Once a path is being typed
   * they are part of it — 2026 is a folder name.
   */
  function onKeydown(e: KeyboardEvent) {
    if (palette.query !== "" || e.metaKey || e.ctrlKey || e.altKey) return;
    if (!/^[0-9]$/.test(e.key)) return;
    e.preventDefault();
    e.stopPropagation();
    slot(Number(e.key));
  }
</script>

<svelte:document onkeydowncapture={onKeydown} />

<PaletteFrame width={760} label="route to destination" count={rows.length} onrun={run}>
  {#snippet header()}
    <span class="badge">{copying ? "COPY" : "MOVE"}</span>
    {#if typed === ""}
      <span class="placeholder">type a path, or press a digit</span>
    {:else}
      <span class="path">{palette.query}</span>
    {/if}
    <span class="caret" aria-hidden="true"></span>
    <span class="spacer"></span>
    <span class="count">
      {targets.length} frame{targets.length === 1 ? "" : "s"} · {files.length} file{files.length === 1 ? "" : "s"}
      {#if sidecars > 0}· {sidecars} sidecar{sidecars === 1 ? "" : "s"}{/if}
    </span>
  {/snippet}

  {#snippet chips()}
    <span class="chip on">sidecars follow the RAW</span>
    <span class="chip on">the card is never modified</span>
    <span class="chip">never overwrites</span>
    <span class="chip">applied on ⏎ in the grid</span>
  {/snippet}

  {#snippet body()}
    <div class="section">
      <span class="stitle">Destinations</span>
      <span class="rule"></span>
      <span class="shint">type any path · ~ works · tokens like {"{date:2006}"} expand per frame</span>
    </div>
    {#if !destinations.loaded}
      <p class="none">reading the destination list…</p>
    {:else if destinations.error !== ""}
      <p class="none">could not read the destinations: {destinations.error}</p>
    {:else if rows.length === 0}
      <p class="none">no destination yet — type one</p>
    {/if}
    {#each rows as row, index (row.path)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="row"
        class:at={index === cursor}
        data-destination={row.path}
        onmouseenter={() => (palette.index = index)}
        onclick={() => void assign(row.path, false)}
      >
        <span class="icon">{row.pinned ? "★" : "▸"}</span>
        <span class="rpath">{row.path}</span>
        <span class="rmeta">{row.meta}</span>
        <span class="chords">
          {#if row.digit > 0}<span class="cap slot">{row.digit}</span>{/if}
          {#if index === cursor}<span class="cap">⏎</span>{/if}
        </span>
      </div>
    {/each}
  {/snippet}

  {#snippet footer()}
    <div class="section tight">
      <span class="stitle">Result</span>
      <span class="rule"></span>
      <span class="shint">nothing moves until the apply</span>
    </div>
    {#if chosen === null}
      <p class="pending">pick a destination to see where the frames would land</p>
    {:else}
      {#each files.slice(0, RESULT_LINES) as file (file)}
        <div class="line">
          <span class="sign">→</span>
          <span class="from">{file}</span>
          <span class="arrow">→</span>
          <span class="to">{chosen.path}/{basename(file)}</span>
        </div>
      {/each}
      {#if files.length > RESULT_LINES}
        <p class="more">and {files.length - RESULT_LINES} more</p>
      {/if}
    {/if}
    <div class="buttons">
      <span class="cancel">esc cancel</span>
      <span class="spacer"></span>
      <span class="secondary">1–9 slot · 0 clears</span>
      <span class="secondary">pin it ⌥⏎</span>
      <span class="primary">route ⏎</span>
    </div>
  {/snippet}
</PaletteFrame>

<style>
  .badge {
    flex: 0 0 auto;
    padding: 3px 8px;
    border-radius: 4px;
    background: var(--accent-wash-18);
    color: var(--accent);
    font-size: 11px;
    font-weight: 700;
    white-space: nowrap;
  }

  .path {
    flex: 0 0 auto;
    font-size: 14px;
    color: var(--text-hi);
    white-space: pre;
  }

  .placeholder {
    flex: 0 0 auto;
    font-size: 14px;
    color: var(--text-ghost);
  }

  .caret {
    flex: 0 0 auto;
    width: 1px;
    height: 16px;
    background: var(--accent);
  }

  .spacer {
    flex: 1;
  }

  .count {
    flex: 0 0 auto;
    font-size: 11px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .chip {
    padding: 4px 9px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .chip.on {
    background: var(--accent-wash-16);
    border-color: var(--accent);
    color: var(--accent);
    font-weight: 700;
  }

  .section {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 9px 16px 5px;
  }

  .section.tight {
    padding: 0 0 6px;
  }

  .stitle {
    font-size: 9.5px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
    white-space: nowrap;
  }

  .rule {
    flex: 1;
    height: 1px;
    background: var(--border-faint);
  }

  .shint {
    font-size: 9.5px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 36px;
    padding: 0 16px;
    cursor: pointer;
  }

  .row.at {
    background: var(--bg-row-active);
    box-shadow: var(--focus-edge);
  }

  .icon {
    flex: 0 0 14px;
    font-size: 11px;
    color: var(--text-dim);
  }

  .row.at .icon {
    color: var(--accent);
  }

  .rpath {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    color: var(--text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row.at .rpath {
    color: var(--text-hi);
  }

  .rmeta {
    flex: 0 0 auto;
    font-size: 10.5px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .chords {
    flex: 0 0 auto;
    display: flex;
    gap: 3px;
    min-width: 22px;
    justify-content: flex-end;
  }

  .cap {
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    font-weight: 600;
    color: var(--text-2);
  }

  .cap.slot {
    background: var(--accent-wash-16);
    border-color: var(--accent);
    color: var(--accent);
  }

  .none {
    margin: 0;
    padding: 18px 16px;
    font-size: 11.5px;
    color: var(--text-ghost);
  }

  .line {
    display: flex;
    align-items: center;
    gap: 11px;
    font-size: 11px;
    line-height: 1.7;
  }

  .sign {
    flex: 0 0 12px;
    font-weight: 700;
    color: var(--accent);
  }

  .from {
    flex: 1;
    min-width: 0;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
  }

  .arrow {
    flex: 0 0 auto;
    color: var(--text-ghost);
  }

  .to {
    flex: 1;
    min-width: 0;
    color: var(--keep-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: left;
  }

  .pending,
  .more {
    margin: 0;
    font-size: 10.5px;
    color: var(--text-ghost);
  }

  .buttons {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 9px;
  }

  .cancel {
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .secondary {
    padding: 6px 12px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .primary {
    padding: 6px 14px;
    border-radius: 5px;
    background: var(--accent);
    color: var(--on-accent);
    font-size: 11px;
    font-weight: 700;
    white-space: nowrap;
  }
</style>
