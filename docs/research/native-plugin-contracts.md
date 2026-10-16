# Native plugin CLI contracts

Checked 2026-10-16 with installed **Claude Code 2.1.296** and **codex-cli 0.162.1**. Evidence combines each installed CLI's `--help`, official documentation, and an isolated Windows smoke test using an inert local plugin. These are version-specific observations, not a promise about older CLIs.

## Isolation and evidence

The smoke fixture had one skill, no hook scripts, no MCP servers and no external dependencies. It declared separate Claude and Codex manifests. Each CLI process inherited a temporary `CLAUDE_CONFIG_DIR` or `CODEX_HOME`; the working directory was also temporary, outside any project. The original environment values were restored afterward. No real home configuration, LLM calls, builds or archives were involved. Both plugins and marketplaces were removed from the temporary homes, and final installed lists were empty.

The observed sequence for both tools was: register local marketplace; install version 1.0.0; list installed/enabled state; change fixture manifests to 1.0.1; update through the commands below; verify version 1.0.1; uninstall; remove marketplace; verify no installed plugins. All operations returned successful results. Codex wrote a warning on stderr that helper PATH aliases cannot be created under a temporary home, but plugin operations succeeded and stdout remained JSON.

## Commands verified against installed CLI help

Use argument arrays, with absolute marketplace paths. `probe` and `spinup-contract-probe` below are fixture names. In production, qualify every selector with its owned marketplace to avoid ambiguous names.

| Action | Claude Code | Codex |
|---|---|---|
| Register local marketplace | `claude plugin marketplace add <root> --scope user --json` | `codex plugin marketplace add <root> --json` |
| Install | `claude plugin install probe@spinup-contract-probe --scope user --json` | `codex plugin add probe@spinup-contract-probe --json` |
| Inspect installed | `claude plugin list --json` | `codex plugin list --json` |
| Inspect marketplaces | `claude plugin marketplace list --json` | `codex plugin marketplace list --json` |
| Apply newer local plugin version | `claude plugin update probe@spinup-contract-probe --scope user --json` | `codex plugin add probe@spinup-contract-probe --json` again |
| Remove plugin | `claude plugin uninstall probe@spinup-contract-probe --scope user --json` | `codex plugin remove probe@spinup-contract-probe --json` |
| Remove marketplace | `claude plugin marketplace remove spinup-contract-probe --scope user --json` | `codex plugin marketplace remove spinup-contract-probe --json` |

The installed Codex CLI's command is **`plugin add`**, not `plugin install`. It has no separate plugin-update subcommand. `codex plugin marketplace upgrade [name] --json` refreshes configured **Git marketplace snapshots**; it is not the command needed for the tested local-source update. Claude's corresponding catalog-refresh command is `claude plugin marketplace update [name] --json`; supply a name to obtain its machine-readable result. Neither catalog-refresh command was needed in the local version-change smoke test.

CLI help also offers Codex Git marketplace `--ref <REF>` and `owner/repo[@ref]` sources. Those remote forms were not tested. Claude supports user/project/local scopes; the adapter can explicitly choose user scope. Codex has no analogous plugin-scope flag in this version's help.

## Local marketplace schemas that installed successfully

Both catalogs can coexist in one marketplace root. Their relative source paths resolve from that root, not from the hidden manifest directory. The inert plugin directory was `plugins/probe/`.

Claude: `<root>/.claude-plugin/marketplace.json`:

```json
{
  "name": "spinup-contract-probe",
  "owner": {"name": "Fixture"},
  "plugins": [{"name": "probe", "source": "./plugins/probe"}]
}
```

Codex: `<root>/.agents/plugins/marketplace.json`:

```json
{
  "name": "spinup-contract-probe",
  "interface": {"displayName": "Contract probe"},
  "plugins": [{
    "name": "probe",
    "source": {"source": "local", "path": "./plugins/probe"},
    "policy": {"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
    "category": "Productivity"
  }]
}
```

The plugin had `plugins/probe/.claude-plugin/plugin.json` containing `{"name":"probe","version":"1.0.0","description":"Inert fixture"}` and `plugins/probe/.codex-plugin/plugin.json` containing `{"name":"probe","version":"1.0.0","description":"Inert fixture","skills":"./skills/","hooks":{}}`. A valid `skills/probe/SKILL.md` supplied frontmatter `name` and `description`. No `agents/openai.yaml` was necessary for this locally authored Codex plugin. Requirements of the official Superpowers publication pipeline must not be confused with minimum local plugin requirements.

