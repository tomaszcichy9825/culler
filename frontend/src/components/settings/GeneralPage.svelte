<script lang="ts">
  // General: what happens at launch, the tools culler hands work to, and the
  // one scanning knob the config carries.

  import { settings } from "../../lib/settings.svelte";
  import Aside from "./Aside.svelte";
  import Choice from "./Choice.svelte";
  import ControlChip from "./ControlChip.svelte";
  import PageShell from "./PageShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import ValueField from "./ValueField.svelte";

  let behaviour = $derived(settings.draft?.behaviour);
</script>

{#if behaviour}
  <PageShell>
    {#snippet content()}
      <SettingGroup title="On launch">
        <SettingRow
          name="Open"
          desc="culler reopens the folder you left. The path is remembered by the app, not by the config file."
          wired={false}
        >
          <Choice
            label="Open on launch"
            value="last"
            options={[
              { value: "last", label: "last folder" },
              { value: "library", label: "library" },
              { value: "nothing", label: "nothing" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="When a card is inserted"
          desc="Nothing watches for cards in this version — open the volume yourself."
          wired={false}
        >
          <Choice
            label="When a card is inserted"
            value="ignore"
            options={[
              { value: "offer", label: "offer to cull" },
              { value: "ignore", label: "ignore" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Restore pending verdicts"
          desc="Verdicts are recorded against the frame's content hash as you press the key, so they are already there after a restart."
          wired={false}
        >
          <Choice
            label="Restore pending verdicts"
            value="on"
            options={[
              { value: "on", label: "on" },
              { value: "off", label: "off" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Scanning">
        <SettingRow
          name="Still-scanning hint"
          desc="How long a scan runs before the loader admits it is slow. A card over SMB regularly takes longer than this."
          field="behaviour.slowScanHintSeconds"
          terms="slow scan hint seconds network"
        >
          <ValueField
            label="Still-scanning hint"
            type="number"
            min={1}
            size={4}
            suffix="seconds"
            value={behaviour.slowScanHintSeconds}
            invalid={settings.errorFor("behaviour.slowScanHintSeconds") !== ""}
            oninput={(v) => settings.patch({ slowScanHintSeconds: Number(v) })}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Tools">
        <SettingRow name="Open in external editor" desc="Which app a frame is handed to. Not built yet." wired={false}>
          <ControlChip label="choose" disabled title="No backend call launches another app yet" />
        </SettingRow>

        <SettingRow
          name="exiftool"
          desc="EXIF editing comes with the EXIF mode; nothing shells out to exiftool today."
          wired={false}
        >
          <Choice
            label="exiftool"
            value="none"
            options={[
              { value: "none", label: "not used" },
              { value: "bundled", label: "bundled" },
              { value: "system", label: "system path" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow name="Reveal in" desc="Finder, Explorer, or your file manager." wired={false}>
          <ControlChip label="system default" disabled title="No backend call reveals a file yet" />
        </SettingRow>
      </SettingGroup>
    {/snippet}

    {#snippet aside()}
      <Aside
        title="Config"
        rows={[
          { k: "file", v: settings.path === "" ? "not known yet" : "config.json", tone: "muted" },
          { k: "format", v: "JSON", tone: "muted" },
          { k: "read", v: "at launch and on save", tone: "muted" },
        ]}
      />
      <Aside
        title="Escape hatches"
        note="Every wired setting on these pages is a line in config.json. Edit the file directly if you prefer — culler reads it at launch, and a value it refuses stops the app rather than being silently ignored."
      />
      <Aside
        title="Not wired yet"
        note="A control drawn back is one the design has and the build has not. It shows what culler does today so the page is never a lie about the app."
      />
    {/snippet}
  </PageShell>
{/if}
