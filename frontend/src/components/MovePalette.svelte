<script lang="ts">
  // m and c. Where the frames go, and what goes with them.
  //
  // Transitional in one respect: there is no destinations service and no move
  // or copy call on the backend yet, so the palette collects a destination,
  // remembers it for next time, and says plainly that the files have not been
  // touched. Everything it shows about the files themselves is real — the
  // paths come from the frames the action would act on.

  import { destinations, palette, rank, rememberDestination } from "../lib/palette.svelte";
  import { app } from "../lib/state.svelte";
  import PaletteFrame from "./PaletteFrame.svelte";

  interface Row {
    path: string;
    /** Why the row is here: a recent destination, or what has just been typed. */
    meta: string;
  }

  /** How many per-file lines the result block shows before it summarises. */
  const RESULT_LINES = 3;

  let copying = $derived(palette.kind === "copy");
  let verb = $derived(copying ? "copy" : "move");
  let other = $derived(copying ? "move" : "copy");

  let typed = $derived(palette.query.trim());

  let rows = $derived.by(() => {
    const recent = destinations();
    const out: Row[] = rank(palette.query, recent, (d) => d).map((path) => ({ path, meta: "recent" }));
    // A path nobody has used before is still a destination; it leads, because
    // typing one is the whole point of the field.
    if (typed !== "" && !recent.includes(typed)) out.unshift({ path: typed, meta: "use this path" });
    return out;
  });

  let cursor = $derived(Math.min(palette.index, Math.max(0, rows.length - 1)));
  let chosen = $derived(rows[cursor]?.path ?? "");

  let targets = $derived(app.targets);

  /** Every file the verb would touch, RAW and JPEG halves alike. */
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

  function run(alt: boolean) {
    if (chosen === "") {
      app.notify("type a destination first");
      return;
    }
    if (targets.length === 0) {
      app.notify("no frames to " + verb);
      return;
    }
    // Alt is the secondary verb, per the button row: ⌥⏎ does the other one.
    const doing = alt ? other : verb;
    rememberDestination(chosen);
    palette.close();
    app.notify(`${chosen} remembered — ${doing} lands with the destinations service`);
  }
</script>

<PaletteFrame width={760} label="{verb} destination" count={rows.length} onrun={run}>
  {#snippet header()}
    <span class="badge">{verb.toUpperCase()}</span>
    {#if typed === ""}
      <span class="placeholder">type a destination</span>
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
    <span class="chip on">both halves</span>
    <span class="chip">never overwrites</span>
    <span class="chip warn">no destinations service yet</span>
  {/snippet}

  {#snippet body()}
    <div class="section">
      <span class="stitle">Destinations</span>
      <span class="rule"></span>
      <span class="shint">type any path · ~ works</span>
    </div>
    {#if rows.length === 0}
      <p class="none">no destination yet — type one</p>
    {/if}
    {#each rows as row, index (row.path)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="row"
        class:at={index === cursor}
        onmouseenter={() => (palette.index = index)}
        onclick={() => run(false)}
      >
        <span class="icon">▸</span>
        <span class="rpath">{row.path}</span>
        <span class="rmeta">{row.meta}</span>
        <span class="chords">{#if index === cursor}<span class="cap">⏎</span>{/if}</span>
      </div>
    {/each}
  {/snippet}

  {#snippet footer()}
    <div class="section tight">
      <span class="stitle">Result</span>
      <span class="rule"></span>
      <span class="shint">sidecars and JPEG halves follow the RAW</span>
    </div>
    {#if chosen === ""}
      <p class="pending">pick a destination to see what would {verb}</p>
    {:else}
      {#each files.slice(0, RESULT_LINES) as file (file)}
        <div class="line">
          <span class="sign">{copying ? "+" : "→"}</span>
          <span class="from">{file}</span>
          <span class="arrow">→</span>
          <span class="to">{chosen}/{basename(file)}</span>
        </div>
      {/each}
      {#if files.length > RESULT_LINES}
        <p class="more">and {files.length - RESULT_LINES} more</p>
      {/if}
    {/if}
    <div class="buttons">
      <span class="cancel">esc cancel</span>
      <span class="spacer"></span>
      <span class="secondary">{other} instead ⌥⏎</span>
      <span class="primary">{verb} ⏎</span>
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

  .chip.warn {
    background: var(--amber-wash-14);
    border-color: var(--amber);
    color: var(--amber);
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
