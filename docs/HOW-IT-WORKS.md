# How your accounts move between machines

The short version, for everyone. The full safety design is in [DESIGN.md](DESIGN.md).

## The one rule

**Only one machine runs CLIProxyAPI with the shared logins at a time:** the **holder**. Other machines route their requests to it. Standbys keep copies of the logins, but their proxies stay stopped until takeover. This avoids competing refreshes of the same login.

## Every machine looks the same to your apps

```
your apps ──> localhost:8317 ──> the holder's proxy
```

On computers running spinup, `ccp`, proxy-backed providers and scripts configured for spinup use `localhost:8317`. spinup knows who holds the accounts right now and sends each request there: to its own proxy if it is the holder, otherwise over Tailscale to the holder. Those apps keep the same endpoint when the holder changes; ordinary Claude Code or Codex logins stay separate.

## The hub, the standby, and the rest

- The **hub** holds the accounts whenever it is on.
- A **standby** keeps a fresh copy of the logins and takes over while the hub is off.
- Every other machine only uses the accounts. It never holds them and never uses its own copy.

## What happens when...

**...everything is on.** The hub holds the accounts. Every time a login is refreshed, the standby gets the new copy within seconds.

**...you move the accounts on purpose** (`spinup handoff`, an update, an uninstall). The holder waits for running requests up to the drain limit, stops, sends its final logins, and the other machine starts. Requests still running at the limit are cut. New requests wait for the next holder up to their routing timeout; a stalled handoff can exceed that timeout. See [the service limits](SERVICE.md).

**...the hub dies suddenly** (power cut, crash). Two things happen, in this order:

1. **The hub stops itself first.** If it is still running but has lost Tailscale, it stops its proxy 10 seconds after it notices. Tailscale can take up to 2 minutes to notice, so the hub is always stopped by about 130 seconds.
2. **Only then may the standby start.** Tailscale must report the hub offline, and at least 3 minutes must have elapsed since its last sighting. The standby takes over with the newest logins it can find.

The stop-before-takeover rule prevents two spinup proxies from using the logins at once. New requests can wait for the standby within the routing timeout; client timeouts can expire sooner. A response already streaming through the failed hub may be interrupted. If a refreshed login had not reached the standby before the failure, that account can need a new login.

**...the hub comes back.** It does not take the accounts straight away. It first copies the newest logins from the standby. The standby hands the accounts back only when it is idle (nothing running for 30 seconds), so no answer is ever cut.

## Why not switch instantly?

When the hub stops answering, nobody can tell at once whether it is dead or just unreachable for a moment. Switching on a guess would let two machines use the same login, and that logs you out. Tailscale's own report, plus a wait long enough to be sure, is what makes the switch safe. With three or more machines that can hold the accounts, a faster vote-based switch would be possible; spinup doesn't do that yet.
