"""Global skills: install the list, link your own, park the rest; list/add/remove and manual/auto modes."""
from __future__ import annotations

from .common import *


def is_link(path: Path) -> bool:
    return path.is_symlink() or bool(getattr(path, "is_junction", lambda: False)())


def remove_link(path: Path) -> None:
    os.rmdir(path) if WINDOWS else path.unlink()


def link_dir(link: Path, target: Path) -> None:
    """Directory link that works without admin: a junction on Windows, a symlink elsewhere."""
    if is_link(link):
        remove_link(link)
    elif link.exists():
        shutil.rmtree(link)
    if WINDOWS:
        run(["cmd", "/c", "mklink", "/J", str(link), str(target)], capture=True)
    else:
        link.symlink_to(target, target_is_directory=True)


MANUAL_CODEX = "policy:\n  allow_implicit_invocation: false\n"  # Codex: only runs when you type $name


def manifest_path(private: bool) -> Path:
    return PRIVATE / "skills.json" if private else SKILLS_MANIFEST


def read_manifest(path: Path) -> dict:
    if not path.exists():
        return {"sources": {}, "manual": []}
    data = json.loads(path.read_text(encoding="utf-8"))
    data.setdefault("sources", {})
    data.setdefault("manual", [])
    return data


def save_manifest(path: Path, data: dict) -> None:
    """Write skills.json with one line per source, so diffs stay readable."""
    data["sources"] = {s: n for s, n in data["sources"].items() if n}
    parts = []
    for key, value in data.items():
        if key == "sources":
            rows = [f"    {json.dumps(s)}: {json.dumps(n)}" for s, n in value.items()]
            parts.append('  "sources": {\n' + ",\n".join(rows) + "\n  }" if rows else '  "sources": {}')
        else:
            parts.append(f"  {json.dumps(key)}: {json.dumps(value)}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(("{\n" + ",\n".join(parts) + "\n}\n").encode())
    log(f"Updated {path}")


def load_manifest() -> dict:
    """skills/skills.json with your private local/skills.json merged in (extra sources, extra manual skills)."""
    m = read_manifest(SKILLS_MANIFEST)
    extra = read_manifest(PRIVATE / "skills.json")
    for source, names in extra["sources"].items():
        m["sources"].setdefault(source, [])
        m["sources"][source] += [n for n in names if n not in m["sources"][source]]
    m["manual"] = sorted(set(m["manual"]) | set(extra["manual"]))
    return m


def frontmatter(skill_dir: Path) -> tuple[str, bool]:
    """(description, manual-only by its author) from a skill's SKILL.md."""
    try:
        text = (skill_dir / "SKILL.md").read_text(encoding="utf-8", errors="ignore")
    except OSError:
        return "", False
    head = text.split("---")[1] if text.startswith("---") else ""
    desc, in_desc = [], False
    for line in head.splitlines():
        if line.startswith("description:"):
            in_desc = True
            line = line[len("description:"):]
        elif line and not line[0].isspace():
            in_desc = False
        if in_desc:
            desc.append(line.strip().strip(">|\"'"))
    return " ".join(d for d in desc if d), "disable-model-invocation: true" in head


def apply_modes(keep: set[str], manual: set[str]) -> None:
    """Manual skills stay installed but only run when you call them: /name in Claude Code, $name in Codex."""
    from .agents import write_managed
    settings_path = CLAUDE_HOME / "settings.json"
    settings = json.loads(settings_path.read_text(encoding="utf-8")) if settings_path.exists() else {}
    overrides = dict(settings.get("skillOverrides") or {})
    for name in keep:
        if name in manual:
            overrides[name] = "user-invocable-only"
        elif overrides.get(name) == "user-invocable-only":
            del overrides[name]
        codex = SKILLS_HOME / name / "agents" / "openai.yaml"
        if name in manual and not codex.exists():
            codex.parent.mkdir(parents=True, exist_ok=True)
            codex.write_bytes(MANUAL_CODEX.encode())
        elif name not in manual and codex.exists() and codex.read_text(encoding="utf-8") == MANUAL_CODEX:
            codex.unlink()
            if not any(codex.parent.iterdir()):
                codex.parent.rmdir()
    if overrides != (settings.get("skillOverrides") or {}):
        if overrides:
            settings["skillOverrides"] = overrides
        else:
            settings.pop("skillOverrides", None)
        write_managed(settings_path, json.dumps(settings, indent=2) + "\n")


def cmd_skills_list(_: argparse.Namespace) -> None:
    m = load_manifest()
    source_of = {n: s for s, names in m["sources"].items() for n in names}
    source_of.update({n: "own (skills/local, local/skills)" for n in own_skills()})
    rows, auto_chars = [], 0
    for name in sorted(source_of):
        path = SKILLS_HOME / name
        desc, author_manual = frontmatter(path)
        mode = "manual" if name in m["manual"] or author_manual else "auto"
        state = "" if path.exists() else "  (not installed: run skills)"
        if mode == "auto" and path.exists():
            auto_chars += len(desc)
        rows.append((name, mode, source_of[name] + state))
    w = max(len(r[0]) for r in rows)
    for name, mode, source in rows:
        print(f"  {name:<{w}}  {mode:<6}  {source}")
    print(f"\n{len(rows)} skills. Auto ones cost about {auto_chars // 4:,} tokens of descriptions in every session; "
          f"manual ones cost nothing until you call them (/name in Claude Code, $name in Codex).")


def cmd_skills_add(args: argparse.Namespace) -> None:
    path = manifest_path(args.private)
    data = read_manifest(path)
    names = data["sources"].setdefault(args.source, [])
    names += [n for n in args.names if n not in names]
    if args.manual:
        data["manual"] = sorted(set(data["manual"]) | set(args.names))
    save_manifest(path, data)
    cmd_skills(args)


def cmd_skills_remove(args: argparse.Namespace) -> None:
    path = manifest_path(args.private)
    data = read_manifest(path)
    found = [n for n in args.names if any(n in v for v in data["sources"].values())]
    if not found:
        die(f"none of {', '.join(args.names)} is in {path}")
    data["sources"] = {s: [n for n in v if n not in args.names] for s, v in data["sources"].items()}
    data["manual"] = [n for n in data["manual"] if n not in args.names]
    save_manifest(path, data)
    cmd_skills(args)  # parks the removed ones


def cmd_skills_mode(args: argparse.Namespace) -> None:
    path = manifest_path(args.private)
    data = read_manifest(path)
    manual = set(data["manual"])
    data["manual"] = sorted(manual | set(args.names) if args.mode == "manual" else manual - set(args.names))
    save_manifest(path, data)
    m = load_manifest()
    keep = {n for v in m["sources"].values() for n in v} | set(own_skills())
    apply_modes(keep, set(m["manual"]))
    log(f"{', '.join(args.names)}: {args.mode}. Restart Claude Code / Codex to apply.")


def cmd_skills(_: argparse.Namespace) -> None:
    manifest = load_manifest()
    agent_args = [arg for agent in manifest["agents"] for arg in ("-a", agent)]
    failed = []
    for source, names in manifest["sources"].items():
        log(f"Skills from {source}: {', '.join(names)}")
        r = run([npx(), "--yes", "skills", "add", source, "-g", *agent_args,
                 *[arg for name in names for arg in ("-s", name)], "-y"], check=False, capture=True)
        if r.returncode != 0:
            failed.append(source)
            print((r.stdout + r.stderr).strip()[-800:])

    # Our own skills: copy into the shared store, link into Claude Code.
    SKILLS_HOME.mkdir(parents=True, exist_ok=True)
    (CLAUDE_HOME / "skills").mkdir(parents=True, exist_ok=True)
    own = own_skills()
    for name, src in own.items():
        dest = SKILLS_HOME / name
        if is_link(dest):
            remove_link(dest)
        elif dest.exists():
            shutil.rmtree(dest)
        shutil.copytree(src, dest)
        link_dir(CLAUDE_HOME / "skills" / name, dest)
    if own:
        log(f"Own skills: {', '.join(own)}")

    # Park everything else, so no agent pays tokens for skills that aren't on the list.
    keep = {n for names in manifest["sources"].values() for n in names} | set(own)
    parked = []
    if not failed:  # a failed install would look "unlisted"; don't park on a partial run
        for entry in sorted(SKILLS_HOME.iterdir()):
            if entry.is_dir() and entry.name not in keep:
                SKILLS_PARKED.mkdir(parents=True, exist_ok=True)
                target = SKILLS_PARKED / entry.name
                if target.exists():
                    shutil.rmtree(target)
                shutil.move(str(entry), str(target))
                parked.append(entry.name)
    for link in (CLAUDE_HOME / "skills").iterdir():  # Claude links whose target is gone
        if is_link(link) and not Path(os.path.realpath(link)).exists():
            remove_link(link)
    apply_modes(keep, set(manifest["manual"]))
    if parked:
        log(f"Parked {len(parked)} unlisted skills in {SKILLS_PARKED} (move one back to re-enable it)")

    print(f"\n{len(keep)} skills for {', '.join(manifest['agents'])}. Restart Claude Code / Codex to load them.")
    if failed:
        die(f"install failed for: {', '.join(failed)} (nothing was parked)")
