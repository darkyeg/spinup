# T3 Code + CLIProxyAPI: send Claude and Codex through the proxy

Researched 2026-10. Proxy: CLIProxyAPI v8.0 at `http://127.0.0.1:8317` on the hub
(from other machines: `http://<hub-name>:8317`). T3 Code nightly 0.0.45.

Markers: **[verified]** means read in the docs or source linked. **[unverified]** means I
could not confirm it. Test it before you rely on it.

Rule: only Claude Code may use the Claude subscription accounts. T3's Claude provider *is*
Claude Code (it runs the `claude` binary through the Agent SDK), so a T3 Claude instance is
allowed. Do not point any other client (OpenCode, Cursor, scripts) at the proxy's Claude models.

---

## 0. What T3 already gives you

- T3 runs one or more **provider instances** for each driver (`claudeAgent`, `codex`, ...).
  Each instance has its own ID, display name, settings and **Environment variables**.
  [verified] `packages/contracts/src/providerInstance.ts`, `docs/user/install.md#providers`
- The instance's variables are layered **on top of** the T3 server's own environment.
  The instance value wins. Variables you do not set are inherited from the T3 process.
  [verified] `apps/server/src/provider/ProviderInstanceEnvironment.ts` (`mergeProviderInstanceEnvironment`)
- Variables marked **Sensitive** are moved to T3's secret store. Settings shows them as `••••••`.
  [verified] `apps/server/src/serverSettings.ts` (`redactProviderEnvironmentVariable`)
- T3 also has a **CLIProxyAPI hub** feature, but it only reads quotas. It does not route traffic.
  [verified] `docs/user/usage.md` "Connect a CLIProxyAPI hub"

- T3's `loadBalancingEnabled` (in `client-settings.json`) spreads work across *T3 environments*
  (computers), not across accounts. [verified] `apps/web/src/components/settings/LoadBalancingSettings.tsx`
- Do not hand-edit T3's `settings.json`. Use **Settings > Providers**. The UI moves secrets for you.

## 1. Add a "Claude (proxy)" instance in T3 (normal Claude stays)

1. On the PC, make an empty config folder for the proxy instance:
   `mkdir %USERPROFILE%\.claude_proxy`
   Why: a cached Anthropic login in the folder can conflict with the proxy token.
   [verified] `docs/user/providers-claude.md` "OpenRouter" and "Other routers"
2. Open T3 > **Settings > Providers**. Pick the PC environment (providers are per machine).
   [verified] `docs/user/project-settings.md`
3. Add another **Claude** instance. Name it `Claude (proxy)`.
4. Set **Binary path** to `claude`. Set **CLAUDE_CONFIG_DIR path** to `~/.claude_proxy`.
   [verified] `docs/user/providers-claude.md`, `packages/contracts/src/settings.ts` (`ClaudeSettings`)
5. In that instance's **Environment variables**, add:

   | Variable | Value |
   | --- | --- |
   | `ANTHROPIC_BASE_URL` | `http://127.0.0.1:8317` (no `/v1`) |
   | `ANTHROPIC_AUTH_TOKEN` | your proxy client key from `access.api-keys`. Tick **Sensitive**. |
   | `ANTHROPIC_API_KEY` | an explicitly empty value |

   [verified] variable set: `providers-claude.md` (OpenRouter table). Base URL without `/v1`:
   CLIProxyAPIDocs `docs/en/agent-client/claude-code.md`.
6. Do not put `VAR=value` in **Launch arguments**. That field is for CLI flags only.
   [verified] `providers-claude.md`
7. Leave the default `Claude` instance alone. It keeps using `~/.claude` and your own login.
8. Pick `Claude (proxy)` in the thread's model picker for new threads.
   An old thread can only switch between Claude instances that share the same config folder.
   So proxy threads and normal threads stay separate. [verified] `providers-claude.md`

Optional extra variables from the CLIProxyAPI Claude Code guide (not required):
`API_TIMEOUT_MS=600000`, `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`.
[verified] CLIProxyAPIDocs `docs/en/agent-client/claude-code.md`

### Model ids
- T3 sends the model you pick in T3 explicitly. `ANTHROPIC_MODEL` is not needed.
  `ANTHROPIC_DEFAULT_*_MODEL` only remaps aliases like `sonnet`; it does not replace T3's pick.
  [verified] `providers-claude.md`
