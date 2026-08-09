<script lang="ts">
  // CULL's second sub-layout (⌥2): one frame at a time.
  //
  // The left rail stops being a list of sources and becomes the frame's own
  // panel — what it is, what has been decided about it, and what an apply
  // would do to each of its files. The centre is the photograph and nothing
  // else, with the state floating over it as chips so that judging a frame
  // never means looking away from it.
  //
  // The component is self-contained: it reads the app's frames and focus, and
  // reports every change back through callbacks, so the screen holds no
  // decision state of its own that could disagree with the store.

  import type { GroupDTO } from "../lib/bindings";
  import { selectFrame } from "../lib/actions";
  import { app } from "../lib/state.svelte";
  import { setRating, setVerdict } from "../lib/decisions";
  import type { Bytes } from "../lib/frame";
  import { factSections, fileRows, shotLine, verdictBadge } from "../lib/frame";
  import { MAX_RATING, verdictOf } from "../lib/verdict";
  import type { CutScope } from "../lib/verdict";
  import Filmstrip from "./Filmstrip.svelte";
  import LoupeStage from "./LoupeStage.svelte";

  interface Props {
    /** Defaults to the opened folder's frames. */
    groups?: GroupDTO[];
    /** Defaults to the app's focused frame. */
    index?: number;
    /** Defaults to the app's own click rules: plain, shift-extend, ⌘-toggle. */
    onfocus?: (index: number, e?: MouseEvent) => void;
    /** Defaults to the same helper the k and x keys run. */
    onverdict?: (verdict: "keep" | "cut") => void;
    /** Defaults to the same helper the 1–5 keys run. */
    onrating?: (stars: number) => void;
    /** File sizes, if the caller has them. The rows do without when it has not. */
    bytes?: Bytes;
    /** How far a cut reaches. Defaults to the configured behaviour. */
    cutRemoves?: CutScope;
  }

  let {
    groups: groupsProp,
    index: indexProp,
    onfocus,
    onverdict,
    onrating,
    bytes,
    cutRemoves,
  }: Props = $props();

  const STARS = Array.from({ length: MAX_RATING }, (_, i) => i + 1);

  let groups = $derived(groupsProp ?? app.groups);
  let index = $derived(indexProp ?? app.focusIndex);
  let cut = $derived(cutRemoves ?? app.cutRemoves);
  let group = $derived<GroupDTO | null>(groups[index] ?? null);
  let verdict = $derived(group ? verdictOf(group) : "");
  let badge = $derived(group ? verdictBadge(group, cut) : null);

  function focus(to: number, e?: MouseEvent) {
    if (onfocus) onfocus(to, e);
    else selectFrame(to, e);
  }

  // The cards and the dots are the keys by another route, so they go through
  // the same helpers rather than reimplementing them: the toggling — pressing
  // a verdict a frame already holds to take it back off, pressing its own
  // rating to clear it — lives there, and behaves the same either way.
  function choose(v: "keep" | "cut") {
    if (onverdict) onverdict(v);
    else setVerdict(v);
  }

  function rate(stars: number) {
    if (onrating) onrating(stars);
    else setRating(stars);
  }
</script>

