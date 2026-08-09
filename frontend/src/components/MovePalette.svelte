<script lang="ts">
  // m and c. Where the frames go.
  //
  // The palette assigns a destination; it moves nothing. That separation is
  // the whole design: routing is a decision like a verdict, reversible until
  // the apply, and the apply is the one place files are touched. Whether the
  // apply copies or moves is a setting, not a key — a card is the only copy of
  // a photograph until an import has finished — so the verb in the title and on
  // the confirm is the verb the user asked for by pressing m or c, and the
  // "nothing moves until the apply" line under the preview is what keeps it
  // from reading as a promise about what the apply will do.
  //
  // Three things a destination can be, and the palette has to make which one is
  // which obvious before ⏎:
  //
  //   a place already in the list  — recent, pinned, on a digit
  //   a folder the library has     — fuzzy-matched against the catalogue
  //   somewhere new                — typed, and offered as an explicit create
  //
  // Digits are slots. With the palette open, 1-9 route straight to the
  // destination bound to that digit and 0 clears the routing, which is the
  // two-keystroke path R8 is about. A digit is only a slot while nothing has
  // been typed: every real destination starts with /, ~ or a letter, so a
  // typed path is never ambiguous with a slot.
  //
  // The field is a real <input>. It holds the text, the caret and the
  // selection, and the frame's keyboard hands it ← → ⌥← ⌘← Home End and
  // Backspace; ↑↓ walk the rows and ⏎ takes the one under the cursor.

  import { clearDestination, message, setDestination } from "../lib/decisions";
  import { basename, expandTemplate, isTemplate, NO_TOKENS, resolve, standalone } from "../lib/destination";
  import type { Tokens } from "../lib/destination";
  import { exifCache, valueOf } from "../lib/exifcache.svelte";
  import { destinations, libraryFolders, palette, rank } from "../lib/palette.svelte";
  import type { DestinationDTO, GroupDTO, LibraryFolderDTO } from "../lib/bindings";
  import { app } from "../lib/state.svelte";
  import PaletteFrame from "./PaletteFrame.svelte";

  /** What a row is: where it came from, and what picking it would mean. */
  type RowKind = "remembered" | "folder" | "create";

  interface Row {
    /** The destination as it would be recorded — relative or absolute. */
    path: string;
    /** Why the row is here, or what it is. */
    meta: string;
    /** The digit that reaches it, 0 for none. */
    digit: number;
    pinned: boolean;
    kind: RowKind;
  }

  interface Group {
    title: string;
    hint: string;
    rows: Row[];
  }

  /** How many per-file lines the result block shows before it summarises. */
  const RESULT_LINES = 3;
  /** How many catalogued folders are offered at once. */
  const FOLDER_ROWS = 8;

  let copying = $derived(palette.kind === "copy");
  let verb = $derived(copying ? "copy" : "move");
  let typed = $derived(palette.query.trim());

  // Both lists are the backend's, so they are fetched when the palette opens
  // rather than held across the session: another window, an index pass or an
  // apply can change either.
  $effect(() => {
    void destinations.load();
    void libraryFolders.load();
  });

  let targets = $derived(app.targets);
  /** The frame a template is expanded against, to show one worked example. */
  let example = $derived(app.focused ?? targets[0] ?? null);

  /* ---- the rows ---- */

  let remembered = $derived.by(() =>
    rank(palette.query, destinations.rows, (d: DestinationDTO) => [d.path, d.label]).map(
      (d): Row => ({
        path: d.path,
        meta: d.pinned ? "pinned" : d.useCount > 0 ? `used ${d.useCount}×` : "remembered",
        digit: d.digit,
        pinned: d.pinned,
        kind: "remembered",
      }),
    ),
  );

  /**
   * The catalogued folders that answer what has been typed, minus the ones
   * already on the remembered list. Deduplication is on where a destination
   * lands rather than on how it is written: "2026/portraits" and the absolute
   * path to the same folder are one place, and offering both would be offering
   * a choice that is not one.
   */
  let suggested = $derived.by(() => {
    const taken = new Set(remembered.map((r) => resolve(r.path, libraryFolders.root)));
    const out: Row[] = [];
    const matches = rank(palette.query, libraryFolders.rows, (f: LibraryFolderDTO) => [
      libraryFolders.destinationFor(f),
      f.name,
    ]);
    for (const f of matches) {
      const path = libraryFolders.destinationFor(f);
      if (path === "" || taken.has(resolve(path, libraryFolders.root))) continue;
      taken.add(resolve(path, libraryFolders.root));
      out.push({
        path,
        meta: `${f.frames} frame${f.frames === 1 ? "" : "s"} filed here`,
        digit: 0,
        pinned: false,
        kind: "folder",
      });
      if (out.length === FOLDER_ROWS) break;
    }
    return out;
  });

  /**
   * A path nobody has used before is still a destination — typing one is the
   * whole point of the field — but it is offered as a create rather than
   * quietly treated as a match, so filing into a new folder is something the
   * user chose rather than something a typo did.
   */
  let creating = $derived.by((): Row | null => {
    if (typed === "") return null;
    const landing = resolve(typed, libraryFolders.root);
    const known = [...remembered, ...suggested].some((r) => resolve(r.path, libraryFolders.root) === landing);
    if (known) return null;
    return {
      path: typed,
      meta: isTemplate(typed) ? "create per frame" : "create and use",
      digit: 0,
      pinned: false,
      kind: "create",
    };
  });

  /**
   * Whether what has been typed is a search or a path. A bare word is a search,
   * so the folder it names leads and ⏎ files into the library the user already
   * has; anything with a separator, a ~ or a token in it is a path being
   * written out, so the create row leads and ⏎ means exactly what was typed —
   * which is what the field did before it suggested anything.
   */
  let searching = $derived(typed !== "" && !typed.includes("/") && !standalone(typed) && !isTemplate(typed));

  let groups = $derived.by(() => {
    const create: Group = {
      title: "New folder",
      hint: "created by the apply, never before it",
      rows: creating === null ? [] : [creating],
    };
    const mru: Group = { title: "Destinations", hint: "recent and pinned · 1–9", rows: remembered };
    const folders: Group = { title: "Library folders", hint: "already in the catalogue", rows: suggested };
    const order = searching ? [mru, folders, create] : [create, mru, folders];
    return order.filter((g) => g.rows.length > 0);
  });

  let rows = $derived(groups.flatMap((g) => g.rows));
  let cursor = $derived(Math.min(palette.index, Math.max(0, rows.length - 1)));
  let chosen = $derived(rows[cursor] ?? null);

  /* ---- what the apply would do with it ---- */

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

  /** A template naming the camera needs the metadata to show a real example. */
  $effect(() => {
    if (chosen === null || example === null) return;
    if (!chosen.path.includes("{camera}") && !chosen.path.includes("{lens}")) return;
    exifCache.request([example]);
  });

  function tokensFor(g: GroupDTO | null): Tokens {
    if (g === null) return NO_TOKENS;
    const primary = g.rawPath !== "" ? g.rawPath : g.jpegPath;
    const dot = primary.lastIndexOf(".");
    const shot = new Date(g.shot);
    const metadata = exifCache.of(g);
    return {
      shot: g.shot !== "" && !Number.isNaN(shot.getTime()) ? shot : null,
      stem: g.stem,
      ext: dot === -1 ? "" : primary.slice(dot + 1).toLowerCase(),
      camera: valueOf(metadata, "Model") || valueOf(metadata, "Make"),
      lens: valueOf(metadata, "LensModel"),
    };
  }

  /**
   * Where the frames under the cursor would actually land: the destination
   * expanded against the example frame if it is a template, then resolved
   * against the library root if it is relative.
   */
  let landing = $derived.by(() => {
    if (chosen === null) return { folder: "", expanded: "", unanswered: [] as string[], error: "" };
    if (!isTemplate(chosen.path)) {
      return { folder: resolve(chosen.path, libraryFolders.root), expanded: "", unanswered: [], error: "" };
    }
    const out = expandTemplate(chosen.path, tokensFor(example));
    if (out.error !== "") return { folder: "", expanded: "", unanswered: out.unanswered, error: out.error };
    if (out.path === "") {
      return {
        folder: "",
        expanded: "",
        unanswered: out.unanswered,
        error: "this frame answers none of the template's tokens",
      };
    }
    return {
      folder: resolve(out.path, libraryFolders.root),
      expanded: out.path,
      unanswered: out.unanswered,
      error: "",
    };
  });

  /** How the destination is written, which is what decides where it lands. */
  let shape = $derived.by(() => {
    if (chosen === null) return "";
    if (standalone(chosen.path)) return "absolute path";
    if (libraryFolders.root === "") return "library-relative";
    return `library-relative · under ${libraryFolders.root}`;
  });

  /* ---- doing it ---- */

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

