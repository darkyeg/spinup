# Validate a hub and standby

Use this procedure to validate a candidate build on the actual machines. It changes account ownership and includes a temporary network interruption, so choose a time when those changes are acceptable. Machine names below mean the existing names chosen by the operator, not example names to copy.

Record the candidate commit, operating systems and pass/fail results. Keep machine names, keys, account identifiers and login files out of public reports.

## Prepare the candidate

Build the same branch or commit on both computers using the [source-build instructions](../README.md#quick-start). Compare `git rev-parse HEAD` in their checkouts; a development version label alone does not identify the source. Until that build has a binary release, `spinup update` cannot install it.

Run `spinup setup <existing-name>` on each to install or repair that candidate. For a first installation, use `--hub` on the chosen hub and `--standby` on the standby, following [SETUP.md](SETUP.md). Existing roles are kept when those flags are omitted. Wait for both machines to finish setup before changing shared files.

Add the accounts to CLIProxyAPI on the holder through its management dashboard if none exist yet. The `ccp` examples below require a shared Claude account; for Codex-only validation, make the equivalent small request from your configured proxy-backed Codex instance. A successful native login alone does not test the account service.

## Check steady state and routing

On both computers:

```sh
spinup doctor
spinup status
ccp --print "Reply with exactly SPINUP_OK"
```

The hub should hold the accounts, the standby should report a synced copy, and both requests should succeed through each computer's local router. The test uses the shared-account launcher, not an ordinary native Claude login.

Inspect `spinup status --json` locally when checking `leading` and `proxy_running`. At each settled stage, only the current holder should report a running proxy. Resolve any double-holder or unsynced-standby result before proceeding.

If testing a phone or another device without spinup, use an online hub or standby's Tailscale address and the existing API key. A phone's localhost is not the computer. Changing holders does not move the router's network address.

## Check planned handoff

From the current holder:

```sh
spinup handoff <standby-name>
```

Check status and repeat the small `ccp` request on both computers. The standby should take ownership with synced logins and both local routers should follow it. With automatic failback enabled, it can hand back to an online hub after 30 idle seconds; that is expected.

Repeat with a response streaming during handoff. A response that finishes within the drain limit should complete. Requests still running at the limit can be cut; handoff normally allows two minutes, while service shutdown allows 30 seconds.

Restore hub ownership with `spinup handoff <hub-name>` if it has not already returned there.

## Check failure takeover and return

With no active requests, temporarily disconnect the hub's network without first stopping spinup. A normal service stop exercises graceful handoff, not failure takeover. Stopping only spinup while Tailscale still reports the hub online does not trigger automatic takeover.

Wait for the standby to take over. Tailscale must report the hub offline, and the configured failover interval (180 seconds by default) is measured from its last sighting of the hub; status updates may lag. Keep the production threshold; do not shorten it to speed up this test. Confirm the standby becomes holder and a local `ccp` request succeeds there.

Restore the hub's network. It should obtain the newest login copies before taking ownership back; automatic failback waits for the standby to be idle. Repeat the request from both computers and check that only one proxy is running in the settled state.

## Check library sharing and cleanup

Save the source library's existing `AGENTS.md` contents outside the library. If it does not exist, record that fact and create the library folder and file for this test. Append this harmless marker to it, then run `spinup agents` on the source:

```markdown
<!-- spinup validation -->
```

With a hub or standby online, confirm the marker reaches the other machine's library and installed normal-home instructions after the sync loop runs. The usual comparison interval is 15 seconds; allow another interval for propagation and installation. A disconnected machine should catch up after reconnection.

Restore the original contents on the source, or remove only the file created for this test if none existed. Run `spinup agents` and confirm the marker disappears from both libraries and installed instructions.

## Check configuration ownership

Follow [AGENT-CONFIG.md](AGENT-CONFIG.md) for the actual normal and proxy homes. Verify unrelated hooks, provider blocks, profiles and credentials survive re-running `spinup agents`; managed defaults are expected to replace their matching values.

Normal and proxy instances should retain their separate authentication. Apply settings to isolated homes using process-scoped home variables; library installs do not discover those homes automatically. Check that proxy URLs and tokens are scoped to the proxy instance rather than system-wide environment settings.

A build is validated for the tested machines when routing, handoff, failure takeover, return, library cleanup and configuration ownership all pass. Automated simulations and cross-builds remain separate evidence; they do not replace these native checks.
