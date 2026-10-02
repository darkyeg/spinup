# Set up a machine

Steps for an agent setting up (or repairing) one machine. Each step ends on a check; move on only when the check passes. Run commands from the repo root. `$PY` is `py` on Windows, `python3` elsewhere.

## 1. Identify the machine

Pick its name (lowercase letters, digits, dashes): it becomes its Tailscale name, the shortcut every other machine uses to reach it at home or away. Role (**hub** holds the accounts and runs the proxy; there is one hub; everything else is a **client**), OS.
Done when: you know the name and role. If the user didn't say and `spinup.py links` shows a hub already, it's a client.

**Fast path:** after cloning (step 2's bootstrap), `$PY spinup.py setup <name>` pulls this repo, runs steps 2-4 in one go and ends with `doctor`. Then do steps 5-7.

## 2. Base tools

Bootstrap by hand: git and Python 3.11+ (python.org build on Windows), then clone this repo. `$PY spinup.py packages` installs the rest of `packages.json` (Node, gh, rg, fd, jq, bun, go, uv, Rust, Claude Code, Codex). Open a new terminal afterwards, run `gh auth login`, and log in to `claude` and `codex` once.
Done when: `$PY spinup.py packages --check` reports all tools installed and `gh auth status` succeeds.

**NixOS:** system packages and services go in `/etc/nixos/configuration.nix` (including `services.tailscale.enable = true;`), applied with `sudo nixos-rebuild switch`. Hand the exact lines to the user; a script can't install them.

## 3. Network + proxy

- Hub: `$PY spinup.py setup <name> --hub` (sets the Tailscale name, installs and starts the proxy)
- Client: `$PY spinup.py setup <name>` (finds the hub on the tailnet, asks for the API key; the user gets it on the hub with `py spinup.py show-key`)

Both log in to Tailscale on first run: the user opens the printed URL and signs in with the **same Tailscale account** as the other machines.
Done when: `$PY spinup.py status` shows Tailscale `Running`; on a hub, `Proxy: running`; on a client, the command printed "Client ready".

## 4. Skills + agent config

`$PY spinup.py skills` then `$PY spinup.py agents`.
Done when: both finish without errors and `~/.agents/skills` holds exactly the skills in `skills/skills.json` plus the user's own skills (`skills/local/`, `local/skills/`).

## 5. Repos

Clone the user's active repos into the machine's usual folder (`~/personal`, `~/work`, or `C:\Users\<user>\Personal` on Windows) with `gh repo clone`.
Then run `$PY spinup.py repo <clone> --apply` for each: it adds the stack's skills and rewrites SSH-alias remotes like `gh:owner/repo` (T3 Code groups the same repo across machines by its github.com URL).
Done when: `$PY spinup.py repo <clone>` prints "Nothing to do" for each clone (AGENTS.md size warnings may remain).

## 6. T3 Code

Install T3 Code. On the hub: Settings → Connections → enable **Tailscale HTTPS** and create a pairing link. On a client: Add environment → paste that link. Then follow [T3-PROXY.md](T3-PROXY.md) to add the proxy-backed provider instances.
Done when: the client's T3 lists the hub as Connected over a `*.ts.net` URL (not a `192.168.*` address).

## 7. Record it

Run `$PY spinup.py doctor` and fix every `[XX]` it prints. If the user keeps a private repo in `local/` ([PRIVATE.md](PRIVATE.md)), note this machine in `local/MACHINES.md` and push it.
Done when: doctor ends with "All good." and the push succeeded.
