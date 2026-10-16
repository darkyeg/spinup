# Plugin packages

Use `spinup packages` for a complete plugin such as Superpowers. A package keeps its skills, hooks, scripts, references and assets together. Spinup resolves a GitHub revision once, verifies the downloaded content, and installs it through Claude Code and Codex's native plugin commands.

```sh
spinup packages add obra/superpowers
spinup packages
spinup packages plan
spinup packages update obra/superpowers
spinup packages rollback obra/superpowers
spinup packages remove obra/superpowers
```

Restart the affected tools after installation, update, rollback or removal. `ready` means the native tool reports the requested plugin installed and enabled, and Spinup's materialized source matches the locked content. It does not prove every workflow or hook will execute successfully in every project.

## Select a source and tools

```sh
spinup packages add owner/repo --ref v1.0.0
spinup packages add https://github.com/owner/repo --agent codex
spinup packages add owner/collection --path plugins/example --agent claude-code
```

Sources are public GitHub repositories. `--ref` accepts a branch, tag or commit; the default is `HEAD`. The resolved commit and content digest are saved together. `update` follows that same ref unless you supply a new `--ref`. A pinned tag or commit consequently stays at that revision until you change the ref.

Spinup recognizes `.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`, and Codex's portable root `plugin.json`. The plugin needs a stable name and a version. When both tool manifests are present, their names and versions must agree. By default all supported manifests become targets; `--agent` selects a subset. One plugin directory per source is supported. The whole selected directory must be self-contained: references to files outside `--path` cannot be installed.

Claude Code and Codex must support native plugin CLI operations. The implementation was verified with Claude Code 2.1.296 and Codex CLI 0.162.1 on Windows. Unsupported or missing tools are reported without substituting plain skill copies. An add saves the requested package even if one tool cannot install it yet; correct that tool and run `spinup packages sync`.

Packages can have their own runtime requirements. Superpowers uses Git and Bash (Git for Windows supplies Bash); its optional visual companion needs Node and a browser. Spinup does not install arbitrary dependencies, launch plugin helpers or configure credentials. Native plugin content can include hooks and MCP declarations; the target tool controls their execution and connections.

## Existing skills and plugins

There is no forced migration of `skills.json`, own skills or manual/auto choices. Continue using `spinup skills` for standalone skills. Native package registrations live outside the standalone skill store, so `skills sync` does not park the package's files.

If you previously copied individual Superpowers skills, review overlaps and remove the copies you no longer want with `spinup skills remove <name>`. Removed standalone skills remain in `spinup skills trash`. Spinup does not rewrite package skill names or silently remove overlapping personal workflows.

An existing native installation of the same plugin name is a conflict. Remove it through its native manager if you want Spinup to manage that plugin, then run `spinup packages sync`. Spinup does not adopt or uninstall external registrations automatically. Two requested sources with the same plugin name for the same tool are also a conflict.

## What is shared

The library's `packages.json` contains the package request and its resolved revision in one atomic document, including the previous revision for rollback. This avoids mismatched declaration and lock files. Native homes, installation receipts, generated catalogs, model settings and credentials remain local.

Verified ZIP artifacts are cached by digest under the library's `artifacts/`. They preserve hidden plugin manifests and executable metadata; extraction rejects links, paths escaping the package, portable filename collisions and oversized payloads. Limits are 32 MiB per downloaded artifact and 64 MiB expanded. Artifacts travel separately from library snapshots: peers exchange only missing, referenced digests. Corrupt artifacts are treated as missing. The service never resolves newer GitHub releases or invokes an interactive installer.

Update Spinup on all participating machines before adding packages. A package-enabled library refuses legacy whole-library replacements, and new clients do not send package-bearing snapshots to legacy peers. Keep `packages.json` even when its list becomes empty; `packages remove` does this for you. Older clients can continue syncing skill-only libraries.

Targets use `CLAUDE_CONFIG_DIR` and `CODEX_HOME` for the current process, or the standard homes. Each tool home has independent installation receipts and catalogs. To configure an isolated provider home, run the command with those environment variables scoped to that invocation. The service reconciles its configured homes, not every provider configured in T3 Code.

