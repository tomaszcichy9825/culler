<script lang="ts">
  // The status bar: what mode you are in, what state that mode is in, the keys
  // that are live right now, and the counts. 30px, and it never scrolls.

  import { formatCount, library } from "../lib/library.svelte";
  import { app } from "../lib/state.svelte";
  import { shell } from "../lib/shell.svelte";
  import ModeBar from "./ModeBar.svelte";

  interface Hint {
    key: string;
    label: string;
  }

  interface Chip {
    text: string;
    tone: "accent" | "amber";
  }

  // What a running apply says, or null. It outranks everything: it is the only
  // state in which files are moving, and the one the user most needs to see is
  // still going.
  let applying = $derived.by<string | null>(() => {
    const p = app.applyProgress;
    if (p === null) return null;
    if (p.phase === "planning") return "PLANNING";
    return p.total > 0 ? `APPLYING ${formatCount(p.done)}/${formatCount(p.total)}` : "APPLYING";
  });

  // The chip names the state the mode is in. Indexing outranks the layout
  // because it is the thing the user is waiting on. A catalogue pass says how
  // far it has got: a climbing frame count while the listing walks, then
  // read-of-total once the hashing phase knows exactly what is left.
  let chip = $derived.by<Chip>(() => {
    if (applying !== null) return { text: applying, tone: "amber" };
    if (app.scanning !== null) return { text: "INDEXING", tone: "amber" };
    const pass = library.indexing;
    if (pass !== null) {
      return {
        text:
          pass.phase === "hashing" && pass.pending > 0
            ? `INDEXING ${formatCount(pass.hashed)}/${formatCount(pass.pending)}`
            : `INDEXING ${formatCount(pass.frames)}`,
        tone: "amber",
      };
    }
    return { text: shell.layoutLabel.toUpperCase(), tone: "accent" };
  });

  // The share of the work done, for the sliver of a bar in the chip: the
  // apply's files while one is running, otherwise the hashing phase. The
  // listing has no total, so it draws no bar — the count is the movement.
  let applyShare = $derived<number | null>(
    app.applyProgress !== null && app.applyProgress.total > 0
      ? Math.max(0, Math.min(100, (app.applyProgress.done / app.applyProgress.total) * 100))
      : null,
  );

  let indexingShare = $derived<number | null>(
    app.applyProgress === null &&
      app.scanning === null &&
      library.indexing !== null &&
      library.indexing.phase === "hashing" &&
      library.indexing.pending > 0
      ? Math.max(0, Math.min(100, (library.indexing.hashed / library.indexing.pending) * 100))
      : null,
  );

  let hints = $derived.by(() => {
    const out: Hint[] = [];
    if (shell.focusedPane !== null || app.view === "loupe") out.push({ key: "esc", label: "back to grid" });
    out.push({ key: "⌘1–3", label: "panes" });
    out.push({ key: "⌃1–3", label: "modes" });
    out.push({ key: "⌥1–3", label: "layout" });
    // Tab walks the same list ⌥1–3 jumps around in, so the two sit together.
    if (shell.nextLayout() !== null) out.push({ key: "⇥", label: "cycle layout" });
    if (app.view !== "loupe") out.push({ key: "space", label: "loupe" });
    return out;
  });
</script>

<footer class="statusbar">
  <ModeBar />

  <span class="chip {chip.tone}">
    {chip.text}
    {#if (applyShare ?? indexingShare) !== null}
      <span class="track" aria-hidden="true">
        <span class="fill" style:width="{applyShare ?? indexingShare}%"></span>
      </span>
    {/if}
  </span>

  <div class="hints">
    {#each hints as hint (hint.key)}
      <span class="hint"><span class="hkey">{hint.key}</span> <span class="hlabel">{hint.label}</span></span>
    {/each}
  </div>

  <div class="counts">
    {#if app.selection.size > 0}
      <span class="count strong">{app.selection.size} selected</span>
    {/if}
    {#if app.pending.length > 0}
      <span class="count decided">{app.pending.length} decided</span>
    {/if}
    <span class="count">{app.groups.length} frames</span>
  </div>
</footer>

<style>
  .statusbar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 12px;
    height: 30px;
    padding: 0 14px;
    background: var(--bg-chrome);
    border-top: 1px solid var(--border);
    font-size: 11px;
    min-width: 0;
  }

  .chip {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 2px 7px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    white-space: nowrap;
  }

  /* The hashing phase's progress, as a sliver inside the chip. It is a
     measurement — read X of M — never an estimate. */
  .track {
    display: inline-block;
    width: 34px;
    height: 3px;
    border-radius: 2px;
    background: var(--amber-wash-16);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    border-radius: 2px;
    background: currentColor;
    transition: width 200ms ease;
  }

  .chip.accent {
    background: var(--accent-wash-16);
    color: var(--accent);
  }

  .chip.amber {
    background: var(--amber-wash-16);
    color: var(--amber);
  }

  .hints {
    flex: 1 1 auto;
    display: flex;
    align-items: center;
    gap: 14px;
    min-width: 0;
    overflow: hidden;
  }

  /* Status-bar hints carry no keycap box — the glyph is simply brighter than
     the word it explains. */
  .hint {
    flex: 0 0 auto;
    font-size: 10px;
    white-space: nowrap;
  }

  .hkey {
    color: var(--text-muted);
  }

  .hlabel {
    color: var(--text-dim);
  }

  .counts {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .count {
    font-size: 10.5px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .count.strong {
    color: var(--accent);
  }

  .count.decided {
    color: var(--gold);
  }
</style>
