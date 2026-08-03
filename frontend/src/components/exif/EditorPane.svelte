<script lang="ts">
  // EXIF mode's centre pane: the form.
  //
  // It carries the focused-pane treatment of screen 2a whenever the centre
  // pane holds the keyboard — the lifted background, the accent inset, and a
  // 30px header strip naming the pane and its keys. The batch layout replaces
  // that header's hint with the rule it has to state in words, because "a
  // value you type replaces every frame" is not something a form can imply.

  import { shell } from "../../lib/shell.svelte";
  import { exifState } from "../../lib/exif.svelte";
  import FieldRow from "./FieldRow.svelte";

  let focused = $derived(shell.mode === "exif" && shell.focusedPane === "centre");
  let batch = $derived(exifState.batch);
  let count = $derived(exifState.targets.length);
</script>

<div class="editor" class:focused data-testid="exif-editor">
  {#if focused}
    <div class="focus-strip">
      <span class="pip" aria-hidden="true"></span>
      <span class="who">{shell.focusedPaneName} · focused</span>
      <span class="keys">⇥ next field · ⏎ commit · esc revert · ⌘Z undo</span>
    </div>
  {/if}

  <div class="head">
    {#if batch}
      <span class="rule-text">
        a value you type replaces every frame · <em>⟨mixed⟩</em> means the frames disagree · empty leaves them alone
      </span>
    {:else}
      <span class="subject">{exifState.focused?.stem ?? "no frame"}</span>
      <span class="meta">{exifState.focused?.kind === "raw" ? "RAW · edits go to a sidecar" : "JPEG · written in place"}</span>
    {/if}
  </div>

  <div class="body">
    {#if exifState.error !== ""}
      <p class="error" role="alert">{exifState.error}</p>
    {/if}

    {#if exifState.loading}
      <p class="empty">reading metadata…</p>
    {:else if count === 0}
      <p class="empty">no frames selected — pick one in the grid, or select several and press ⌥2 to edit them together</p>
    {:else}
      {#each exifState.sections as section (section.name)}
        <section class="group">
          <div class="label">
            <span class="title">{section.name}</span>
            <span class="hairline"></span>
            {#if section.name === "Location" && exifState.stripping}
              <span class="note">GPS drafted for removal</span>
            {/if}
          </div>
          {#each section.rows as row (row.tag)}
            <FieldRow {row} />
          {/each}
        </section>
      {/each}
    {/if}
  </div>
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 0;
    height: 100%;
    background: var(--bg-window);
  }

  /* The focused-pane treatment, drawn only here and only at pane scale. */
  .editor.focused {
    background: var(--bg-raised);
    box-shadow: var(--focus-inset);
  }

  .focus-strip {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 7px;
    height: 22px;
    padding: 0 12px;
    background: var(--accent-wash-14);
  }

  .pip {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--accent);
  }

  .who {
    font-size: 10px;
    color: var(--accent);
  }

  .keys {
    margin-left: auto;
    font-size: 10px;
    color: var(--text-on-focus-hint);
  }

  .head {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 30px;
    padding: 0 14px;
    border-bottom: 1px solid var(--border);
  }

  .subject {
    font-size: 12px;
    font-weight: 500;
    color: var(--text-2);
  }

  .meta,
  .rule-text {
    font-size: 10.5px;
    color: var(--text-muted);
  }

  .rule-text em {
    font-style: normal;
    color: var(--gold);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px 14px 22px;
  }

  .group {
    margin-bottom: 20px;
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

  .note {
    font-size: 10px;
    color: var(--gold);
  }

  .empty {
    margin: 0;
    font-size: 10.5px;
    line-height: 1.7;
    color: var(--text-ghost);
    text-wrap: pretty;
  }

  .error {
    margin: 0 0 12px;
    padding: 7px 9px;
    border-radius: 4px;
    background: var(--cut-wash-09);
    border: 1px solid var(--cut-wash-16);
    font-size: 10.5px;
    line-height: 1.55;
    color: var(--cut);
    text-wrap: pretty;
  }
</style>
