# spinup

This repo is the single source of truth for the user's machines: which machines exist, how each is set up, the shared Claude Code / Codex config, and the skills every agent gets. `spinup.py` applies it; `--help` lists the commands.

## Tasks

- **Check a machine** ("is everything OK?"): run `spinup.py doctor`; each problem prints its fix.
- **Add a machine**: `spinup.py setup <name>` on it, with a name the user chose (never an example name like `pc`) (`--hub` only for the hub). The name becomes its Tailscale name; clients find the hub on the tailnet. There is no machine list in the repo: Tailscale is the list, `spinup.py links` prints it.
- **Set up or repair a machine** ("set up this PC/laptop/Mac"): `spinup.py setup <name>` does the machine part; follow [docs/SETUP.md](docs/SETUP.md) end to end.
- **Change skills**: `spinup.py skills list | add <owner/repo> <skill> | remove <skill> | manual <skill> | auto <skill>` (add `--private` for the user's machines only, in `local/skills.json`). Stack rules: `skills/per-repo.json`, applied by `spinup.py repo <path> --apply`. See [skills/README.md](skills/README.md).
- **Prepare a project repo for agents**: `spinup.py repo <path>` reports, `--apply` changes; commit the result in that repo.
- **Add a dev tool to every machine**: add it to `packages.json`, then `spinup.py packages`.
- **Change agent behaviour or token settings**: edit `agents/` (`AGENTS.md`, `claude/settings.json`, `codex/config.toml`, `claude/agents/`), then run `spinup.py agents`. Reasons behind the current values: [docs/WHY.md](docs/WHY.md).
- **Code changes**: `spinup.py` is only the CLI; each command lives in `spin/<area>.py` and shares `spin/common.py`.
- **The `spinup` service** (Go: `cmd/spinup`, `internal/`; failover between hub and standby without logging accounts out): [docs/SERVICE.md](docs/SERVICE.md), design and safety rules in [docs/DESIGN.md](docs/DESIGN.md). Leadership policy is the pure `internal/leadership`; any change there needs a case in its table test, and `internal/service/machine_test.go` must keep "never two proxies at once". Wire shapes live only in `internal/api`. Code follows the clean-code rules below. Check with `go vet ./... && go test ./...`. Where it is installed, `spinup.py hub`/`client` step aside.
- **Proxy or T3 Code wiring**: [docs/T3-PROXY.md](docs/T3-PROXY.md).

## Rules

- The repo is the source; installed copies (`~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, `~/.claude/settings.json`, proxy `config.yaml`) get overwritten. Change the repo, then re-run the command.
- Secrets stay on machines: proxy keys live in the hub's `secrets.json`, read with `spinup.py show-key`. Commit only files without keys or tokens.
- This repo is public: nothing personal (names, IPs, paths, emails, project names). Personal things go in `local/`, the user's private repo ([docs/PRIVATE.md](docs/PRIVATE.md)); after setting up or changing a machine, update `local/MACHINES.md` there if it exists.
- Python: `py` on Windows (the Microsoft Store `python` breaks AppData paths), `python3` elsewhere. Standard library only.
- Go: standard library plus `golang.org/x/{sys,term}` and `kong` (the CLI); no cgo (cross-built for every OS).
- Clean code: one unit, one job; pure rules with thin adapters; export only what callers use; typed outcomes instead of boolean bags; name things instead of inlining them. No comment unless the code can't say it, and no divider comments (`conventions_test.go` fails on them): a file that needs sections becomes several files.
- `spinup.py` commands are idempotent; re-running one is the way to repair.
