# Skills

`skills.json` is the **global** set, installed on every machine by `spinup.py skills`. Anything else in `~/.agents/skills` gets moved to `~/.agents/skills-parked`. Global stays small because every auto-triggered skill's description costs tokens in every session (see [../docs/research/skills.md](../docs/research/skills.md)).

- `sources`: GitHub repos installed with the `skills` CLI. `-s` takes the skill's frontmatter `name`, which can differ from its folder.
- Your own skills: folders with a `SKILL.md` in `skills/local/` (shared) or `local/skills/` (your private repo, [docs/PRIVATE.md](../docs/PRIVATE.md)). They are copied to `~/.agents/skills` and linked into `~/.claude/skills`.

## Per-repo skills

Stack-specific skills live in the repo that needs them, so Go skills don't load in storefront repos and the other way round. The rules (which files or packages trigger which skills) are in `per-repo.json`. Apply them with:

```
py spinup.py repo <path>           # report
py spinup.py repo <path> --apply   # install into <path>/.agents/skills, then commit in that repo
```
