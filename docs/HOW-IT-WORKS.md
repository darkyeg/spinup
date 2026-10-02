# How your accounts move between machines

The short version, for everyone. The full safety design is in [DESIGN.md](DESIGN.md).

## The one rule

Every Claude or Codex login has a **refresh token that works once**. If two machines refresh the same login, the provider logs the account out everywhere. So **exactly one machine uses the accounts at a time**: the **holder**. Everything below exists to keep that true while still never making you wait long.

## Every machine looks the same to your apps

```
your apps ──> localhost:8317 ──> the holder's proxy
```

Claude Code, Codex and T3 Code always talk to `localhost:8317` on their own machine. spinup on that machine knows who holds the accounts right now and sends each request there: to its own proxy if it is the holder, otherwise over Tailscale to the holder. Your apps never change a setting.

## The hub, the standby, and the rest

- The **hub** holds the accounts whenever it is on.
- A **standby** keeps a fresh copy of the logins and takes over while the hub is off.
- Every other machine only uses the accounts. It never holds them and never uses its own copy.

## What happens when...

**...everything is on.** The hub holds the accounts. Every time a login is refreshed, the standby gets the new copy within seconds.

**...you move the accounts on purpose** (`spinup handoff`, an update, an uninstall). The holder lets the running requests finish, stops, sends its final logins, and the other machine starts. New requests wait a second for the new holder. Nothing fails.

**...the hub dies suddenly** (power cut, crash). Two things happen, in this order:

1. **The hub stops itself first.** If it is still running but has lost Tailscale, it stops its proxy 10 seconds after it notices. Tailscale can take up to 2 minutes to notice, so the hub is always stopped by about 130 seconds.
2. **Only then may the standby start.** It takes over after Tailscale has reported the hub offline for 3 minutes, with the newest logins it can find.

Because the hub always stops before the standby can start, two machines never use the accounts at once. During those minutes your apps' requests **wait** instead of failing, then go to the standby. Only a request that was running on the hub at the instant it died is lost; send it again.

**...the hub comes back.** It does not take the accounts straight away. It first copies the newest logins from the standby. The standby hands the accounts back only when it is idle (nothing running for 30 seconds), so no answer is ever cut.

## Why not switch instantly?

When the hub stops answering, nobody can tell at once whether it is dead or just unreachable for a moment. Switching on a guess would let two machines use the same login, and that logs you out. Tailscale's own report, plus a wait long enough to be sure, is what makes the switch safe. With three or more machines that can hold the accounts, a faster vote-based switch would be possible; spinup doesn't do that yet.