- If the proxy does not know a built-in id, add the proxy's id with **Add custom model** on that
  instance. [verified] `providers-claude.md`
- Model ids: **[verified 2026-10-02]** with one Codex account, `/v1/models` lists the Codex models (`gpt-6.1-sol`, `gpt-6-luna`, `gpt-6-sol`, ...). Claude ids appear once a Claude account is added; check with `curl -H "Authorization: Bearer <key>" http://127.0.0.1:8317/v1/models`.
  The README lists Claude Fable 5.1, Opus 5.5 and Sonnet 5.5. The docs example uses
  `claude-opus-5`. Check the real list in the dashboard (**System > available models**) or with
  `curl -H "Authorization: Bearer <key>" http://127.0.0.1:8317/v1/models`.
  Map names with **Auth files > model aliases** in the dashboard if needed.
  [verified that the page exists] Management Center README.

## 2. Add a "Codex (proxy)" instance in T3

T3's **ChatGPT-connected (managed)** Codex cannot be redirected. T3 deletes `OPENAI_BASE_URL`
and custom launch args for it. Use an **existing Codex CLI** instance instead.
[verified] `apps/server/src/provider/CodexManagedRuntime.ts`

1. Make a separate Codex home: `mkdir %USERPROFILE%\.codex_proxy`.
   Your normal `~/.codex` stays untouched. [verified] `providers-codex.md` "Multiple CLI logins"
2. Create `%USERPROFILE%\.codex_proxy\config.toml`. This is the CLIProxyAPI "API mode" recipe:

   ```toml
   model_provider = "cliproxyapi"
   model = "gpt-5.6-sol"   # use an id your proxy lists
   model_reasoning_effort = "high"

   [model_providers.cliproxyapi]
   name = "cliproxyapi"
   base_url = "http://127.0.0.1:8317/v1"
   wire_api = "responses"
   http_headers = { "X-OpenAI-Actor-Authorization" = "local-proxy" }
   requires_openai_auth = true
   ```
   And `%USERPROFILE%\.codex_proxy\auth.json`: `{ "OPENAI_API_KEY": "<proxy client key>" }`
   [verified] CLIProxyAPIDocs `docs/en/agent-client/codex.md` "Configure as API Mode"
3. Alternative "OAuth login mode" (the docs call it recommended): same provider block plus
   `model_catalog_url = "http://127.0.0.1:8317/v1/models"`,
   `experimental_bearer_token = "<proxy client key>"`, `name = "OpenAI"`, and
   `[features] api_key_model_discovery = true` (needed for Codex v0.156.0+). You then run
   `codex login` with ChatGPT inside that home: `set CODEX_HOME=%USERPROFILE%\.codex_proxy && codex login`.
   [verified] same file, "OAuth Login Mode"
4. In T3 **Settings > Providers**, add a **Codex** instance `Codex (proxy)`.
   Set **CODEX_HOME path** to `~/.codex_proxy`. Leave **Shadow home path** empty.
   A fully separate home cannot continue threads from `~/.codex`. That is fine here.
   [verified] `providers-codex.md`
5. Pass a secret by variable instead of a file (optional): replace `experimental_bearer_token`
   with `env_key = "CLIPROXY_KEY"` and add `CLIPROXY_KEY` (Sensitive) to the instance variables.
   **[unverified]**: `env_key` is a standard Codex provider option, but CLIProxyAPI's docs do not use it.
6. T3 marks Codex "not authenticated" when `requires_openai_auth` is true and there is no account.
   [verified] `apps/server/src/provider/Layers/CodexProvider.ts`. So in API mode `auth.json` must
   exist; in OAuth mode the `codex login` must be done. **[unverified]** which one T3 shows as ready.

## 3. Add accounts to CLIProxyAPI

Dashboard (Management Center) at `http://127.0.0.1:8317/management.html`.
Log in with the management key. [verified] CLIProxyAPIDocs `docs/en/management/webui.md`

1. Open the **OAuth** page (README calls the area "Auth files & OAuth").
2. Click **Claude** login. Sign in to the first Max account in the browser.
3. Repeat for each Claude account. Before each one, sign out of claude.ai in the browser
   (or use a private window), or you will link the same account twice. **[unverified]** tip.
