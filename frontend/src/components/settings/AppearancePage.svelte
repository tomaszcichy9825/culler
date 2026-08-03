<script lang="ts">
  // Appearance: theme, density, and what a tile shows.
  //
  // Scheme, accent and face are the app's own memory rather than lines in
  // config.json — the file has no fields for them — and the aside says so, so
  // that "the config file is the source of truth" is not quietly untrue.

  import { ACCENTS, DEFAULT_MONO, SCHEMES, appearance } from "../../lib/appearance.svelte";
  import type { AccentName, Scheme } from "../../lib/appearance.svelte";
  import Aside from "./Aside.svelte";
  import Choice from "./Choice.svelte";
  import PageShell from "./PageShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import ValueField from "./ValueField.svelte";

  const schemeOptions = SCHEMES.map((value: Scheme) => ({ value, label: value }));
  const accentOptions = ACCENTS.map((value: AccentName) => ({ value, label: value }));
</script>

<PageShell>
  {#snippet content()}
    <SettingGroup title="Theme">
      <SettingRow name="Colour scheme" desc="Follows the OS unless you pin it." terms="dark light system theme">
        <Choice
          label="Colour scheme"
          value={appearance.scheme}
          options={schemeOptions}
          onchange={(scheme) => appearance.setScheme(scheme)}
        />
      </SettingRow>

      <SettingRow name="Accent" desc="Used for focus, selection, and the active mode." terms="colour blue cyan purple">
        <span class="swatch" style:background={appearance.swatch(appearance.accent)} aria-hidden="true"></span>
        <Choice
          label="Accent"
          value={appearance.accent}
          options={accentOptions}
          onchange={(accent) => appearance.setAccent(accent)}
        />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Density">
      <SettingRow name="Row height" desc="Affects tables, rails, and the status line." wired={false}>
        <Choice
          label="Row height"
          value="compact"
          options={[
            { value: "compact", label: "compact" },
            { value: "comfortable", label: "comfortable" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="UI text size" desc="Chrome only — never the photographs." wired={false}>
        <Choice
          label="UI text size"
          value="12"
          options={[
            { value: "11", label: "11 px" },
            { value: "12", label: "12 px" },
            { value: "13", label: "13 px" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow
        name="Monospace face"
        desc="Anything installed. Empty uses {DEFAULT_MONO}, which ships with the app."
        terms="font typeface mono"
      >
        <ValueField
          label="Monospace face"
          size={16}
          placeholder={DEFAULT_MONO}
          value={appearance.mono}
          oninput={(face) => appearance.setMono(face)}
        />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Tiles">
      <SettingRow
        name="Show on a tile"
        desc="Filename and the R/J pair are always shown."
        wired={false}
        terms="verdict rating size time"
      >
        <Choice
          label="Show on a tile"
          value="verdict"
          options={[
            { value: "verdict", label: "verdict" },
            { value: "rating", label: "rating" },
            { value: "size", label: "size" },
            { value: "time", label: "time" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="Thumbnail fit" desc="Fill crops to the tile; fit shows the whole frame." wired={false}>
        <Choice
          label="Thumbnail fit"
          value="fill"
          options={[
            { value: "fill", label: "fill" },
            { value: "fit", label: "fit" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="Grid columns" desc="Or − and + in the grid header, which the app already has." wired={false}>
        <Choice
          label="Grid columns"
          value="auto"
          options={[
            { value: "auto", label: "auto" },
            { value: "fixed", label: "fixed" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>
    </SettingGroup>

    <SettingGroup title="Chrome">
      <SettingRow name="Show the keys in the status line" desc="Turn off once they are in your fingers." wired={false}>
        <Choice
          label="Show the keys in the status line"
          value="on"
          options={[
            { value: "on", label: "on" },
            { value: "off", label: "off" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>

      <SettingRow name="Inspector on launch" desc="Right pane starts open or collapsed." wired={false}>
        <Choice
          label="Inspector on launch"
          value="open"
          options={[
            { value: "open", label: "open" },
            { value: "collapsed", label: "collapsed" },
          ]}
          onchange={() => {}}
          disabled
        />
      </SettingRow>
    </SettingGroup>
  {/snippet}

  {#snippet aside()}
    <Aside
      title="Preview"
      rows={[
        { k: "scheme", v: `${appearance.scheme} → ${appearance.theme}` },
        { k: "accent", v: `${appearance.accent} · ${appearance.swatchHex(appearance.accent)}`, tone: "accent" },
        { k: "face", v: appearance.mono === "" ? DEFAULT_MONO : appearance.mono },
      ]}
    />
    <Aside
      title="Where these live"
      note="Scheme, accent and face are remembered by the app, not written to config.json — the file has no fields for them yet. Everything else on every other page is a line in that file."
    />
    <Aside
      title="Rules"
      note="Themes only swap token values. No layout, size, or weight changes between light and dark, so a screenshot of one maps onto the other."
    />
  {/snippet}
</PageShell>

<style>
  .swatch {
    width: 12px;
    height: 12px;
    border-radius: 3px;
    border: 1px solid var(--border-strong);
  }
</style>
