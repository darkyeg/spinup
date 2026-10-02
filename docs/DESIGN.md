# spinup 2: design

Status: **Phase 1 built** (the service: `cmd/spinup`, `internal/`; how to use it: [SERVICE.md](SERVICE.md)). Phase 2 (self-update, the rest of the Python CLI) is planned. This document describes the design: one Go program that runs in the background, keeps your machines in sync, and moves your AI accounts to another machine when the main one goes off, **without ever logging the accounts out**.

In the commands, a follower that may take over is called a **standby**, and the machine you prefer as leader is the **hub**.

## What changes for you

| Today (Python) | spinup 2 (Go) |
|---|---|
| Run commands by hand; `git pull` to update | Runs in the background; updates itself from GitHub Releases |
| One hub; if it's off, nobody has the accounts | A **leader** and **followers**: if the leader goes off, a follower takes over the accounts |
| Clients point at the hub's name (`http://<hub-name>:8317`) | Every machine uses **`http://localhost:8317`**, whoever is leader |
| Needs Python on every machine | One file per OS, nothing else to install |

## The pieces

```
  ┌──────────────────────── your tailnet (Tailscale) ─────────────────────────┐
  │                                                                           │
  │  leader     spinupd: localhost:8317 ──> CLIProxyAPI ──> Claude / Codex    │
  │                │                                                          │
  │                │ pushes every refreshed login within seconds              │
  │                v                                                          │
  │  follower   spinupd: localhost:8317 ──> forwards to the leader            │
  │             (keeps copies of the logins, never uses them)                 │
  │                                                                           │
  │  client     spinupd: localhost:8317 ──> forwards to the leader            │
  │                                                                           │
  └───────────────────────────────────────────────────────────────────────────┘
```

- **`spinup`** is one binary: the CLI you type (`spinup setup`, `spinup status`, ...) and the background service (`spinupd`).
- **Leader**: the one machine that runs CLIProxyAPI with your accounts.
- **Follower**: a machine that keeps an up-to-date copy of the account logins and can become leader. You choose which machines may be followers.
- **Client**: uses the accounts through the leader, never holds them.

## One address that never changes: `localhost:8317`

Every machine runs a small front on `http://localhost:8317`:

- on the **leader** it passes requests to the local CLIProxyAPI;
- on **every other machine** it forwards them over Tailscale to the current leader.

So `ccp`, T3 Code's proxy provider, and your scripts are configured **once**, with `localhost:8317`, and keep working when the leader changes. No names to rename, no settings to edit.

## The most important rule: one machine uses the accounts at a time

Claude and Codex logins use **refresh tokens**. Each time a login is refreshed, the provider issues a new refresh token and the old one stops working. If two machines used the same login at once, each refresh would invalidate the other's token, and the accounts would get logged out.

spinup 2 is built around a **single writer**: at any moment, exactly one machine may refresh the logins.

1. **One leader, chosen safely.** Leadership is a lease with a number that goes up on every change. A follower takes over only when **Tailscale's control server** reports the leader as offline for a few minutes. "I can't reach it" is not enough: if your laptop simply has bad Wi-Fi, the leader still shows online, and nothing moves. This prevents two leaders.
2. **Followers never refresh.** They store copies and never use them while they are followers.
3. **Sync within seconds.** The leader watches its login folder and pushes every refreshed login to the followers as soon as it changes.
4. **The newest copy always wins.** Each login carries the time it was last refreshed. An older copy never overwrites a newer one, in either direction.
5. **Handoffs without risk.** When the leader shuts down normally, or you run `spinup handoff <machine>`, it stops its proxy first, sends its final logins, and only then does the next leader start.
6. **Self-repair.** If a refresh is ever rejected, spinupd first asks the other machines for a newer token for that account and uses it, instead of marking the account logged out.
7. **Coming back.** When the old leader turns on again, it does not start its proxy straight away. It pulls the newest logins from the current leader, then the two do a normal handoff (if you want it back as leader).

**The one remaining gap:** if the leader loses power within a few seconds of refreshing a login, the followers may have the previous token for that one account, and you log that account in again. Normal shutdowns and handoffs have no gap.

## Security

- **Nothing on the internet.** All traffic stays inside your tailnet and is end-to-end encrypted (WireGuard).
- **Logins are fetched only with a key.** Followers authenticate to the leader with the proxy's management key; clients never receive logins at all.
- **Least privilege.** spinupd runs as your user, never as SYSTEM or root. On Windows, starting at boot (before anyone logs in) and the Tailscale-only firewall rule need one admin prompt at install time, and never again.
- **At rest.** The login folder is readable by your user only. Use disk encryption (BitLocker, FileVault, LUKS) on every machine that can be leader or follower.
- **Never in git.** Logins and keys never touch any repository.
- **Signed updates (Phase 2).** Releases are built by GitHub Actions; spinupd verifies each download's checksum and signature before it replaces itself.

## Updates (Phase 2)

- spinupd checks GitHub Releases (daily, and on start), verifies the new version, swaps the binary and restarts itself. You can pin a version or turn this off.
- Skills, agent instructions and settings ship inside each release, so a machine is up to date without `git pull`.
- CLIProxyAPI is updated the same way: the latest release, checksum-verified, applied by the leader during a quiet moment, without losing accounts.
- Your private layer (`local/`) stays a git repo you control; spinupd pulls it.

## Commands

| Command | What it does |
|---|---|
| `spinup install --role hub\|standby\|client` | Start the service at boot with this role (built) |
| `spinup status` | Who is leader, this machine's sync age, each login, every machine (built) |
| `spinup handoff <machine>` | Move the accounts to another machine, safely (built) |
| `spinup lead --force` | Take the accounts when the leader is lost for good (built) |
| `spinup setup`, `doctor`, `links`, `skills`, `agents`, `repo` | Still in `spinup.py`; move here in Phase 2 |

## Plan

1. **Phase 1 (built):** the service with the `localhost:8317` front, leader election (Tailscale as referee), sync within seconds with newest-wins, safe handoff, self-fencing and sleep detection, self-repair, `install`/`status`/`handoff`/`lead --force`, CI and tagged releases with checksums.
2. **Phase 2:** move the remaining Python commands into the same binary; self-update with signed releases; CLIProxyAPI updates by the leader. The Python version keeps working until then.

The safety rules are tested in `internal/cluster`: `Decide` is a pure function with a table of cases, and a simulated tailnet runs whole lifecycles (failover, partition, hand-back, planned shutdown) while checking that no two proxies ever run at once.

Questions and ideas: open an issue.
