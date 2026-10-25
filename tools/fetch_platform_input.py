#!/usr/bin/env python3
"""Download one URL/SHA-256 pair from the immutable platform lock."""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path
import sys
import tempfile
from urllib import request

from tools import platform_lock


def sha256(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def fetch(lock_path: str, destination: Path) -> None:
    asset = platform_lock.get(platform_lock.load(), lock_path)
    if not isinstance(asset, dict) or set(("url", "sha256")) - set(asset):
        raise SystemExit(f"{lock_path!r} is not a locked downloadable asset")
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.is_file() and sha256(destination) == asset["sha256"]:
        return
    with tempfile.NamedTemporaryFile(
        prefix=destination.name + ".", suffix=".partial", dir=destination.parent, delete=False
    ) as temporary:
        partial = Path(temporary.name)
    try:
        with request.urlopen(asset["url"]) as source, partial.open("wb") as output:
            while chunk := source.read(1024 * 1024):
                output.write(chunk)
        if sha256(partial) != asset["sha256"]:
            raise SystemExit(f"downloaded {lock_path} failed SHA-256 verification")
        partial.replace(destination)
    finally:
        partial.unlink(missing_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("lock_path")
    parser.add_argument("destination", type=Path)
    args = parser.parse_args()
    fetch(args.lock_path, args.destination)
    return 0


if __name__ == "__main__":
    sys.exit(main())