<PaletteFrame width={760} label="{verb} frames to a destination" count={rows.length} onrun={run}>
  {#snippet header()}
    <div class="stack">
      <div class="line">
        <span class="badge">{copying ? "COPY" : "MOVE"}</span>
        <span class="title">
          {verb}
          {targets.length}
          frame{targets.length === 1 ? "" : "s"} to…
        </span>
        <span class="spacer"></span>
        <span class="count">
          {files.length} file{files.length === 1 ? "" : "s"}
          {#if sidecars > 0}· {sidecars} sidecar{sidecars === 1 ? "" : "s"}{/if}
        </span>
      </div>
      <div class="line">
        <span class="prompt">›</span>
        <input
          class="field"
          data-palette-field
          type="text"
          autocomplete="off"
          autocorrect="off"
          spellcheck="false"
          aria-label="destination"
          placeholder="type a path, search the library, or press a digit"
          bind:value={palette.query}
          oninput={() => (palette.index = 0)}
        />
      </div>
    </div>
  {/snippet}

  {#snippet chips()}
    <span class="chip on">sidecars follow the RAW</span>
    <span class="chip on">the card is never modified</span>
    <span class="chip">never overwrites</span>
    <span class="chip">applied on ⏎ in the grid</span>
  {/snippet}

  {#snippet body()}
    {#if isTemplate(palette.query)}
      <div class="tokens">
        <span class="tlabel">template</span>
        <span class="thint">
          {"{date:2006-01-02}"} · {"{camera}"} · {"{lens}"} · {"{stem}"} · {"{ext}"} — one folder per frame, worked
          out at the apply
        </span>
      </div>
    {/if}
    {#if !destinations.loaded}
      <p class="none">reading the destination list…</p>
    {:else if destinations.error !== ""}
      <p class="none">could not read the destinations: {destinations.error}</p>
    {:else if rows.length === 0}
      <p class="none">no destination yet — type one</p>
    {/if}
    {#each groups as group (group.title)}
      <div class="section">
        <span class="stitle">{group.title}</span>
        <span class="rule"></span>
        <span class="shint">{group.hint}</span>
      </div>
      {#each group.rows as row (row.kind + ":" + row.path)}
        {@const index = rows.indexOf(row)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="row"
          class:at={index === cursor}
          data-destination={row.path}
          data-kind={row.kind}
          onmouseenter={() => (palette.index = index)}
          onclick={() => void assign(row.path, false)}
        >
          <span class="icon">{row.kind === "create" ? "+" : row.pinned ? "★" : row.kind === "folder" ? "▪" : "▸"}</span>
          <span class="rpath">{row.path}</span>
          <span class="rmeta">{row.meta}</span>
          <span class="chords">
            {#if row.digit > 0}<span class="cap slot">{row.digit}</span>{/if}
            {#if index === cursor}<span class="cap">⏎</span>{/if}
          </span>
        </div>
      {/each}
    {/each}
    {#if libraryFolders.loaded && libraryFolders.error !== ""}
      <p class="none">no folders to suggest: {libraryFolders.error}</p>
    {/if}
  {/snippet}

  {#snippet footer()}
    <div class="section tight">
      <span class="stitle">Lands in</span>
      <span class="rule"></span>
      <span class="shint">nothing moves until the apply</span>
    </div>
    {#if chosen === null}
      <p class="pending">pick a destination to see where the frames would land</p>
    {:else}
      <div class="dest">
        <span class="sign">→</span>
        {#if landing.error !== ""}
          <span class="warn">{landing.error}</span>
        {:else}
          <span class="folder">{landing.folder}</span>
          <span class="shape">{shape}</span>
        {/if}
      </div>
      {#if landing.expanded !== ""}
        <p class="more">
          {chosen.path} expands per frame · {example === null ? "no frame to show" : `e.g. ${example.stem} → ${landing.expanded}`}
          {#if landing.unanswered.length > 0}· this frame answers no {landing.unanswered.join(", ")}{/if}
        </p>
      {/if}
      {#if landing.folder !== ""}
        {#each files.slice(0, RESULT_LINES) as file (file)}
          <div class="line-file">
            <span class="from">{file}</span>
            <span class="arrow">→</span>
            <span class="to">{landing.folder}/{basename(file)}</span>
          </div>
        {/each}
        {#if files.length > RESULT_LINES}
          <p class="more">and {files.length - RESULT_LINES} more</p>
        {/if}
      {/if}
    {/if}
    <div class="buttons">
      <span class="cancel">esc cancel</span>
      <span class="spacer"></span>
      <span class="secondary">1–9 slot · 0 clears</span>
      <span class="secondary">pin it ⌥⏎</span>
      <span class="primary">{verb} ⏎</span>
    </div>
  {/snippet}
</PaletteFrame>

<style>
  .stack {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 9px;
  }

  .line {
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }

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

  .title {
    flex: 0 0 auto;
    font-size: 11.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .prompt {
    flex: 0 0 auto;
    font-size: 14px;
    color: var(--accent);
  }

  .field {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    outline: none;
    font: inherit;
    font-size: 14px;
    color: var(--text-hi);
    caret-color: var(--accent);
  }

  .field::placeholder {
    color: var(--text-ghost);
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

  .tokens {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 9px 16px 0;
  }

  .tlabel {
    flex: 0 0 auto;
    padding: 2px 6px;
    border-radius: 3px;
    background: var(--accent-wash-16);
    color: var(--accent);
    font-size: 9.5px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .thint {
    min-width: 0;
    font-size: 10.5px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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

  .dest {
    display: flex;
    align-items: baseline;
    gap: 9px;
    min-width: 0;
    padding-bottom: 3px;
  }

  .folder {
    min-width: 0;
    font-size: 12.5px;
    color: var(--keep-text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .shape {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--text-ghost);
    white-space: nowrap;
  }

  .warn {
    font-size: 11.5px;
    color: var(--cut-text);
  }

  .line-file {
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