4. Same for **Codex**. The device-flow option suits headless logins.
5. Check **Auth files**: one JSON per account. Multiple accounts are round-robined.
   [verified] README "Multiple accounts with round-robin load balancing"

CLI alternative (run on the PC, where the callback ports are local):
```
cli-proxy-api.exe -claude-login -config "%LOCALAPPDATA%\CLIProxyAPI\config.yaml"      # callback port 54545
cli-proxy-api.exe -codex-login -config "%LOCALAPPDATA%\CLIProxyAPI\config.yaml"       # callback port 1455
cli-proxy-api.exe -codex-device-login -config "%LOCALAPPDATA%\CLIProxyAPI\config.yaml"
```
Add `-no-browser` to print the URL instead. [verified] `cmd/server/main.go` flags;
CLIProxyAPIDocs `configuration/provider/claude-code.md`, `codex.md`.
The service uses `%LOCALAPPDATA%\CLIProxyAPI\config.yaml` (written by spinup from `proxy/config.template.yaml`; the proxy is run by the spinup accounts service, boot task `spinup`, on the machine holding the accounts) **[verified]**.

Auth files land in `auth-dir` from config.yaml, by default `~/.cli-proxy-api`.
[verified] CLIProxyAPIDocs `configuration/auth-dir.md`

## 4. Pitfalls

- **Env precedence.** Instance variable > T3 process env > nothing. If you ever set
  `ANTHROPIC_BASE_URL` as a Windows user variable, the **default** Claude instance inherits it
  too and stops using your own login. Keep proxy variables per instance only.
  [verified] `ProviderInstanceEnvironment.ts`. T3 must restart to see new Windows variables (normal process behaviour).
- **`ccp` launcher** sets the same two variables per shell, so it does not leak. Good.
- **Cached login.** If `~/.claude_proxy` ever gets a real Anthropic login, run `/logout` there
  first. [verified] `providers-claude.md`
- **Empty `ANTHROPIC_API_KEY`.** Set it empty so an inherited key cannot override the token.
- **Tailscale URL.** From another machine use `http://<hub-name>:8317`. But T3 runs providers on
  the environment's machine, so a PC environment should use `127.0.0.1`.
  [verified] `install.md#providers` ("belong to that environment's machine")
- **Limits view.** T3 cannot show limits for a Claude instance that uses `ANTHROPIC_AUTH_TOKEN`.
  Add the proxy under **Settings > Providers > Usage providers > Add hub** (URL + management key)
  to see pooled quotas. [verified] `docs/user/usage.md`
- **Welcome wizard terminal** uses the selected instance's home and environment.
  [verified] `docs/user/welcome-wizard.md`

## 5. Check that traffic really goes through the proxy

1. Start a thread on `Claude (proxy)` and send "hi".
2. Dashboard **Logs** page: you should see a `/v1/messages` request. If the page is missing,
   enable **Logging to file** in Basic Settings. [verified] Management Center README.
3. Dashboard **Quotas**: the used account's 5h window should move.
4. In T3 **Usage > Limits** (after adding the hub) the pooled accounts appear.
5. Built-in usage statistics were removed in v6.10.0. For per-request history you need an
   add-on such as CPA Usage Keeper. [verified] CLIProxyAPI README.
6. Negative test: stop nothing, but set a wrong token on the instance. The thread should fail
   with an auth error. That proves it is not using your own login. **[unverified]** exact message.

## Sources
- https://github.com/pingdotgg/t3code docs/user: providers-claude.md, providers-codex.md,
  install.md, welcome-wizard.md, project-settings.md, usage.md
- t3code source: packages/contracts/src/{providerInstance,settings}.ts,
  apps/server/src/provider/{ProviderInstanceEnvironment,CodexManagedRuntime}.ts,
  apps/server/src/provider/Drivers/ClaudeHome.ts, apps/server/src/provider/Layers/CodexProvider.ts,
  apps/server/src/serverSettings.ts
- https://github.com/router-for-me/CLIProxyAPI README.md, cmd/server/main.go
- https://github.com/router-for-me/CLIProxyAPIDocs docs/en: agent-client/claude-code.md,
  agent-client/codex.md, configuration/provider/{claude-code,codex}.md, configuration/auth-dir.md,
  management/webui.md (published at https://help.router-for.me/)
- https://github.com/router-for-me/Cli-Proxy-API-Management-Center README.md
