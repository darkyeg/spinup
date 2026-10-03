<img src="assets/icon.svg" width="64" height="64" alt="">

# spinup

**Every computer you code with AI on: set up with one command, kept consistent, sharing your AI accounts with hub/standby failover.**

You have a desktop, a laptop, maybe a Mac. You use Claude Code and Codex. On each machine you install the same tools, copy the same skills and settings, and sign in to the same accounts, then watch them drift apart. Competing refreshes of the same login can also leave you repairing account access.

spinup fixes that:

- **One command per machine.** `spinup setup <name>` installs your dev tools, joins your private network ([Tailscale](https://tailscale.com)), installs your skills and agent settings, and ends with a health check.
- **Your accounts on every machine.** One machine holds your Claude and Codex logins in [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI); every computer running spinup uses them at `http://localhost:8317`. If the holder goes off, a standby takes over, and gives the accounts back when it returns. spinup keeps one holder active to avoid competing login refreshes. An abrupt failure just after a refresh can still require logging that account in again; see [the service limits](docs/SERVICE.md).
- **The same agents everywhere.** Your skills and instructions sync through your library. Claude Code and Codex settings come from spinup's defaults, merged into each computer's own config. [Choose where a setting belongs](docs/AGENT-CONFIG.md).
- **It tells you what's wrong.** `spinup doctor` checks everything and prints the command that fixes each problem.

One Go binary, no dependencies. Windows, macOS and Linux. Every command is safe to re-run; re-running is how you repair.

On a Mac the service runs as a LaunchAgent, so a hub or standby Mac holds the accounts only while you are logged in; make an always-on Linux or Windows machine the hub.

## Quick start

**1. Install spinup** on each machine:

The install scripts download binaries from the [latest GitHub release](https://github.com/darkyeg/spinup/releases/latest); publishing source alone does not make them usable. Until the first binary release, get a checkout and build it with Go:

```sh
git clone https://github.com/darkyeg/spinup.git
cd spinup
go install ./cmd/spinup
```

Put Go's bin directory on PATH. For changes on another branch, check out that branch before running `go install`; an unreleased change is unavailable through `spinup update`. Once a release is published, use the installers:

```sh
# macOS, Linux
curl -fsSL https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.ps1 | iex
```

**2. Set up the machine that holds your accounts** (the hub, usually your desktop). Pick a name: it becomes the machine's address on your network.

```sh
spinup setup office-pc --hub
```

Then add your accounts in the dashboard it shows (`http://localhost:8317/management.html`, **OAuth Login**).

**3. Set up every other machine.** Make a laptop a standby, so the accounts keep working while the hub is off:

```sh
spinup setup laptop --standby     # asks for the dashboard password: `spinup keys` on a hub or standby
spinup setup work-mac             # asks for the API key: `spinup keys` on any configured computer
```

Use the **same Tailscale account** on every machine. Then run `ccp` instead of `claude` to use Claude Code with the shared accounts (plain `claude` keeps its own login).

Prefer your agent to do it? Tell it *"set up this machine with spinup"*: it follows [docs/SETUP.md](docs/SETUP.md).

## Commands

| | |
|---|---|
| `spinup setup <name> [--hub\|--standby]` | Set up or repair the whole machine |
| `spinup doctor` | Check everything; each problem comes with its fix |
| `spinup update` | Update spinup, CLIProxyAPI, skills and Tailscale |
| `spinup status` | Who holds the accounts, every login, every machine |
| `spinup handoff <machine>` | Move the accounts to another machine, safely |
| `spinup machines` | Every device on your network and its address |
| `spinup skills [list\|add\|remove\|manual\|auto]` | Your skills for Claude Code and Codex |
| `spinup agents` | Shared instructions, subagents and settings |
| `spinup repo <path> [--apply]` | Get a project ready for agents: its stack's skills, AGENTS.md, git remote |
| `spinup tools` | Install the dev tools a machine lacks |
| `spinup keys` | API key on any configured computer; dashboard password on a hub or standby |

`spinup <command> --help` explains each one.

## How it works

```
 computer running spinup ── localhost:8317 ─────────┐
 phone on Tailscale ─────── <hub-or-standby>:8317 ────┴── current holder's CLIProxyAPI
                         private Tailscale network
```

spinup itself is the router, running on each configured computer. Keep port `8317` unless it is occupied. Each computer has its own localhost, so using the same port across computers causes no conflict. CLIProxyAPI itself uses a separate internal port, `8327`; only the current holder runs it. The routers follow hub/standby changes automatically, and `spinup handoff <machine>` requests a planned move.

A phone without spinup uses an online hub or standby's Tailscale name and the API key, for example `http://office-pc:8317`. That router follows the current holder while it stays online. If the router computer itself goes offline, the phone must use another hub or standby's address; spinup does not move one shared network address between computers.

- **Names, not IPs.** Tailscale gives each machine a fixed private address and the name you chose. At home traffic goes straight over your router; away, directly over the internet, or through an encrypted relay when it must.
- **No machine list to keep.** Tailscale is the list; spinup reads it.
- **One holder at a time.** A standby takes over only when Tailscale reports the holder offline and at least three minutes have elapsed since its last sighting, never just because it can't reach it. Planned moves wait for running requests up to the drain limit; new requests wait for the next holder up to their routing timeout. In plain words: [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md); the full safety design: [docs/DESIGN.md](docs/DESIGN.md). Day-to-day use: [docs/SERVICE.md](docs/SERVICE.md).
- **Change it once, your online machines receive it.** Your skills list, own skills and instructions live in your library (`~/.spinup`). With a hub or standby online, spinup shares changes within seconds; disconnected machines catch up when they reconnect. No private repo or GitHub account is needed for your library.

## Make it yours

| Change | Do |
|---|---|
| Your skills | `spinup skills add <owner/repo> <skill>` / `remove <skill>` |
| Which skills run by themselves | `spinup skills list` shows auto/manual and the token cost; `spinup skills manual <skill>` |
| Your own skills | a folder with a `SKILL.md` in `~/.spinup/skills/`, then `spinup skills` |
| Your instructions | `~/.spinup/AGENTS.md`, then `spinup agents` |

All of it lives in your library: [docs/LIBRARY.md](docs/LIBRARY.md). Why spinup's defaults are what they are: [docs/WHY.md](docs/WHY.md). Using the accounts from T3 Code: [docs/T3-PROXY.md](docs/T3-PROXY.md).

Before relying on a new build across machines, follow the [hub/standby validation guide](docs/VALIDATION.md).

To change spinup's own defaults (suggested skills, stack rules in `skills/per-repo.json`, `agents/`, `tools.json`), fork the repo: spinup uses the checkout it runs in (or `SPINUP_REPO`).

Agent settings files are merged locally, not copied between machines. Use the same spinup defaults on each computer and run `spinup agents` to apply them. Keep provider credentials and machine-specific paths local; use separate provider homes in T3 Code when you want separate logins. Details: [docs/AGENT-CONFIG.md](docs/AGENT-CONFIG.md).

## Security

- **Nothing faces the internet.** The accounts are reachable only on your tailnet (a firewall rule on Windows limits the port to Tailscale addresses), and Tailscale traffic is end-to-end encrypted.
- **Keys stay on your machines** (`secrets.json`, never committed): every machine keeps the API key, and a hub or standby also keeps the dashboard password. Machines prove they know the keys before any key or login is sent to them.
- **Downloads are checksum-verified**: spinup's own updates and CLIProxyAPI's.
- Turn on 2FA for **GitHub** and for your **Tailscale** login, and disable Tailscale key expiry for your machines (`doctor` warns you).
- Found a problem? See [SECURITY.md](SECURITY.md).

## Important

Routing subscription accounts through a proxy may conflict with your provider's terms. Read them and decide for yourself; you are responsible for how you use your accounts. spinup's own rule: use Claude accounts only through Claude Code (`ccp`, or T3 Code's Claude provider), never from other apps.

## Contributing

Ideas and fixes are welcome: [CONTRIBUTING.md](CONTRIBUTING.md). Terms used everywhere: [GLOSSARY.md](GLOSSARY.md).

## License

[MIT](LICENSE)
