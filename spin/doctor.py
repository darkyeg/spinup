"""`doctor`: one health check for this machine, with the command that fixes each problem."""
from __future__ import annotations

from . import agents, packages
from .common import *
from .proxy import installed_version, latest_release
from .skills import load_manifest
from .tailscale import dns_name, tailnet_nodes, tailscale_bin, tailscale_status


class Report:
    def __init__(self) -> None:
        self.failures = 0

    def ok(self, label: str) -> None:
        print(f"  [ok] {label}")

    def warn(self, label: str, fix: str = "") -> None:
        print(f"  [!!] {label}" + (f"\n       fix: {fix}" if fix else ""))

    def fail(self, label: str, fix: str = "") -> None:
        self.failures += 1
        print(f"  [XX] {label}" + (f"\n       fix: {fix}" if fix else ""))


def ccp_target() -> tuple[str, str] | None:
    """(url, key) from this machine's ccp launcher, if there is one."""
    for name in ("ccp.cmd", "ccp"):
        path = BIN_DIR / name
        if path.exists():
            text = path.read_text(encoding="utf-8", errors="ignore")
            url = re.search(r'ANTHROPIC_BASE_URL="?([^"\r\n]+)', text)
            key = re.search(r'ANTHROPIC_AUTH_TOKEN="?([^"\r\n]+)', text)
            if url and key:
                return url.group(1), key.group(1)
    return None


def check_tools(r: Report) -> None:
    print("Tools")
    if sys.version_info < (3, 11):
        r.fail(f"Python {sys.version.split()[0]} (need 3.11+)", "install Python 3.13 from python.org")
    else:
        r.ok(f"Python {sys.version.split()[0]}")
    missing = packages.missing_tools()
    if missing:
        r.fail(f"missing: {', '.join(t['name'] for t in missing)}", f"{PY} packages")
    else:
        r.ok(f"all {len(packages.load_tools())} tools in packages.json")


def check_network(r: Report) -> None:
    print("Network")
    st = tailscale_status()
    if st.get("BackendState") == "Running":
        me = st.get("Self", {})
        r.ok(f"Tailscale running as {dns_name(me)} {' '.join(me.get('TailscaleIPs', [])[:1])}")
    else:
        r.fail(f"Tailscale: {st.get('BackendState', 'not installed')}", f"{PY} setup <name>")
        return
    for node in tailnet_nodes():
        if node.get("KeyExpiry") and not node.get("Expired"):
            # An expired key drops the machine off the tailnet until someone logs in on it, in person.
            r.warn(f"{dns_name(node)}: Tailscale login expires {node['KeyExpiry'][:10]}",
                   "https://login.tailscale.com/admin/machines > ... > Disable key expiry")
        elif node.get("Expired"):
            r.fail(f"{dns_name(node)}: Tailscale login expired", f"log in on {dns_name(node)}: tailscale up")
    if not st.get("CertDomains"):
        r.warn("Tailscale HTTPS certificates are off, so `tailscale serve` (and T3 Code's "
               "`t3 pair --tailscale`) hang", "https://login.tailscale.com/admin/dns > HTTPS Certificates > Enable")
    if not (st.get("CurrentTailnet") or {}).get("MagicDNSEnabled"):
        r.warn("MagicDNS is off, so machine names like `pc` don't resolve",
               "https://login.tailscale.com/admin/dns > Enable MagicDNS")
    for peer in tailnet_nodes()[1:]:
        name = dns_name(peer)
        if not peer.get("Online"):
            r.ok(f"{name}: offline")
        elif (path := peer_path(peer["TailscaleIPs"][0])).startswith("relay"):
            r.warn(f"{name}: {path}", "slower; a strict NAT or firewall blocks the direct path")
        else:
            r.ok(f"{name}: {path}")


def peer_path(name: str) -> str:
    """How traffic to `name` flows: over the LAN, direct over the internet, or through a Tailscale relay."""
    out = run([tailscale_bin(), "ping", "-c", "5", "--timeout", "2s", name], check=False, capture=True).stdout
    pongs = [line for line in out.splitlines() if line.startswith("pong")]
    if not pongs:
        return "online, but ping got no answer"
    via = pongs[-1].split(" via ")[-1].split(" in ")[0]
    if via.startswith("DERP"):
        return f"relay {via}"
    host = via.rsplit(":", 1)[0].strip("[]")
    lan = host.startswith(("192.168.", "10.")) or (host.startswith("172.") and 16 <= int(host.split(".")[1]) <= 31)
    return f"{'LAN' if lan else 'direct'} {via}"


