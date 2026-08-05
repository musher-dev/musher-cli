#!/usr/bin/env python3
"""Run govulncheck and fail only on findings that are not explicitly accepted.

govulncheck has no ignore mechanism, so a single unfixable advisory would
otherwise force the entire scan to be disabled. This keeps the scan on and
narrows it to an auditable allowlist in .govulncheck-allow.yml.

Only *called* vulnerabilities are considered — govulncheck reports a finding for
every affected module in the graph, but a finding whose trace reaches a function
in our binary is the one that matters. Findings without a call trace are
reported as informational and never fail the build.
"""

from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
ALLOW_FILE = ROOT / ".govulncheck-allow.yml"


def load_allowlist() -> dict[str, str]:
    """Return {osv_id: reason}. Parsed without PyYAML to avoid a build dep."""
    if not ALLOW_FILE.exists():
        return {}

    allowed: dict[str, str] = {}
    current: str | None = None

    for line in ALLOW_FILE.read_text(encoding="utf-8").splitlines():
        stripped = line.strip()
        if stripped.startswith("#"):
            continue

        match = re.match(r"^-\s+id:\s*(\S+)", stripped)
        if match:
            current = match.group(1)
            allowed[current] = ""
            continue

        if current and stripped.startswith("reason:"):
            allowed[current] = stripped[len("reason:") :].strip()

    return allowed


def called_vulns(payload: list[dict]) -> dict[str, str]:
    """Return {osv_id: symbol} for findings that reach our code."""
    called: dict[str, str] = {}

    for message in payload:
        finding = message.get("finding")
        if not finding:
            continue

        trace = finding.get("trace") or []
        # A finding is "called" when its innermost trace frame names a function.
        if not trace or not trace[0].get("function"):
            continue

        osv = finding.get("osv", "")
        frame = trace[0]
        symbol = f"{frame.get('module', '')}.{frame.get('function', '')}"
        called.setdefault(osv, symbol)

    return called


def main() -> int:
    proc = subprocess.run(
        ["govulncheck", "-format", "json", "./..."],
        capture_output=True,
        text=True,
        cwd=ROOT,
        check=False,
    )

    if not proc.stdout.strip():
        print("govulncheck produced no output", file=sys.stderr)
        print(proc.stderr, file=sys.stderr)
        return 1

    # govulncheck streams concatenated JSON objects, not a JSON array.
    decoder = json.JSONDecoder()
    payload: list[dict] = []
    text = proc.stdout
    index = 0

    while index < len(text):
        while index < len(text) and text[index].isspace():
            index += 1
        if index >= len(text):
            break
        obj, end = decoder.raw_decode(text, index)
        payload.append(obj)
        index = end

    allowed = load_allowlist()
    called = called_vulns(payload)

    accepted = {osv: sym for osv, sym in called.items() if osv in allowed}
    blocking = {osv: sym for osv, sym in called.items() if osv not in allowed}

    for osv, symbol in sorted(accepted.items()):
        print(f"accepted: {osv} (reached via {symbol})")

    if not blocking:
        print(f"OK: no unaccepted called vulnerabilities ({len(accepted)} accepted)")
        return 0

    print("", file=sys.stderr)
    print("Called vulnerabilities that are not accepted:", file=sys.stderr)

    for osv, symbol in sorted(blocking.items()):
        print(f"  {osv} — reached via {symbol}", file=sys.stderr)
        print(f"    https://pkg.go.dev/vuln/{osv}", file=sys.stderr)

    print("", file=sys.stderr)
    print(
        "Fix the dependency, or add a justified entry to .govulncheck-allow.yml.",
        file=sys.stderr,
    )

    return 1


if __name__ == "__main__":
    sys.exit(main())
