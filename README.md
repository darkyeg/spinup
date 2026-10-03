<img src="assets/icon.svg" width="64" height="64" alt="">

# spinup

**Set up every computer you code with AI on, keep them the same, and share your Claude and Codex accounts between them.**

You have a desktop, a laptop, maybe a Mac. On each one you install the same tools, copy the same skills, and sign in to the same accounts — then watch them drift apart.

spinup does that once, per machine:

```sh
spinup setup my-desktop --hub
```

That installs your dev tools, joins your private network, installs your skills and agent settings, and ends with a health check.

One Go binary, no dependencies. Windows, macOS, Linux. Every command is safe to re-run — re-running is how you repair.

## What you get

**Your accounts, everywhere.** One machine holds your Claude and Codex logins. Every other machine uses them at `http://localhost:8317`. If the holder goes offline, a standby takes over and hands them back when it returns.

**The same agents, everywhere.** Your skills and instructions live in one folder (`~/.spinup`) that spinup keeps identical on all your machines. Change it anywhere; the others follow within seconds.

**A straight answer when something breaks.** `spinup doctor` checks everything and prints the command that fixes each problem.

## Quick start

### 1. Install

```sh
# macOS, Linux
curl -fsSL https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.sh | sh
```

```powershell
# Windows
irm https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.ps1 | iex
```

Check it worked: `spinup --version`. Open a new terminal if the command isn't found.

### 2. Set up the machine that holds your accounts

This is the **hub** — usually the desktop that's on most. The name you pick becomes its address on your network.

```sh
spinup setup my-desktop --hub
```

Then add your accounts in the dashboard it opens (`http://localhost:8317/management.html` → **OAuth Login**).

### 3. Set up your other machines

Use the **same Tailscale account** on every machine.

```sh
spinup setup my-laptop --standby   # takes over while the hub is off
spinup setup my-mac                # just uses the accounts
```

Each asks for a key, which `spinup keys` prints on a machine you already set up.

### 4. Use it

Run `ccp` instead of `claude` to use Claude Code with the shared accounts. Plain `claude` keeps its own login.

> **Prefer your agent to do all this?** Tell it *"set up this machine with spinup"* and it will follow [docs/SETUP.md](docs/SETUP.md).

## Commands

| | |
|---|---|
| `spinup setup <name> [--hub\|--standby]` | Set up or repair the whole machine |
| `spinup doctor` | Check everything; each problem comes with its fix |
| `spinup update` | Update spinup, CLIProxyAPI, skills and Tailscale |
| `spinup status` | Who holds the accounts, every login, every machine |
| `spinup handoff <machine>` | Move the accounts to another machine, safely |
| `spinup machines` | Every device on your network and its address |
| `spinup skills` | Your skills for Claude Code and Codex — see below |
| `spinup agents` | Shared instructions, subagents and settings |
| `spinup repo <path> [--apply]` | Get a project ready for agents: stack skills, AGENTS.md, git remote |
| `spinup tools` | Install the dev tools a machine lacks |
| `spinup keys` | The API key here; the dashboard password on a hub or standby |

`spinup <command> --help` explains each one.

## Skills

Your skills live in one folder, your **library** (`~/.spinup/skills`). spinup copies it to every machine and links it into Claude Code and Codex. Edit a skill on any machine; the others follow within seconds.

```sh
spinup skills        # see them all, and change them
```

It opens a list with three tabs:

| Tab | What's in it |
|---|---|
| **Shared** | Skills in your library. Shows which agents load each one. |
| **Only here** | Skills on this machine that spinup doesn't share yet, such as a folder only Codex has. |
| **Removed** | Everything you removed. Nothing is ever deleted. |

Arrows move, `space` marks, `tab` switches list, `q` quits. Then `d` remove, `a` keep everywhere, `r` restore, `m` auto/manual, `n` rename. Outside a terminal, or with `--plain`, it prints the list and changes nothing.

### Bring a skill in

