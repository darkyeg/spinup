"""Shared agent config: instructions, Claude subagents, Claude/Codex settings."""
from __future__ import annotations

from .common import *


def backup_once(path: Path) -> None:
    backup = path.with_name(path.name + ".spinup-backup")
    if path.exists() and not backup.exists():
        shutil.copy2(path, backup)


def write_managed(path: Path, content: str) -> None:
    if path.exists() and path.read_text(encoding="utf-8") == content:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    backup_once(path)
    path.write_bytes(content.encode("utf-8"))  # keep \n line endings on Windows too
    log(f"Wrote {path}")


def deep_merge(base: dict, patch: dict) -> dict:
    for key, value in patch.items():
        if key == "_comment":
            continue
        if isinstance(value, dict) and isinstance(base.get(key), dict):
            deep_merge(base[key], value)
        else:
            base[key] = value
    return base


def toml_value(value: object) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (int, float)):
        return str(value)
    if isinstance(value, str):
        return json.dumps(value)
    die(f"unsupported TOML value: {value!r}")
    return ""


def toml_set(text: str, table: str | None, key: str, value: object) -> str:
    """Set one key in one table, keeping the rest of the file (comments, order) as is."""
    lines = text.splitlines()
    header = re.compile(r"^\s*\[")
    if table is None:
        start, end = 0, next((i for i, l in enumerate(lines) if header.match(l)), len(lines))
    else:
        found = next((i for i, l in enumerate(lines) if l.strip() == f"[{table}]"), None)
        if found is None:
            return text.rstrip("\n") + f"\n\n[{table}]\n{key} = {toml_value(value)}\n"
        start = found + 1
        end = next((i for i in range(start, len(lines)) if header.match(lines[i])), len(lines))
    line = f"{key} = {toml_value(value)}"
    for i in range(start, end):
        if re.match(rf"^\s*{re.escape(key)}\s*=", lines[i]):
            lines[i] = line
            return "\n".join(lines) + "\n"
    insert = end
    while insert > start and not lines[insert - 1].strip():
        insert -= 1
    lines.insert(insert, line)
    return "\n".join(lines) + "\n"


def desired_files() -> list[tuple[Path, str]]:
    """Every file `agents` manages, with the content it should have on this machine."""
    try:
        import tomllib
    except ImportError:
        die("needs Python 3.11+ (tomllib)")
    files: list[tuple[Path, str]] = []

    # 1. One instruction file for both tools.
    instructions = (AGENTS_DIR / "AGENTS.md").read_text(encoding="utf-8")
    personal = PRIVATE / "AGENTS.md"  # your own preferences, appended (docs/PRIVATE.md)
    if personal.exists():
        instructions = instructions.rstrip() + "\n\n" + personal.read_text(encoding="utf-8")
    files += [(CLAUDE_HOME / "CLAUDE.md", instructions), (CODEX_HOME / "AGENTS.md", instructions)]

    # 2. Claude Code subagents (e.g. a cheaper Explore).
    for agent in sorted((AGENTS_DIR / "claude" / "agents").glob("*.md")):
        files.append((CLAUDE_HOME / "agents" / agent.name, agent.read_text(encoding="utf-8")))

    # 3. Claude Code settings: merge our keys, keep everything else (hooks, statusLine, ...).
    settings_path = CLAUDE_HOME / "settings.json"
    current = json.loads(settings_path.read_text(encoding="utf-8")) if settings_path.exists() else {}
    patch = json.loads((AGENTS_DIR / "claude" / "settings.json").read_text(encoding="utf-8"))
    merged = deep_merge(json.loads(json.dumps(current)), patch)
    if merged != current:
        files.append((settings_path, json.dumps(merged, indent=2) + "\n"))

    # 4. Codex config: set our keys, keep everything else.
    config_path = CODEX_HOME / "config.toml"
    text = config_path.read_text(encoding="utf-8") if config_path.exists() else ""
    wanted = tomllib.loads((AGENTS_DIR / "codex" / "config.toml").read_text(encoding="utf-8"))
    new = text
    for key, value in wanted.items():
        if isinstance(value, dict):
            for sub_key, sub_value in value.items():
                new = toml_set(new, key, sub_key, sub_value)
        else:
            new = toml_set(new, None, key, value)
    tomllib.loads(new)  # never write a config Codex can't parse
    if new != text:
        files.append((config_path, new))
    return files


def drifted() -> list[Path]:
    """Managed files whose content differs from the repo (what `agents` would rewrite)."""
    return [path for path, content in desired_files()
            if not path.exists() or path.read_text(encoding="utf-8") != content]


def cmd_agents(_: argparse.Namespace) -> None:
    for path, content in desired_files():
        write_managed(path, content)
    print("\nAgent config installed. Restart Claude Code / Codex (and T3 Code) to pick it up.")
