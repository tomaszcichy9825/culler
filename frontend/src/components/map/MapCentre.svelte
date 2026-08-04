<script lang="ts">
  // MAP's centre pane: the map itself, in whichever of the three sub-layouts is
  // showing.
  //
  // Leaflet is bundled — imported here, so vite puts the script and the
  // stylesheet in the app's own bundle and the webview fetches neither over the
  // network. The one thing that does go out is the raster tiles, and the tile
  // layer is not constructed at all until the consent gate has been answered
  // yes. Everything else on this pane is drawn from the photographs in the open
  // folder, so a declined map is a working map on an empty background rather
  // than a broken one.
  //
  // The basemap is darkened with a CSS filter over the tile pane rather than by
  // asking a provider for dark tiles: one endpoint, one attribution, and the
  // treatment survives whatever the tiles look like.

  import { untrack } from "svelte";
  import L from "leaflet";
  import "leaflet/dist/leaflet.css";
  import TileConsent from "./TileConsent.svelte";
  import { app } from "../../lib/state.svelte";
  import { previewURL } from "../../lib/preview";
  import {
    clusterPositions,
    mapState,
    positionToGroup,
    TILE_ATTRIBUTION,
    TILE_MAX_ZOOM,
    TILE_URL,
  } from "../../lib/map.svelte";
  import type { MapCluster, MapPosition } from "../../lib/map.svelte";

  interface Props {
    /** The mode's sub-layout: 0 pins, 1 heat, 2 track. */
    layout?: number;
  }

  let { layout = 0 }: Props = $props();

  let wrap = $state<HTMLDivElement | null>(null);
  let host = $state<HTMLDivElement | null>(null);
  let canvas = $state<HTMLCanvasElement | null>(null);

  let map: L.Map | null = null;
  let tiles: L.TileLayer | null = null;
  let pins: L.LayerGroup | null = null;
  let track: L.LayerGroup | null = null;

  /** The −/+ blob scale of the heat layout. */
  let blobScale = $state(1);

  /** Where the map opens when there is nothing to fit: the whole world. */
  const HOME: [number, number] = [20, 0];
  const HOME_ZOOM = 2;

  /** Metres per pixel at a latitude and zoom, which is how a blob gets a size. */
  function metresPerPixel(lat: number, zoom: number): number {
    return (156543.03392 * Math.cos((lat * Math.PI) / 180)) / Math.pow(2, zoom);
  }

  // The pane fills itself from whichever folder is open, so MAP is populated
  // without the shell having to prime it — the same way IMPORT's panes do. The
  // frames come along too, purely so a pin can draw the grid's cached
  // thumbnail; a folder still being hashed simply has none to give yet.
  $effect(() => {
    const folder = app.folder;
    const groups = app.groups;
    if (folder === null) return;
    untrack(() => {
      mapState.attach(groups);
      void mapState.load(folder.dir);
    });
  });

  // --- the map ---------------------------------------------------------------

  $effect(() => {
    if (host === null) return;
    const created = L.map(host, {
      zoomControl: false,
      attributionControl: true,
      // Leaflet binds the arrows to panning. The app binds them to walking the
      // frames, and a map that swallowed them would strand the keyboard.
      keyboard: false,
    }).setView(HOME, HOME_ZOOM);
    L.control.zoom({ position: "bottomright" }).addTo(created);

    pins = L.layerGroup().addTo(created);
    track = L.layerGroup().addTo(created);
    map = created;

    // Clusters are cut in projected pixels at the current zoom, so panning
    // leaves them alone and only a zoom has to recut them.
    const recut = () => recluster();
    created.on("zoomend", recut);
    created.on("move", drawHeat);
    created.on("resize", drawHeat);
    recut();

    return () => {
      created.off("zoomend", recut);
      created.off("move", drawHeat);
      created.off("resize", drawHeat);
      created.remove();
      map = null;
      pins = null;
      track = null;
      // Removing the map takes its layers with it, so the handle to the tile
      // layer has to go too — a stale one would read as "tiles are already up"
      // and leave a rebuilt map with no basemap.
      tiles = null;
    };
  });

  // The tile layer exists only while consent stands. Revoking it takes the
  // basemap away without touching anything else on the pane.
  $effect(() => {
    const enabled = mapState.tilesEnabled;
    if (map === null) return;
    if (enabled && tiles === null) {
      tiles = L.tileLayer(TILE_URL, {
        attribution: TILE_ATTRIBUTION,
        maxZoom: TILE_MAX_ZOOM,
      }).addTo(map);
    } else if (!enabled && tiles !== null) {
      map.removeLayer(tiles);
      tiles = null;
    }
  });

  /**
   * recluster cuts the pins for the zoom the map is at.
   *
   * It is untracked throughout, because it both reads the positions and writes
   * the clusters and the focus into them: an effect that called it and were
   * left subscribed to what it wrote would invalidate itself. That is not a
   * theoretical tidiness — the effect that does this is the one that builds the
   * map, so a self-invalidating version tears the Leaflet map down and rebuilds
   * it at the default view, silently undoing the fit.
   */
  function recluster() {
    const current = map;
    if (current === null) return;
    untrack(() => {
      const zoom = current.getZoom();
      mapState.setClusters(
        clusterPositions(mapState.positions, (p) => current.project(L.latLng(p.latitude, p.longitude), zoom)),
      );
    });
  }

  // A new folder recuts and reframes. Reading `positions` is what subscribes
  // this effect to it, and the only thing that should.
  $effect(() => {
    const frames = mapState.positions;
    if (map === null) return;
    recluster();
    fit(frames);
  });

  /**
   * fit frames every position, or goes home when there is nothing to frame.
   *
   * The reframe is not animated. Flying across the world every time a folder
   * opens is a swoop nobody asked for, and the clusters are cut on zoomend —
   * so an animation that a throttled tab never finishes would leave the pins
   * clustered for a zoom the map is no longer at.
   */
  function fit(frames: MapPosition[] = mapState.positions) {
    if (map === null) return;
    if (frames.length === 0) {
      map.setView(HOME, HOME_ZOOM, { animate: false });
      return;
    }
    const bounds = L.latLngBounds(frames.map((f) => L.latLng(f.latitude, f.longitude)));
    // A folder shot from one spot has no extent to fit, so it gets a sensible
    // street-level zoom rather than Leaflet's maximum.
    if (bounds.getNorthEast().equals(bounds.getSouthWest())) {
      map.setView(bounds.getCenter(), 16, { animate: false });
      return;
    }
    map.fitBounds(bounds, { padding: [48, 48], animate: false });
  }

  // --- pins ------------------------------------------------------------------

  /** The thumbnail behind a pin's swatch, when the preview route can serve one. */
  function swatchURL(cluster: MapCluster): string {
    const first = cluster.frames[0];
    return previewURL(positionToGroup(first, mapState.hashOf(first)), "grid");
  }

  function pinHTML(cluster: MapCluster, selected: boolean): string {
    const src = swatchURL(cluster);
    const swatch =
      src === ""
        ? '<span class="sw"></span>'
        : `<span class="sw"><img alt="" loading="lazy" src="${src}"></span>`;
    return `<div class="pin${selected ? " sel" : ""}">${swatch}<span class="ct">${cluster.count}</span></div>`;
  }

  /**
   * Pins are rebuilt whenever the clusters or the selection change. Leaflet has
   * no diffing to lean on and a folder's worth of pins is a few dozen markers,
   * so replacing the layer is both simpler and fast enough.
   */
  $effect(() => {
    const clusters = mapState.clusters;
    const focused = mapState.clusterIndex;
    const showing = layout === 0;
    if (map === null || pins === null) return;

    pins.clearLayers();
    if (!showing) return;

    clusters.forEach((cluster, index) => {
      const selected = index === focused;
      const marker = L.marker([cluster.latitude, cluster.longitude], {
        zIndexOffset: selected ? 1000 : 0,
        keyboard: true,
        alt: `${cluster.count} frames`,
        icon: L.divIcon({
          className: "",
          html: pinHTML(cluster, selected),
          iconSize: [60, 26],
          iconAnchor: [30, 13],
        }),
      });
      marker.on("click", () => mapState.focusCluster(index));
      marker.on("dblclick", () => {
        mapState.focusCluster(index);
        mapState.open(cluster.frames[0]);
      });
      marker.on("keypress", (e: L.LeafletKeyboardEvent) => {
        if (e.originalEvent.key !== "Enter") return;
        mapState.focusCluster(index);
        mapState.open(cluster.frames[0]);
      });
      pins!.addLayer(marker);
    });
  });

  // --- track -----------------------------------------------------------------

  /**
   * The line joins the folder's own positions in capture order. It is not a
   * GPX track: reading a recorder's file and matching frames to it by timestamp
   * is a separate piece of work, and drawing this line as though it were one
   * would claim a match that was never made.
   */
  $effect(() => {
    const frames = mapState.positions;
    const showing = layout === 2;
    if (map === null || track === null) return;

    track.clearLayers();
    if (!showing || frames.length === 0) return;

    const line = frames.map((f) => L.latLng(f.latitude, f.longitude));
    if (line.length > 1) {
      track.addLayer(L.polyline(line, { color: "#c678dd", weight: 3, opacity: 0.9, lineCap: "round" }));
    }
    for (const at of line) {
      track.addLayer(
        L.circleMarker(at, { radius: 4, color: "#0e1013", weight: 1.5, fillColor: "#98c379", fillOpacity: 1 }),
      );
    }
    track.addLayer(
      L.circleMarker(line[0], { radius: 5, color: "#c678dd", weight: 2, fillColor: "#0e1013", fillOpacity: 1 }),
    );
    track.addLayer(
      L.circleMarker(line[line.length - 1], {
        radius: 5,
        color: "#c678dd",
        weight: 2,
        fillColor: "#c678dd",
        fillOpacity: 1,
      }),
    );
  });

  // --- heat ------------------------------------------------------------------

  /**
   * Density is drawn onto a plain canvas over the map rather than through a
   * plugin: it is four filled circles per place, and the whole of it is the
   * design's own maths — a radius of 26 + √frames × 5.4 metres, three warm
   * rings and a hotter core.
   */
  function drawHeat() {
    const surface = canvas;
    const current = map;
    if (surface === null || current === null) return;

    const size = current.getSize();
    const dpr = window.devicePixelRatio || 1;
    if (surface.width !== Math.round(size.x * dpr) || surface.height !== Math.round(size.y * dpr)) {
      surface.width = Math.round(size.x * dpr);
      surface.height = Math.round(size.y * dpr);
    }
    const ctx = surface.getContext("2d");
    if (ctx === null) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, size.x, size.y);
    if (layout !== 1) return;

    const zoom = current.getZoom();
    for (const cluster of mapState.clusters) {
      const at = current.latLngToContainerPoint(L.latLng(cluster.latitude, cluster.longitude));
      const metres = 26 + Math.sqrt(cluster.count) * 5.4;
      const radius = (metres / metresPerPixel(cluster.latitude, zoom)) * blobScale;
      if (!Number.isFinite(radius) || radius <= 0) continue;

      for (const [factor, alpha] of [
        [1.9, 0.07],
        [1.35, 0.11],
        [1, 0.2],
      ] as [number, number][]) {
        ctx.beginPath();
        ctx.arc(at.x, at.y, radius * factor, 0, Math.PI * 2);
        ctx.fillStyle = `rgba(224, 108, 117, ${alpha})`;
        ctx.fill();
      }
      ctx.beginPath();
      ctx.arc(at.x, at.y, radius * 0.55, 0, Math.PI * 2);
      ctx.fillStyle = "rgba(229, 192, 123, 0.42)";
      ctx.fill();
    }
  }

  // Redraw whenever anything the drawing reads has changed.
  $effect(() => {
    void mapState.clusters;
    void layout;
    void blobScale;
    void canvas;
    drawHeat();
  });

  // --- keys ------------------------------------------------------------------

  /**
   * The pane's keys are bound to the element rather than declared on it: a map
   * is a region full of Leaflet's own controls, not a widget with a role that
   * would justify a keyboard handler in the markup, and `data-keys="local"`
   * already tells the application's global listener to keep out.
   */
  $effect(() => {
    const region = wrap;
    if (region === null) return;
    region.addEventListener("keydown", onKeydown);
    return () => region.removeEventListener("keydown", onKeydown);
  });

  function onKeydown(e: KeyboardEvent) {
    switch (e.key) {
      case "f":
        e.preventDefault();
        fit();
        break;
      case "+":
      case "=":
        if (layout !== 1) return;
        e.preventDefault();
        blobScale = Math.min(4, blobScale * 1.25);
        break;
      case "-":
        if (layout !== 1) return;
        e.preventDefault();
        blobScale = Math.max(0.25, blobScale / 1.25);
        break;
      case "Enter":
        e.preventDefault();
        mapState.open();
        break;
    }
  }

  let placed = $derived(`${mapState.positions.length} of ${mapState.total} frames placed`);
