"""Global skills: install the list, link local ones, park the rest."""
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


def cmd_skills(_: argparse.Namespace) -> None:
    manifest = json.loads(SKILLS_MANIFEST.read_text(encoding="utf-8"))
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
    if parked:
        log(f"Parked {len(parked)} unlisted skills in {SKILLS_PARKED} (move one back to re-enable it)")

    print(f"\n{len(keep)} skills for {', '.join(manifest['agents'])}. Restart Claude Code / Codex to load them.")
    if failed:
        die(f"install failed for: {', '.join(failed)} (nothing was parked)")