def check_proxy(r: Report) -> None:
    print("Proxy")
    if EXE.exists():  # hub
        key = json.loads(SECRETS.read_text(encoding="utf-8"))["api_key"] if SECRETS.exists() else ""
        models = proxy_ok(f"http://127.0.0.1:{PORT}", key)
        if models is None:
            r.fail("hub proxy not answering on 127.0.0.1", f"{PY} hub")
        else:
            r.ok(f"hub proxy running, {models} models")
        try:
            latest, current = latest_release()[0], installed_version()
            if latest != current:
                r.warn(f"CLIProxyAPI {current}, latest is {latest}", f"{PY} update")
            else:
                r.ok(f"CLIProxyAPI {current} (latest)")
        except (urllib.error.URLError, OSError):
            r.warn("couldn't check the latest CLIProxyAPI release (offline?)")
        logins = [p.name for p in AUTH_DIR.glob("*.json")] if AUTH_DIR.exists() else []
        claude = sum(n.startswith("claude") for n in logins)
        codex = sum(n.startswith("codex") for n in logins)
        if claude == 0:
            r.warn(f"accounts: {claude} Claude, {codex} Codex",
                   f"add Claude accounts: http://127.0.0.1:{PORT}/management.html > OAuth Login")
        else:
            r.ok(f"accounts: {claude} Claude, {codex} Codex")
        return
    target = ccp_target()
    if target is None:
        r.warn("no proxy on this machine and no ccp launcher", f"{PY} setup")
        return
    url, key = target
    models = proxy_ok(url, key)
    if models is None:
        r.fail(f"hub proxy at {url} not answering", "is the hub on and on Tailscale? then re-run client")
    else:
        r.ok(f"hub proxy at {url}: {models} models")


def check_agents(r: Report) -> None:
    print("Agents")
    manifest = load_manifest()
    want = {n for names in manifest["sources"].values() for n in names} | set(own_skills())
    have = {p.name for p in SKILLS_HOME.iterdir() if p.is_dir()} if SKILLS_HOME.exists() else set()
    if want - have or have - want:
        parts = []
        if want - have:
            parts.append(f"missing {', '.join(sorted(want - have))}")
        if have - want:
            parts.append(f"unlisted {', '.join(sorted(have - want))}")
        r.fail(f"skills: {'; '.join(parts)}", f"{PY} skills")
    else:
        r.ok(f"skills: {len(want)} installed, none extra")
    drift = agents.drifted()
    if drift:
        r.fail(f"config differs from repo: {', '.join(str(p) for p in drift)}", f"{PY} agents")
    else:
        r.ok("instructions, subagents and settings match the repo")


def check_repo(r: Report) -> None:
    for name, path in (("This repo", REPO), ("Private repo (local/)", PRIVATE)):
        if (path / ".git").exists():
            print(name)
            check_git(r, path)


def check_git(r: Report, path: Path) -> None:
    git = ["git", "-C", str(path)]
    run([*git, "fetch", "-q"], check=False, capture=True)
    dirty = run([*git, "status", "--porcelain"], check=False, capture=True).stdout.strip()
    counts = run([*git, "rev-list", "--left-right", "--count", "HEAD...@{u}"], check=False, capture=True).stdout.split()
    ahead, behind = (int(counts[0]), int(counts[1])) if len(counts) == 2 else (0, 0)
    if behind:
        r.warn(f"{behind} commit(s) behind GitHub", "git pull, then re-run skills/agents")
    if ahead or dirty:
        r.warn("local changes not pushed", "commit and push so other machines get them")
    if not (behind or ahead or dirty):
        r.ok("up to date with GitHub")


def cmd_doctor(_: argparse.Namespace) -> None:
    r = Report()
    for check in (check_tools, check_network, check_proxy, check_agents, check_repo):
        check(r)
    print(f"\n{'All good.' if not r.failures else f'{r.failures} problem(s); run the fix commands above.'}")
    if r.failures:
        sys.exit(1)
