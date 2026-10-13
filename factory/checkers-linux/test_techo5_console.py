import importlib.util
from pathlib import Path
import unittest
from unittest import mock


SOURCE = Path(__file__).parent / "tools/techo5/techo5lib.py"
SPEC = importlib.util.spec_from_file_location("checkers_techo5lib", SOURCE)
lib = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(lib)


class ConsoleTests(unittest.TestCase):
    def test_serial_write_cannot_wait_forever_for_rescue_reader(self):
        port = lib.SerialPort.__new__(lib.SerialPort)
        port.name = "/dev/ttyACM0"
        port.fd = 7
        with mock.patch.object(lib, "IS_WINDOWS", False), \
             mock.patch.object(lib.os, "write", side_effect=BlockingIOError), \
             mock.patch.object(lib.time, "monotonic", side_effect=[0, 6]), \
             mock.patch.object(lib.time, "sleep"):
            with self.assertRaisesRegex(TimeoutError, "did not accept serial input"):
                port.write(b"probe")

    def test_present_but_unresponsive_gadget_is_not_called_missing(self):
        console = lib.Console("SERIAL", lib.CONSOLE_TECHO5)
        with mock.patch.object(lib, "list_consoles", return_value=[("/dev/ttyACM0", "techo5")]):
            self.assertIn("present, but its shell has not answered", console.waiting_hint())


class FastbootTests(unittest.TestCase):
    def test_present_times_out_instead_of_blocking_the_rescue_deadline(self):
        fastboot = lib.Fastboot("SERIAL")
        with mock.patch.object(
            lib.subprocess,
            "run",
            side_effect=lib.subprocess.TimeoutExpired(["fastboot", "devices"], 10),
        ) as run:
            self.assertFalse(fastboot.present())
        self.assertEqual(10, run.call_args.kwargs["timeout"])

    def test_continue_timeout_is_reported_to_the_caller(self):
        fastboot = lib.Fastboot("SERIAL")
        with mock.patch.object(
            lib.subprocess,
            "run",
            side_effect=lib.subprocess.TimeoutExpired(["fastboot", "continue"], 15),
        ):
            code, output = fastboot.run("continue", timeout=15)
        self.assertEqual(124, code)
        self.assertIn("timed out after 15 seconds", output)


if __name__ == "__main__":
    unittest.main()
