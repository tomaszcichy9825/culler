<script lang="ts">
  // EXIF mode, assembled.
  //
  // The shell owns the three-pane frame, so this exists for the case where the
  // whole mode is mounted in one place — the harness does exactly that, and it
  // is the shortest path to wiring the mode up before the shell's pane slots
  // exist. A shell that mounts panes individually should use FramesRail,
  // EditorPane and TargetsPane directly and skip this file; the write plan
  // dialog belongs at the top of the window either way, above the panes.
  //
  // It also owns the one effect the mode needs: the frames on the rail follow
  // the grid's selection, so switching to ⌃2 with fourteen frames selected
  // finds fourteen frames already read.

  import { app } from "../../lib/state.svelte";
  import { exifState } from "../../lib/exif.svelte";
  import { shell } from "../../lib/shell.svelte";
  import FramesRail from "./FramesRail.svelte";
  import EditorPane from "./EditorPane.svelte";
  import TargetsPane from "./TargetsPane.svelte";
  import WritePlanDialog from "./WritePlanDialog.svelte";

  interface Props {
    /** Read the grid's selection into the rail. Off when a caller drives it. */
    follow?: boolean;
  }

  let { follow = true }: Props = $props();

  /**
   * The paths the editor works on: one file per frame, the JPEG where there is
   * one because that is the frame the user is looking at, and it is the half a
   * write can reach in place.
   */
  let paths = $derived(
    app.targets.map((g) => (g.hasJpeg && g.jpegPath !== "" ? g.jpegPath : g.rawPath)).filter((p) => p !== ""),
  );

  // Re-read whenever the selection changes. A path list that has not actually
  // changed is not re-read: the effect sees the joined list, not the array.
  let key = $derived(paths.join("\n"));

  $effect(() => {
    if (!follow) return;
    const wanted = key;
    if (wanted === "") {
      exifState.frames = [];
      return;
    }
    void exifState.load(wanted.split("\n"));
  });
</script>

<div class="mode" data-testid="exif-mode">
  <div class="pane left">
    <FramesRail mosaic={exifState.batch} />
  </div>
  <div class="pane centre">
    <EditorPane />
  </div>
  <div class="pane right">
    <TargetsPane />
  </div>
</div>

<WritePlanDialog />

<!-- The layout the mode is in, for the shell's segmented control to echo. -->
<span class="sr-only" data-testid="exif-layout">{shell.layoutLabel}</span>

<style>
  .mode {
    display: grid;
    grid-template-columns: 208px minmax(0, 1fr) 296px;
    min-height: 0;
    height: 100%;
  }

  .pane {
    min-width: 0;
    min-height: 0;
  }

  .left {
    border-right: 1px solid var(--border);
  }

  .right {
    border-left: 1px solid var(--border);
  }

  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
</style>
