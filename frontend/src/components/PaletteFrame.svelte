<script lang="ts">
  // The dialog frame all three palettes are drawn in, and the one place their
  // keyboard is handled.
  //
  // Containment is the established data-keys="local" pattern rather than a
  // focus trap: the frame takes the keyboard while it is up, stops its own key
  // events from reaching the window listener, and gives everything back on Esc
  // or on a click outside. Tab is swallowed rather than trapped — it is simply
  // not bound to anything in a palette yet.

  import type { Snippet } from "svelte";

  import { palette, routeKey } from "../lib/palette.svelte";

  interface Props {
    /** 720 for the command palette, 760 for move and copy. */
    width: number;
    /** What the palette is called, for assistive technology. */
    label: string;
    /** How many rows the body has, so the cursor wraps at the right place. */
    count: number;
    /** Run whatever the cursor is on. alt is held for the secondary verb. */
    onrun: (alt: boolean) => void;
    header: Snippet;
    chips?: Snippet;
    body: Snippet;
    footer: Snippet;
  }

  let { width, label, count, onrun, header, chips, body, footer }: Props = $props();

  let root = $state<HTMLElement | null>(null);
  let bodyEl = $state<HTMLElement | null>(null);

  // Taking the keyboard is what makes ownsKeys() true for everything typed
  // here, which is what keeps the grid's bindings quiet behind the dialog.
  //
  // A palette that draws a real text field wants the field focused instead:
  // ownsKeys() is true either way, and only the field can hold a caret, a
  // selection and a paste. The panel is the fallback for the palettes that
  // collect their query a keystroke at a time.
  $effect(() => {
    const field = root?.querySelector<HTMLElement>("[data-palette-field]");
    (field ?? root)?.focus();
  });

  // The body scrolls, and the cursor must not walk out of it: whichever
  // palette is drawing the rows marks the one under the cursor `.at`, so the
  // frame can keep it in view as ↑↓ wrap and Home/End jump. `nearest` leaves
  // the scroll alone for a row already visible, so the pointer setting the
  // index on hover never yanks the list about.
  $effect(() => {
    void palette.index;
    void count;
    bodyEl?.querySelector(".at")?.scrollIntoView({ block: "nearest" });
  });

  /**
   * editing reports that the press came from the palette's own text field, so
   * routeKey knows to leave the caret keys alone. It is asked of the event
   * rather than of a prop: the field is the only element in a palette that
   * holds text, and an event that came from anywhere else in the panel is the
   * list's however the palette is built.
   */
  function editing(target: EventTarget | null): boolean {
    return target instanceof HTMLElement && target.hasAttribute("data-palette-field");
  }

  function onKeydown(e: KeyboardEvent) {
    // Nothing typed into a palette belongs to the window listener, including
    // the Esc it would otherwise answer by blurring.
    e.stopPropagation();
    const outcome = routeKey(e, count, onrun, editing(e.target));
    // "caret" is a press the field is about to answer itself. Preventing it
    // would leave ← and ⌥← doing nothing at all, which is worse than the row
    // cursor they used to move.
    if (outcome !== "ignore" && outcome !== "caret") e.preventDefault();
  }
</script>

<!-- The scrim is a click target, not a control: mousedown outside the panel
     dismisses, which is the pointer half of Esc. -->
<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="palette-scrim" onmousedown={() => palette.close()}>
  <div
    class="panel"
    style="width: {width}px"
    role="dialog"
    aria-modal="true"
    aria-label={label}
    data-keys="local"
    tabindex="-1"
    bind:this={root}
    onkeydown={onKeydown}
    onmousedown={(e) => e.stopPropagation()}
  >
    <div class="head">{@render header()}</div>
    {#if chips}<div class="chips">{@render chips()}</div>{/if}
    <div class="body" bind:this={bodyEl}>{@render body()}</div>
    <div class="foot">{@render footer()}</div>
  </div>
</div>

<style>
  .palette-scrim {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    justify-content: center;
    padding: 96px 60px 60px;
    background: var(--scrim-palette);
  }

  .panel {
    max-width: 100%;
    max-height: 100%;
    display: flex;
    flex-direction: column;
    border-radius: 9px;
    background: var(--bg-chrome);
    border: 1px solid var(--border-dialog);
    box-shadow: var(--shadow-palette);
    overflow: hidden;
    outline: none;
  }

  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 16px;
    border-bottom: 1px solid var(--border);
  }

  .chips {
    flex: 0 0 auto;
    display: flex;
    gap: 5px;
    padding: 9px 16px;
    border-bottom: 1px solid var(--border);
    overflow: hidden;
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .foot {
    flex: 0 0 auto;
    padding: 9px 16px;
    border-top: 1px solid var(--border);
    background: var(--bg-raised);
    font-size: 10.5px;
    color: var(--text-dim);
  }

  /* The panes behind a palette drop back rather than competing with it. The
     rule reaches out of this component deliberately: the alternative is a flag
     App.svelte has to remember to pass down. */
  :global(body:has(.palette-scrim) .app > .body) {
    opacity: 0.3;
  }
</style>
