# Skills

Every installed skill's description sits in the agent's context in **every** session, so spinup keeps the global set small and installs stack-specific skills only in the projects that use them. Background: [docs/WHY.md](../docs/WHY.md).

## Commands

```bash
spinup.py skills                          # install the list on this machine, park anything not on it
spinup.py skills list                     # every skill: auto or manual, where from, token cost
spinup.py skills add <owner/repo> <skill> # add from a GitHub repo and install (--manual to add as manual)
spinup.py skills remove <skill>           # take it off the list (it gets parked, not deleted)
spinup.py skills manual <skill>           # runs only when you call it: /skill (Claude Code), $skill (Codex)
spinup.py skills auto <skill>             # the agent may use it by itself again
```

Add `--private` to `add`, `remove`, `manual` or `auto` to change only **your** machines: it edits `local/skills.json` in your private repo ([docs/PRIVATE.md](../docs/PRIVATE.md)) instead of the shared `skills.json`.

**Auto or manual?** Auto skills cost tokens in every session but trigger on their own. Manual skills cost nothing until you type `/name`. Make workflows you start yourself (planning, handoffs, PR writing) manual; keep auto the ones you want the agent to reach for unprompted (debugging, TDD, verification).

## Files

- `skills.json`: the global list. `sources` maps a GitHub repo to skill names (`-s` takes the skill's frontmatter `name`, which can differ from its folder). `manual` lists skills that only run when called.
- Your own skills: folders with a `SKILL.md` in `skills/local/` (shared) or `local/skills/` (private). They are copied to `~/.agents/skills` and linked into `~/.claude/skills`.
- Parked skills: `~/.agents/skills-parked`. Move one back, or `skills add` it, to use it again.

## Per-repo skills

Stack-specific skills (React, shadcn, Go, Effect, Turborepo, ...) go into the repo that needs them. The rules (which files or packages trigger which skills) are in `per-repo.json`:

```bash
spinup.py repo <path>           # report: stack found, skills missing
spinup.py repo <path> --apply   # install into <path>/.agents/skills, then commit in that repo
```
