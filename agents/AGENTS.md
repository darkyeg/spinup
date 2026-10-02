# Working agreements (Claude Code + Codex)

Installed by spinup (`agents/AGENTS.md`) to `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`. Edit it in the repo, not in place.

## Communication
- Lead with the result: no preamble, narration, or closing recap.
- Progress notes are one line. Final report: what changed, how it was verified, open risks.
- When blocked or facing an irreversible choice, ask one focused question; otherwise pick a sensible default and say which.

## Reading code
- Search first (grep/glob, LSP go-to-definition), then read only the relevant range.
- Start from the files the request names; widen only when the trail leads there.
- Prefer CLIs (git, gh, bun, pnpm, go, cargo, dotnet, uv) over MCP tools when both work.

## Making changes
- Smallest change that solves the task, in the patterns the repo already uses.
- Plan first only for multi-file or architectural work; keep plans short.
- The repo's own AGENTS.md / CLAUDE.md and bundled framework docs (e.g. `node_modules/next/dist/docs`, `node_modules/effect/AGENTS.md`) win over memory for library APIs.

## Verifying
- Prove each change with the narrowest check: the touched test file or package, then typecheck/lint, then wider suites only if needed.
- Filter noisy output to failures (`| tail -n 50`, test-name filters). Run one heavy build/test at a time.
- Claim success only after a check passed; if a check couldn't run, say so.

## Context hygiene
- Delegate log, test and doc digging to a subagent that returns a summary.
- When compacting, keep: current goal, files touched, failing checks, decisions made.

## Windows shells (Codex)
- Run routine shell commands with `login: false` and `tty: false`.
- Start background helpers with `Start-Process -WindowStyle Hidden`; show a window only when asked.
- Keep the sandbox and the default terminal as configured.
