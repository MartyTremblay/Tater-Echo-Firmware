#!/usr/bin/env python3
"""Verify or install the bundled Tater Linux image for Echo Show 8 Gen 2."""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


ROOT = Path(__file__).resolve().parent


class Fail(Exception):
    pass


def sha256(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            value.update(chunk)
    return value.hexdigest()


def verify_bundle() -> dict:
    try:
        manifest = json.loads((ROOT / "bundle-manifest.json").read_text())
    except (OSError, ValueError) as error:
        raise Fail(f"cannot read bundle manifest: {error}") from error
    if (manifest.get("target") != "athena" or manifest.get("base_os") != "tater-linux" or
            not isinstance(manifest.get("files"), dict)):
        raise Fail("this is not an Athena Tater Linux factory bundle")
    files = manifest["files"]
    required = {
        "install.sh", "install.py", "recovery_install.py",
        "payload/rootfs.tar.gz", "payload/system.img.gz", "payload/boot.img",
        "tools/install-athena.py", "tools/techo5lib.py",
    }
    missing = sorted(required - files.keys())
    if missing:
        raise Fail(f"bundle manifest is missing required files: {', '.join(missing)}")
    for relative, metadata in sorted(files.items()):
        if not isinstance(relative, str) or not isinstance(metadata, dict):
            raise Fail("bundle manifest has an invalid file entry")
        path = (ROOT / relative).resolve()
        if ROOT.resolve() not in path.parents or not path.is_file():
            raise Fail(f"bundle file is missing or unsafe: {relative}")
        if path.stat().st_size != int(metadata.get("size", -1)):
            raise Fail(f"bundle file has the wrong size: {relative}")
        if sha256(path) != metadata.get("sha256"):
            raise Fail(f"bundle file failed SHA-256 verification: {relative}")
    return manifest


def main(argv: list[str]) -> int:
    try:
        manifest = verify_bundle()
        if argv == ["--verify-bundle"]:
            print(f"Bundle verified: Athena Tater Linux {manifest['version']}")
            return 0
        if "--verify-bundle" in argv:
            raise Fail("--verify-bundle cannot be combined with install options")
        if "--repair" in argv:
            command = [sys.executable, str(ROOT / "recovery_install.py"), *argv]
        else:
            forbidden = {"--image", "--boot", "--release", "--kernel"}
            if any(argument == option or argument.startswith(option + "=")
                   for argument in argv for option in forbidden):
                raise Fail("the Tater bundle pins its image, boot kernel, and mainline install mode")
            if not any(argument == "--name" or argument.startswith("--name=") for argument in argv):
                argv = ["--name", "Tater Athena", *argv]
            command = [
                sys.executable,
                str(ROOT / "tools/install-athena.py"),
                "--image", str(ROOT / "payload/system.img.gz"),
                "--boot", str(ROOT / "payload/boot.img"),
                "--no-watch",
                *argv,
            ]
        return subprocess.call(command, cwd=ROOT)
    except (Fail, OSError, KeyError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
