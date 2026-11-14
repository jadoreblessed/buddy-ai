#!/usr/bin/env python3
"""Summarize executable Go source lines and Vitest line coverage.

Go's built-in percent measures statements, not lines. This script maps Go coverprofile
counter spans onto non-empty source lines, then combines them with Vitest's line
counts. A line is covered if any instrumented span on it ran.
"""

import argparse
import json
import re
from pathlib import Path

PROFILE_ROW = re.compile(r"^(.*):(\d+)\.(\d+),(\d+)\.(\d+)\s+(\d+)\s+(\d+)$")
PUNCTUATION = re.compile(rb"[{}();]+")


def go_lines(profile: Path, root: Path) -> tuple[dict[str, tuple[int, int]], tuple[int, int]]:
    module = (root / "go.mod").read_text().splitlines()[0].removeprefix("module ") + "/"
    all_lines: dict[str, set[int]] = {}
    hit_lines: dict[str, set[int]] = {}
    sources: dict[str, list[bytes]] = {}
    for row in profile.read_text().splitlines()[1:]:
        match = PROFILE_ROW.fullmatch(row)
        if not match:
            raise ValueError(f"Invalid Go coverprofile block: {row}")
        name, start_line, start_col, end_line, end_col, _, hits = match.groups()
        if not name.startswith(module):
            raise ValueError(f"Unexpected source outside module: {name}")
        relative = name[len(module):]
        if relative not in sources:
            sources[relative] = (root / relative).read_bytes().splitlines()
            all_lines[relative] = set()
            hit_lines[relative] = set()
        lines = sources[relative]
        first, last = int(start_line), int(end_line)
        for number in range(first, last + 1):
            raw = lines[number - 1]
            start = int(start_col) - 1 if number == first else 0
            end = int(end_col) - 1 if number == last else len(raw)
            part = raw[start:end].strip()
            if not part or part.startswith((b"//", b"/*", b"*")) or PUNCTUATION.fullmatch(part):
                continue
            all_lines[relative].add(number)
            if int(hits):
                hit_lines[relative].add(number)
    packages: dict[str, tuple[int, int]] = {}
    for name in all_lines:
        package = name.split("/", 1)[0]
        covered, total = packages.get(package, (0, 0))
        packages[package] = (covered + len(hit_lines[name]), total + len(all_lines[name]))
    app_file = (len(hit_lines["backend/app.go"]), len(all_lines["backend/app.go"]))
    return packages, app_file


def frontend_lines(summary: Path) -> tuple[int, int]:
    data = json.loads(summary.read_text())
    files = {name: value for name, value in data.items() if name != "total"}
    source_files = {name: value for name, value in files.items()
                    if name.endswith(("/src/App.tsx", "/src/main.tsx"))}
    if len(source_files) != 2:
        raise ValueError("Vitest summary must cover both src/App.tsx and src/main.tsx")
    return (sum(value["lines"]["covered"] for value in source_files.values()),
            sum(value["lines"]["total"] for value in source_files.values()))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("go_profile", type=Path)
    parser.add_argument("frontend_summary", type=Path)
    args = parser.parse_args()
    root = Path(__file__).resolve().parent
    packages, app_file = go_lines(args.go_profile, root)
    if set(packages) != {"backend", "mock-service"}:
        raise ValueError(f"Go profile must include backend and mock-service: {sorted(packages)}")
    results = [(*packages[name], name) for name in ("backend", "mock-service")]
    results.append((*frontend_lines(args.frontend_summary), "frontend"))
    covered = total = 0
    for count, size, name in results:
        covered += count
        total += size
        print(f"{name:13} {count:4}/{size:<4} {count / size * 100:.1f}% executable lines")
    print(f"{'backend/app.go':13} {app_file[0]:4}/{app_file[1]:<4} {app_file[0] / app_file[1] * 100:.1f}% executable lines (included above)")
    percent = covered / total * 100
    print(f"{'total':13} {covered:4}/{total:<4} {percent:.1f}% executable lines")
    if percent <= 80:
        raise SystemExit("Coverage gate failed: line coverage must exceed 80%")


if __name__ == "__main__":
    main()
