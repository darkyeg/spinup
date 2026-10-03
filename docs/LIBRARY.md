# Your library: `~/.spinup`

What is yours (your skills list, your own skills, your instructions) lives in one folder on each machine, and spinup keeps that folder the same on all your machines. There is no repo to clone, no GitHub account, nothing to pull.

```
~/.spinup/
  skills.json       your skills list
  skills/<name>/    your own skills: a folder with a SKILL.md each
  AGENTS.md         your instructions, added after spinup's
  fetched/<name>/   a copy of each skill on your list, fetched from GitHub once
```

Everything is optional. Without a library, spinup uses its suggested skills and instructions.

## How it fills

- **Skills list:** it starts as spinup's suggested list. Your first `spinup skills add`, `remove`, `manual` or `auto` copies that list into `skills.json` and changes the copy; from then on your list is the one used.
- **Own skills:** drop a folder with a `SKILL.md` into `skills/`, then run `spinup skills`. It is installed for Claude Code and Codex like the others.
- **Instructions:** write `AGENTS.md`, then run `spinup agents`. It is added after spinup's instructions in `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`.

`SPINUP_LIBRARY` moves the library to another folder.

## Shared between your machines

Change your library on any machine (a `spinup skills` command, or by hand) and within about 15 seconds every other machine has the change and has installed it: the skills for Claude Code and Codex, and your instructions.

- **Through the hub or standby.** The spinup service on each machine compares libraries with the machine holding the accounts, and the hub and standbys compare with each other. So a change reaches everyone as long as a hub or standby is on.
- **The newest change wins.** If two machines change the library at nearly the same time, the later change is kept on all machines.
- **Commands start from the newest.** `spinup skills` commands first bring in a newer library from your other machines, so your change builds on it.
- **A new machine never overwrites yours.** A library you never changed loses to any library you did, so a new machine takes yours.
- **Skills are fetched once.** The machine where you add a skill fetches it from GitHub (with Node.js) and keeps a copy in `fetched/`; the other machines install that copy, without Node.js or GitHub.
- **Only your machines.** Libraries travel inside your tailnet, and only to and from machines that know your API key.

## Coming from the private repo

Earlier versions kept these files in a private git repo cloned into `local/`. The first time spinup runs from a checkout that has one, it copies `skills.json`, `skills/` and `AGENTS.md` into the library (never over what is already there). After that `local/` is not used and can go.

## Examples for `AGENTS.md`

```markdown
## Personal
- Reply in English, even when I write in another language.
- My projects live in ~/work and ~/personal.
```

Keep it short: it loads in every session.
