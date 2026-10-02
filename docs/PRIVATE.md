# Your private layer: `local/`

This repo is public and the same for everyone. Things that are only yours go in a **private** git repo cloned into `local/` inside your spinup folder. `local/` is in `.gitignore`, so nothing in it can end up in the public repo.

```
spinup/
  local/            <- your private repo (optional)
    AGENTS.md       appended to the shared instructions (agents/AGENTS.md)
    skills/<name>/  your own skills, installed like the others
    MACHINES.md     notes about your machines (anything you like)
```

All parts are optional. Without `local/`, spinup works the same with the shared defaults.

## Set it up once

```bash
gh repo create my-spinup-private --private --clone   # or create it on github.com
mv my-spinup-private local                           # run from the spinup folder
```

On every other machine, clone it into `local/` too: `git clone <your private repo> local`.

## What spinup does with it

- `setup` pulls `local/` together with spinup, so all machines get your changes.
- `agents` appends `local/AGENTS.md` to the shared instructions for Claude Code (`~/.claude/CLAUDE.md`) and Codex (`~/.codex/AGENTS.md`).
- `skills` installs every `local/skills/<name>/SKILL.md` for both agents, next to the skills from `skills/skills.json`.
- `doctor` reports whether `local/` has changes you haven't pushed.

## Examples for `local/AGENTS.md`

```markdown
## Personal
- Reply in English, even when I write in another language.
- My projects live in ~/work and ~/personal.
```

Keep it short: it loads in every session.
