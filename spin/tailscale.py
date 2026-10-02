"""Tailscale: install, start at boot, log in."""
from __future__ import annotations

from .common import *


# ---------------------------------------------------------------- tailscale

def tailscale_bin() -> str | None:
    found = shutil.which("tailscale")
    if found:
        return found
    for p in [r"C:\Program Files\Tailscale\tailscale.exe",
              "/opt/homebrew/bin/tailscale", "/usr/local/bin/tailscale",
              "/Applications/Tailscale.app/Contents/MacOS/Tailscale"]:
        if Path(p).exists():
            return p
    return None


def tailscale_status() -> dict:
    ts = tailscale_bin()
    if not ts:
        return {}
    r = run([ts, "status", "--json"], check=False, capture=True)
    try:
        return json.loads(r.stdout)
    except ValueError:
        return {}


NIXOS_TAILSCALE = """NixOS: add this to /etc/nixos/configuration.nix, then run `sudo nixos-rebuild switch`:

  services.tailscale.enable = true;

Then run `sudo tailscale up` once to log in, and re-run this command."""


def dns_name(node: dict) -> str:
    """The MagicDNS short name (`pc` from `pc.tailXXXX.ts.net.`); this is what other machines type."""
    return (node.get("DNSName") or node.get("HostName") or "").split(".")[0].lower()


def tailnet_nodes() -> list[dict]:
    """Every device on the tailnet, this one first. Tailscale is the only list of machines; nothing is kept in the repo."""
    st = tailscale_status()
    me = dict(st.get("Self") or {}, Online=True, IsSelf=True)
    return [n for n in [me, *(st.get("Peer") or {}).values()] if n.get("TailscaleIPs")]


def is_proxy(ip: str) -> bool:
    try:
        return b"CLI Proxy API" in http_get(f"http://{ip}:{PORT}/", timeout=2)
    except Exception:
        return False


def find_hub() -> dict | None:
    """The tailnet device running CLIProxyAPI: probe every online device's proxy port at once."""
    from concurrent.futures import ThreadPoolExecutor
    online = [n for n in tailnet_nodes() if n.get("Online")]
    with ThreadPoolExecutor(max_workers=16) as pool:
        hits = list(pool.map(lambda n: is_proxy(n["TailscaleIPs"][0]), online))
    return next((n for n, hit in zip(online, hits) if hit), None)


def setup_tailscale(name: str | None = None) -> None:
    """Install, start at boot, log in; with `name`, make this machine reachable as `name` on the tailnet."""
    if NIXOS:
        if not tailscale_bin() or run(["systemctl", "is-enabled", "tailscaled"],
                                      check=False, capture=True).returncode != 0:
            die(NIXOS_TAILSCALE)
    elif not tailscale_bin():
        log("Installing Tailscale")
        if WINDOWS:
            run(["winget", "install", "--id", "Tailscale.Tailscale", "-e", "--silent",
                 "--accept-package-agreements", "--accept-source-agreements"])
        elif SYSTEM == "Darwin":
            if not shutil.which("brew"):
                die("Install Homebrew first: https://brew.sh")
            run(["brew", "install", "tailscale"])  # CLI daemon: runs at boot, no GUI login needed
        else:
            run(["sh", "-c", "curl -fsSL https://tailscale.com/install.sh | sh"])

    # Start at boot, before anyone logs in.
    if WINDOWS:
        state = powershell("(Get-Service Tailscale).StartType.ToString() + ' ' + "
                           "(Get-Service Tailscale).Status.ToString()").stdout.split()
        if state != ["Automatic", "Running"]:
            windows_admin("Set-Service Tailscale -StartupType Automatic; Start-Service Tailscale",
                          "Start Tailscale at boot")
    elif SYSTEM == "Darwin":
        if shutil.which("brew") and not Path("/Library/LaunchDaemons/homebrew.mxcl.tailscale.plist").exists():
            run(sudo(["brew", "services", "start", "tailscale"]))
    elif not NIXOS:
        run(sudo(["systemctl", "enable", "--now", "tailscaled"]))

    ts = tailscale_bin() or die("tailscale not found after install")
    for _ in range(10):  # the service needs a moment after starting
        if tailscale_status().get("BackendState") not in (None, "NoState"):
            break
        time.sleep(1)
    if tailscale_status().get("BackendState") != "Running":
        log("Log in to Tailscale (open the URL it prints) — use the same account on every machine")
        run(sudo([ts, "up", *(["--unattended"] if WINDOWS else []), *([f"--hostname={name}"] if name else [])]))
    elif WINDOWS:
        # Keep Tailscale connected after logout / before login.
        run([ts, "set", "--unattended"], check=False, capture=True)
    if name and dns_name(tailscale_status().get("Self", {})) != name:
        # The Tailscale IP never changes for a machine; the name is the shortcut. If the admin console
        # renamed this machine by hand, that name wins and this has no effect.
        run(sudo([ts, "set", f"--hostname={name}"]))
    me = tailscale_status().get("Self", {})
    log(f"Tailscale up: {me.get('HostName')} {', '.join(me.get('TailscaleIPs', [])[:1])}")
