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

Open a new terminal, run `gh auth login`, and log in to `claude` and `codex` once.
Done when: `gh auth status` succeeds.

## 5. Repos

Clone the user's active repos into the machine's usual folder (`~/personal`, `~/work`, or `C:\Users\<user>\Personal` on Windows) with `gh repo clone`.
Then run `spinup repo <clone> --apply` for each: it adds the stack's skills and rewrites SSH-alias remotes like `gh:owner/repo` (T3 Code groups the same repo across machines by its github.com URL).
Done when: `spinup repo <clone>` prints "Nothing to do" for each clone (AGENTS.md size warnings may remain).

## 6. T3 Code

Install T3 Code. On the hub: Settings → Connections → enable **Tailscale HTTPS** and create a pairing link. On another machine: Add environment → paste that link. Then follow [T3-PROXY.md](T3-PROXY.md) to add the proxy-backed provider instances.
Done when: the client's T3 lists the hub as Connected over a `*.ts.net` URL (not a `192.168.*` address).

## 7. Record it

Run `spinup doctor` and fix every `[XX]` it prints (each comes with its fix).
Done when: doctor ends with "All good."
