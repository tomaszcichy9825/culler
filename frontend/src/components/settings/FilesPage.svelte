<script lang="ts">
  // Files and writes: which files culler recognises, and what it does to them.
  //
  // The extension lists are the only place the app decides what a photograph
  // is, so they live here rather than under Advanced. The naming preview is
  // built from the draft, not from a sample: it shows what the settings on
  // the Culling page would actually do to a file.

  import { settings } from "../../lib/settings.svelte";
  import Aside from "./Aside.svelte";
  import ChipList from "./ChipList.svelte";
  import Choice from "./Choice.svelte";
  import ControlChip from "./ControlChip.svelte";
  import PageShell from "./PageShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";

  /** Extensions are stored lowercase and with the dot; anything else is folded. */
  function normaliseExt(raw: string): string {
    const trimmed = raw.trim().toLowerCase().replace(/^\.+/, "");
    if (trimmed === "" || /[^a-z0-9]/.test(trimmed)) return "";
    return `.${trimmed}`;
  }

  let draft = $derived(settings.draft);
  let folder = $derived(settings.draft?.behaviour.rejectedFolderName ?? "");
  let toFolder = $derived(settings.draft?.behaviour.trashMode === "rejected-folder");
  let collision = $derived(settings.draft?.behaviour.collisionPolicy ?? "rename-suffix");

  let samples = $derived([
    toFolder ? `DSCF1234.RAF → ${folder}/DSCF1234.RAF` : "DSCF1234.RAF → system trash",
    toFolder ? `DSCF1234.JPG → ${folder}/DSCF1234.JPG` : "DSCF1234.JPG → system trash",
    collision === "rename-suffix"
      ? "…and if the name is taken: DSCF1234-1.RAF"
      : collision === "skip"
        ? "…and if the name is taken: left where it is"
        : "…and if the name is taken: replaced",
  ]);
</script>

{#if draft}
  <PageShell>
    {#snippet content()}
      <SettingGroup title="Extensions" hint="order is priority">
        <SettingRow
          name="RAW"
          desc="What counts as the RAW half of a pair. The first match in the list wins when a stem has several."
          field="rawExts"
          terms="raw extensions raf arw cr3 nef dng"
        >
          <ChipList
            label="RAW extensions"
            values={draft.rawExts ?? []}
            normalise={normaliseExt}
            onchange={(v) => settings.setExts("rawExts", v)}
          />
        </SettingRow>

        <SettingRow
          name="JPEG and other renderable files"
          desc="The half a viewer can show directly. HEIC, PNG and TIFF live here too."
          field="jpegExts"
          terms="jpeg extensions heic png tiff webp"
        >
          <ChipList
            label="JPEG extensions"
            values={draft.jpegExts ?? []}
            normalise={normaliseExt}
            onchange={(v) => settings.setExts("jpegExts", v)}
          />
        </SettingRow>

        <SettingRow
          name="Sidecars"
          desc="Files that follow their frame through a move or a cut rather than being culled themselves."
          field="sidecarExts"
          terms="sidecar extensions xmp aae dop"
        >
          <ChipList
            label="Sidecar extensions"
            values={draft.sidecarExts ?? []}
            normalise={normaliseExt}
            onchange={(v) => settings.setExts("sidecarExts", v)}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Writes" danger hint="set on the Culling page">
        <SettingRow
          name="Where a cut file goes"
          desc="Shown here because it is what a write does; the control for it is under Culling → Applying."
          wired={false}
          terms="trash rejected"
        >
          <ControlChip
            label={toFolder ? `folder · ${folder}` : "system trash"}
            title="Change it on the Culling page"
          />
        </SettingRow>

        <SettingRow
          name="Purge previews older than 90 days"
          desc="Would sweep the thumbnail cache. Nothing measures or clears it yet."
          wired={false}
        >
          <ControlChip label="purge" disabled title="No backend call clears the cache yet" />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Exports" hint="run from the palette">
        <SettingRow
          name="XMP sidecar export"
          desc="Write each frame's rating and a colour label — green for a keep, red for a cut — into a .xmp beside it, for Lightroom and Bridge. Turning this on adds the export to the command palette; it never runs on its own, and a sidecar another tool wrote keeps everything else it holds."
          field="behaviour.xmpExport"
          terms="xmp sidecar lightroom bridge adobe label rating export interop"
        >
          <Choice
            label="XMP sidecar export"
            value={draft.behaviour.xmpExport ? "on" : "off"}
            options={[
              { value: "on", label: "on" },
              { value: "off", label: "off" },
            ]}
            onchange={(v) => settings.patch({ xmpExport: v === "on" })}
          />
        </SettingRow>
      </SettingGroup>
    {/snippet}

    {#snippet aside()}
      <Aside title="Naming preview">
        <div class="samples">
          {#each samples as sample (sample)}
            <div class="sample">{sample}</div>
          {/each}
        </div>
      </Aside>

      <Aside title="Safety net">
        <div class="safety">
          <div class="line"><span class="mark">✓</span><span>a cut is a move, never a delete</span></div>
          <div class="line"><span class="mark">✓</span><span>sidecars and JPEG halves follow the RAW</span></div>
          <div class="line"><span class="mark">✓</span><span>every apply is journalled and undoable</span></div>
          <div class="line"><span class="mark">✓</span><span>nothing is ever written to the card</span></div>
        </div>
      </Aside>

      <Aside
        title="Extensions"
        note="Lowercase, with the dot. An extension culler does not know is left alone entirely — it is neither culled nor carried, which is the safe way to be wrong."
      />
    {/snippet}
  </PageShell>
{/if}

<style>
  .samples {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .sample {
    padding: 6px 8px;
    border-radius: 4px;
    background: var(--bg-field-alt);
    border: 1px solid var(--border-strong);
    font-size: 10.5px;
    color: var(--text-2);
    overflow-wrap: anywhere;
  }

  .safety {
    display: flex;
    flex-direction: column;
  }

  .line {
    display: flex;
    align-items: flex-start;
    gap: 9px;
    min-height: 23px;
    padding: 3px 0;
    font-size: 11px;
    line-height: 1.45;
    color: var(--text-3);
  }

  .mark {
    flex: 0 0 auto;
    display: inline-grid;
    place-items: center;
    width: 13px;
    height: 13px;
    border-radius: 3px;
    background: var(--keep);
    color: var(--on-accent);
    font-size: 9px;
    font-weight: 700;
  }
</style>
