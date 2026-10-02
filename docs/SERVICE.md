# The spinup service

`spinup` (Go, in `cmd/` and `internal/`) is a small background service that runs on each of your machines. It keeps your AI accounts usable when the machine that holds them is off, and it **never lets two machines use the same login at once**, so the accounts don't get logged out. The design and its reasoning are in [DESIGN.md](DESIGN.md).

It replaces the proxy part of `spinup.py` (`hub` / `client`). Everything else (`setup`, `skills`, `agents`, `repo`, `doctor`, `links`) stays in `spinup.py` for now and keeps working next to the service.

## Using and holding the accounts

**Every machine uses the accounts.** On top of that, a machine may be able to **hold** them, which means running CLIProxyAPI with the logins:

| Install with | The machine | How many |
|---|---|---|
| (nothing) | only uses the accounts and never stores logins | any number |
| `--standby` | also keeps a synced copy of the logins, takes the accounts while the hub is off, and gives them back when it returns | any number (e.g. your laptop) |
| `--hub` | holds the accounts whenever it's up | one per tailnet |

The machine holding the accounts right now is the **leader**. Usually that's the hub.

## One address everywhere: `http://localhost:8317`

Every machine with the service answers on `http://localhost:8317`:

- on the leader, it passes requests to its own CLIProxyAPI (which now listens on `127.0.0.1:8327` only);
- everywhere else, it forwards them over Tailscale to the leader.

So `ccp`, T3 Code's proxy provider, and your scripts use `http://localhost:8317` and never need to change when the leader moves. The dashboard is at `http://localhost:8317/management.html`.

Hub and standby machines also answer on their Tailscale address (`http://<machine-name>:8317`), so devices without the service (a phone, an old `spinup.py client` setup) keep working. That port is open to your tailnet only. A machine with the service finds the accounts only through a spinup hub or standby; it never falls back to a plain `spinup.py hub` proxy.

## Install

