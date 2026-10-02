#!/usr/bin/env python3
"""spinup: set up and keep healthy every machine used for AI coding.

    py spinup.py setup [name]        # whole machine: tools, Tailscale name, proxy, skills, agents
    py spinup.py links               # every device on the tailnet and its addresses
    py spinup.py doctor              # health check: what's wrong and the command that fixes it
    py spinup.py hub                 # this machine holds the accounts and runs the proxy
    py spinup.py client              # this machine uses the hub's proxy over Tailscale
    py spinup.py packages            # install missing dev tools (packages.json)
    py spinup.py skills              # global skills (skills/skills.json) for Claude + Codex
    py spinup.py skills list         # what's installed, auto or manual, token cost (also: add, remove, manual, auto)
    py spinup.py agents              # shared AGENTS.md, cheap Explore subagent, token-saving settings
    py spinup.py repo <path>         # per-repo: stack skills, AGENTS.md size, git remote
    py spinup.py update              # update CLIProxyAPI (keeps config + accounts), skills, Tailscale
    py spinup.py status              # proxy + Tailscale at a glance
    py spinup.py show-key            # API key + dashboard password (to set up clients)

(`py` on Windows, `python3` on macOS/Linux.)
Every command is safe to re-run. Works on Windows, macOS and Linux. Standard library only.
"""
from __future__ import annotations

import argparse
import sys

from spin.agents import cmd_agents
from spin.common import WINDOWS, die
from spin.doctor import cmd_doctor
from spin.machines import cmd_links, cmd_setup
from spin.packages import cmd_packages
from spin.proxy import cmd_client, cmd_hub, cmd_show_key, cmd_status, cmd_update
from spin.repo import cmd_repo
from spin.skills import cmd_skills, cmd_skills_add, cmd_skills_list, cmd_skills_mode, cmd_skills_remove


def main() -> None:
    if WINDOWS and "WindowsApps" in sys.executable:
        die("Microsoft Store Python redirects files in AppData, so the proxy wouldn't find them. "
            "Use python.org Python instead: `py spinup.py ...` (install: winget install Python.Python.3.13)")
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("setup", help="set up this machine end to end; clients find the hub themselves")
    s.add_argument("name", nargs="?", help="machine name, becomes its Tailscale name (default: current one)")
    s.add_argument("--hub", action="store_true", help="make this machine the hub (default: client, unless it already is the hub)")
    s.add_argument("--key", help="client only: the hub's API key (prompted if omitted)")
    s.set_defaults(fn=cmd_setup)
    sub.add_parser("links", help="every device on the tailnet and its addresses").set_defaults(fn=cmd_links)
    sub.add_parser("doctor", help="health check with fixes").set_defaults(fn=cmd_doctor)
    h = sub.add_parser("hub", help="set up this machine as the hub")
    h.add_argument("--import-auth", metavar="DIR", help="copy account logins from an old hub's ~/.cli-proxy-api")
    h.set_defaults(fn=cmd_hub)
    c = sub.add_parser("client", help="use the hub's proxy from this machine")
    c.add_argument("--hub", help="hub Tailscale name or IP (default: found on the tailnet)")
    c.add_argument("--key", help="API key (prompted if omitted)")
    c.set_defaults(fn=cmd_client)
    k = sub.add_parser("packages", help="install missing dev tools from packages.json")
    k.add_argument("--check", action="store_true", help="only list what's missing")
    k.set_defaults(fn=cmd_packages)
    sk = sub.add_parser("skills", help="install skills/skills.json for Claude Code + Codex, park the rest")
    sk.set_defaults(fn=cmd_skills)
    sks = sk.add_subparsers(dest="skills_cmd", metavar="{list,add,remove,manual,auto}")
    sks.add_parser("list", help="every skill: auto or manual, where from, token cost").set_defaults(fn=cmd_skills_list)
    private = argparse.ArgumentParser(add_help=False)
    private.add_argument("--private", action="store_true", help="edit local/skills.json (only your machines) "
                         "instead of skills/skills.json")
    a = sks.add_parser("add", parents=[private], help="add skills from a GitHub repo and install them")
    a.add_argument("source", help="GitHub owner/repo, e.g. anthropics/skills")
    a.add_argument("names", nargs="+", help="skill names in that repo")
    a.add_argument("--manual", action="store_true", help="install as manual: runs only when you call it")
    a.set_defaults(fn=cmd_skills_add)
    rm = sks.add_parser("remove", parents=[private], help="remove skills from the list (they get parked)")
    rm.add_argument("names", nargs="+")
    rm.set_defaults(fn=cmd_skills_remove)
    for mode, text in (("manual", "make skills run only when you call them (/name, $name); no tokens until then"),
                       ("auto", "let the agent use skills by itself (the default)")):
        m = sks.add_parser(mode, parents=[private], help=text)
        m.add_argument("names", nargs="+")
        m.set_defaults(fn=cmd_skills_mode, mode=mode)
    sub.add_parser("agents", help="install agents/: shared instructions, subagents, settings"
                   ).set_defaults(fn=cmd_agents)
    r = sub.add_parser("repo", help="per-repo: stack skills, AGENTS.md size, git remote")
    r.add_argument("path", nargs="?", default=".", help="repo root (default: current folder)")
    r.add_argument("--apply", action="store_true", help="make the changes (default: only report)")
    r.set_defaults(fn=cmd_repo)
    u = sub.add_parser("update", help="update CLIProxyAPI, skills and Tailscale")
    u.add_argument("--force", action="store_true", help="reinstall even if already latest")
    u.set_defaults(fn=cmd_update)
    sub.add_parser("status", help="show what's installed and running").set_defaults(fn=cmd_status)
    sub.add_parser("show-key", help="print API key and dashboard password").set_defaults(fn=cmd_show_key)
    args = p.parse_args()
    args.fn(args)


if __name__ == "__main__":
    main()
