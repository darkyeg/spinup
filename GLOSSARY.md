# Glossary

The words spinup uses, in the code, the commands and the docs. Use these and nothing else.

## Machines

- **Machine**: a computer you code on that runs spinup. Its **name** is its Tailscale name, which other machines type to reach it.
- **Tailnet**: your private Tailscale network. It is the only list of machines; the repo keeps none.
- **Device**: anything on the tailnet, with or without spinup (a phone, a server).
- **Library**: the folder with what is yours, `~/.spinup`: your skills list, own skills and instructions. The spinup service keeps it the same on all your machines; the newest change wins.
- **Checkout**: a clone of the spinup repo, for working on spinup itself; spinup then uses its data in place of the built-in copy.

## Accounts

- **Accounts**: the AI subscriptions (Claude, Codex) spinup shares between your machines.
- **Logins**: the files that keep an account signed in. Each holds a refresh token that works only once.
- **Proxy**: CLIProxyAPI, the program that uses the logins. At most one runs at a time on the whole tailnet.
- **Hold**: what a machine may do with the accounts: `never` (it only uses them), `standby`, or `hub`.
- **Hub**: the machine that holds the accounts whenever it is up. One per tailnet.
- **Standby**: a machine that holds the accounts while the hub is off, and gives them back when it returns.
- **Holder**: a hub or a standby.
- **Leader**: the holder holding the accounts right now. Every machine reaches the accounts through it, at `http://localhost:8317`.
- **Member**: a holder this machine has seen; it shares logins with members only.
- **Epoch**: a number that grows each time the leader changes; the higher epoch wins.
- **Hand-off**: the leader passes the accounts to another holder on purpose.
- **Failover**: a standby takes the accounts because Tailscale reports the leader offline long enough.
- **Takeover**: the user tells a holder to take the accounts because the leader is lost for good.
- **`ccp`**: Claude Code started through the accounts; plain `claude` keeps its own login.

## Agents

- **Skill**: a folder with a `SKILL.md` that Claude Code and Codex load.
- **Auto / manual**: an auto skill runs when the agent decides; a manual one only when you call it (`/name`, `$name`).
- **Own skill**: a skill you keep as a folder in your library (`~/.spinup/skills/`).
- **Parked skill**: an installed skill not on the list, moved aside so no agent loads it.
- **Agent config**: spinup's instructions, subagents and settings (`agents/`), plus your library's `AGENTS.md`.
