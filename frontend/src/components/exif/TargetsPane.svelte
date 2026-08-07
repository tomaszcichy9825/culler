<script lang="ts">
  // EXIF mode's right pane.
  //
  // Screen 3a gives it Targets, Pending diff and Presets; screen 3b swaps the
  // first two for Affected and Shift capture time. Both are the same three
  // questions in a different order — what is being written to, what is about
  // to change, and what can be applied in one stroke — so both live here and
  // the batch layout decides which sections are drawn.

  import { exifState } from "../../lib/exif.svelte";

  let batch = $derived(exifState.batch);
  let targets = $derived(exifState.targets);
  let jpegs = $derived(targets.filter((f) => f.kind === "jpeg").length);
  let raws = $derived(targets.filter((f) => f.kind === "raw").length);

  /** The rows of the pending diff: every drafted change on the covered frames. */
  let pending = $derived(exifState.rows.filter((r) => r.state === "dirty"));

  /** Shift capture time is drafted as a relative nudge, not a value. */
  let shift = $state("+02:00:00");

  /**
   * applyShift moves every covered frame's capture time by the offset typed,
   * which keeps the interval between frames intact — the point of shifting
   * rather than setting. A frame with no capture time has nothing to shift.
   */
  function applyShift(sign: 1 | -1) {
    const seconds = parseShift(shift);
    if (seconds === null) return;
    for (const frame of targets) {
      const row = (frame.fields ?? []).find((f) => f.tag === "DateTimeOriginal");
      if (row === undefined || !row.writable || !row.present) continue;
      // A value without a zone suffix is a wall clock whose zone was never
      // recorded. It is held in UTC for the arithmetic — not because it is
      // UTC, but because UTC has no daylight-saving step to trip over.
      const when = new Date(zoneOf(row.value) === null ? row.value + "Z" : row.value);
      if (Number.isNaN(when.getTime())) continue;
      when.setTime(when.getTime() + sign * seconds * 1000);
      exifState.editingTag = "DateTimeOriginal";
      exifState.buffer = isoWithOffset(when, row.value);
      commitOne(frame.path);
    }
    exifState.editingTag = null;
  }

  /**
   * A shift is per frame, so it cannot go through the shared commit — that one
   * writes the same value to every covered frame, which is the opposite of
   * keeping the interval.
   */
  function commitOne(path: string) {
    const value = exifState.buffer;
    const edits = { ...exifState.edits };
    edits[path] = { ...(edits[path] ?? {}), DateTimeOriginal: value };
    exifState.edits = edits;
  }

  /** parseShift reads "+02:00:00" or "1:30" into seconds. */
  function parseShift(text: string): number | null {
    const m = /^\s*[+-]?(\d+):([0-5]?\d)(?::([0-5]?\d))?\s*$/.exec(text);
    if (m === null) return null;
    return Number(m[1]) * 3600 + Number(m[2]) * 60 + Number(m[3] ?? 0);
  }

  /** The zone suffix a value carries, or null when its zone is unknown. */
  function zoneOf(value: string): string | null {
    return /([+-]\d{2}:\d{2}|Z)$/.exec(value)?.[1] ?? null;
  }

  /**
   * isoWithOffset renders a shifted time in the zone the original carried, so
   * nudging a frame by two hours does not also move it to the local zone. A
   * frame whose zone was never recorded keeps its wall clock unqualified —
   * shifting is not a licence to invent the zone the backend refuses to write.
   */
  function isoWithOffset(when: Date, original: string): string {
    const zone = zoneOf(original);
    if (zone === null) return when.toISOString().replace(/\.\d+Z$/, "Z").replace("Z", "");
    if (zone === "Z") return when.toISOString().replace(/\.\d+Z$/, "Z");
    const sign = zone.startsWith("-") ? -1 : 1;
    const minutes = sign * (Number(zone.slice(1, 3)) * 60 + Number(zone.slice(4, 6)));
    const local = new Date(when.getTime() + minutes * 60_000);
    return local.toISOString().replace(/\.\d+Z$/, "").replace("Z", "") + zone;
  }
</script>

