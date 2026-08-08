<script lang="ts">
  // The status bar: what mode you are in, what state that mode is in, the keys
  // that are live right now, and the counts. 30px, and it never scrolls.

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

  // The chip names the state the mode is in. Indexing outranks the layout
  // because it is the thing the user is waiting on.
  let chip = $derived<Chip>(
    app.scanning !== null
      ? { text: "INDEXING", tone: "amber" }
      : { text: shell.layoutLabel.toUpperCase(), tone: "accent" },
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

  <span class="chip {chip.tone}">{chip.text}</span>

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
    padding: 2px 7px;
    border-radius: 3px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    white-space: nowrap;
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
