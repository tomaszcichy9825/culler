# culler

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

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **culler** (670 symbols, 1357 relationships, 28 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> If any GitNexus tool warns the index is stale, run `npx gitnexus analyze` in terminal first.

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `gitnexus_impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `gitnexus_detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `gitnexus_query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `gitnexus_context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `gitnexus_impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `gitnexus_rename` which understands the call graph.
- NEVER commit changes without running `gitnexus_detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/culler/context` | Codebase overview, check index freshness |
| `gitnexus://repo/culler/clusters` | All functional areas |
| `gitnexus://repo/culler/processes` | All execution flows |
| `gitnexus://repo/culler/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
