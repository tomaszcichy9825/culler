<script lang="ts">
  // A vertical split of one pane into a top and a bottom section, with a
  // draggable divider between them and the split remembered across launches.
  //
  // The size is kept as a fraction of the pane's height, not a pixel count, so
  // the split holds its proportions when the window is resized rather than the
  // top section pinning to a fixed height. It is clamped away from the extremes
  // so a section can never be dragged shut and lost.

  import type { Snippet } from "svelte";

  interface Props {
    /** Where the fraction is remembered. Unique per split. */
    storageKey: string;
    top: Snippet;
    bottom: Snippet;
    /** How small each section may be dragged, as a fraction of the height. */
    min?: number;
    /** The split before anyone has dragged it. */
    initial?: number;
  }

  let { storageKey, top, bottom, min = 0.2, initial = 0.45 }: Props = $props();

  function clamp(f: number): number {
    return Math.min(1 - min, Math.max(min, f));
  }

  function read(): number {
    try {
      const stored = parseFloat(localStorage.getItem(storageKey) ?? "");
      return Number.isFinite(stored) ? clamp(stored) : initial;
    } catch {
      // A webview with storage off just starts from the default every time.
      return initial;
    }
  }

  let fraction = $state(read());
  let container = $state<HTMLDivElement | null>(null);
  let dragging = $state(false);

  function onpointerdown(e: PointerEvent) {
    dragging = true;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function onpointermove(e: PointerEvent) {
    if (!dragging || container === null) return;
    const rect = container.getBoundingClientRect();
    if (rect.height === 0) return;
    fraction = clamp((e.clientY - rect.top) / rect.height);
  }

  function onpointerup(e: PointerEvent) {
    if (!dragging) return;
    dragging = false;
    (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
    try {
      localStorage.setItem(storageKey, String(fraction));
    } catch {
      // The size still holds for the session; it just will not survive a
      // relaunch, which is better than refusing to drag.
    }
  }

  // The divider is keyboard-reachable so the split is not mouse-only. Up and
  // down nudge it by a step, and the move is saved like a drag is. It carries
  // data-keys="local" so the global keymap stays out while it holds focus —
  // without that the same arrow press also moved grid focus or panned the
  // loupe, and Esc hands the keyboard back the way the tree's does.
  function onkeydown(e: KeyboardEvent) {
    const step = e.shiftKey ? 0.1 : 0.02;
    if (e.key === "ArrowUp") fraction = clamp(fraction - step);
    else if (e.key === "ArrowDown") fraction = clamp(fraction + step);
    else return;
    e.preventDefault();
    try {
      localStorage.setItem(storageKey, String(fraction));
    } catch {
      // As above: session-only is an acceptable fallback.
    }
  }
</script>

<div class="vsplit" bind:this={container}>
  <div class="section" style:flex="0 0 {fraction * 100}%">
    {@render top()}
  </div>
  <div
    class="divider"
    class:dragging
    role="separator"
    aria-orientation="horizontal"
    aria-label="Resize"
    tabindex="0"
    data-keys="local"
    {onpointerdown}
    {onpointermove}
    {onpointerup}
    {onkeydown}
  >
    <span class="grip" aria-hidden="true"></span>
  </div>
  <div class="section grow">
    {@render bottom()}
  </div>
</div>

<style>
  .vsplit {
    flex: 1;
    min-height: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .section {
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }

  .section.grow {
    flex: 1 1 auto;
  }

  /* A slim hit area with a hairline down its middle. It widens its reach with
     padding without taking layout height, so the two sections meet cleanly. */
  .divider {
    flex: 0 0 auto;
    position: relative;
    height: 7px;
    cursor: row-resize;
    border: none;
    background: none;
    padding: 0;
    display: grid;
    place-items: center;
    --wails-draggable: no-drag;
  }

  .grip {
    width: 100%;
    height: 1px;
    background: var(--border);
  }

  .divider:hover .grip,
  .divider.dragging .grip {
    height: 2px;
    background: var(--accent);
  }

  .divider:focus-visible {
    outline: none;
  }

  .divider:focus-visible .grip {
    height: 2px;
    background: var(--accent);
  }
</style>
