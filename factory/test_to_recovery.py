from __future__ import annotations

import importlib.util
from pathlib import Path
import unittest


HELPER_PATH = Path(__file__).with_name("to_recovery.py")
SPEC = importlib.util.spec_from_file_location("tater_echo_to_recovery", HELPER_PATH)
assert SPEC is not None and SPEC.loader is not None
helper = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(helper)


class FakeConsoleLibrary:
    CONSOLE_DOT = ("18d1", "4ee7")
    CONSOLE_TECHO5 = ("1d6b", "0104")

    def __init__(self, ports=None, answers=None) -> None:
        self.ports = {
            self.CONSOLE_DOT: [("/dev/ttyACM0", "serial")],
            self.CONSOLE_TECHO5: [],
        } if ports is None else ports
        self.answers = list(answers or [])
        self.commands: list[tuple[str, str, int]] = []

    def list_consoles(self, ids):
        return self.ports.get(ids, [])

    def console_exchange(self, port: str, command: str, wait: int):
        self.commands.append((port, command, wait))
        return self.answers.pop(0)


class TaterEchoRecoveryHelperTests(unittest.TestCase):
    def test_deduplicates_overlapping_console_discovery(self) -> None:
        same = [("/dev/cu.usbmodem123", "123")]
        console = FakeConsoleLibrary(ports={
            FakeConsoleLibrary.CONSOLE_DOT: same,
            FakeConsoleLibrary.CONSOLE_TECHO5: same,
        })
        self.assertEqual("/dev/cu.usbmodem123", helper.select_port(console, None))

    def test_arms_one_boot_tater_rescue_and_reboots(self) -> None:
        console = FakeConsoleLibrary(
            answers=["TATER_RECOVERY_SUPPORTED", "TATER_RECOVERY_REBOOTING"]
        )

        helper.reboot_to_recovery(console, "/dev/ttyACM0")

        self.assertEqual(2, len(console.commands))
        self.assertIn("slotctl", console.commands[0][1])
        self.assertIn("slotctl rescue", console.commands[1][1])
        self.assertIn("tater-reboot-now", console.commands[1][1])
        self.assertNotIn("to-twrp", console.commands[1][1])

    def test_refuses_an_unrecognized_console(self) -> None:
        console = FakeConsoleLibrary(answers=[""])
        with self.assertRaisesRegex(helper.Fail, "not a supported Tater Linux Echo"):
            helper.reboot_to_recovery(console, "/dev/ttyACM0")
        self.assertEqual(1, len(console.commands))


if __name__ == "__main__":
    unittest.main()
