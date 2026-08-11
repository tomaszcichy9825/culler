<script lang="ts">
  // Catalogue: what is indexed and what is cached.
  //
  // This is the page the design drew furthest ahead of the build. The
  // catalogue itself exists now — roots are indexed and re-indexed, and the
  // Sessions group is drawn from it — but the watcher and the preview budget
  // are not, so those controls stay disabled and say so rather than being
  // drawn as if pressing them would change anything.

  import { library } from "../../lib/library.svelte";
  import { settings } from "../../lib/settings.svelte";
  import Aside from "./Aside.svelte";
  import Choice from "./Choice.svelte";
  import ControlChip from "./ControlChip.svelte";
  import PageShell from "./PageShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import ValueField from "./ValueField.svelte";

  let behaviour = $derived(settings.draft?.behaviour);

  /** The data directory is the config file's own, which the backend has told us. */
  let dataDir = $derived(settings.path.replace(/[/\\][^/\\]*$/, ""));
  let decisions = $derived(dataDir === "" ? "not known yet" : `${dataDir}/decisions.db`);
  let journal = $derived(dataDir === "" ? "not known yet" : `${dataDir}/journal.jsonl`);
</script>

<PageShell>
  {#snippet content()}
    <SettingGroup title="Location">
      <SettingRow
        name="Decisions database"
        desc="One SQLite file, keyed by content hash. Verdicts survive a folder being moved."
        terms="sqlite db path"
      >
        <ControlChip label={decisions} title={decisions} />
      </SettingRow>

      <SettingRow
        name="Undo journal"
        desc="What every apply did, so it can be undone after a restart."
        terms="journal undo path"
      >
        <ControlChip label={journal} title={journal} />
      </SettingRow>

      <SettingRow
        name="Preview cache"
        desc="Decoded thumbnails under the OS cache directory, in culler/thumbs. Safe to delete at any time."
        wired={false}
        terms="cache thumbnails previews"
      >
        <ControlChip label="browse" disabled title="No backend call opens a folder yet" />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Indexing">
      <SettingRow
        name="Watch folders"
        desc="Re-index when files change on disk. Nothing is indexed in this version — a folder is scanned when you open it."
        wired={false}
      >
        <Choice
          label="Watch folders"
          value="off"
          options={[
            { value: "on", label: "on" },
            { value: "off", label: "off" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="Watched roots" desc="Cards are never watched — they are read on mount." wired={false}>
        <ControlChip label="edit" disabled title="Nothing is watched in this version" />
      </SettingRow>

      <SettingRow name="Read metadata on index" desc="Slower first pass, but search works immediately." wired={false}>
        <Choice
          label="Read metadata on index"
          value="lazy"
          options={[
            { value: "on", label: "on" },
            { value: "lazy", label: "lazy" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow
        name="Network volumes"
        desc="Read on demand only, so a sleeping NAS never blocks launch. The read slots for one are on Advanced."
        wired={false}
        terms="nas smb network"
      >
        <Choice
          label="Network volumes"
          value="demand"
          options={[
            { value: "demand", label: "on demand" },
            { value: "always", label: "always" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Sessions" hint="how the catalogue is grouped into shoots">
      {#if behaviour}
        <SettingRow
          name="Smallest shoot listed"
          desc="A library is full of one- and two-frame fragments, and at four hours' gap each is a session of its own. Below this they are left out of the list. One shows every last one; nothing is hidden from the grid or from search either way."
          field="behaviour.minSessionFrames"
          terms="session shoot minimum size frames"
        >
          <ValueField
            label="Smallest shoot listed"
            type="number"
            min={1}
            size={4}
            suffix="frames"
            value={behaviour.minSessionFrames}
            invalid={settings.errorFor("behaviour.minSessionFrames") !== ""}
            oninput={(v) => settings.patch({ minSessionFrames: Number(v) })}
          />
        </SettingRow>
      {/if}

      <SettingRow
        name="Currently listed"
        desc="How many of the library's shoots the sidebar is showing at the floor above."
        wired={false}
      >
        <ControlChip
          label="{library.sessions.length} of {library.sessions.length + library.sessionsHidden}"
          disabled
          title="Counted at the last pass over the catalogue"
        />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Cache">
      <SettingRow name="Preview budget" desc="Oldest previews would be evicted first once this is hit." wired={false}>
        <Choice
          label="Preview budget"
          value="none"
          options={[
            { value: "20", label: "20 GB" },
            { value: "50", label: "50 GB" },
            { value: "none", label: "no limit" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="Keep previews for" desc="After this, a preview would be rebuilt on demand." wired={false}>
        <ControlChip label="90 days" disabled title="No eviction policy is implemented yet" />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Maintenance" danger hint="none of these can run yet">
      <SettingRow name="Re-index everything" desc="There is no index to rebuild in this version." wired={false}>
        <ControlChip label="run" disabled title="No index exists yet" />
      </SettingRow>

      <SettingRow
        name="Rebuild previews"
        desc="Would clear the thumbnail cache and decode it again on demand."
        wired={false}
      >
        <ControlChip label="run" disabled title="No backend call clears the cache yet" />
      </SettingRow>

      <SettingRow
        name="Forget missing files"
        desc="Would drop decision rows whose file is gone. Cannot be undone."
        wired={false}
      >
        <ControlChip label="run" danger disabled title="No backend call prunes the database yet" />
      </SettingRow>
    </SettingGroup>
  {/snippet}

  {#snippet aside()}
    <Aside
      title="On disk"
      rows={[
        { k: "decisions", v: "decisions.db", tone: "muted" },
        { k: "journal", v: "journal.jsonl", tone: "muted" },
        { k: "previews", v: "culler/thumbs", tone: "muted" },
        { k: "index", v: "none in this build", tone: "attention" },
      ]}
      note="Counts and sizes appear here once there is a catalogue to measure. Until then this page shows what exists, not what the design drew."
    />
    <Aside
      title="Safety"
      note="The file on disk is always authoritative. Deleting the decisions database loses verdicts and session history — never a photograph."
    />
  {/snippet}
</PageShell>