| You have | Do |
|---|---|
| A skill that only exists on this machine (in Claude's or Codex's folder) | `spinup skills`, open **Only here**, press `a` |
| A folder or `.zip` someone sent you | `spinup skills import <path>` |
| A new idea | `spinup skills new <name>` |
| A skill on GitHub | `spinup skills add <owner/repo> <skill>` |

**Auto or manual?** An auto skill's description costs tokens in every session. A manual one costs nothing until you call it (`/name` in Claude Code, `$name` in Codex). Press `m` to switch.

spinup installs for the agents it finds on the machine; you don't list them. More: [docs/LIBRARY.md](docs/LIBRARY.md).

## Make it yours

Your instructions go in `~/.spinup/AGENTS.md`, then run `spinup agents`. To change spinup's own defaults (stack rules, `agents/`, `tools.json`), fork the repo: spinup uses the checkout it runs in, or `SPINUP_REPO`.

## How it works

```
 computer running spinup ── localhost:8317 ─────────┐
 phone on Tailscale ─────── <hub-or-standby>:8317 ──┴── current holder's CLIProxyAPI
                         private Tailscale network
```

spinup runs on each machine and routes to whichever one currently holds the accounts.

- **Names, not IPs.** [Tailscale](https://tailscale.com) gives each machine a fixed private address and the name you chose. At home, traffic goes straight over your router; away, over the internet.
- **No machine list to keep.** Tailscale is the list; spinup reads it.
- **One holder at a time.** A standby takes over only when Tailscale reports the holder offline and three minutes have passed — never just because it can't be reached. This is what stops two machines refreshing the same login and locking you out.
- **Changes travel in seconds.** With a hub or standby online, your library reaches every machine; disconnected ones catch up when they return.

Port `8317` is spinup's router; CLIProxyAPI uses `8327` internally, and only on the holder. Each machine has its own localhost, so the same port everywhere is fine.

In plain words: [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md). The safety design: [docs/DESIGN.md](docs/DESIGN.md). Day-to-day: [docs/SERVICE.md](docs/SERVICE.md).

## Good to know

- **A Mac shouldn't be your hub.** Its service runs as a LaunchAgent, so it holds the accounts only while you're logged in. Use an always-on Linux or Windows machine.
- **Agent settings are merged locally, not copied between machines.** Run `spinup agents` on each. Keep credentials and machine-specific paths local: [docs/AGENT-CONFIG.md](docs/AGENT-CONFIG.md).
- **A phone** without spinup points at an online hub or standby by name (`http://my-desktop:8317`) with the API key. If that machine goes offline, point it at another; spinup doesn't move one address between machines.
- **Before trusting a new build** across machines, follow the [validation guide](docs/VALIDATION.md). A local health check doesn't exercise failover.
- **Building from source?** `go install ./cmd/spinup` from a checkout. Unreleased changes don't arrive through `spinup update`.

## Security

- **Nothing faces the internet.** The accounts are reachable only on your tailnet, and Tailscale traffic is end-to-end encrypted. On Windows a firewall rule limits the port to Tailscale addresses.
- **Keys stay on your machines** (`secrets.json`, never committed). Machines prove they know the key before any login is sent to them.
- **Downloads are checksum-verified**, both spinup's updates and CLIProxyAPI's.
- Turn on 2FA for **GitHub** and **Tailscale**, and disable Tailscale key expiry for your machines — `doctor` warns you.
- Found a problem? [SECURITY.md](SECURITY.md).

## Important

Routing subscription accounts through a proxy may conflict with your provider's terms. Read them and decide for yourself; you are responsible for how you use your accounts. spinup's own rule: use Claude accounts only through Claude Code (`ccp`, or T3 Code's Claude provider), never from other apps.

The accounts service keeps one holder active to avoid competing login refreshes. An abrupt failure just after a refresh can still mean logging that account in again: [the service limits](docs/SERVICE.md).

## Contributing

Ideas and fixes are welcome: [CONTRIBUTING.md](CONTRIBUTING.md). Terms used everywhere: [GLOSSARY.md](GLOSSARY.md).

## License

[MIT](LICENSE)
