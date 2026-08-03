<script lang="ts">
  // Space, from the grid: the loupe over the top of it.
  //
  // The same frame the grid had focused, full size, on a scrim — and the grid
  // still underneath, which is the point of the overlay rather than a screen
  // of its own. Space or Esc drops back to exactly where the user was, so
  // looking closely at a frame costs nothing.
  //
  // The verdict keys keep working while it is up, so the overlay only draws
  // state: the badge over the image, the rows in the card. The one control it
  // owns is the rating, because the dots are already there to be read.

  import type { GroupDTO } from "../lib/bindings";
  import { app } from "../lib/state.svelte";
  import { setRating } from "../lib/decisions";
  import type { Bytes } from "../lib/frame";
  import { factSections, fileRows, shotLine, verdictBadge } from "../lib/frame";
  import { MAX_RATING } from "../lib/verdict";
  import type { CutScope } from "../lib/verdict";
  import { previewURL } from "../lib/preview";
  import { groupKey } from "../lib/state.svelte";
  import Filmstrip from "./Filmstrip.svelte";
  import Histogram from "./Histogram.svelte";
  import LoupeStage from "./LoupeStage.svelte";

  interface Props {
    /** Defaults to the opened folder's frames. */
    groups?: GroupDTO[];
    /** Defaults to the app's focused frame. */
    index?: number;
    /** Defaults to moving the app's focus. */
    onfocus?: (index: number) => void;
    /** Defaults to the same helper the 1–5 keys run. */
    onrating?: (stars: number) => void;
    /** File sizes, if the caller has them. The rows do without when it has not. */
    bytes?: Bytes;
    /** How far a cut reaches. Defaults to the configured behaviour. */
    cutRemoves?: CutScope;
  }

  let { groups: groupsProp, index: indexProp, onfocus, onrating, bytes, cutRemoves }: Props = $props();

  const STARS = Array.from({ length: MAX_RATING }, (_, i) => i + 1);

  let groups = $derived(groupsProp ?? app.groups);
  let index = $derived(indexProp ?? app.focusIndex);
  let cut = $derived(cutRemoves ?? app.cutRemoves);
  let group = $derived<GroupDTO | null>(groups[index] ?? null);
  let badge = $derived(group ? verdictBadge(group, cut) : null);

  function focus(to: number) {
    if (onfocus) onfocus(to);
    else app.setFocus(to);
  }

  function rate(stars: number) {
    if (onrating) onrating(stars);
    else setRating(stars);
  }
</script>

<section class="overlay" aria-label="Loupe">
  <div class="row">
    <LoupeStage {group} padding={0} framed>
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
        <span class="chip">z 1:1</span>
        <span class="chip">c compare</span>
        <span class="chip">space close</span>
      </div>
    </LoupeStage>

    <aside class="card">
      {#if group}
        <div>
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
          </div>
        </div>

        <div class="histo">
          <span class="hlabel">Histogram</span>
          <Histogram url={previewURL(group, "grid")} id={groupKey(group)} height={40} />
        </div>

        <div class="files">
          {#each fileRows(group, cut, bytes) as row (row.half)}
            <div class="file {row.state}">
              <span class="tag">{row.half.toUpperCase()}</span>
              <span class="fname" title={row.name}>{row.name}</span>
              {#if row.size !== ""}<span class="fsize">{row.size}</span>{/if}
            </div>
          {/each}
        </div>

        {#each factSections(group) as section (section.title)}
          <div class="section">
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
  </div>

  <Filmstrip
    {groups}
    {index}
    onselect={focus}
    height={78}
    thumbWidth={96}
    thumbHeight={62}
    caption={false}
    badges={false}
    surface={false}
    keyboard={!app.zoom}
  />
</section>

<style>
  /* Absolutely positioned, not fixed: the overlay covers the three panes and
     leaves the title bar and the status bar showing, which is where the mode
     chip and the verdict keys are named. Its parent must be positioned. */
  .overlay {
    position: absolute;
    inset: 0;
    z-index: 20;
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 26px 30px 20px;
    background: var(--scrim-loupe);
  }

  .row {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 20px;
  }

  .card {
    flex: 0 0 268px;
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 12px;
    border-radius: 5px;
    background: var(--bg-chrome);
    border: 1px solid var(--border-strong);
    overflow-y: auto;
  }

  .name {
    font-size: 15px;
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
    gap: 3px;
    margin-top: 8px;
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

  .histo {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .hlabel {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-faint);
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
    padding-bottom: 5px;
  }

  .label {
    font-size: 10px;
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: var(--text-faint);
  }

  .hairline {
    flex: 1;
    height: 1px;
    background: var(--border);
  }

  /* The card is 268px against the rail's 250px, and it carries no verdict
     block, so here the key sits beside its value rather than above it. */
  .fact {
    display: flex;
    align-items: baseline;
    gap: 10px;
    height: 19px;
    min-width: 0;
  }

  .fkey {
    flex: 0 0 78px;
    font-size: 10.5px;
    color: var(--text-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fval {
    flex: 1;
    min-width: 0;
    font-size: 10.5px;
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
    color: var(--text-ghost);
    font-size: 11px;
  }

  .cluster {
    position: absolute;
    top: 16px;
    left: 16px;
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
    bottom: 16px;
    right: 16px;
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