<div class="loupe-first">
  <aside class="panel">
    {#if group}
      <div class="head">
        <div class="name" title={group.stem}>{group.stem}</div>
        <div class="when">{shotLine(group)}</div>
        <div class="stars">
          {#each STARS as n}
            <button
              type="button"
              class="dot"
              class:on={group.rating >= n}
              aria-label="rate {n}"
              aria-pressed={group.rating >= n}
              onclick={() => rate(n)}
            ></button>
          {/each}
          <span class="spacer"></span>
          <span class="hint">1–5 to rate</span>
        </div>
      </div>

      <div class="block">
        <div class="label">Verdict</div>
        <div class="verdicts">
          <button
            type="button"
            class="card keep"
            class:active={verdict === "keep"}
            aria-pressed={verdict === "keep"}
            onclick={() => choose("keep")}
          >
            <span class="vname">KEEP</span>
            <span class="vkey">k</span>
          </button>
          <button
            type="button"
            class="card cut"
            class:active={verdict === "cut"}
            aria-pressed={verdict === "cut"}
            onclick={() => choose("cut")}
          >
            <span class="vname">CUT</span>
            <span class="vkey">x</span>
          </button>
        </div>

        <div class="label spaced">Files kept</div>
        <div class="files">
          {#each fileRows(group, cut, bytes) as row (row.half)}
            <div class="file {row.state}">
              <span class="tag">{row.half.toUpperCase()}</span>
              <span class="fname" title={row.name}>{row.name}</span>
              {#if row.size !== ""}<span class="fsize">{row.size}</span>{/if}
            </div>
          {/each}
        </div>
      </div>

      {#each factSections(group) as section (section.title)}
        <div class="block">
          <div class="rule">
            <span class="label">{section.title}</span>
            <span class="hairline"></span>
          </div>
          {#each section.rows as row (row.key)}
            <div class="fact">
              <span class="fkey">{row.key}</span>
              <span class="fval {row.tone ?? ''}" title={row.value}>{row.value}</span>
            </div>
          {/each}
        </div>
      {/each}
    {:else}
      <div class="empty">no frame</div>
    {/if}
  </aside>

  <div class="centre">
    <LoupeStage {group}>
      {#if badge}
        <div class="cluster">
          <div class="verdict-badge {badge.tone}">
            <span class="swatch"></span>
            <span class="btext">{badge.label}</span>
          </div>
          {#if group}
            <div class="meta">
              <span>{shotLine(group)}</span>
              <span class="sep">·</span>
              <span>{group.kind}</span>
            </div>
          {/if}
        </div>
      {/if}
      <div class="hints">
        <span class="chip">Z 1:1</span>
        <span class="chip">C compare</span>
        <span class="chip">Tab grid</span>
      </div>
    </LoupeStage>

    <Filmstrip
      {groups}
      {index}
      onselect={focus}
      isSelected={(g) => app.isSelected(g)}
      height={104}
      thumbWidth={112}
      thumbHeight={72}
      keyboard={!app.zoom}
    />
  </div>
</div>

<style>
  .loupe-first {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
  }

  /* Wider than the sources rail it replaces: a filename, a verdict and a stack
     of values need the room, and the photograph still gets everything left. */
  .panel {
    flex: 0 0 250px;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    background: var(--bg-pane);
    border-right: 1px solid var(--border);
  }

  .head {
    flex: 0 0 auto;
    padding: 12px 12px 10px;
    border-bottom: 1px solid var(--border);
  }

  .name {
    font-size: 15px;
    font-weight: 500;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .when {
    margin-top: 3px;
    font-size: 11px;
    color: var(--text-dim);
  }

  .stars {
    display: flex;
    align-items: center;
    gap: 3px;
    margin-top: 9px;
  }

  .dot {
    width: 10px;
    height: 10px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    background: var(--undecided);
    cursor: pointer;
    appearance: none;
  }

  .dot.on {
    background: var(--gold);
  }

  .spacer {
    flex: 1;
  }

  .hint {
    font-size: 10px;
    color: var(--text-faint);
  }

  .block {
    flex: 0 0 auto;
    padding: 11px 12px;
    border-bottom: 1px solid var(--border);
  }

  .label {
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .verdicts {
    display: flex;
    gap: 6px;
    padding-top: 8px;
  }

  /* Two cards rather than one toggle: both verdicts are always visible with
     the key that sets them, so the keyboard is learnable from the screen. */
  .card {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 7px 8px;
    border-radius: 5px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
    font: inherit;
    text-align: left;
    cursor: pointer;
    appearance: none;
  }

  .vname {
    font-size: 11px;
    font-weight: 700;
    color: var(--text-dim);
  }

  .vkey {
    font-size: 10px;
    color: var(--text-faint);
  }

  .card.keep.active {
    background: var(--keep-wash-12);
    border-color: var(--keep);
  }

  .card.keep.active .vname {
    color: var(--keep-text);
  }

  .card.cut.active {
    background: var(--cut-wash-14);
    border-color: var(--cut);
  }

  .card.cut.active .vname {
    color: var(--cut-text);
  }

  .card.active .vkey {
    color: var(--text-dim);
  }

  .spaced {
    display: block;
    padding: 14px 0 8px;
  }

  .files {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .file {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    border-radius: 5px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
    min-width: 0;
  }

  .tag {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 15px;
    height: 15px;
    border-radius: 3px;
    background: var(--text-dim);
    color: var(--on-accent);
    font-size: 9px;
    font-weight: 700;
  }

  .fname {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fsize {
    flex: 0 0 auto;
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .file.kept {
    background: var(--keep-wash-10);
    border-color: color-mix(in srgb, var(--keep) 55%, transparent);
  }

  .file.kept .tag {
    background: var(--keep);
  }

  /* A struck-through name is the whole point of the block: the frame is kept,
     and this file still goes. */
  .file.cut {
    background: var(--cut-wash-09);
    border-color: color-mix(in srgb, var(--cut) 50%, transparent);
  }

  .file.cut .tag {
    background: var(--cut);
  }

  .file.cut .fname {
    color: var(--text-muted);
    text-decoration: line-through;
  }

  .file.cut .fsize {
    color: var(--cut-text);
  }

  .rule {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 6px;
  }

  .hairline {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  /* Key above value, not beside it: the rail is 250px and a lens name beside a
     72px key would be an ellipsis every time. */
  .fact {
    display: flex;
    flex-direction: column;
    gap: 1px;
    padding: 3px 0;
    min-width: 0;
  }

  .fkey {
    font-size: 9.5px;
    letter-spacing: 0.04em;
    color: var(--text-faint);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fval {
    font-size: 11.5px;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fval.dim {
    color: var(--text-dim);
  }

  .fval.warn {
    color: var(--amber);
  }

  .empty {
    padding: 14px 12px;
    color: var(--text-ghost);
    font-size: 11px;
  }

  .centre {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--bg-app);
  }

  .cluster {
    position: absolute;
    top: 34px;
    left: 34px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }

  .verdict-badge {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 5px 9px;
    border-radius: 4px;
    background: var(--glass);
    border: 1px solid var(--border-strong);
    white-space: nowrap;
  }

  .swatch {
    width: 7px;
    height: 7px;
    border-radius: 2px;
    background: var(--undecided);
  }

  .btext {
    font-size: 11px;
    font-weight: 700;
    color: var(--text-muted);
  }

  .verdict-badge.keep {
    border-color: color-mix(in srgb, var(--keep) 60%, transparent);
  }

  .verdict-badge.keep .swatch {
    background: var(--keep);
  }

  .verdict-badge.keep .btext {
    color: var(--keep-text);
  }

  .verdict-badge.cut {
    border-color: color-mix(in srgb, var(--cut) 60%, transparent);
  }

  .verdict-badge.cut .swatch {
    background: var(--cut);
  }

  .verdict-badge.cut .btext {
    color: var(--cut-text);
  }

  /* Where exposure will sit. Until the scan reads EXIF this is what it
     genuinely knows about the frame, rather than a plausible-looking
     1/250s the user would have no way to distrust. */
  .meta {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 4px 9px;
    border-radius: 4px;
    background: var(--glass);
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .sep {
    color: var(--text-dead);
  }

  .hints {
    position: absolute;
    bottom: 34px;
    right: 34px;
    display: flex;
    gap: 6px;
  }

  .chip {
    padding: 4px 8px;
    border-radius: 4px;
    background: var(--glass);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-muted);
    white-space: nowrap;
  }
</style>
