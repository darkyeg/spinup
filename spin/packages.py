"""Dev tools from packages.json: check what's missing, install it with the OS package manager."""
from __future__ import annotations

from .common import *

PACKAGES = REPO / "packages.json"


def load_tools() -> list[dict]:
    return json.loads(PACKAGES.read_text(encoding="utf-8"))["tools"]


def is_installed(tool: dict) -> bool:
    return any(shutil.which(cmd) for cmd in tool["check"])


def refresh_path() -> None:
    """Windows: re-read PATH from the registry, so tools installed by this run are found."""
    if WINDOWS:
        import winreg
        parts = []
        for hive, key in ((winreg.HKEY_LOCAL_MACHINE, r"SYSTEM\CurrentControlSet\Control\Session Manager\Environment"),
                          (winreg.HKEY_CURRENT_USER, "Environment")):
            try:
                with winreg.OpenKey(hive, key) as k:
                    parts.append(os.path.expandvars(winreg.QueryValueEx(k, "Path")[0]))
            except OSError:
                pass
        os.environ["PATH"] = os.pathsep.join([os.environ.get("PATH", ""), *parts])


def missing_tools() -> list[dict]:
    refresh_path()
    return [t for t in load_tools() if not is_installed(t)]


def install_command(tool: dict) -> list[str] | None:
    """The first installer in packages.json that fits this OS, or None."""
    npm = shutil.which("npm")
    if WINDOWS:
        if "winget" in tool:
            return ["winget", "install", "--id", tool["winget"], "-e", "--silent",
                    "--accept-package-agreements", "--accept-source-agreements"]
        if "powershell" in tool:
            return ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", tool["powershell"]]
    elif SYSTEM == "Darwin":
        if "brew" in tool and shutil.which("brew"):
            return ["brew", "install", tool["brew"]]
        if "script" in tool:
            return ["sh", "-c", tool["script"]]
    else:
        if "apt" in tool and shutil.which("apt-get"):
            return sudo(["apt-get", "install", "-y", *tool["apt"].split()])
        if "script" in tool:
            return ["sh", "-c", tool["script"]]
    if "npm" in tool and npm:
        return [npm, "install", "-g", tool["npm"]]
    return None


def cmd_packages(args: argparse.Namespace) -> None:
    missing = missing_tools()
    if not missing:
        log(f"All {len(load_tools())} tools installed")
        return
    log(f"Missing: {', '.join(t['name'] for t in missing)}")
    if args.check:
        return
    if NIXOS:
        attrs = " ".join(t["nix"] for t in missing if "nix" in t)
        print(f"NixOS: add to environment.systemPackages in configuration.nix, then `sudo nixos-rebuild switch`:\n"
              f"  environment.systemPackages = with pkgs; [ {attrs} ];")
        return
    failed = []
    for tool in missing:
        cmd = install_command(tool)
        if cmd is None:
            failed.append(f"{tool['name']} (no installer for this OS, or npm missing)")
            continue
        log(f"Installing {tool['name']}")
        if run(cmd, check=False).returncode != 0:
            failed.append(tool["name"])
    print("\nDone. Open a new terminal so PATH picks up new tools, then run `doctor`.")
    if failed:
        die(f"not installed: {', '.join(failed)}")
