<script lang="ts">
  // Culling: what the verdict keys mean, and what an apply does with them.
  //
  // The Verdicts and Ratings groups describe behaviour the app has but the
  // config file has no field for. Their chips show what the app actually does
  // today — they are marked as not wired rather than drawn as if pressing them
  // would change anything.

  import { app } from "../../lib/state.svelte";
  import { CUT_SCOPES, KEEP_MASKS, TRASH_MODES, COLLISION_POLICIES, settings } from "../../lib/settings.svelte";
  import { verdictOf } from "../../lib/verdict";
  import Aside from "./Aside.svelte";
  import Choice from "./Choice.svelte";
  import PageShell from "./PageShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import ValueField from "./ValueField.svelte";

  const MASK_LABELS: Record<string, string> = { rj: "both", r: "raw only", j: "jpeg only" };
  const CUT_LABELS: Record<string, string> = { both: "both halves", masked: "masked only" };
  const TRASH_LABELS: Record<string, string> = { system: "system trash", "rejected-folder": "a folder" };
  const COLLISION_LABELS: Record<string, string> = {
    skip: "skip",
    "rename-suffix": "rename",
    overwrite: "overwrite",
  };

  let behaviour = $derived(settings.draft?.behaviour);

  let counts = $derived.by(() => {
    let keep = 0;
    let cut = 0;
    for (const g of app.groups) {
      const v = verdictOf(g);
      if (v === "keep") keep++;
      else if (v === "cut") cut++;
    }
    return { keep, cut, frames: app.groups.length, pending: app.pending.length };
  });

  function options<T extends string>(values: T[], labels: Record<string, string>) {
    return values.map((value) => ({ value, label: labels[value] ?? value }));
  }
</script>

{#if behaviour}
  <PageShell>
    {#snippet content()}
      <SettingGroup title="Verdicts">
        <SettingRow
          name="Default verdict for a new frame"
          desc="Undecided means nothing happens until you say so."
          wired={false}
        >
          <Choice
            label="Default verdict"
            value="undecided"
            options={[
              { value: "undecided", label: "undecided" },
              { value: "keep", label: "keep" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Advance after a verdict"
          desc="A verdict on a single frame moves to the next one. A verdict on a selection stays put."
          wired={false}
        >
          <Choice
            label="Advance after a verdict"
            value="on"
            options={[
              { value: "on", label: "on" },
              { value: "off", label: "off" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="A verdict on a selection"
          desc="Whether k applies to every selected frame or only the cursor."
          wired={false}
        >
          <Choice
            label="A verdict on a selection"
            value="whole"
            options={[
              { value: "whole", label: "whole selection" },
              { value: "cursor", label: "cursor only" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Second press"
          desc="Pressing k on a frame that is already keep clears the verdict."
          wired={false}
        >
          <Choice
            label="Second press"
            value="clears"
            options={[
              { value: "clears", label: "clears" },
              { value: "noop", label: "no-op" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Pairs">
        <SettingRow
          name="Default mask for keep"
          desc="Which halves survive when you press k without touching r or j."
          field="behaviour.defaultKeepMask"
          terms="raw jpeg mask"
        >
          <Choice
            label="Default mask for keep"
            value={behaviour.defaultKeepMask}
            options={options(KEEP_MASKS, MASK_LABELS)}
            onchange={(defaultKeepMask) => settings.patch({ defaultKeepMask })}
          />
        </SettingRow>

        <SettingRow
          name="Cut removes"
          desc="A cut frame removes both halves, or only the ones the mask leaves out."
          field="behaviour.cutRemoves"
          terms="raw jpeg mask delete"
        >
          <Choice
            label="Cut removes"
            value={behaviour.cutRemoves}
            options={options(CUT_SCOPES, CUT_LABELS)}
            onchange={(cutRemoves) => settings.patch({ cutRemoves })}
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Ratings">
        <SettingRow name="Scale" desc="Five stars, with 0 clearing the rating." wired={false}>
          <Choice
            label="Rating scale"
            value="stars"
            options={[
              { value: "stars", label: "1–5 stars" },
              { value: "three", label: "3-step" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>

        <SettingRow
          name="Rating implies keep"
          desc="Rating a frame leaves its verdict alone today. Toggling a mask half is what implies a keep."
          wired={false}
        >
          <Choice
            label="Rating implies keep"
            value="off"
            options={[
              { value: "on", label: "on" },
              { value: "off", label: "off" },
            ]}
            onchange={() => {}}
            disabled
          />
        </SettingRow>
      </SettingGroup>

      <SettingGroup title="Applying" danger hint="these decide what leaves the disk">
        <SettingRow
          name="Confirm above"
          desc="Ask a second time when a plan removes more than this many files."
          field="behaviour.bulkConfirmThreshold"
          terms="bulk threshold confirm"
        >
          <ValueField
            label="Confirm above"
            type="number"
            min={0}
            size={5}
            suffix="files"
            value={behaviour.bulkConfirmThreshold}
            invalid={settings.errorFor("behaviour.bulkConfirmThreshold") !== ""}
            oninput={(v) => settings.patch({ bulkConfirmThreshold: Number(v) })}
          />
        </SettingRow>

        <SettingRow
          name="Where a cut file goes"
          desc="The system trash, or a folder beside the originals. Neither one deletes anything."
          field="behaviour.trashMode"
          terms="trash rejected delete"
        >
          <Choice
            label="Where a cut file goes"
            value={behaviour.trashMode}
            options={options(TRASH_MODES, TRASH_LABELS)}
            onchange={(trashMode) => settings.patch({ trashMode })}
          />
        </SettingRow>

        <SettingRow
          name="Rejected folder name"
          desc="Created inside the folder being culled, when cuts go to a folder."
          field="behaviour.rejectedFolderName"
          terms="trash rejected folder name"
        >
          <ValueField
            label="Rejected folder name"
            size={14}
            value={behaviour.rejectedFolderName}
            disabled={behaviour.trashMode !== "rejected-folder"}
            invalid={settings.errorFor("behaviour.rejectedFolderName") !== ""}
            oninput={(rejectedFolderName) => settings.patch({ rejectedFolderName })}
          />
        </SettingRow>

        <SettingRow
          name="When the destination is taken"
          desc="A copy or a move landing on an existing file. Renaming is the default because overwriting is not undoable."
          field="behaviour.collisionPolicy"
          terms="collision overwrite rename skip"
        >
          <Choice
            label="When the destination is taken"
            value={behaviour.collisionPolicy}
            options={options(COLLISION_POLICIES, COLLISION_LABELS)}
            onchange={(collisionPolicy) => settings.patch({ collisionPolicy })}
          />
        </SettingRow>
      </SettingGroup>
    {/snippet}

    {#snippet aside()}
      <Aside
        title="Current session"
        rows={[
          { k: "frames", v: String(counts.frames) },
          { k: "pending", v: String(counts.pending), tone: "attention" },
          { k: "keep", v: String(counts.keep), tone: "good" },
          { k: "cut", v: String(counts.cut) },
        ]}
      />
      <Aside
        title="Effect of these settings"
        note="A verdict on a selection judges every frame in it, so a burst of four can be culled with one x — read the pending count before ↩, not after."
      />
    {/snippet}
  </PageShell>
{/if}
