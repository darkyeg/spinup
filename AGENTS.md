# spinup

This repo is the single source of truth for the user's machines: how each is set up, the shared Claude Code / Codex config, the skills every agent gets, and the accounts service that shares AI accounts between machines. The Go command `spinup` applies it; `spinup --help` lists the commands. Terms: [GLOSSARY.md](GLOSSARY.md).

## Tasks

- **Check a machine** ("is everything OK?"): `spinup doctor`; each problem prints its fix.
- **Set up, add or repair a machine**: `spinup setup <name>` on it, with a name the user chose (never an example name like `pc`). Add `--hub` for the machine that normally holds the accounts, `--standby` for one that takes over while the hub is off. Then follow [docs/SETUP.md](docs/SETUP.md) end to end. There is no machine list in the repo: Tailscale is the list, `spinup machines` prints it.
- **Change skills**: `spinup skills list | add <owner/repo> <skill> | remove <skill> | manual <skill> | auto <skill>`; they edit the user's library (`~/.spinup`, [docs/LIBRARY.md](docs/LIBRARY.md)). spinup's suggested list is `skills/skills.json`; stack rules: `skills/per-repo.json`, applied by `spinup repo <path> --apply`. See [skills/README.md](skills/README.md).
- **Prepare a project repo for agents**: `spinup repo <path>` reports, `--apply` changes; commit the result in that repo.
- **Add a dev tool to every machine**: add it to `tools.json`, then `spinup tools`.
- **Change personal agent behaviour**: edit the library's `AGENTS.md`, then `spinup agents`. Models, effort and limits stay in each tool's local settings. `agents/` contains shared guidelines and optional templates for forks, not a contributor's personal preferences. See [docs/AGENT-CONFIG.md](docs/AGENT-CONFIG.md).
- **The accounts** (hub/standby failover without logging accounts out): [docs/SERVICE.md](docs/SERVICE.md); design and safety rules: [docs/DESIGN.md](docs/DESIGN.md).
- **Proxy or T3 Code wiring**: [docs/T3-PROXY.md](docs/T3-PROXY.md).

## Code

- `cmd/spinup` is only the CLI (kong): one file per command, printing and prompts. Each capability is its own package in `internal/`: `tailnet`, `tools`, `skills`, `agentconfig`, `project`, `doctor`, the accounts (`service`, `leadership`, `logins`, `proxy`, `api`), and the shared `host` (where things live), `shell` (running programs), `source` (spinup's data: a checkout, or the copy built into the binary), `library` (the user's own: `~/.spinup`), `release`, `config`.
- Leadership policy is the pure `internal/leadership`; any change there needs a case in its table test, and `internal/service/machine_test.go` must keep "never two proxies at once". Wire shapes live only in `internal/api`.
- Check with `go vet ./... && go test ./...`; CI also cross-builds every release target.

## Rules

- Installed instructions combine shared guidelines with the user's library. Edit the appropriate source, then re-run the command. Keep personal settings local; shared templates only replace keys explicitly listed in them.
- Secrets stay on machines (`secrets.json` next to the proxy, printed by `spinup keys`). Commit only files without keys or tokens.
- This repo is public: nothing personal (names, IPs, paths, emails, project names). The user's own skills and instructions go in their library ([docs/LIBRARY.md](docs/LIBRARY.md)); personal notes about this repo's machines go in the git-ignored `local/` if it exists (update `local/MACHINES.md` after setting up or changing a machine).
- Go: standard library plus `golang.org/x/{sys,term}` and `kong`; no cgo (cross-built for every OS).
- Clean code: one unit, one job; pure rules with thin adapters; export only what callers use; typed outcomes instead of boolean bags; name things instead of inlining them. No comment unless the code can't say it, and no divider comments (`conventions_test.go` fails on them): a file that needs sections becomes several files.
- Commands are idempotent; re-running one is the way to repair.
