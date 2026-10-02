"""CLIProxyAPI on the hub, `ccp` launcher, and the hub/client/update/status/show-key commands."""
from __future__ import annotations

from .common import *
from .tailscale import dns_name, find_hub, setup_tailscale, tailnet_nodes, tailscale_bin, tailscale_status


# ---------------------------------------------------------------- CLIProxyAPI binary

def installed_version() -> str | None:
    if not EXE.exists():
        return None
    out = run([str(EXE), "--help"], check=False, capture=True)
    for word in (out.stdout + out.stderr).split():
        if word[:1].isdigit() and word.count(".") == 2:
            return word.rstrip(",")
    return None


def latest_release() -> tuple[str, dict[str, str]]:
    rel = json.loads(http_get(f"https://api.github.com/repos/{PROXY_REPO}/releases/latest", token=True))
    return rel["tag_name"].lstrip("v"), {a["name"]: a["browser_download_url"] for a in rel["assets"]}


def asset_name(version: str) -> str:
    arch = platform.machine().lower()
    arch = {"x86_64": "amd64", "amd64": "amd64", "arm64": "aarch64", "aarch64": "aarch64"}.get(arch)
    if not arch:
        die(f"unsupported CPU: {platform.machine()}")
    osname = {"Windows": "windows", "Darwin": "darwin", "Linux": "linux"}[SYSTEM]
    return f"CLIProxyAPI_{version}_{osname}_{arch}.{'zip' if WINDOWS else 'tar.gz'}"


def install_proxy_binary(version: str, assets: dict[str, str]) -> None:
    name = asset_name(version)
    if name not in assets:
        die(f"release {version} has no {name}")
    log(f"Downloading CLIProxyAPI {version}")
    data = http_get(assets[name])
    sums = http_get(assets["checksums.txt"]).decode()
    want = next((line.split()[0] for line in sums.splitlines() if line.endswith(name)), None)
    if want != hashlib.sha256(data).hexdigest():
        die(f"checksum mismatch for {name}")

    files: dict[str, bytes] = {}
    keep = {EXE.name, "config.example.yaml", "README.md", "LICENSE"}
    if name.endswith(".zip"):
        with zipfile.ZipFile(io.BytesIO(data)) as z:
            files = {Path(n).name: z.read(n) for n in z.namelist() if Path(n).name in keep}
    else:
        with tarfile.open(fileobj=io.BytesIO(data)) as t:
            files = {Path(m.name).name: t.extractfile(m).read()
                     for m in t.getmembers() if m.isfile() and Path(m.name).name in keep}
    if EXE.name not in files:
        die(f"{EXE.name} missing from {name}")

    PROXY_DIR.mkdir(parents=True, exist_ok=True)
    for fname, content in files.items():
        if fname != EXE.name:
            (PROXY_DIR / fname).write_bytes(content)
    # Swap the binary. A running .exe can't be overwritten on Windows but can be renamed.
    new = EXE.with_name(EXE.name + ".new")
    new.write_bytes(files[EXE.name])
    new.chmod(0o755)
    if EXE.exists():
        for stale in PROXY_DIR.glob(EXE.name + ".old*"):
            stale.unlink(missing_ok=True) if not WINDOWS else _try_unlink(stale)
        os.replace(EXE, EXE.with_name(f"{EXE.name}.old{int(time.time())}"))
    os.replace(new, EXE)


def _try_unlink(path: Path) -> None:
    try:
        path.unlink()
    except OSError:
        pass  # still running; removed on the next update


# ---------------------------------------------------------------- config + secrets

def load_secrets() -> dict[str, str]:
    if SECRETS.exists():
        return json.loads(SECRETS.read_text(encoding="utf-8"))
    data = {}
    legacy = PROXY_DIR / "SECRETS.txt"  # from the first manual install
    if legacy.exists():
        for line in legacy.read_text(encoding="utf-8").splitlines():
            if line.startswith("API key"):
                data["api_key"] = line.split(":")[-1].strip()
            elif line.startswith("Management password"):
                data["management_password"] = line.split(":")[-1].strip()
    data.setdefault("api_key", "sk-" + secrets.token_hex(24))
    data.setdefault("management_password", secrets.token_urlsafe(18))
    PROXY_DIR.mkdir(parents=True, exist_ok=True)
    SECRETS.write_text(json.dumps(data, indent=2), encoding="utf-8")
    if not WINDOWS:
        SECRETS.chmod(0o600)
    legacy.unlink(missing_ok=True)
    return data