You need Tailscale installed and logged in. Get the binary for your OS from the [releases](https://github.com/darkyeg/spinup/releases) (`checksums.txt` is next to them), or build it: `go build ./cmd/spinup`.

Install the hub first, then the others:

```sh
spinup install --hub        # on the machine that normally holds the accounts
spinup install --standby    # asks for the dashboard password (on the hub: spinup.py show-key)
spinup install              # only uses the accounts; asks for the API key (on the hub: spinup.py show-key)
```

`--password` and `--api-key` (or `SPINUP_PASSWORD` and `SPINUP_API_KEY`) pass those values without being asked. The API key may only have letters, digits and `. _ ~ + / = -`. `spinup install --help` lists everything.

If the hub already runs the `spinup.py hub` proxy, the service takes it over: same logins, same keys, same dashboard password. Nothing has to be logged in again.

What `install` does:

1. Keys: the hub reuses `secrets.json` or makes new keys; a standby fetches them from the leader, using the dashboard password; a machine that only uses the accounts keeps just the API key, in `ccp`.
2. Installs CLIProxyAPI on hub/standby if it's missing (latest release, checksum-verified).
3. Copies itself to the state folder (`%LOCALAPPDATA%\spinup` on Windows, `~/.local/share/spinup` elsewhere).
4. Starts the service at boot, and turns off the old always-on proxy from `spinup.py hub`, which would otherwise hold the port:
   - **Windows:** a scheduled task `spinup` that runs as your user without a logon (no stored password, no window). On hub/standby, a firewall rule opens port 8317 to `100.64.0.0/10` (Tailscale) for this program only. This is the one step that needs admin, so Windows asks once (UAC).
   - **Linux:** a systemd user unit `spinup.service`. On hub/standby, `loginctl enable-linger` keeps it running without a login.
   - **macOS:** a LaunchAgent `dev.spinup.daemon`. It runs while you're logged in.
5. Writes `ccp` to point at `http://localhost:8317`.

Re-running `install` stops the running service (it hands the accounts on first), then repairs the machine and starts the service from the binary you ran it from. It keeps the machine's hold unless you pass `--hub` or `--standby`. To make a hub or standby stop holding, run `spinup uninstall`, then `spinup install`.

## Commands

```sh
spinup status              # who holds the accounts, how fresh this machine's copy is, each login, every machine
spinup status --json
spinup handoff <machine>   # move the accounts to another hub/standby, safely
spinup takeover            # hold the accounts here (only when their holder is lost for good; refused on the machine that holds them; valid for a minute)
spinup uninstall           # remove spinup; stops the service, which hands the accounts to a synced machine first
spinup --version
```

`spinup --help` and `spinup <command> --help` explain each command.

## What happens when...

**...the leader refreshes a login.** Within a few seconds it pushes the new copy to every hub/standby. A copy only ever replaces an older one (each login carries its last-refresh time).

**...you shut the leader down normally.** It stops its proxy, sends its final logins to a synced hub/standby, and that machine takes over. This takes under a second. `spinup install` and `spinup uninstall` stop the service this way through a localhost-only call. On Windows, an OS shutdown or restart, and a power cut, stop the service without it: the others take over after `failover_after_seconds`.

**...a hand-off gets no answer.** The old leader stays stopped and presumes the target holds the accounts. If the target shows it never took them, the old leader leads again; if the target is gone, the usual failover applies.

**...the leader loses power or its network.** The others wait until **Tailscale's control server** has reported it offline for 3 minutes (`failover_after_seconds`). If they just can't reach it while Tailscale still sees it online, they wait instead, because it may still be using the accounts. Meanwhile, a leader that has lost Tailscale for half that time stops its own proxy. Then the best candidate (the hub first, then by name) takes over with the newest logins it can collect.

**...the hub comes back.** It doesn't start its proxy. It syncs from the current leader, and the leader hands the accounts back (`auto_failback`, on by default).

**...a computer sleeps.** On wake, a leader stops using the accounts until it has checked that nobody took over meanwhile.

**...a token is refused anyway.** Every 30 seconds the leader looks for refused logins and takes a newer copy from another machine if one has it. If none does, the log says which account to log in again.

**The one gap:** if the leader loses power within seconds of refreshing a login, before pushing it, the others have the previous token for that account, and you log that one account in again. Planned shutdowns and handoffs have no gap.

## Configuration

`config.json` in the state folder (written by `install`):

| Field | Default | Meaning |
|---|---|---|
| `hold` | `never` | `never`, `standby` or `hub` |
| `port` | `8317` | `localhost` on every machine and, on hub/standby, the Tailscale port |
| `proxy_port` | `8327` | CLIProxyAPI's own port, `127.0.0.1` only |
| `failover_after_seconds` | `180` | How long Tailscale must report the leader offline before another machine takes over |
| `auto_failback` | `true` | Hand the accounts back to the hub when it returns |
| `proxy_dir`, `auth_dir` | same as `spinup.py` | CLIProxyAPI and the logins |

Restart the service after editing (re-run `spinup install`).

## Security

- **Tailnet only.** Nothing listens on the internet. The front listens on `127.0.0.1`; the peer port on the Tailscale address, behind a firewall rule for `100.64.0.0/10` on Windows.
- **Logins move only with the key.** Machines authenticate to each other with the dashboard password (header `X-Spinup-Key`, constant-time compared). Machines that only use the accounts never receive logins or the password.
- **No secrets to imposters.** A device that says it holds the accounts must first answer a random challenge on `/spinup/leader` with a proof (HMAC-SHA256) of the password; a machine that only uses the accounts requires a proof of the API key before it forwards traffic. `spinup install --standby` checks the password proof before it asks for the keys.
- **Least privilege.** The service runs as your user, never as SYSTEM or root. Admin is needed once on Windows (boot task and firewall rule) and for `enable-linger` on some Linux systems.
- **Files.** Keys and logins are written readable by your user only, atomically. Logins that disappear from the leader are moved to `<auth_dir>-removed`, never deleted.
- Use disk encryption (BitLocker, FileVault, LUKS) on every hub and standby.

## Troubleshooting

- **Log:** `spinup.log` in the state folder (rotated at 5 MB). It records every decision ("decision: ...", "waiting: ...") with the reason.
- **`spinup status` says "nobody holds them"** and shows a `Waiting` line: that line is the reason. Most often, the leader is online in Tailscale but its service doesn't answer. Check it with `spinup status` on that machine.
- **The leader died for good** (stolen, disk gone): run `spinup takeover` on a standby. Don't do it while the old leader could come back with newer logins.
- **You uninstalled the leader with no synced standby:** the others see it online without spinup and wait. Run `spinup takeover` on a standby.
- **Go back to the Python setup:** run `spinup uninstall`, then `py spinup.py hub` (Windows) or `python3 spinup.py hub`. The logins and keys are unchanged.
- **The log says "a CLIProxyAPI that spinup didn't start answers on port 8327":** an old CLIProxyAPI is still running. Stop it (or re-run `spinup install`, which stops it), and the service starts its own.
- **`spinup.py hub` / `client` refuse to run:** that's on purpose while the service is installed, because the two would fight over the port.

## Not yet

Phase 2 ([DESIGN.md](DESIGN.md#plan)): self-update, CLIProxyAPI updates by the service, the rest of `spinup.py` in the same binary.
