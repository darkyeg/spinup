# Set up a machine

Steps for an agent setting up (or repairing) one machine. Each step ends on a check; move on only when the check passes.

## 1. Name and hold

**Ask the user for the machine's name** (lowercase letters, digits, dashes); never copy a name from an example or from another machine. The name becomes its Tailscale name: every other machine reaches it as `<name>`, e.g. `http://<name>:8317`, at home or away.

Ask what it **holds**:

| Hold | Meaning | Flag |
|---|---|---|
| `never` | only uses the accounts | none |
| `standby` | takes the accounts while the hub is off | `--standby` |
| `hub` | holds the accounts whenever it is up; one per tailnet | `--hub` |

Done when: you know the name and the hold. If the user didn't say and `spinup status` (or `spinup machines`) shows a hub already, it is not the hub.

## 2. Install spinup

The installers require a published binary release. For an unreleased build, follow the [README's source-build instructions](../README.md#quick-start) from the checkout of the intended branch or commit. `go install github.com/darkyeg/spinup/cmd/spinup@latest` resolves Go's latest module version; it does not install unmerged branch changes.

- Linux/macOS: `curl -fsSL https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.sh | sh`
- Windows: `irm https://raw.githubusercontent.com/darkyeg/spinup/main/scripts/install.ps1 | iex`
- Or, with Go: `go install github.com/darkyeg/spinup/cmd/spinup@latest`

The script installs the release binary into `~/.local/bin` (checksum-verified). Open a new terminal if `spinup` is not found.

The user's own skills and instructions live in their library, `~/.spinup` ([LIBRARY.md](LIBRARY.md)); nothing needs cloning.
Done when: `spinup --version` prints a version.

## 3. Set up the machine

`spinup setup <name> [--hub|--standby]`

It runs: Tailscale, the accounts service, dev tools (`tools.json`), skills, agent config. It prints the machine's addresses and ends with `spinup doctor`. Tailscale and the accounts service must succeed or setup stops; the other steps are reported at the end, so fix them and re-run.

- Tailscale asks the user to open a printed URL and sign in with the **same Tailscale account** as the other machines.
- Hub: makes the keys. Standby: asks for the dashboard password (the user gets it on the hub with `spinup keys`; or `SPINUP_PASSWORD`). Never-hold: asks for the API key once (`spinup keys` on the hub; or `SPINUP_API_KEY`).

**NixOS:** system packages and services go in `/etc/nixos/configuration.nix` (including `services.tailscale.enable = true;`), applied with `sudo nixos-rebuild switch`. Hand the exact lines to the user; `spinup tools` prints the package line.

Done when: setup finishes and `spinup status` shows who holds the accounts; on a hub, that is this machine.

## 4. Log in

On the hub or current holder, open `http://localhost:8317/management.html` and add the needed shared accounts through **OAuth Login**. Signing in to the ordinary Claude Code or Codex CLI does not populate CLIProxyAPI's account store.

Open a new terminal and run `gh auth login` for GitHub access. If the user also wants native Claude Code or Codex providers, sign in to those separately in their normal homes; keep proxy instances separate as described in [AGENT-CONFIG.md](AGENT-CONFIG.md).
Done when: `gh auth status` succeeds and a small request through each configured proxy-backed provider succeeds. `ccp --print "Reply with exactly SPINUP_OK"` checks the shared Claude route when a Claude account was added.

## 5. Repos

Clone the user's active repos into the machine's usual folder (`~/personal`, `~/work`, or `C:\Users\<user>\Personal` on Windows) with `gh repo clone`.
Then run `spinup repo <clone> --apply` for each: it adds the stack's skills and rewrites SSH-alias remotes like `gh:owner/repo` (T3 Code groups the same repo across machines by its github.com URL).
Done when: `spinup repo <clone>` prints "Nothing to do" for each clone (AGENTS.md size warnings may remain).

## 6. HTTPS certificates

Turn them on **once per tailnet**, by its owner: https://login.tailscale.com/admin/dns → **HTTPS Certificates** → Enable. It is a tailnet-wide setting, so a second machine never needs it again; a second *tailnet* (a friend's) does.

Certificates are issued for the full MagicDNS name only (`<name>.<tailnet>.ts.net`), never for the short name. Everything spinup does between machines stays plain HTTP on port 8317 (`http://<name>:8317`) and is unaffected. What needs the certificate is `tailscale serve`, and so T3 Code's pairing link: without it, pairing hangs.

Done when: `spinup doctor` no longer warns about HTTPS certificates, and `tailscale status --json` lists the machine under `CertDomains`.

## 7. T3 Code

Install T3 Code. On the hub: Settings → Connections → enable **Tailscale HTTPS** and create a pairing link. On another machine: Add environment → paste that link. Then follow [T3-PROXY.md](T3-PROXY.md) to add the proxy-backed provider instances.
Done when: the client's T3 lists the hub as Connected over a `*.ts.net` URL (not a `192.168.*` address).

## 8. Record it

Run `spinup doctor` and fix every `[XX]` it prints (each comes with its fix).
Done when: doctor ends with "All good."

For a hub and standby, also follow [the cross-machine validation guide](VALIDATION.md). A local health check alone does not exercise handoff or failure takeover.
