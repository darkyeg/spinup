# spinup

This repo is the single source of truth for the user's machines: which machines exist, how each is set up, the shared Claude Code / Codex config, and the skills every agent gets. `spinup.py` applies it; `--help` lists the commands.

## Tasks

- **Check a machine** ("is everything OK?"): run `spinup.py doctor`; each problem prints its fix.
- **Add a machine**: `spinup.py setup <name>` on it (`--hub` only for the hub). The name becomes its Tailscale name; clients find the hub on the tailnet. There is no machine list in the repo: Tailscale is the list, `spinup.py links` prints it.
- **Set up or repair a machine** ("set up this PC/laptop/Mac"): `spinup.py setup <name>` does the machine part; follow [docs/SETUP.md](docs/SETUP.md) end to end.
- **Change skills**: edit `skills/skills.json` (global) or `skills/per-repo.json` (stack rules), then run `spinup.py skills` / `spinup.py repo <path> --apply`. See [skills/README.md](skills/README.md).
- **Prepare a project repo for agents**: `spinup.py repo <path>` reports, `--apply` changes; commit the result in that repo.
- **Add a dev tool to every machine**: add it to `packages.json`, then `spinup.py packages`.
- **Change agent behaviour or token settings**: edit `agents/` (`AGENTS.md`, `claude/settings.json`, `codex/config.toml`, `claude/agents/`), then run `spinup.py agents`. Reasons behind the current values: [docs/WHY.md](docs/WHY.md).
- **Code changes**: `spinup.py` is only the CLI; each command lives in `spin/<area>.py` and shares `spin/common.py`.
- **Proxy or T3 Code wiring**: [docs/T3-PROXY.md](docs/T3-PROXY.md).

## Rules

- The repo is the source; installed copies (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, `~/.claude/settings.json`, proxy `config.yaml`) get overwritten. Change the repo, then re-run the command.
- Secrets stay on machines: proxy keys live in the hub's `secrets.json`, read with `spinup.py show-key`. Commit only files without keys or tokens.
- This repo is public: nothing personal (names, IPs, paths, emails, project names). Personal things go in `local/`, the user's private repo ([docs/PRIVATE.md](docs/PRIVATE.md)); after setting up or changing a machine, update `local/MACHINES.md` there if it exists.
- Python: `py` on Windows (the Microsoft Store `python` breaks AppData paths), `python3` elsewhere. Standard library only.
- `spinup.py` commands are idempotent; re-running one is the way to repair.
