<script lang="ts">
  // Advanced: the concurrency caps, and the knobs the design drew for a
  // decoder and a logger that are not configurable yet.
  //
  // The caps are the reason a card over SMB is usable at all: local disks
  // tolerate parallel reads and network shares stall under them, so the two
  // are separate numbers rather than one.

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
      <SettingGroup title="Concurrency">
        <SettingRow
          name="Local read slots"
          desc="Preview reads in flight at once on a local volume."
          field="behaviour.localReadSlots"
          terms="parallel reads slots local"
        >
          <ValueField
            label="Local read slots"
            type="number"
            min={1}
            size={4}
            value={behaviour.localReadSlots}
            invalid={settings.errorFor("behaviour.localReadSlots") !== ""}
            oninput={(v) => settings.patch({ localReadSlots: Number(v) })}
          />
        </SettingRow>

        <SettingRow
          name="Network read slots"
          desc="The same, on a network volume. Raising this is usually slower, not faster."
          field="behaviour.networkReadSlots"
          terms="parallel reads slots network smb nas"
        >
          <ValueField
            label="Network read slots"
            type="number"
            min={1}
            size={4}
            value={behaviour.networkReadSlots}
            invalid={settings.errorFor("behaviour.networkReadSlots") !== ""}
            oninput={(v) => settings.patch({ networkReadSlots: Number(v) })}
          />
        </SettingRow>

        <SettingRow
          name="Network hash workers"
          desc="Identity hashes computed at once on a network volume. Hashing reads the whole file."
          field="behaviour.networkHashWorkers"
          terms="hash workers network identity"
        >
          <ValueField
            label="Network hash workers"
            type="number"
            min={1}
            size={4}
            value={behaviour.networkHashWorkers}
            invalid={settings.errorFor("behaviour.networkHashWorkers") !== ""}
            oninput={(v) => settings.patch({ networkHashWorkers: Number(v) })}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Decoding">
        <SettingRow
          name="Decode with GPU"
          desc="Previews are decoded in pure Go on the CPU, embedded JPEG first. There is no GPU path to turn off."
          wired={false}
        >
          <Choice
            label="Decode with GPU"
            value="off"
            options={[
              { value: "on", label: "on" },
              { value: "off", label: "off" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Decode threads"
          desc="Set by the read slots above rather than on its own."
          wired={false}
          terms="threads cores"
        >
          <ControlChip label="auto" disabled title="Not a separate setting in this version" />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Diagnostics">
        <SettingRow name="Log level" desc="Nothing writes a log file yet." wired={false}>
          <Choice
            label="Log level"
            value="warn"
            options={[
              { value: "warn", label: "warn" },
              { value: "info", label: "info" },
              { value: "debug", label: "debug" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Reload config on save"
          desc="Saving from this screen reloads it. An edit made in a text editor while culler is running needs a restart."
          wired={false}
        >
          <Choice
            label="Reload config on save"
            value="screen"
            options={[
              { value: "screen", label: "from this screen" },
              { value: "watch", label: "watch the file" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Privacy">
        <SettingRow
          name="Network calls"
          desc="culler makes none of its own. No telemetry, no update check, no fonts fetched at runtime."
          wired={false}
        >
          <ControlChip label="never" on title="A property of the build, not a setting" />
        </SettingRow>

        <SettingRow
          name="Reverse geocoding"
          desc="Would send a coordinate to look up a place name. Nothing does that in this version."
          wired={false}
        >
          <Choice
            label="Reverse geocoding"
            value="off"
            options={[
              { value: "ask", label: "ask first" },
              { value: "off", label: "off" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>
      </SettingGroup>
    {/snippet}

    {#snippet aside()}
      <Aside
        title="In use"
        rows={[
          { k: "local slots", v: String(behaviour.localReadSlots) },
          { k: "network slots", v: String(behaviour.networkReadSlots) },
          { k: "hash workers", v: String(behaviour.networkHashWorkers) },
          { k: "slow hint", v: `${behaviour.slowScanHintSeconds}s`, tone: "muted" },
        ]}
      />
      <Aside
        title="Why two numbers"
        note="A local disk is quicker the more you ask of it at once. A share over SMB is not: past a handful of reads it queues, and the scan that felt slow becomes a scan that has stopped. Lower the network numbers before you raise them."
      />
    {/snippet}
  </PageShell>
{/if}
