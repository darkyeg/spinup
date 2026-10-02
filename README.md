<img src="assets/icon.svg" width="64" height="64" alt="">

# spinup

**Every computer you code with AI on: set up with one command, kept the same, sharing your AI accounts without ever logging them out.**

You have a desktop, a laptop, maybe a Mac. You use Claude Code and Codex. On each machine you install the same tools, copy the same skills and settings, and sign in to the same accounts, then watch them drift apart. And if two machines refresh the same login, the provider logs you out everywhere.

spinup fixes that:

- **One command per machine.** `spinup setup <name>` installs your dev tools, joins your private network ([Tailscale](https://tailscale.com)), installs your skills and agent settings, and ends with a health check.
- **Your accounts on every machine.** One machine holds your Claude and Codex logins in [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI); every machine uses them at `http://localhost:8317`. If the holder goes off, a standby takes over, and gives the accounts back when it returns. Two machines never refresh the same login, so you are never logged out.
- **The same agents everywhere.** Skills, instructions and token-saving settings for Claude Code and Codex live in one repo. Change them once; every machine gets them.
- **It tells you what's wrong.** `spinup doctor` checks everything and prints the command that fixes each problem.

One Go binary, no dependencies. Windows, macOS and Linux. Every command is safe to re-run; re-running is how you repair.

On a Mac the service runs as a LaunchAgent, so a hub or standby Mac holds the accounts only while you are logged in; make an always-on Linux or Windows machine the hub.

## Quick start

**1. Install spinup** on each machine:

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
spinup setup laptop --standby     # asks for the dashboard password: run `spinup keys` on the hub
spinup setup work-mac             # only uses the accounts; asks for the API key once
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
| `spinup keys` | The API key and dashboard password (hub or standby) |

`spinup <command> --help` explains each one.

## How it works

```
 laptop (standby) ──┐                        ┌─ office-pc (hub): holds the accounts
 work-mac        ───┼── Tailscale, private ──┤   in CLIProxyAPI
 phone           ───┘                        └─ every machine: http://localhost:8317
```

- **Names, not IPs.** Tailscale gives each machine a fixed private address and the name you chose. At home traffic goes straight over your router; away, directly over the internet, or through an encrypted relay when it must.
- **No machine list to keep.** Tailscale is the list; spinup reads it.
- **One holder at a time.** A standby takes over only when Tailscale itself reports the holder offline for three minutes, never just because it can't reach it. Planned moves wait for running requests and never cut one; requests sent during any switch wait for the new holder instead of failing. In plain words: [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md); the full safety design: [docs/DESIGN.md](docs/DESIGN.md). Day-to-day use: [docs/SERVICE.md](docs/SERVICE.md).
- **One source of truth.** Fork this repo to make it yours: skills, instructions, settings and tools live in it, and `setup` pulls your latest version first.

## Make it yours

Fork the repo and clone your fork; spinup uses the checkout it runs in (or `SPINUP_REPO`).

| Change | Edit, then run |
|---|---|
| Skills on every machine | `spinup skills add <owner/repo> <skill>` / `remove <skill>` |
| Which skills run by themselves | `spinup skills list` shows auto/manual and the token cost; `spinup skills manual <skill>` |
| Skills per project type (Go, Next.js, ...) | `skills/per-repo.json` → `spinup repo <path> --apply` |
| Instructions and settings | `agents/` → `spinup agents` |
| Dev tools | `tools.json` → `spinup tools` |

**Personal things** (your preferences, private skills, notes about your machines) go in a private repo cloned into `local/`; the public repo ignores it: [docs/PRIVATE.md](docs/PRIVATE.md). Why the defaults are what they are: [docs/WHY.md](docs/WHY.md). Using the accounts from T3 Code: [docs/T3-PROXY.md](docs/T3-PROXY.md).

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