def write_config(sec: dict[str, str]) -> None:
    text = (TEMPLATE.read_text(encoding="utf-8")
            .replace("{{PORT}}", str(PORT))
            .replace("{{HOST}}", "")
            .replace("{{AUTH_DIR}}", "~/.cli-proxy-api")
            .replace("{{API_KEY}}", sec["api_key"])
            .replace("{{SECRET_KEY}}", sec["management_password"]))  # proxy hashes it on start
    CONFIG.write_text(text, encoding="utf-8")
    if not WINDOWS:
        CONFIG.chmod(0o600)


# ---------------------------------------------------------------- run at boot

def register_autostart() -> None:
    log("Registering CLIProxyAPI to start at boot")
    if WINDOWS:
        user = f"{os.environ['USERDOMAIN']}\\{os.environ['USERNAME']}"
        windows_admin(f"""
$exe = '{EXE}'
Get-NetFirewallRule -DisplayName '{FIREWALL_RULE}' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
New-NetFirewallRule -DisplayName '{FIREWALL_RULE}' -Direction Inbound -Action Allow -Protocol TCP `
  -LocalPort {PORT} -RemoteAddress {TAILNET_RANGE} -Program $exe -Profile Any | Out-Null
$a = New-ScheduledTaskAction -Execute $exe -Argument '-config "{CONFIG}"' -WorkingDirectory '{PROXY_DIR}'
$t = New-ScheduledTaskTrigger -AtStartup
$p = New-ScheduledTaskPrincipal -UserId '{user}' -LogonType S4U -RunLevel Limited
$s = New-ScheduledTaskSettingsSet -ExecutionTimeLimit 0 -RestartCount 999 `
  -RestartInterval (New-TimeSpan -Minutes 1) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
Register-ScheduledTask -TaskName '{SERVICE}' -Action $a -Trigger $t -Principal $p -Settings $s -Force | Out-Null
""", "Firewall rule + boot task")
    elif SYSTEM == "Darwin":
        plist = f"""<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>{LAUNCHD_LABEL}</string>
  <key>UserName</key><string>{getpass.getuser()}</string>
  <key>EnvironmentVariables</key><dict><key>HOME</key><string>{HOME}</string></dict>
  <key>ProgramArguments</key><array><string>{EXE}</string><string>-config</string><string>{CONFIG}</string></array>
  <key>WorkingDirectory</key><string>{PROXY_DIR}</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
"""
        target = f"/Library/LaunchDaemons/{LAUNCHD_LABEL}.plist"
        subprocess.run(sudo(["tee", target]), input=plist, text=True, stdout=subprocess.DEVNULL, check=True)
        run(sudo(["launchctl", "bootout", f"system/{LAUNCHD_LABEL}"]), check=False, capture=True)
        run(sudo(["launchctl", "bootstrap", "system", target]))
    elif NIXOS:
        die("On NixOS, run the proxy as a service in configuration.nix (systemd.services.cliproxyapi "
            f"with ExecStart = \"{EXE} -config {CONFIG}\" and User = \"{getpass.getuser()}\"), "
            "then re-run.")
    else:
        unit = f"""[Unit]
Description=CLIProxyAPI
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
User={getpass.getuser()}
WorkingDirectory={PROXY_DIR}
ExecStart={EXE} -config {CONFIG}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
"""
        subprocess.run(sudo(["tee", f"/etc/systemd/system/{SYSTEMD_UNIT}.service"]), input=unit,
                       text=True, stdout=subprocess.DEVNULL, check=True)
        run(sudo(["systemctl", "daemon-reload"]))
        run(sudo(["systemctl", "enable", SYSTEMD_UNIT]))


