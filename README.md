<img src="assets/icon.svg" width="64" height="64" alt="">

# spinup

**Set up every computer you code with AI on, with one command, and keep them the same.**

You have a main PC and maybe a laptop or a Mac. You use Claude Code and Codex. spinup:

- installs your dev tools (git, node, rg, fd, gh, go, rust, uv, bun, Claude Code, Codex);
- connects your machines privately with [Tailscale](https://tailscale.com), so they reach each other by the names you give them, at home or away;
- makes one machine the **hub**: it holds your Claude/Codex logins in [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI), and the others use them through it;
- gives every machine the same **skills**, instructions and token-saving settings for Claude Code and Codex;
- checks itself: `doctor` lists every problem with the command that fixes it.

Python standard library only. Windows, macOS and Linux. Every command is safe to re-run; re-running is how you repair.

## Quick start

You need git and Python 3.11+ (on Windows the python.org build; run it as `py`, not the Microsoft Store `python`).

**1. Choose a name for each machine.** The name becomes the machine's address on your private network: if you call your hub `office-pc`, every other machine reaches it as `office-pc` (`http://office-pc:8317`, `ssh office-pc`). Lowercase letters, digits and dashes. Below, `<hub-name>` and `<machine-name>` are **placeholders**: replace them with your own names.

**2. On your main machine (the hub, which holds the accounts):**

```bash
git clone https://github.com/darkyeg/spinup && cd spinup
python3 spinup.py setup <hub-name> --hub
```

**3. On every other machine (it finds the hub by itself):**

```bash
git clone https://github.com/darkyeg/spinup && cd spinup
python3 spinup.py setup <machine-name>
```

Without a name, `setup` asks for one (and suggests the machine's current name). The first run opens a Tailscale login: use the **same Tailscale account** everywhere. A client asks for the hub's key once; get it on the hub with `spinup.py show-key`. At the end, `setup` prints the machine's addresses.

Or just tell your agent: *"set up this machine with spinup"*. It follows [docs/SETUP.md](docs/SETUP.md).

## Every day

| You want to... | Run |
|---|---|
| Use Claude Code with the hub's accounts | `ccp` (plain `claude` keeps its own login) |
| See all your machines and their addresses | `spinup.py links` |
| Check that everything is fine | `spinup.py doctor` |
| Update the proxy, skills and Tailscale | `spinup.py update` |
| Get a project ready for agents | `spinup.py repo <path> --apply` |

## Commands

| Command | What it does |
|---|---|
| `setup [name] [--hub]` | The whole machine: tools, Tailscale name, proxy (hub, or a client that finds the hub), skills, agent config, then `doctor` |
| `links` | Every device on your tailnet: name, IP, full domain, OS, online, which one is the hub |
| `doctor` | Health check with a fix for each problem |
| `packages` | Install missing tools from `packages.json` (winget / brew / apt; NixOS gets a config snippet) |
| `skills` | Install the skills in `skills/skills.json` for Claude Code + Codex; park the rest. Also `skills list / add / remove / manual / auto` |
| `agents` | Install the shared instructions, a cheap Explore subagent and token-saving settings |
| `repo <path>` | Detect a project's stack, add its skills (`--apply`), check its AGENTS.md size and git remote |
| `hub` / `client` | Just the proxy part of `setup` |
| `update` | Update CLIProxyAPI (checksum-verified, keeps config and accounts), skills, Tailscale |
| `status` / `show-key` | Proxy and Tailscale at a glance / the hub's API key and dashboard password |

Run them as `py spinup.py <command>` on Windows, `python3 spinup.py <command>` elsewhere.

## How it works

```
 <machine-name> ──┐                         ┌── Claude / Codex accounts
 <machine-name> ──┼── Tailscale (private) ──┤   (CLIProxyAPI, http://<hub-name>:8317)
 phone          ──┘                         └── <hub-name>
```

- **Names, not IPs.** Tailscale gives each machine a fixed private IP and a name. spinup sets the name you choose. At home traffic goes straight over your router; away it goes direct over the internet, or through an encrypted relay if it must.
- **No machine list to maintain.** Tailscale *is* the list. Clients find the hub by asking each online device whether it runs the proxy.
- **One source of truth.** Skills, instructions and settings live in this repo. `setup` pulls the latest version first, so all machines stay the same.

## Make it yours

| Change | Edit, then run |
|---|---|
| Skills for every machine | `skills add <owner/repo> <skill>`, `skills remove <skill>` (or edit `skills/skills.json` → `skills`) |
| Which skills run by themselves | `skills list` shows auto/manual and the token cost; `skills manual <skill>` makes one run only when you call it (`/skill` in Claude Code, `$skill` in Codex) |
| Skills per project type (Go, Next.js, ...) | `skills/per-repo.json` → `repo <path> --apply` |
| Instructions and settings for Claude Code / Codex | `agents/` → `agents` |
| Dev tools | `packages.json` → `packages` |
| Proxy config | `proxy/config.template.yaml` → `hub` |

**Personal things** (your own preferences, private skills, machine notes) go in a private repo cloned into `local/`. This repo ignores that folder, and spinup picks it up automatically: see [docs/PRIVATE.md](docs/PRIVATE.md).

Why the defaults are what they are: [docs/WHY.md](docs/WHY.md). Using the proxy from T3 Code: [docs/T3-PROXY.md](docs/T3-PROXY.md).

**Next version:** a background service that moves your accounts to another machine when the main one goes off, without logging them out. See [docs/DESIGN.md](docs/DESIGN.md).

## Security

- Nothing is exposed to the internet. The proxy only accepts connections from Tailscale addresses (a firewall rule on Windows), and Tailscale traffic is end-to-end encrypted (WireGuard).
- Keys stay on the hub (`secrets.json`, never committed). Clients keep their copy only in their local `ccp` launcher.
- Tools come from your OS package manager or the vendor's official installer.
- Protect the two accounts everything depends on: turn on 2FA for **GitHub** and for the login you use for **Tailscale**.
- In the Tailscale admin console, **disable key expiry** for your machines, or they drop off the network when the login expires (`doctor` warns you).

## Important

Routing subscription accounts through a proxy may conflict with your provider's terms. Read them and decide for yourself; you are responsible for how you use your accounts. spinup's own rule: use Claude accounts only through Claude Code (`ccp`, or T3 Code's Claude provider), never from other apps.

## License

[MIT](LICENSE)