## Bandwidth and retries

Healthy peers exchange small state summaries; unchanged package archives are not sent again. There is no background GitHub polling. Downloads from GitHub happen only during explicit `add`, `update` or `fetch` operations. An explicit update checks the ref first and reuses a verified current or previous archive when its commit matches; a pinned commit does not need a ref lookup. An unchanged update does not rewrite the shared manifest. Native installation uses local catalogs; the native tools and plugin runtime may have their own network activity outside Spinup's transfer policy.

For each peer, direction and digest, a missing-content episode has at most three automatic attempts: immediately, after one minute, then after five more minutes. A successful transfer is not repeated while the destination still reports that same content missing. Unrelated library edits and service restarts do not reset the budget. Seeing verified content at the destination allows a later genuine loss to be repaired again. Failed attempts log once per attempt, not every comparison tick.

The artifact endpoints independently allow at most three transfers per caller and digest in a 24-hour window, with the same spacing. Uploads use HTTP `100-continue`, so an already-present or throttled archive can be refused before sending its body. Transfer budgets are written atomically to `artifact-transfers.json` next to the service state, before transferring bytes. Unreadable or unwritable transfer memory pauses automatic archive transfers with one diagnostic instead of allowing a retry storm.

If automatic attempts stop, fix the underlying disk or native-tool problem and run `spinup packages fetch` on the receiving machine. This is an explicit network repair of missing archives at their locked revisions; `packages sync` uses cached content. Repeated external requests can still produce small error responses, and normal machine discovery/state checks still use the network.

## Repair and recovery

```sh
spinup packages plan --json
spinup packages sync
spinup packages fetch
spinup doctor
```

- `plan` reports actions without installing packages. `sync` applies the saved revisions, without selecting newer releases. `setup`, `update` and incoming library updates also reconcile these saved revisions; only `packages update` advances a package revision.
- `fetch` repairs missing or damaged archives from their locked GitHub commits, then syncs. If GitHub returns different bytes, Spinup refuses the replacement rather than silently changing the lock. A healthy peer can supply the exact cached bytes.
- Updates retain the previous revision. If activation fails, Spinup attempts to restore the previous working revision and reports the failure. The desired update remains pending, so a later sync can retry. `rollback` changes the shared desired revision back explicitly.
- An interrupted install has a persisted ownership receipt and can be resumed. Removal deletes only owned native registrations. Cached archives and materialized revisions remain for recovery; this version does not automatically garbage-collect them.
- Local changes to the materialized package are reported as conflicts instead of being overwritten. Restore the original content before syncing, or keep customizations as a separate package. Direct edits to the native tool's separate cache are outside Spinup's content verification.
- A plugin disabled in its native tool stays disabled; pending updates wait until you enable it there. Spinup also refuses to overwrite an owned plugin whose reported native version changed independently.

The two tools are reconciled independently. A failure in one does not change the other's catalog. Replacement is not a cross-tool transaction: Claude needs uninstall-with-data-preservation followed by install to reliably refresh same-version revisions; Codex supports refreshing through `plugin add`.

## Implementation and verification

`internal/packages` owns resolution, package identity, validation, plans, native adapters and installation receipts. `internal/library` owns portable library data and immutable artifacts. `internal/api` defines the transfer protocol, and `internal/service` exchanges requested artifacts and schedules reconciliation. CLI files print outcomes and route user commands. Package source content never determines an arbitrary installer shell command.

Normal tests use inert archives and fake native adapters. Opt-in tests exercise the actual installed CLIs in temporary homes, with no model calls:

```sh
SPINUP_TEST_NATIVE_PLUGINS=1 go test ./internal/packages -run '^TestNativePluginLifecycle$' -v -count=1
SPINUP_TEST_SUPERPOWERS=1 go test ./internal/packages -run '^TestSuperpowersNativePackage$' -v -count=1
```

On PowerShell, set the corresponding environment variable for the test process and remove it afterward. The Superpowers smoke test downloads a fixed upstream commit and tests installation/removal, not workflow execution. Native command and packaging evidence: [contracts](research/native-plugin-contracts.md), [upstream research](research/superpowers-upstream.md).
