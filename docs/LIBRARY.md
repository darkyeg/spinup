# Your library: `~/.spinup`

What is yours (your skills list, your own skills, your instructions) lives in one folder on your machine. There is no repo to clone, no GitHub account, nothing to pull.

```
~/.spinup/
  skills.json       your skills list
  skills/<name>/    your own skills: a folder with a SKILL.md each
  AGENTS.md         your instructions, added after spinup's
```

Everything is optional. Without a library, spinup uses its suggested skills and instructions.

## How it fills

- **Skills list:** it starts as spinup's suggested list. Your first `spinup skills add`, `remove`, `manual` or `auto` copies that list into `skills.json` and changes the copy; from then on your list is the one used.
- **Own skills:** drop a folder with a `SKILL.md` into `skills/`, then run `spinup skills`. It is installed for Claude Code and Codex like the others.
- **Instructions:** write `AGENTS.md`, then run `spinup agents`. It is added after spinup's instructions in `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`.

`SPINUP_LIBRARY` moves the library to another folder.

## Coming from the private repo

Earlier versions kept these files in a private git repo cloned into `local/`. The first time spinup runs from a checkout that has one, it copies `skills.json`, `skills/` and `AGENTS.md` into the library (never over what is already there). After that `local/` is not used and can go.

## Examples for `AGENTS.md`

```markdown
## Personal
- Reply in English, even when I write in another language.
- My projects live in ~/work and ~/personal.
```

Keep it short: it loads in every session.
