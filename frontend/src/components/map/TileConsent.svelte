<script lang="ts">
  // The gate over the map pane, up until the user has answered it.
  //
  // The app's non-goals say no cloud, and map tiles are the single accepted
  // exception — so they are opt-in, and the opt-in is a real gate rather than a
  // notice: no tile layer exists behind this panel, and therefore no request
  // has been made. Declining is not a dead end; the pins, the heat and the
  // track all draw on the empty background, because they are the folder's own
  // data and owe nothing to a tile server.

  import { mapState, TILE_ATTRIBUTION } from "../../lib/map.svelte";

  let panel = $state<HTMLDivElement | null>(null);

  // The panel takes the keyboard when it appears: it is the only thing on the
  // pane that can be acted on, and it must be answerable without the pointer.
  $effect(() => {
    if (panel !== null) panel.querySelector<HTMLElement>(".enable")?.focus();
  });

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      mapState.declineTiles();
    }
  }
</script>

<div class="scrim" data-keys="local" role="presentation" onkeydown={onKeydown}>
  <div
    class="panel"
    bind:this={panel}
    role="dialog"
    aria-modal="false"
    aria-label="Map tiles"
    tabindex="-1"
  >
    <p class="lead">Map tiles are fetched from OpenStreetMap — the one network call culler makes.</p>
    <p class="note">
      Nothing else on this screen leaves the machine: the pins, the density and the track are read from the
      photographs in the open folder. Declining keeps the map, without a basemap under it. Tiles carry the
      attribution “{TILE_ATTRIBUTION}”.
    </p>
    <div class="row">
      <button type="button" class="enable" onclick={() => mapState.grantTiles()}>Enable</button>
      <button type="button" class="later" onclick={() => mapState.declineTiles()}>Not now</button>
    </div>
  </div>
</div>

<style>
  .scrim {
    position: absolute;
    inset: 0;
    z-index: 600;
    display: grid;
    place-items: center;
    padding: 24px;
    background: var(--scrim-plan);
  }

  .panel {
    width: min(420px, 100%);
    padding: 16px 18px;
    border-radius: 9px;
    background: var(--bg-chrome);
    border: 1px solid var(--border-dialog);
    box-shadow: var(--shadow-dialog);
    outline: none;
  }

  .lead {
    margin: 0;
    font-size: 12.5px;
    line-height: 1.5;
    color: var(--text-hi);
    text-wrap: pretty;
  }

  .note {
    margin: 9px 0 0;
    font-size: 11px;
    line-height: 1.6;
    color: var(--text-muted);
    text-wrap: pretty;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding-top: 14px;
  }

  button {
    height: 26px;
    padding: 0 13px;
    border-radius: 5px;
    font: inherit;
    font-size: 11.5px;
    cursor: pointer;
  }

  .enable {
    border: none;
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 700;
  }

  .later {
    border: 1px solid var(--border-strong);
    background: var(--bg-field);
    color: var(--text-muted);
  }

  .later:hover {
    color: var(--text);
    border-color: var(--border-dialog);
  }

  .enable:focus-visible,
  .later:focus-visible {
    outline: none;
    box-shadow: var(--focus-ring);
  }
</style>
