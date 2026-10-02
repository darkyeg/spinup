"""Per-repo setup: detect the stack, check AGENTS.md size and the git remote, add stack-specific skills."""
from __future__ import annotations

import fnmatch

from .common import *

RULES = REPO / "skills" / "per-repo.json"
SKIP_DIRS = {"node_modules", "dist", "build", "target", "vendor", "out", "bin", "obj"}
GITHUB_URL = re.compile(r"^(git@github\.com:|https://github\.com/|ssh://git@github\.com/)")
SCP_URL = re.compile(r"^(?:[\w.-]+@)?(?P<host>[\w.-]+):(?P<path>[\w.-]+/[\w.-]+?)(?:\.git)?/?$")


def project_files(root: Path, max_depth: int = 3) -> list[Path]:
    """Files near the top of the repo, skipping dependency/build folders and dot-folders."""
    found = []
    for dirpath, dirnames, filenames in os.walk(root):
        depth = len(Path(dirpath).relative_to(root).parts)
        dirnames[:] = ([d for d in dirnames if d not in SKIP_DIRS and not d.startswith(".")]
                       if depth < max_depth else [])
        found += [Path(dirpath) / f for f in filenames]
    return found


def package_deps(files: list[Path]) -> set[str]:
    deps: set[str] = set()
    for f in files:
        if f.name != "package.json":
            continue
        try:
            data = json.loads(f.read_text(encoding="utf-8"))
        except (ValueError, OSError):
            continue
        for key in ("dependencies", "devDependencies", "peerDependencies"):
            deps |= set(data.get(key) or {})
    return deps


def rule_matches(rule: dict, files: list[Path], deps: set[str]) -> bool:
    hits = [f for f in files for pattern in rule.get("files", []) if fnmatch.fnmatch(f.name, pattern)]
    by_package = any(p in deps for p in rule.get("packages", []))
    if not hits and not by_package:
        return False
    if "contains" in rule:
        return any(c in f.read_text(encoding="utf-8", errors="ignore") for f in hits for c in rule["contains"])
    return True


def remote_fix(root: Path) -> tuple[str | None, str | None]:
    """(origin URL, github.com URL it should be) — the second is None when it's already fine."""
    r = run(["git", "-C", str(root), "remote", "get-url", "origin"], check=False, capture=True)
    if r.returncode != 0:
        return f"(can't read: {(r.stderr.strip().splitlines() or ['no origin remote'])[0]})", None
    url = r.stdout.strip()
    if GITHUB_URL.match(url):
        return url, None
    m = SCP_URL.match(url)
    if not m:
        return url, None
    # An SSH alias (e.g. `gh:owner/repo`): rewrite only if it really points at github.com.
    ssh = run(["ssh", "-G", m["host"]], check=False, capture=True)
    if "\nhostname github.com" in "\n" + ssh.stdout.lower():
        return url, f"git@github.com:{m['path']}.git"
    return url, None


def cmd_repo(args: argparse.Namespace) -> None:
    root = Path(args.path).resolve()
    if not (root / ".git").exists():
        die(f"{root} is not a git repo root")
    files = project_files(root)
    deps = package_deps(files)
    rules = [r for r in json.loads(RULES.read_text(encoding="utf-8"))["rules"] if rule_matches(r, files, deps)]
    print(f"Repo:   {root}")
    print(f"Stack:  {', '.join(r['stack'] for r in rules) or 'nothing recognised'}")

    todo = []  # (description, action) — actions run only with --apply

    url, fixed = remote_fix(root)
    if fixed:
        print(f"Remote: {url}  ->  {fixed}  (T3 Code groups repos across machines by github.com URL)")

        def fix_remote(url=url, fixed=fixed):
            run(["git", "-C", str(root), "remote", "set-url", "origin", fixed])
            if run(["git", "-C", str(root), "ls-remote", "origin", "HEAD"], check=False, capture=True).returncode:
                run(["git", "-C", str(root), "remote", "set-url", "origin", url])
                log(f"Kept {url}: github.com didn't accept your SSH key")
            else:
                log(f"Remote is now {fixed}")
        todo.append(("fix the remote", fix_remote))
    else:
        print(f"Remote: {url}" + ("" if url.startswith("(") else "  ok"))

    # Claude Code (2.1.277+) and Codex both read AGENTS.md; a CLAUDE.md is optional.
    agents_md = root / "AGENTS.md"
    if agents_md.exists():
        n = len(agents_md.read_text(encoding="utf-8", errors="ignore").splitlines())
        print(f"AGENTS.md: {n} lines" + ("  (over 200: trim it, it loads every session)" if n > 200 else "  ok"))
    else:
        print("AGENTS.md: missing. Ask an agent: \"write an AGENTS.md for this repo (use the writing-for-agents skill)\"")

    for rule in rules:
        if rule.get("note"):
            print(f"Note ({rule['stack']}): {rule['note']}")
        for source, names in rule.get("skills", {}).items():
            missing = [n for n in names if not (root / ".agents" / "skills" / n).exists()]
            if not missing:
                continue
            print(f"Skills ({rule['stack']}): {', '.join(missing)} from {source}")

            def add_skills(source=source, missing=missing):
                cmd = [npx(), "--yes", "skills", "add", source, "-a", "claude-code", "-a", "codex",
                       *[a for n in missing for a in ("-s", n)], "-y"]
                if subprocess.run(cmd, cwd=root, text=True).returncode != 0:
                    die(f"skills add {source} failed")
            todo.append((f"add {', '.join(missing)}", add_skills))

    if not todo:
        print("\nNothing to do.")
    elif not args.apply:
        print(f"\nRe-run with --apply to: {'; '.join(d for d, _ in todo)}.")
    else:
        for _, action in todo:
            action()
        print("\nApplied. Review and commit the changes in the repo (git status).")