Official schema references: [Claude marketplace creation](https://code.claude.com/docs/en/plugins/create-marketplace), [Claude marketplace reference](https://code.claude.com/docs/en/plugins/marketplace-reference), [Claude loading reference](https://code.claude.com/docs/en/plugins/loading-reference), [Codex build plugins](https://developers.openai.com/plugins/build/plugins), [Codex developer commands](https://learn.chatgpt.com/docs/developer-commands). These are live documentation URLs; local CLI help and smoke evidence establish the installed-version behavior above.

## Machine-readable results

Claude `plugin list --json` returned an array, whose entry included:

```json
{
  "id": "probe@spinup-contract-probe",
  "version": "1.0.0",
  "scope": "user",
  "enabled": true,
  "installPath": "<temporary-claude-home>/plugins/cache/spinup-contract-probe/probe/1.0.0",
  "readFromFolder": "<marketplace-root>/plugins/probe",
  "folderVersion": "1.0.0"
}
```

Claude mutation output was a JSON object with fields such as `command`, `outcome: "ok"`, `pluginId`, `scope` and `message`; update also returned `updateOutcome: "updated"`, `oldVersion` and `newVersion`. Marketplace add/remove help specifies the machine-readable result as the **last stdout line**, so avoid assuming every Claude operation emits only one JSON document with no other output.

Codex `plugin list --json` returned an object with `installed` and `available` arrays. Each installed entry included `pluginId`, `name`, `marketplaceName`, `version`, `installed: true`, `enabled: true`, local `source`, `marketplaceSource`, `installPolicy` and `authPolicy`. Install returned `pluginId`, `version`, `installedPath` and `authPolicy`. Marketplace add returned `marketplaceName`, `installedRoot` and `alreadyAdded`; remove returned its marketplace name and `installedRoot: null`. Treat timestamps, absolute paths and extra fields as opaque; use identity, version, enabled state and ownership for reconciliation.

## Pinning and adapter limits

An immutable materialized directory can provide the desired source revision without relying on remote marketplace pin syntax: fetch/verify a commit, retain its whole package layout, generate catalogs that reference that immutable directory, and pass the generated local marketplace to the native CLI. This is a packaging deduction; the smoke verified local directories, not remote artifact fetching or cryptographic verification.

Keep materialized directories available for the lifetime of an installation. Claude explicitly reported `readFromFolder` pointing at the original local plugin source; a native install must not be assumed to detach runtime loading from that source. Both tools reported versioned cache paths. Track the desired commit separately from semantic version.

### Same-version revision replacement: separately tested

A second isolated fixture test used immutable directories `revisions/digest1` and `revisions/digest2`, both with plugin version `1.0.0`, but different marker text in the skill. The marketplace name and plugin identity stayed unchanged. After installing digest1, both catalogs' plugin source paths were changed to digest2. Results:

| Operation | Result |
|---|---|
| Claude `plugin update <id> --scope user --json` | Returned `updateOutcome: "up_to_date"`; the cache still contained digest1. The subsequent list's `readFromFolder` already pointed at digest2. No live session was run, so do not infer consistent hook/skill activation from that mixed state. |
| Codex `plugin add <id> --json` again | Refreshed the same-version cache to digest2, verified by reading its skill marker; installed-list `source.path` also pointed at digest2. |
| Claude `plugin uninstall <id> --scope user --keep-data --json`, then `plugin install <id> --scope user --json` | Refreshed the same-version cache to digest2, verified by reading its skill marker. Installed list reported version 1.0.0, enabled true and `readFromFolder` digest2. |

Therefore the verified adapter path for an owned Claude installation whose commit changes without a manifest-version change is uninstall-with-data-preservation then install, rather than relying on `update`. This briefly removes the installation and requires sensible error recovery; disabled-state preservation was not tested. Codex `add` was sufficient for this case. Upstream plugin manifests need not be rewritten. All temporary native registrations were removed again after this follow-up, and both final installed lists were empty.

A package-specific marketplace identity can separate ownership. Switching the whole marketplace root under an existing marketplace name, duplicate registration, disable-state preservation, rollback and missing-CLI behavior were not exercised in this bounded pass; switching the plugin's local source path inside the same marketplace root was verified above. A production adapter should detect CLI capability before mutation and should not overwrite an unrelated existing marketplace identity. Scope cleanup to its own plugin and marketplace.

The fixture lifecycle proves native local registration, installation, update and removal for these CLI versions. It does not prove skill auto-triggering, session-start/compaction hooks, the full Superpowers runtime or compatibility with offline model use. No broad `--yes` acceptance or execution of marketplace-declared commands was needed or tested.
