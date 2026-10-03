# Configure agents without mixing logins or settings

Use one source for each kind of setting. `spinup agents` installs instructions for Claude Code and Codex, Claude subagents, and a small set of defaults in each agent's own settings file. It does not configure T3 Code provider instances or change agent authentication files.

| Kind | Edit here | How it reaches other computers |
|---|---|---|
| Shared general instructions | spinup's `agents/AGENTS.md` | Use the same spinup defaults and run `spinup agents` |
| Your own instructions and skills | `~/.spinup/AGENTS.md`, `skills.json`, `skills/` | The library service syncs them; run `spinup agents` after editing your instructions locally |
| Shared Claude defaults | `agents/claude/settings.json` in your spinup checkout | Apply that checkout on each computer with `spinup agents` |
| Shared Codex defaults | `agents/codex/config.toml` in your spinup checkout | Apply that checkout on each computer with `spinup agents` |
| Project rules | The project's `AGENTS.md` and agent settings | Commit them in that project |
| Provider endpoints, credentials and machine paths | Local agent config or that T3 Code provider instance | Configure them locally; keep secrets out of git and the library |

The library shares instructions and skills, not the whole `~/.claude` or `~/.codex` folder and not the settings templates in `agents/`. Computers with different spinup versions or different checkouts can apply different defaults. Use the same version and source before expecting identical managed values.

## What a merge changes

Claude settings objects merge recursively. Keys in spinup's template replace matching values, and arrays replace matching arrays; keys absent from the template remain. Codex updates only the keys listed in its template, keeping other tables and unrelated comments. Each existing file gets a `.spinup-backup` once before spinup first changes it.

The defaults set Claude effort, output style, prompt suggestions and its subagent model, and Codex effort, reasoning summary and its subagent defaults. They do not choose your main Codex model, provider or base URL. Your hooks, provider blocks and credentials are not part of the shared defaults.

Use Claude Code v2.1.237 or later for the built-in `Concise` style selected by the defaults. See [Claude output styles](https://code.claude.com/docs/en/output-styles).

A direct edit to a managed key will be replaced on the next install, including an install triggered by an incoming library change. Change the template for a common default, or use an agent's project or session override for a different task. Installed instruction files and Claude subagents with names managed by spinup are replaced as whole files; put personal instructions in your library and give personal subagents distinct names.

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