def restart_proxy() -> None:
    if WINDOWS:
        powershell(f"Stop-ScheduledTask -TaskName '{SERVICE}'; "
                   f"Get-Process '{EXE.stem}' -ErrorAction SilentlyContinue | Stop-Process -Force; "
                   f"Start-Sleep 1; Start-ScheduledTask -TaskName '{SERVICE}'")
    elif SYSTEM == "Darwin":
        run(sudo(["launchctl", "kickstart", "-k", f"system/{LAUNCHD_LABEL}"]))
    else:
        run(sudo(["systemctl", "restart", SYSTEMD_UNIT]))


def wait_healthy(key: str) -> int:
    for _ in range(20):
        n = proxy_ok(f"http://127.0.0.1:{PORT}", key)
        if n is not None:
            return n
        time.sleep(1)
    die(f"proxy didn't answer on port {PORT}; check the logs in {PROXY_DIR / 'logs'}")
    return 0


# ---------------------------------------------------------------- ccp launcher

def write_ccp(url: str, key: str) -> None:
    """`ccp` = Claude Code through the proxy. Plain `claude` keeps its normal login."""
    BIN_DIR.mkdir(parents=True, exist_ok=True)
    if WINDOWS:
        path = BIN_DIR / "ccp.cmd"
        path.write_text("@echo off\r\nrem Claude Code through CLIProxyAPI (generated by spinup.py)\r\n"
                        f'set "ANTHROPIC_BASE_URL={url}"\r\nset "ANTHROPIC_AUTH_TOKEN={key}"\r\n'
                        'set "ANTHROPIC_API_KEY="\r\nclaude %*\r\n', encoding="ascii")
    else:
        path = BIN_DIR / "ccp"
        path.write_text("#!/bin/sh\n# Claude Code through CLIProxyAPI (generated by spinup.py)\n"
                        f'export ANTHROPIC_BASE_URL="{url}"\nexport ANTHROPIC_AUTH_TOKEN="{key}"\n'
                        'unset ANTHROPIC_API_KEY\nexec claude "$@"\n', encoding="utf-8")
        path.chmod(0o700)
    log(f"Wrote {path} -> {url}")
    on_path = any(Path(p).resolve() == BIN_DIR.resolve()
                  for p in os.environ.get("PATH", "").split(os.pathsep) if p)
    if not on_path:
        log(f"Add {BIN_DIR} to your PATH to run `ccp` from anywhere")


# ---------------------------------------------------------------- commands

def service_guard() -> None:
    if service_installed():
        die("the spinup service runs the proxy on this machine (`spinup status`); "
            "to go back to this setup, run `spinup uninstall` first")


def cmd_hub(args: argparse.Namespace) -> None:
    service_guard()
    setup_tailscale(getattr(args, "name", None))
    if not EXE.exists():
        install_proxy_binary(*latest_release())
    else:
        log(f"CLIProxyAPI {installed_version()} already installed (use `update` to upgrade)")
    if args.import_auth:
        AUTH_DIR.mkdir(parents=True, exist_ok=True)
        files = list(Path(args.import_auth).glob("*.json"))
        for f in files:
            shutil.copy2(f, AUTH_DIR / f.name)
        log(f"Imported {len(files)} account login(s) into {AUTH_DIR}")
    sec = load_secrets()
    write_config(sec)
    register_autostart()
    restart_proxy()
    models = wait_healthy(sec["api_key"])
    write_ccp(f"http://127.0.0.1:{PORT}", sec["api_key"])
    ip = (tailscale_status().get("Self", {}).get("TailscaleIPs") or ["<tailscale-ip>"])[0]
    print(f"""
Hub ready. Proxy is running and starts at boot ({models} models available).
  Dashboard (this machine): http://127.0.0.1:{PORT}/management.html
  Dashboard (other devices): http://{ip}:{PORT}/management.html
  Password + API key:        {PY} show-key
Add accounts: Dashboard > OAuth Login (private browser window per Claude account).
Other machines:            {PY} setup <name>  (they find this hub on the tailnet by themselves)
""")


