# culler

A keyboard-driven desktop app for reviewing photos straight off a card and deciding, per frame, what survives: both files, the JPEG only, the RAW only, or neither.

> **Status: early development.** Nothing here is usable yet. The design is settled; the code is being written.

## Why

Existing open-source cullers treat the RAW as the primary asset and de-duplicate the JPEG away. If you shoot RAW+JPEG where the JPEG is a first-class deliverable (Fujifilm film simulations, in-camera profiles, straight-out-of-camera delivery) that is backwards.

culler is built around a **per-frame three-way decision** over a RAW+JPEG pair, with the JPEG as the thing you actually look at when it exists.

Explicit non-goals: no develop/edit module, no catalogue or library, no AI culling, no cloud, no telemetry, no plugin system.

## How it works

Point it at a folder or card. Files are grouped into frames by shared basename (`DSCF1234.RAF` + `DSCF1234.JPG` is one frame). Sidecars follow their RAW. Then you move through a grid with the keyboard:

| Key | Decision |
|---|---|
| `1` | keep all |
| `2` | drop RAW, keep JPEG |
| `3` | drop JPEG, keep RAW |
| `4` | drop both |
| `0` | clear decision |
| `Enter` | apply |

Every action is reachable without a mouse. Arrow keys or `h j k l` to move, `Tab` for the loupe, `Z` for 1:1 zoom, `Cmd/Ctrl+K` for a fuzzy command palette, `?` for the keymap overlay. The keymap is remappable.

## Safety

This is code that deletes people's photographs, so:

- **Deletion is never permanent by default.** Rejects go to the OS trash (Finder Trash, Recycle Bin, XDG trash), or optionally to a `_Rejected/` subfolder. The only real destruction in the app is an explicit "empty rejects" command that shows a full count breakdown first.
- **Every applied batch is journaled** to an append-only log with per-action outcomes. Undo replays the journal backwards.
- Nothing is ever written to the card: no cache, no database, no sidecars. Removable and read-only media are detected and refused for writes.
- Collisions on copy/move never silently overwrite.

## Formats

RAW: RAF, ARW, CR2, CR3, NEF, NRW, ORF, RW2, DNG, PEF, SRW, RWL, 3FR, IIQ, X3F and more. Previews come from the full-size JPEG embedded in the RAW, so RAW-only folders work without a demosaicer.

JPEG-class: JPG, HEIC, HEIF, PNG, TIFF, WebP, AVIF.

The extension lists are config-driven, so unknown formats can be added without a new release. Unrecognised files are ignored, never touched.

## Platforms

macOS first (signed, notarised DMG), Windows and Linux as supported secondaries. Built with Go and [Wails v3](https://wails.io); a single binary per platform, no runtime dependencies.

## Roadmap

- **v0.1**: scan, grid, loupe, the four decision keys, apply, undo. Trash-based deletion only.
- **v0.2**: copy/move with a destination palette and MRU slots, filters, bulk operations, EXIF panel.
- **v0.3**: card ingest with folder templates and verified copy, 1:1 demosaiced zoom, compare mode.
- **v0.4**: Windows and Linux builds, auto-update, XMP export.

Project board: [github.com/users/tomaszcichy9825/projects/5](https://github.com/users/tomaszcichy9825/projects/5/views/1)

## Support

culler is free and MIT-licensed, with no paywalled features. If it saves you time on the
review after a shoot, you can [sponsor its development](https://github.com/sponsors/tomaszcichy9825).

## Licence

[MIT](LICENSE)
