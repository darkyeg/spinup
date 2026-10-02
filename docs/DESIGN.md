# spinup: design

The design of `spinup` (code: `cmd/spinup`, `internal/`; how to use it: [SERVICE.md](SERVICE.md), [SETUP.md](SETUP.md)): one Go program that runs in the background, keeps your machines in sync, and moves your AI accounts to another machine when the main one goes off, **without ever logging the accounts out**.

Terms: every machine **uses** the accounts. A **standby** can also **hold** them (run CLIProxyAPI with the logins); the **hub** is the standby you prefer. The machine holding them right now is the **leader**.

## The pieces

```
  ┌──────────────────────── your tailnet (Tailscale) ─────────────────────────┐
  │                                                                           │
  │  leader     spinup: localhost:8317 ──> CLIProxyAPI ──> Claude / Codex     │
  │                │                                                          │
  │                │ pushes every refreshed login within seconds              │
  │                v                                                          │
  │  standby    spinup: localhost:8317 ──> forwards to the leader             │
  │             (keeps copies of the logins, never uses them)                 │
  │                                                                           │
  │  any other  spinup: localhost:8317 ──> forwards to the leader             │
  │                                                                           │
  └───────────────────────────────────────────────────────────────────────────┘
```

- **`spinup`** is one binary and nothing else to install: the commands you type (`spinup setup`, `spinup status`, ...) and the background service (`spinup daemon`, started at boot).
- **Leader**: the one machine that runs CLIProxyAPI with your accounts.
- **Standby** (`spinup setup <name> --standby`): keeps an up-to-date copy of the logins and can become leader. The **hub** (`--hub`) is the standby that leads whenever it's up.
- **Any other machine** (`spinup setup <name>`): uses the accounts through the leader and never holds them.

## One address that never changes: `localhost:8317`

Every machine runs a small front on `http://localhost:8317`:

- on the **leader** it passes requests to the local CLIProxyAPI;
- on **every other machine** it forwards them over Tailscale to the current leader.

So `ccp`, T3 Code's proxy provider, and your scripts are configured **once**, with `localhost:8317`, and keep working when the leader changes. No names to rename, no settings to edit.

## The most important rule: one machine uses the accounts at a time

Claude and Codex logins use **refresh tokens**. Each time a login is refreshed, the provider issues a new refresh token and the old one stops working. If two machines used the same login at once, each refresh would invalidate the other's token, and the accounts would get logged out.

spinup is built around a **single writer**: at any moment, exactly one machine may refresh the logins.

1. **One leader, chosen safely.** Leadership is a lease with a number that goes up on every change. A standby takes over only when **Tailscale's control server** reports the leader as offline for a few minutes. "I can't reach it" is not enough: if your laptop simply has bad Wi-Fi, the leader still shows online, and nothing moves. A machine also publishes its claim (the new number) before it starts its proxy, so others see a proxy that is coming up. This prevents two leaders.
2. **Standbys never refresh.** They store copies and never use them while another machine leads.
3. **Sync within seconds.** The leader checks its logins every few seconds and pushes every refreshed login to the standbys as soon as one changes.
4. **The newest copy always wins.** Each login carries the time it was last refreshed. An older copy never overwrites a newer one, in either direction.
5. **Handoffs without risk.** When the leader shuts down normally, or you run `spinup handoff <machine>`, it stops its proxy first, sends its final logins, and only then does the next leader start. If the leader never learns whether the target took them, it stays stopped and presumes the target holds them: it follows the target, or leads again only when the target answers without having taken them, or replaces it by the normal rule once Tailscale reports it offline.
6. **Self-repair.** If a refresh is ever rejected, the leader first asks the other machines for a newer token for that account and uses it, instead of marking the account logged out.
7. **Coming back.** When the old leader turns on again, it does not start its proxy straight away. It pulls the newest logins from the current leader, then the two do a normal handoff (if you want it back as leader).

**The one remaining gap:** if the leader loses power within a few seconds of refreshing a login, the standbys may have the previous token for that one account, and you log that account in again. Normal shutdowns and handoffs have no gap.

## Security

- **Nothing on the internet.** All traffic stays inside your tailnet and is end-to-end encrypted (WireGuard).
- **Logins are fetched only with a key.** Standbys authenticate to the leader with the proxy's management key; machines that only use the accounts never receive logins at all.
- **Holders prove themselves first.** `/spinup/leader` answers a random challenge with proofs (HMAC-SHA256, bound to its machine name so a device can't pass on another's) of the secrets it knows. A machine sends the management key only to a device that proves it knows it, and a machine that only uses the accounts sends its traffic (which carries the API key) only to a leader that proves it knows the API key.
- **Least privilege.** spinup runs as your user, never as SYSTEM or root. On Windows, starting at boot (before anyone logs in) and the Tailscale-only firewall rule need one admin prompt at setup time, and never again.
- **At rest.** The login folder is readable by your user only. Use disk encryption (BitLocker, FileVault, LUKS) on every machine that can hold the accounts.
- **Never in git.** Logins and keys never touch any repository.
- **Verified updates.** Releases are built by GitHub Actions; spinup verifies each download against the release checksums before it replaces itself.

## Updates

- `spinup update` checks GitHub Releases, verifies the new version, swaps the binary and restarts the accounts service on it.
- CLIProxyAPI is updated the same way on a hub or standby (latest release, checksum-verified), and the proxy is restarted through the local service.
- Skills, agent instructions and settings come from your checkout (or the copy built into the binary); `spinup setup` pulls the checkout and your private layer (`local/`, a git repo you control).

## Commands

Everything is in the one binary. `spinup --help` lists the commands; `spinup <command> --help` explains each.

| Command | What it does |
|---|---|
| `spinup setup [<name>] [--hub \| --standby]` | Set up or repair the machine: Tailscale, the accounts service (started at boot), dev tools, skills, agent config; then `doctor`. Without a flag the machine only uses the accounts |
| `spinup doctor` | Check the machine; every problem comes with its fix |
| `spinup update` | Update spinup, CLIProxyAPI, skills and Tailscale |
| `spinup status` | Who is leader, this machine's sync age, each login, every machine |
| `spinup handoff <machine>` | Move the accounts to another holder, safely |
| `spinup takeover` | Hold the accounts here when the leader is lost for good |
| `spinup keys` | Print the API key and dashboard password |
| `spinup uninstall` | Stop the accounts service here; logins and keys stay |
| `spinup machines`, `tools` | Devices on the tailnet; dev tools this machine lacks |
| `spinup skills`, `agents`, `repo` | Skills, shared agent config, a project's stack skills |

The safety rules are tested in `internal/leadership` (`Decide` is a pure function with a table of cases) and `internal/service/machine_test.go`, where a simulated tailnet runs whole lifecycles (failover, partition, hand-back, planned shutdown, a slow target, imposters) while checking that no two proxies ever run at once.

Questions and ideas: open an issue.
