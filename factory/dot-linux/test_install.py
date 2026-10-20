from __future__ import annotations

import importlib.util
from pathlib import Path
import unittest


INSTALL_PATH = Path(__file__).with_name("install.py")
SPEC = importlib.util.spec_from_file_location("dot_linux_install", INSTALL_PATH)
assert SPEC is not None and SPEC.loader is not None
install = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(install)


class FakeAdb:
    def __init__(self, data_ready: bool = True, cache_ready: bool = False,
                 format_ready: bool = True) -> None:
        self.data_ready = data_ready
        self.cache_ready = cache_ready
        self.format_ready = format_ready
        self.commands: list[str] = []

    def sh(self, command: str) -> str:
        self.commands.append(command)
        if "mke2fs" in command:
            return "TATER_CACHE_READY" if self.format_ready else "mke2fs failed"
        if "TATER_DATA_READY" in command:
            return "TATER_DATA_READY" if self.data_ready else ""
        if "TATER_CACHE_READY" in command:
            return "TATER_CACHE_READY" if self.cache_ready else ""
        return ""


class DotLinuxStorageTests(unittest.TestCase):
    def test_blank_cache_is_deferred_without_formatting(self) -> None:
        adb = FakeAdb(cache_ready=False)

        ready = install.mount_install_storage(adb, "/dev/userdata", "/dev/cache")

        self.assertFalse(ready)
        self.assertFalse(any("mke2fs" in command for command in adb.commands))

    def test_existing_cache_is_reused(self) -> None:
        adb = FakeAdb(cache_ready=True)

        self.assertTrue(install.mount_install_storage(adb, "/dev/userdata", "/dev/cache"))
        self.assertFalse(any("mke2fs" in command for command in adb.commands))

    def test_userdata_must_be_mountable(self) -> None:
        with self.assertRaisesRegex(install.Fail, "could not mount userdata"):
            install.mount_install_storage(
                FakeAdb(data_ready=False), "/dev/userdata", "/dev/cache"
            )

    def test_cache_initialization_requires_verified_mount(self) -> None:
        adb = FakeAdb(format_ready=True)

        install.initialize_cache(adb, "/dev/cache")

        command = next(command for command in adb.commands if "mke2fs" in command)
        self.assertIn("mke2fs -F -t ext4 -L cache /dev/cache", command)
        self.assertIn("grep -q ' /cache ' /proc/mounts", command)

    def test_cache_initialization_failure_is_fatal(self) -> None:
        with self.assertRaisesRegex(install.Fail, "could not initialize"):
            install.initialize_cache(FakeAdb(format_ready=False), "/dev/cache")


if __name__ == "__main__":
    unittest.main()