</script>

<div class="wrap" bind:this={wrap} data-keys="local">
  <div class="map" bind:this={host}></div>
  <canvas class="heat" class:on={layout === 1} bind:this={canvas}></canvas>

  <div class="float">
    <div class="chip">
      {#if mapState.loading}
        <span class="lit">reading positions</span>
        {#if mapState.progress !== null}
          <span class="sep">|</span><span>{mapState.progress.done} of {mapState.progress.total}</span>
        {/if}
      {:else}
        <span class="lit">{placed}</span>
        {#if mapState.withoutGPS !== ""}
          <span class="sep">|</span><span class="warn">{mapState.withoutGPS}</span>
        {/if}
      {/if}
    </div>

    {#if !mapState.tilesEnabled && mapState.asked}
      <div class="chip quiet">no basemap — tiles are off</div>
    {/if}

    {#if mapState.error !== null}
      <div class="chip warn">{mapState.error}</div>
    {/if}
  </div>

  <div class="hints">
    <span class="chip">f fit</span>
    {#if layout === 1}<span class="chip">−/+ blob scale</span>{/if}
    <span class="chip">⏎ open the focused frame</span>
  </div>

  {#if !mapState.asked}
    <TileConsent />
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    flex: 1;
    min-width: 0;
    min-height: 0;
    outline: none;
  }

  .map {
    width: 100%;
    height: 100%;
    background: var(--bg-app);
  }

  /* The density canvas sits over the tiles and under the controls, and never
     takes the pointer: panning and zooming go on working through it. */
  .heat {
    position: absolute;
    inset: 0;
    z-index: 450;
    pointer-events: none;
    display: none;
  }

  .heat.on {
    display: block;
  }

  .float {
    position: absolute;
    top: 12px;
    left: 12px;
    z-index: 500;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    pointer-events: none;
  }

  .hints {
    position: absolute;
    bottom: 12px;
    left: 12px;
    z-index: 500;
    display: flex;
    gap: 6px;
    pointer-events: none;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 9px;
    padding: 5px 10px;
    border-radius: 5px;
    background: var(--glass);
    border: 1px solid var(--border-strong);
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .hints .chip {
    padding: 4px 8px;
    font-size: 10.5px;
  }

  .chip.quiet {
    color: var(--text-dim);
  }

  .lit {
    color: var(--text-hi);
    font-weight: 600;
  }

  .sep {
    color: var(--border-strong);
  }

  .warn {
    color: var(--amber);
  }

  /* --- Leaflet's own chrome, and the pins it draws from raw HTML -------------
     Both have to be global: the tile pane and the controls are Leaflet's
     elements, and a pin is a string handed to L.divIcon, so neither carries
     this component's scoping attribute. */

  :global(.leaflet-container) {
    background: var(--bg-app);
    font-family: var(--font-mono);
  }

  :global(.leaflet-tile-pane) {
    filter: invert(1) hue-rotate(185deg) saturate(0.55) brightness(0.82) contrast(1.06);
  }

  :global(.leaflet-control-attribution) {
    background: var(--glass) !important;
    color: var(--text-dim) !important;
    font-size: 9.5px !important;
    border: none !important;
  }

  :global(.leaflet-control-attribution a) {
    color: var(--text-dim) !important;
  }

  :global(.leaflet-bar) {
    border: 1px solid var(--border-strong) !important;
  }

  :global(.leaflet-bar a) {
    background: var(--bg-chrome) !important;
    color: var(--text-muted) !important;
    border-bottom-color: var(--border-strong) !important;
  }

  :global(.leaflet-bar a:hover) {
    background: #1f242b !important;
    color: var(--text) !important;
  }

  :global(.pin) {
    display: flex;
    align-items: center;
    gap: 5px;
    padding: 3px 3px 3px 4px;
    border-radius: 5px;
    background: rgba(14, 16, 19, 0.9);
    border: 1px solid var(--border-dialog);
    box-shadow: var(--shadow-pin);
    cursor: pointer;
    white-space: nowrap;
  }

  :global(.pin .sw) {
    width: 26px;
    height: 18px;
    border-radius: 2px;
    background: var(--bg-thumb);
    overflow: hidden;
  }

  :global(.pin .sw img) {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  :global(.pin .ct) {
    font-family: var(--font-mono);
    font-size: 10.5px;
    font-weight: 700;
    color: var(--text-2);
    padding-right: 3px;
  }

  :global(.pin.sel) {
    border-color: var(--accent);
    box-shadow:
      var(--focus-ring-soft),
      var(--shadow-pin);
  }

  :global(.pin.sel .ct) {
    color: var(--text-hi);
  }
</style>
