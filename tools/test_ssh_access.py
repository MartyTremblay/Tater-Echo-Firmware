from __future__ import annotations

import os
from pathlib import Path
import stat
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
AUTHORIZE = ROOT / "linux/common/tater-ssh-authorize"
SUPERVISOR = ROOT / "linux/common/tater-sshd"
PUBLIC_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestPublicKeyMaterialOnly laptop"
STORED_KEY = "no-agent-forwarding,no-X11-forwarding,no-port-forwarding " + PUBLIC_KEY


class SSHAuthorizeTests(unittest.TestCase):
    def environment(self, root: Path) -> dict[str, str]:
        environment = os.environ.copy()
        environment.update({
            "TATER_SSH_DIR": str(root / "ssh"),
            "TATER_SSH_STATE": str(root / "run/tater-sshd.state"),
            "TATER_SSH_PID_FILE": str(root / "run/tater-sshd.pid"),
        })
        return environment

    def run_authorize(
        self, root: Path, *arguments: str, input_text=None
    ) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["sh", str(AUTHORIZE), *arguments],
            env=self.environment(root),
            input=input_text,
            capture_output=True,
            text=True,
            check=False,
        )

    def test_add_is_unique_and_disable_enable_is_recoverable(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            first = self.run_authorize(root, "add", PUBLIC_KEY)
            duplicate = self.run_authorize(root, "add", PUBLIC_KEY)
            status_result = self.run_authorize(root, "status")
            keys = root / "ssh/authorized_keys"

            self.assertEqual(0, first.returncode, first.stderr)
            self.assertEqual(0, duplicate.returncode, duplicate.stderr)
            self.assertIn("already authorized", duplicate.stdout)
            self.assertEqual(STORED_KEY + "\n", keys.read_text())
            self.assertEqual(0o600, stat.S_IMODE(keys.stat().st_mode))
            self.assertEqual(0o700, stat.S_IMODE(keys.parent.stat().st_mode))
            self.assertIn("enabled keys=1", status_result.stdout)

            disabled = self.run_authorize(root, "disable")
            self.assertEqual(0, disabled.returncode, disabled.stderr)
            self.assertFalse(keys.exists())
            self.assertEqual(STORED_KEY + "\n", (root / "ssh/authorized_keys.disabled").read_text())
            self.assertNotEqual(0, self.run_authorize(root, "add", PUBLIC_KEY).returncode)

            enabled = self.run_authorize(root, "enable")
            self.assertEqual(0, enabled.returncode, enabled.stderr)
            self.assertEqual(STORED_KEY + "\n", keys.read_text())

    def test_add_accepts_stdin_and_rejects_non_keys(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            added = self.run_authorize(root, "add", input_text=PUBLIC_KEY + "\n")
            self.assertEqual(0, added.returncode, added.stderr)
            invalid = self.run_authorize(root, "add", "password hunter2")
            self.assertNotEqual(0, invalid.returncode)
            self.assertIn("unsupported public-key type", invalid.stderr)


class SSHSupervisorTests(unittest.TestCase):
    def write_executable(self, path: Path, contents: str) -> None:
        path.write_text(contents)
        path.chmod(0o755)

    def environment(self, root: Path) -> dict[str, str]:
        environment = os.environ.copy()
        environment.update({
            "TATER_SSH_DIR": str(root / "ssh"),
            "TATER_SSH_STATE": str(root / "run/tater-sshd.state"),
            "TATER_SSH_LOG": str(root / "data/sshd.log"),
            "TATER_SSH_PID_FILE": str(root / "run/tater-sshd.pid"),
            "TATER_DROPBEAR": str(root / "bin/dropbear"),
            "TATER_DROPBEARKEY": str(root / "bin/dropbearkey"),
            "TATER_SSH_RUN_ONCE": "1",
            "TATER_TEST_ARGS": str(root / "dropbear.args"),
        })
        return environment

    def make_stubs(self, root: Path) -> None:
        (root / "bin").mkdir(parents=True)
        self.write_executable(
            root / "bin/dropbearkey",
            """#!/bin/sh
mode=$1
shift
key=
while [ "$#" -gt 0 ]; do
    if [ "$1" = -f ]; then key=$2; shift 2; else shift; fi
done
case "$mode" in
-t) printf 'test-ed25519-host-key\n' > "$key" ;;
-y) grep -q '^test-ed25519-host-key$' "$key" && printf 'Public key portion is:\nssh-ed25519 TEST\n' ;;
*) exit 2 ;;
esac
""",
        )
        self.write_executable(
            root / "bin/dropbear",
            """#!/bin/sh
printf '%s\n' "$@" > "$TATER_TEST_ARGS"
""",
        )

    def test_no_authorized_key_keeps_listener_disabled(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_stubs(root)
            result = subprocess.run(
                ["sh", str(SUPERVISOR)],
                env=self.environment(root),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertIn("disabled waiting-for-authorized-key", (root / "run/tater-sshd.state").read_text())
            self.assertFalse((root / "dropbear.args").exists())
            self.assertFalse((root / "ssh/dropbear_ed25519_host_key").exists())

    def test_key_enables_hardened_dropbear_with_persistent_host_key(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_stubs(root)
            ssh_dir = root / "ssh"
            ssh_dir.mkdir()
            (ssh_dir / "authorized_keys").write_text(STORED_KEY + "\n")
            environment = self.environment(root)

            first = subprocess.run(
                ["sh", str(SUPERVISOR)], env=environment, capture_output=True, text=True, check=False
            )
            self.assertEqual(0, first.returncode, first.stderr)
            host_key = ssh_dir / "dropbear_ed25519_host_key"
            self.assertEqual("test-ed25519-host-key\n", host_key.read_text())
            self.assertEqual(0o600, stat.S_IMODE(host_key.stat().st_mode))
            arguments = (root / "dropbear.args").read_text().splitlines()
            for flag in ("-F", "-E", "-s", "-g", "-j", "-k"):
                self.assertIn(flag, arguments)
            self.assertEqual("tater-ssh", arguments[arguments.index("-G") + 1])
            self.assertEqual(str(ssh_dir), arguments[arguments.index("-D") + 1])
            self.assertEqual(str(host_key), arguments[arguments.index("-r") + 1])
            self.assertEqual("22", arguments[arguments.index("-p") + 1])
            self.assertIn("active port=22", (root / "run/tater-sshd.state").read_text())

            second = subprocess.run(
                ["sh", str(SUPERVISOR)], env=environment, capture_output=True, text=True, check=False
            )
            self.assertEqual(0, second.returncode, second.stderr)
            self.assertEqual("test-ed25519-host-key\n", host_key.read_text())

    def test_invalid_existing_host_key_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.make_stubs(root)
            ssh_dir = root / "ssh"
            ssh_dir.mkdir()
            (ssh_dir / "authorized_keys").write_text(STORED_KEY + "\n")
            (ssh_dir / "dropbear_ed25519_host_key").write_text("not-a-key\n")
            result = subprocess.run(
                ["sh", str(SUPERVISOR)],
                env=self.environment(root),
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("error existing Ed25519 host key is invalid", (root / "run/tater-sshd.state").read_text())
            self.assertFalse((root / "dropbear.args").exists())


if __name__ == "__main__":
    unittest.main()
