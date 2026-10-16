# Configure agents without mixing logins or settings

Use one source for each kind of setting. `spinup agents` combines shared instructions with your own for Claude Code and Codex. It leaves model choices, effort, limits and other settings unchanged by default, and bundles no subagents. It does not configure T3 Code provider instances or change agent authentication files. Forks can still supply shared settings and subagent definitions.

## Add your own instructions

Create `~/.spinup/AGENTS.md` with your preferences, then run `spinup agents`. On Windows, `~` means your user profile folder. The file is optional and user-written; spinup does not download someone else's personal policy. If you use `SPINUP_LIBRARY`, create it in that library instead.

For example, the file can contain `Keep explanations concise and report the checks you ran.` Spinup appends it after its shared guidelines in both `~/.claude/CLAUDE.md` and `~/.codex/AGENTS.md`. The library service also syncs it between your connected machines. A project's own `AGENTS.md` remains part of that repository and should contain project rules, not a contributor's subscription or model preferences.

| Kind | Edit here | How it reaches other computers |
|---|---|---|
| Shared general instructions | spinup's `agents/AGENTS.md` | Use the same spinup defaults and run `spinup agents` |
| Your own instructions and skills | `~/.spinup/AGENTS.md`, `skills.json`, `skills/` | The library service syncs them; run `spinup agents` after editing your instructions locally |
| Personal models, effort and limits | Claude's `settings.json` and Codex's `config.toml` in their user homes | Configure locally; these settings are not library content |
| Optional fork defaults | `agents/claude/settings.json`, `agents/codex/config.toml`, `agents/claude/agents/` | Apply that fork on each computer with `spinup agents` |
| Project rules | The project's `AGENTS.md` and agent settings | Commit them in that project |
| Provider endpoints, credentials and machine paths | Local agent config or that T3 Code provider instance | Configure them locally; keep secrets out of git and the library |

The library shares instructions and skills, not the whole `~/.claude` or `~/.codex` folder and not the settings templates in `agents/`. MCP server settings, device URLs and plugin installation state stay local; see the [sync boundary](LIBRARY.md#what-stays-on-each-machine). Computers with different spinup versions or different checkouts can apply different defaults. Use the same version and source before expecting identical managed values.

Complete plugins are managed separately with `spinup packages`. Portable package requests and locked content are shared, while each native manager maintains its own local registration. Package management preserves personal model settings and uses independent receipts for each resolved tool home. See [PACKAGES.md](PACKAGES.md).

## What a merge changes

Claude settings objects merge recursively. Keys in spinup's template replace matching values, and arrays replace matching arrays; keys absent from the template remain. Codex updates only the keys listed in its template, keeping other tables and unrelated comments. Each existing file gets a `.spinup-backup` once before spinup first changes it.

The bundled templates contain no setting overrides. Existing installations retain values and subagents installed by earlier releases; upgrading does not reset your settings or delete your files. Review unwanted old choices in your local settings and remove an old `~/.claude/agents/Explore.md` only if you no longer want that override. Keep any personal edits before removing it.

If a fork supplies settings, a direct edit to a key listed there will be replaced on the next install, including one triggered by an incoming library change. Installed instruction files and any fork-provided Claude subagents with matching names are replaced as whole files; put personal instructions in your library and avoid name collisions with a fork's subagents.

Claude supports project settings in `.claude/settings.json` and personal project overrides in `.claude/settings.local.json`. Keep a manually created personal override out of git. See [Claude settings scopes](https://code.claude.com/docs/en/settings).

Codex supports behavior overrides in a trusted project's `.codex/config.toml` and one-off CLI overrides. Keep provider, authentication and machine-specific routing in user config or a separate Codex home: Codex does not accept those routing keys from project config. See [OpenAI configuration precedence](https://learn.chatgpt.com/docs/config-file/config-basic) and [configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference).

## Separate normal and proxy providers

In T3 Code, give normal and proxy providers separate homes when their authentication must stay separate: for example, `~/.claude` and `~/.claude_proxy`, or `~/.codex` and `~/.codex_proxy`. Set the proxy URL and token only on the proxy provider instance through **Settings > Providers**, with the token marked Sensitive. Keep `ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN` and proxy OpenAI variables out of system-wide environment settings. The [T3 proxy guide](T3-PROXY.md) covers the provider wiring.

`spinup agents` targets `CLAUDE_CONFIG_DIR` and `CODEX_HOME` when they are set for that invocation; otherwise it targets the normal homes. It does not discover every T3 Code provider home. To apply the same defaults to a separate home, set those variables only for the installer process. Do not copy a normal home's authentication or complete config into a proxy home.

On Unix, for example:

```sh
CLAUDE_CONFIG_DIR="$HOME/.claude_proxy" CODEX_HOME="$HOME/.codex_proxy" spinup agents
```

These variables apply to that process only. The accounts daemon normally uses the standard homes; its automatic library installs do not update every isolated provider home. Re-run the scoped install when you want an isolated home to pick up updated defaults or instructions.

Apply changes with `spinup agents`, check `spinup doctor`, then restart the affected Claude Code, Codex and T3 Code instances. Atomic file writes prevent partial settings files, but the installer does not lock against another program editing the same file concurrently; finish those edits before installing.
