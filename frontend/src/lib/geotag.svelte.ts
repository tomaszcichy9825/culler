// Setting a location on frames that have none: a pin dropped on the map, or a
// position copied from a frame that already carries one. Both end here, and
// both go out through the same EXIF write the metadata editor uses — planned,
// confirmed, journalled and undoable — so a geotag is no less safe than any
// other metadata edit and no different to undo.

import { ExifService } from "./bindings";
import type { ExifPlanDTO, GPSCoordDTO } from "./exif.svelte";
import type { GroupDTO } from "./bindings";
import { mapState } from "./map.svelte";
import { app } from "./state.svelte";

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** The path a frame's location is written to: the JPEG in place, or the RAW's
 *  sidecar. It is the same half the metadata editor writes — the one a write can
 *  reach without opening the RAW. */
function writePath(g: GroupDTO): string {
  return g.hasJpeg && g.jpegPath !== "" ? g.jpegPath : g.rawPath;
}

function editsFor(paths: string[], coord: GPSCoordDTO) {
  return paths.map((path) => ({
    path,
    dateTimeOriginal: null,
    artist: null,
    copyright: null,
    stripGps: false,
    setGps: coord,
  }));
}

class GeotagState {
  /** Whether a map click will drop a location rather than pan. */
  armed = $state(false);

  /** The plan awaiting confirmation. Non-null means the dialog is up. */
  plan = $state<ExifPlanDTO | null>(null);

  busy = $state(false);

  #pending: { paths: string[]; coord: GPSCoordDTO } | null = null;

  /** How many frames a place-or-copy would act on right now. */
  get targetCount(): number {
    return app.selected.length;
  }

  arm() {
    if (app.selected.length === 0) {
      app.notify("select the photos to place first");
      return;
    }
    this.armed = true;
  }

  disarm() {
    this.armed = false;
  }

  /** place is the dropped-pin path: the selection takes the clicked coordinate. */
  place(latitude: number, longitude: number) {
    this.armed = false;
    void this.request(app.selected, { latitude, longitude, altitude: 0, hasAltitude: false });
  }

  /** copyFrom borrows one located frame's coordinate onto the selection. */
  copyFrom(coord: GPSCoordDTO) {
    void this.request(app.selected, coord);
  }

  /** request plans writing coord onto frames and puts the plan up to confirm. */
  async request(frames: GroupDTO[], coord: GPSCoordDTO) {
    const paths = frames.map(writePath).filter((p) => p !== "");
    if (paths.length === 0) {
      app.notify("select the photos to place first");
      return;
    }
    this.#pending = { paths, coord };
    this.busy = true;
    try {
      this.plan = await ExifService.Plan(editsFor(paths, coord) as never);
    } catch (err) {
      app.notify(`could not plan the location: ${message(err)}`, "error");
      this.#pending = null;
    } finally {
      this.busy = false;
    }
  }

  /** confirm writes the planned location and refreshes the map over it. */
  async confirm() {
    if (this.#pending === null) return;
    const { paths, coord } = this.#pending;
    this.busy = true;
    try {
      const batch = await ExifService.Apply(editsFor(paths, coord) as never);
      const failed = (batch.actions ?? []).filter((a) => a.outcome !== "ok").length;
      this.plan = null;
      this.#pending = null;
      await mapState.reload();
      if (failed > 0) app.notify(`${failed} frame(s) could not be located`, "error");
      else app.notify(`located ${paths.length} frame${paths.length === 1 ? "" : "s"}`);
    } catch (err) {
      this.plan = null;
      this.#pending = null;
      app.notify(`could not write the location: ${message(err)}`, "error");
    } finally {
      this.busy = false;
    }
  }

  cancel() {
    this.plan = null;
    this.#pending = null;
  }
}

export const geotag = new GeotagState();
