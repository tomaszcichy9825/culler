# cull

RAW/JPEG culling and ingest tool. Go + Wails v3, Svelte 5 frontend.

The full design lives in `docs/DESIGN.md` — read it before making changes. It covers the data
model, requirements, performance gates, milestones, and resolved decisions. Do not contradict
it silently; propose changes to the doc first.

Ground rules:

- This is code that deletes people's photographs. The op engine, journal, and grouping logic
  must keep real test coverage. `Plan()` stays pure; assert plans, not the filesystem, in unit
  tests, and use temp-dir integration tests for apply/undo.
- Deletion is never `os.Remove` — OS trash or `_Rejected/` only.
- No `runtime.GOOS` checks outside `internal/platform`.
- Never write anything to the source card: no cache, no database, no sidecars.
- Pure Go by default; cgo only behind a build tag and only after the benchmark gate in
  DESIGN.md §5 fails.
- Public repo, personal GitHub account (tomaszcichy9825) only.
- Project board: https://github.com/users/tomaszcichy9825/projects/5