def cmd_client(args: argparse.Namespace) -> None:
    service_guard()
    setup_tailscale(getattr(args, "name", None))
    hub_os = ""
    if not args.hub:
        node = find_hub() or die("no hub found on your tailnet: is it on? On the hub run `setup <name> --hub`")
        args.hub, hub_os = dns_name(node), node.get("OS", "")
        log(f"Found the hub: {args.hub}")
    hub = args.hub
    hub_os = hub_os or next((n.get("OS", "") for n in tailnet_nodes() if dns_name(n) == hub), "")
    hub_py = "py spinup.py" if hub_os == "windows" else "python3 spinup.py" if hub_os else "spinup.py"
    if not hub[0].isdigit():  # Tailscale machine name -> IP
        r = run([tailscale_bin(), "ip", "-4", hub], check=False, capture=True)
        if r.returncode != 0:
            die(f"can't find '{hub}' on your tailnet: {r.stderr.strip()}")
        hub = r.stdout.split()[0]
    url = f"http://{hub}:{PORT}"
    key = args.key or getpass.getpass(f"API key (on {args.hub}, run `{hub_py} show-key`): ").strip()
    models = proxy_ok(url, key)
    if models is None:
        die(f"hub proxy at {url} didn't answer (is the hub on, and is the key right?)")
    write_ccp(url, key)
    print(f"\nClient ready. Hub {url} offers {models} models. Open a new terminal and run `ccp`.")


def cmd_update(args: argparse.Namespace) -> None:
    if service_installed():
        log("CLIProxyAPI is run by the spinup service here; skipping its update")
    elif EXE.exists():
        current = installed_version()
        latest, assets = latest_release()
        if current == latest and not args.force:
            log(f"CLIProxyAPI {current} is the latest")
        else:
            install_proxy_binary(latest, assets)
            restart_proxy()
            wait_healthy(load_secrets()["api_key"])
            log(f"CLIProxyAPI {current} -> {latest}")
    if shutil.which("npx"):
        # Re-adding the list fetches the latest versions; `skills update -g` would also
        # restore parked skills that are still in the skills CLI's lock file.
        from .skills import cmd_skills
        log("Updating skills")
        cmd_skills(args)
    ts = tailscale_bin()
    if ts and NIXOS:
        log("Tailscale on NixOS updates with the system (nixos-rebuild)")
    elif ts:
        log("Updating Tailscale")
        if WINDOWS:
            run(["winget", "upgrade", "--id", "Tailscale.Tailscale", "-e", "--silent",
                 "--accept-package-agreements", "--accept-source-agreements"], check=False)
        elif SYSTEM == "Darwin" and shutil.which("brew"):
            run(["brew", "upgrade", "tailscale"], check=False)
        else:
            run(sudo([ts, "update", "--yes"]), check=False)


def cmd_status(_: argparse.Namespace) -> None:
    st = tailscale_status()
    me = st.get("Self", {})
    print(f"Tailscale:   {st.get('BackendState', 'not installed')}  "
          f"{me.get('HostName', '')} {' '.join(me.get('TailscaleIPs', [])[:1])}")
    if service_installed():
        print("Proxy:       run by the spinup service (`spinup status` shows the accounts and machines)")
        return
    if not EXE.exists():
        print("CLIProxyAPI: not installed (this is a client, or run `hub`)")
        return
    try:
        latest = latest_release()[0]
    except (urllib.error.URLError, OSError):
        latest = "?"
    current = installed_version()
    print(f"CLIProxyAPI: {current} (latest {latest}{', run `update`' if latest not in ('?', current) else ''})")
    models = proxy_ok(f"http://127.0.0.1:{PORT}", load_secrets()["api_key"])
    print(f"Proxy:       {'running, %d models' % models if models is not None else 'NOT answering'}")
    accounts = sorted(p.stem for p in AUTH_DIR.glob("*.json")) if AUTH_DIR.exists() else []
    print(f"Accounts:    {len(accounts)}")
    for a in accounts:
        print(f"  - {a}")


def cmd_show_key(_: argparse.Namespace) -> None:
    if not SECRETS.exists() and not (PROXY_DIR / "SECRETS.txt").exists():
        die("no hub secrets on this machine (run this on the hub)")
    sec = load_secrets()
    print(f"API key:             {sec['api_key']}\nDashboard password:  {sec['management_password']}")