<div class="pane" data-testid="exif-targets">
  <section class="block">
    <div class="label">
      <span class="title">{batch ? "Affected" : "Targets"}</span>
      <span class="hairline"></span>
      <span class="hint">{targets.length}</span>
    </div>
    <div class="row">
      <span class="chip jpeg">J</span>
      <span class="name">JPEG frames</span>
      <span class="mode">in place</span>
      <span class="count">{jpegs}</span>
    </div>
    <div class="row">
      <span class="chip raw">R</span>
      <span class="name">RAW frames</span>
      <span class="mode">sidecar</span>
      <span class="count">{raws}</span>
    </div>
    <div class="row">
      <span class="chip gps" class:on={exifState.stripping}>G</span>
      <span class="name">GPS</span>
      <span class="mode">{exifState.stripping ? "drafted for removal" : "kept"}</span>
      <button type="button" class="action" onclick={() => exifState.toggleStrip()}>
        {exifState.stripping ? "keep ⇧D" : "strip ⇧D"}
      </button>
    </div>
  </section>

  {#if batch}
    <section class="block">
      <div class="label">
        <span class="title">Shift capture time</span>
        <span class="hairline"></span>
        <span class="hint">relative</span>
      </div>
      <input class="inset" bind:value={shift} data-keys="local" spellcheck="false" aria-label="Shift capture time by" />
      <p class="prose">keeps the interval between frames intact</p>
      <div class="pair">
        <button type="button" class="action" onclick={() => applyShift(-1)}>earlier</button>
        <button type="button" class="action" onclick={() => applyShift(1)}>later</button>
      </div>
    </section>
  {:else}
    <section class="block">
      <div class="label">
        <span class="title">Pending diff</span>
        <span class="hairline"></span>
        <span class="hint">{pending.length}</span>
      </div>
      {#if pending.length === 0}
        <p class="prose">nothing drafted on this frame</p>
      {:else}
        {#each pending as row (row.tag)}
          <div class="diff">
            <span class="tag">{row.label}</span>
            <span class="was">− {row.previous === "" ? "not set" : row.previous}</span>
            <span class="now">+ {row.value === "" ? "removed" : row.value}</span>
          </div>
        {/each}
      {/if}
    </section>
  {/if}

  <section class="block">
    <div class="label">
      <span class="title">Presets</span>
      <span class="hairline"></span>
    </div>
    <p class="prose">saved field sets come with the catalogue; nothing to apply yet</p>
    <button type="button" class="action wide" disabled={exifState.unwritten === 0} onclick={() => exifState.discard()}>
      discard {exifState.unwritten} unwritten
    </button>
  </section>
</div>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    gap: 20px;
    min-height: 0;
    height: 100%;
    overflow-y: auto;
    padding: 12px;
    background: var(--bg-pane);
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-bottom: 7px;
  }

  .title {
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

  .hint {
    font-size: 10px;
    color: var(--text-ghost);
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 21px;
  }

  .chip {
    flex: 0 0 auto;
    display: grid;
    place-items: center;
    width: 15px;
    height: 15px;
    border-radius: 3px;
    font-size: 9px;
    font-weight: 700;
    background: var(--absent-wash);
    color: var(--text-muted);
  }

  .chip.jpeg {
    background: var(--keep-wash-20);
    color: var(--keep-text);
  }

  .chip.raw {
    background: var(--accent-wash-18);
    color: var(--accent);
  }

  .chip.gps.on {
    background: var(--cut-wash-22);
    color: var(--cut-text);
  }

  .name {
    flex: 1;
    min-width: 0;
    font-size: 11px;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .mode {
    font-size: 10px;
    color: var(--text-dim);
    white-space: nowrap;
  }

  .count {
    min-width: 22px;
    font-size: 11px;
    color: var(--text);
    text-align: right;
  }

  .diff {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 4px 0;
    border-bottom: 1px solid var(--border-hair);
  }

  .tag {
    font-size: 10px;
    color: var(--text-muted);
  }

  .was {
    font-size: 10.5px;
    color: var(--cut);
    overflow-wrap: anywhere;
  }

  .now {
    font-size: 10.5px;
    color: var(--keep);
    overflow-wrap: anywhere;
  }

  .inset {
    padding: 6px 8px;
    border-radius: 4px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 14px;
    color: var(--text);
    outline: none;
  }

  .inset:focus {
    border-color: var(--border-focus);
  }

  .prose {
    margin: 2px 0 0;
    font-size: 10.5px;
    line-height: 1.55;
    color: var(--text-dim);
    text-wrap: pretty;
  }

  .pair {
    display: flex;
    gap: 7px;
    margin-top: 8px;
  }

  .action {
    flex: 1;
    padding: 6px 9px;
    border-radius: 5px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    text-align: center;
    cursor: pointer;
    appearance: none;
  }

  .action:disabled {
    color: var(--text-ghost);
    cursor: default;
  }

  .action.wide {
    margin-top: 8px;
    width: 100%;
  }
</style>
