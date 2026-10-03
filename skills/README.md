# Skills

Every installed skill's description sits in the agent's context in **every** session, so spinup keeps the global set small and installs stack-specific skills only in the projects that use them. Background: [docs/WHY.md](../docs/WHY.md).

## Commands

```bash
spinup skills                          # interactive list: remove, restore, keep, rename (--plain prints it)
spinup skills sync                     # install the list on this machine, park anything not on it
spinup skills new <skill>              # start a skill of your own in your library
spinup skills import <path|zip>        # take a skill someone sent you into your library
spinup skills add <owner/repo> <skill> # add from a GitHub repo and install (--manual to add as manual)
spinup skills remove [skill...]        # take it off the list (parked, not deleted); no name: pick from a list
spinup skills manual [skill...]        # runs only when you call it: /skill (Claude Code), $skill (Codex)
spinup skills auto [skill...]          # the agent may use it by itself again
```

The bare command never changes anything, so it is safe to run first: it reports what is missing, what is
installed but not in your library, and any two skills answering to one name. `sync` is what applies it.

They edit **your** list, `~/.spinup/skills.json` in your library ([docs/LIBRARY.md](../docs/LIBRARY.md)). Until your first edit, spinup's suggested list (`skills.json` here) is used; the first edit copies it.

**Auto or manual?** Auto skills cost tokens in every session but trigger on their own. Manual skills cost nothing until you type `/name`. Make workflows you start yourself (planning, handoffs, PR writing) manual; keep auto the ones you want the agent to reach for unprompted (debugging, TDD, verification).

## Files

- `skills.json`: spinup's suggested list. `sources` maps a GitHub repo to skill names (`-s` takes the skill's frontmatter `name`, which can differ from its folder). `manual` lists skills that only run when called.
- Your own skills: folders with a `SKILL.md` in `~/.spinup/skills/`. `spinup skills new` and `import` write them for you. They are copied to `~/.agents/skills` and linked into `~/.claude/skills` and `~/.codex/skills`.
- Fetched copies: `~/.spinup/fetched/<name>/`. A skill on your list is fetched from GitHub once, on the machine where you add it; every other machine installs this copy, which spinup shares with your library.
- Parked skills: `~/.agents/skills-parked`. Move one back, or `skills add` it, to use it again.

## Per-repo skills

Stack-specific skills (React, shadcn, Go, Effect, Turborepo, ...) go into the repo that needs them. The rules (which files or packages trigger which skills) are in `per-repo.json`:

```bash
spinup repo <path>           # report: stack found, skills missing
spinup repo <path> --apply   # install into <path>/.agents/skills, then commit in that repo
```
