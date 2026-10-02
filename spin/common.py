"""Paths, constants and small helpers shared by every spin module."""
from __future__ import annotations

import argparse
import getpass
import hashlib
import io
import json
import os
import platform
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
TEMPLATE = REPO / "proxy" / "config.template.yaml"
PROXY_REPO = "router-for-me/CLIProxyAPI"
PORT = 8317
SERVICE = "CLIProxyAPI"                 # Windows scheduled task name
LAUNCHD_LABEL = "com.spinup.cliproxyapi"
SYSTEMD_UNIT = "cliproxyapi"
FIREWALL_RULE = "CLIProxyAPI (Tailscale only)"
TAILNET_RANGE = "100.64.0.0/10"

SYSTEM = platform.system()              # "Windows" | "Darwin" | "Linux"
WINDOWS = SYSTEM == "Windows"
NIXOS = Path("/etc/NIXOS").exists()   # system services come from configuration.nix there
HOME = Path.home()
BIN_DIR = HOME / ".local" / "bin"       # where the `ccp` launcher goes
AUTH_DIR = HOME / ".cli-proxy-api"      # account logins (matches oauth.auth-dir in the template)

if WINDOWS:
    PROXY_DIR = Path(os.environ["LOCALAPPDATA"]) / "CLIProxyAPI"
elif SYSTEM == "Darwin":
    PROXY_DIR = HOME / "Library" / "Application Support" / "CLIProxyAPI"
else:
    PROXY_DIR = HOME / ".local" / "share" / "cliproxyapi"
EXE = PROXY_DIR / ("cli-proxy-api.exe" if WINDOWS else "cli-proxy-api")
CONFIG = PROXY_DIR / "config.yaml"
SECRETS = PROXY_DIR / "secrets.json"
SKILLS_MANIFEST = REPO / "skills" / "skills.json"
LOCAL_SKILLS = REPO / "skills" / "local"
PRIVATE = REPO / "local"                # optional: your private repo cloned here (gitignored), see docs/PRIVATE.md
AGENTS_DIR = REPO / "agents"
SKILLS_HOME = HOME / ".agents" / "skills"             # shared store; Codex reads it directly
SKILLS_PARKED = HOME / ".agents" / "skills-parked"    # not scanned by any agent
CLAUDE_HOME = Path(os.environ.get("CLAUDE_CONFIG_DIR") or HOME / ".claude")
CODEX_HOME = Path(os.environ.get("CODEX_HOME") or HOME / ".codex")
PY = "py spinup.py" if WINDOWS else "python3 spinup.py"  # how to run this script, for messages
# The Go service (cmd/spinup, docs/SERVICE.md) keeps its config here once installed; from then on it
# runs the proxy, and the hub/client commands below step aside.
SERVICE_HOME = Path(os.environ.get("SPINUP_HOME") or (
    Path(os.environ["LOCALAPPDATA"]) / "spinup" if WINDOWS else HOME / ".local" / "share" / "spinup"))


def own_skills() -> dict[str, Path]:
    """Skills shipped as folders (not from a skills repo): skills/local/* plus your private local/skills/*."""
    found = {}
    for base in (LOCAL_SKILLS, PRIVATE / "skills"):
        if base.is_dir():
            found.update({p.name: p for p in sorted(base.iterdir()) if (p / "SKILL.md").exists()})
    return found


def service_installed() -> bool:
    """True when the spinup service runs the proxy on this machine (see docs/SERVICE.md)."""
    return (SERVICE_HOME / "config.json").exists()


def log(msg: str) -> None:
    print(f"==> {msg}", flush=True)


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def run(cmd: list[str], check: bool = True, capture: bool = False) -> subprocess.CompletedProcess:
    return subprocess.run(cmd, check=check, text=True,
                          capture_output=capture, encoding="utf-8", errors="replace")


def sudo(cmd: list[str]) -> list[str]:
    """Prefix with sudo on macOS/Linux unless already root."""
    if WINDOWS or os.geteuid() == 0:
        return cmd
    return ["sudo", *cmd]


def powershell(script: str, check: bool = True) -> subprocess.CompletedProcess:
    return run(["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script],
               check=check, capture=True)


def windows_admin(script: str, what: str) -> None:
    """Run a PowerShell snippet as Administrator (Windows `sudo`, which shows a UAC prompt)."""
    import ctypes
    # Not tempfile.TemporaryDirectory: its owner-only ACL stops us reading what the admin step wrote.
    tmp = Path(tempfile.gettempdir()) / f"spinup-{secrets.token_hex(4)}"
    tmp.mkdir()
    try:
        ps1, result = tmp / "step.ps1", tmp / "result.txt"
        ps1.write_text(
            "$ErrorActionPreference = 'Stop'\ntry {\n" + script +
            f"\n'OK' | Out-File -Encoding utf8 '{result}'\n}} catch {{ $_ | Out-String | Out-File -Encoding utf8 '{result}' }}\n",
            encoding="utf-8")
        cmd = ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", str(ps1)]
        if not ctypes.windll.shell32.IsUserAnAdmin():
            if not shutil.which("sudo"):
                die(f"'{what}' needs admin. Run this from an Administrator terminal, or turn on "
                    "Settings > System > For developers > Enable sudo.")
            log(f"{what} (needs admin, approve the UAC prompt)")
            cmd = ["sudo", *cmd]
        run(cmd, check=False)
        # sudo in "new window" mode returns before the elevated step finishes, so wait for it.
        for _ in range(180):
            if result.exists():
                time.sleep(0.5)  # let the writer finish
                break
            time.sleep(1)
        out = result.read_text(encoding="utf-8-sig").strip() if result.exists() else "no result (UAC declined?)"
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
    if out != "OK":
        die(f"{what} failed:\n{out}")


def http_get(url: str, token: bool = False, timeout: float = 120) -> bytes:
    headers = {"User-Agent": "spinup"}
    if token and os.environ.get("GITHUB_TOKEN"):
        headers["Authorization"] = f"Bearer {os.environ['GITHUB_TOKEN']}"
    with urllib.request.urlopen(urllib.request.Request(url, headers=headers), timeout=timeout) as r:
        return r.read()


def proxy_ok(base_url: str, key: str) -> int | None:
    """Number of models the proxy offers, or None if it doesn't answer."""
    req = urllib.request.Request(f"{base_url}/v1/models", headers={"Authorization": f"Bearer {key}"})
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return len(json.load(r).get("data", []))
    except (urllib.error.URLError, OSError, ValueError):
        return None


def npx() -> str:
    found = shutil.which("npx")
    if not found:
        die("Node.js is needed for skills (npx not found). Install Node.js, then re-run.")
    return found
