# Why the defaults are what they are

Short version: every token an agent reads costs money and attention. The defaults keep what loads in **every** session small, and spend effort only where it pays. Checked against Claude Code 2.1 and Codex CLI 0.16 docs (2026); re-check when they change.

## Skills: few global, the rest per project

- Every installed skill's name and description is in context on **every turn**. Claude Code budgets about 1% of the context window for the list, Codex about 2%. When the list overflows, descriptions get cut and skills stop triggering. ([Claude Code skills](https://code.claude.com/docs/en/skills), [Codex skills](https://learn.chatgpt.com/docs/build-skills.md))
- So: about 20-30 general skills globally (`skills/skills.json`), and stack-specific ones only in the projects that use them (`skills/per-repo.json`, installed by `spinup repo`). Go skills don't load in a React project, and the other way round.
- `spinup skills` **parks** anything not on the list in `~/.agents/skills-parked` instead of deleting it.
- Frameworks that ship their own agent docs (Next.js 16+, Effect) need no skill: point the agent at the bundled docs.

## Instructions: one short file for both agents

- `agents/AGENTS.md` is installed as `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`. It loads at the start of every session and in every subagent, so it stays under about 40 lines. Longer files cost more *and* are followed less well. ([Claude Code memory](https://code.claude.com/docs/en/memory))
- Project rules belong in the project's own `AGENTS.md` (Claude Code reads it too). `spinup repo` warns when one is over 200 lines.

## Claude Code settings (`agents/claude/settings.json`)

| Setting | Why |
|---|---|
| Effort `medium` for Opus and Sonnet | Thinking tokens bill as output. Medium is the model default; raise it per task with `/effort`. |
| `outputStyle: "Concise"` | Output tokens are the most expensive kind. Concise leads with the result and drops narration. |
| `CLAUDE_CODE_SUBAGENT_MODEL=sonnet` | Subagents otherwise inherit the session model (often Opus). |
| `agents/claude/agents/Explore.md` on Haiku | The built-in Explore subagent runs on Opus on a subscription; searching doesn't need it. |
| `promptSuggestionEnabled: false` | Skips a small extra request after each reply. |

`spinup agents` merges these keys into your existing `settings.json`; everything else in it (hooks, status line) is kept.

## Codex settings (`agents/codex/config.toml`)

| Setting | Why |
|---|---|
| `model_reasoning_effort = "medium"` | Codex docs: start at the default effort, raise it per task. |
| `plan_mode_reasoning_effort = "high"` | Spend reasoning on the plan, not on every edit. |
| `model_reasoning_summary = "concise"` | Shorter reasoning summaries. |
| Subagents on the small model, effort `low` | Focused, repeatable subtasks don't need the big model. |

## Habits that save more than any setting

- `/clear` between unrelated tasks; a long session resends its whole history on every message.
- Name the files and functions in your request; vague requests make the agent scan the repo.
- Send test and build runs to a subagent that reports only failures.
- Prefer CLIs (`gh`, `git`) over MCP servers when both work, and turn off MCP servers you don't use.
