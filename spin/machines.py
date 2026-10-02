"""`setup [name]`: bring this machine up end to end; `links`: every device on the tailnet."""
from __future__ import annotations

from .agents import cmd_agents
from .common import *
from .doctor import cmd_doctor
from .packages import cmd_packages
from .proxy import cmd_client, cmd_hub
from .skills import cmd_skills
from .tailscale import dns_name, find_hub, tailnet_nodes, tailscale_status

NAME = re.compile(r"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$")  # a valid Tailscale / DNS name


def pull_latest() -> None:
    """Fast-forward this repo (and local/, if it's your private repo) first; restart if the code changed."""
    if (PRIVATE / ".git").exists():
        run(["git", "-C", str(PRIVATE), "pull", "--ff-only", "-q"], check=False, capture=True)
    head = run(["git", "-C", str(REPO), "rev-parse", "HEAD"], check=False, capture=True).stdout
    if run(["git", "-C", str(REPO), "pull", "--ff-only", "-q"], check=False, capture=True).returncode != 0:
        log("Couldn't update the repo (offline, or local changes); continuing with this copy")
    elif run(["git", "-C", str(REPO), "rev-parse", "HEAD"], check=False, capture=True).stdout != head:
        log("Repo updated; restarting with the new version")
        os.execv(sys.executable, [sys.executable, *sys.argv])


def cmd_setup(args: argparse.Namespace) -> None:
    pull_latest()
    st = tailscale_status()
    current = (dns_name(st.get("Self", {})) or re.sub(r"[^a-z0-9-]", "-", platform.node().lower())).strip("-")
    name = args.name
    if not name and sys.stdin.isatty():
        print("This machine's name becomes its address on your tailnet: other machines reach it as\n"
              f"`<name>` (e.g. http://<name>:{PORT}, ssh <name>). Lowercase letters, digits, dashes.")
        name = input(f"Name for this machine [{current}]: ").strip().lower()
    name = name or current
    if not NAME.match(name):
        die(f"'{name}' can't be a machine name: use lowercase letters, digits and dashes (e.g. office-pc)")
    hub = args.hub or EXE.exists()  # a machine that already runs the proxy stays the hub
    log(f"Setting up {name} as {'the hub' if hub else 'a client (finds the hub on the tailnet)'}")
    steps = [("dev tools", cmd_packages, {"check": False})]
    if hub:
        steps.append(("hub: Tailscale + proxy", cmd_hub, {"import_auth": None}))
    else:
        steps.append(("client: Tailscale + proxy", cmd_client, {"hub": None, "key": args.key}))
    steps += [("skills", cmd_skills, {}), ("agent config", cmd_agents, {}), ("health check", cmd_doctor, {})]
    for i, (what, fn, extra) in enumerate(steps, 1):
        if fn is cmd_doctor:
            print_addresses(name, hub)
        log(f"[{i}/{len(steps)}] {what}")
        fn(argparse.Namespace(name=name, **extra))


def print_addresses(name: str, hub: bool) -> None:
    full = (tailscale_status().get("Self", {}).get("DNSName") or "").rstrip(".")
    print(f"\nThis machine is `{name}` on your tailnet{f' ({full})' if full else ''}. From your other machines:")
    if hub:
        print(f"  proxy API:        http://{name}:{PORT}")
        print(f"  proxy dashboard:  http://{name}:{PORT}/management.html")
    print(f"  any service:      http://{name}:<port>     ssh: ssh <user>@{name}")
    print(f"  all machines:     {PY} links\n")


def cmd_links(_: argparse.Namespace) -> None:
    """Every device on the tailnet with its addresses. Straight from Tailscale, so it's never out of date."""
    hub = find_hub()
    rows = []
    for n in tailnet_nodes():
        name = dns_name(n)
        tags = [t for t, on in (("this device", n.get("IsSelf")), ("hub", hub and dns_name(hub) == name)) if on]
        state = "online" if n.get("Online") else "offline"
        rows.append((name, n["TailscaleIPs"][0], (n.get("DNSName") or "").rstrip("."),
                     f"{n.get('OS', '')}, {state}" + "".join(f", {t}" for t in tags)))
    w = [max(len(r[i]) for r in rows) for i in range(3)]
    for r in rows:
        print("  ".join(c.ljust(w[i]) if i < 3 else c for i, c in enumerate(r)))
    if hub:
        print(f"\nProxy dashboard: http://{dns_name(hub)}:{PORT}/management.html  (password: show-key on the hub)")
    print("Any service on a device: http://<name>:<port>. If a short name doesn't resolve, use the full name or the IP.")
