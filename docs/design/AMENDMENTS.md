# Design amendments

Decisions made after the spec was extracted; these override DESIGN-SPEC.md where they conflict.

- **2026-08-03 — Tab cycles sub-layouts in every mode.** The `cycle-layout` action (Tab)
  cycles the current mode's `⌥1–3` sub-layouts in order, wrapping: CULL contact sheet →
  loupe-first → table; MAP pins → heat → track; LIBRARY search → sessions → storage; EXIF
  likewise once its sub-layouts exist. The cycle must read the active mode's layout list from
  shell state so new modes inherit it. `⌥1–3` still jump directly. The space loupe overlay is
  unchanged.
