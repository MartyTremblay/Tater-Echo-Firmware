from __future__ import annotations

import hashlib
import json
import lzma
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock

from tools import package_echo_release


REAL_REPO = Path(__file__).resolve().parents[1]


class PackageEchoReleaseTests(unittest.TestCase):
    @staticmethod
    def _copy(root: Path, relative: str) -> None:
        source = REAL_REPO / relative
        destination = root / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)

    @staticmethod
    def _elf(path: Path, elf_class: int, machine: int) -> None:
        header = bytearray(64)
        header[:4] = b"\x7fELF"
        header[4] = elf_class
        header[5] = 1  # little-endian
        header[6] = 1  # ELF version
        struct.pack_into("<H", header, 16, 2)  # ET_EXEC is sufficient here
        struct.pack_into("<H", header, 18, machine)
        path.write_bytes(header)

    @classmethod
    def _write_emos_elf_fixtures(cls, root: Path) -> None:
        for relative in (
            "device/build/server",
            "device/build/microwakeword-android/libtater_microwakeword.so",
            "device/build/onnxruntime/armeabi-v7a/libonnxruntime.so",
            "emos/build/init32",
            "emos/build/wpa/wpa_supplicant",
            "emos/build/wpa/wpa_cli",
            "emos/build/bb/busybox",
        ):
            cls._elf(root / relative, package_echo_release.ELFCLASS32,
                     package_echo_release.EM_ARM)
        cls._elf(root / "emos/build/init", package_echo_release.ELFCLASS64,
                 package_echo_release.EM_AARCH64)

    @staticmethod
    def _rook_boot(path: Path) -> None:
        ramdisk = lzma.compress(b"INSTALLING TATER\nTATER RECOVERY\nTATER LINUX\n")
        page = 2048
        header = b"ANDROID!" + struct.pack(
            "<9I",
            0, 0, len(ramdisk), 0, 0, 0, 0, page, 0,
        )
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(header.ljust(page, b"\0") + ramdisk)

    @staticmethod
    def _rook_rootfs(path: Path, version: str) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "etc/techo5").mkdir(parents=True)
            (root / "etc/tater-release").write_text(f"Tater Linux Rook {version}\n")
            (root / "etc/techo5/device.conf").write_text("STORE_DEV=/dev/mmcblk0p11\n")
            path.parent.mkdir(parents=True, exist_ok=True)
            with tarfile.open(path, "w:gz") as archive:
                archive.add(root / "etc", arcname="etc")

    def test_biscuit_factory_contains_the_verified_default_wake_pair(self) -> None:
        version = "v9.8.7"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "repo"
            output = Path(temporary) / "release"
            targets = root / "targets/targets.json"
            targets.parent.mkdir(parents=True)
            targets.write_text(json.dumps({"targets": {"biscuit": {
                "display_name": "Echo Dot",
                "factory_install": True,
                "ota": True,
                "status": "hardware-tested",
            }}}))
            inputs = (
                "device/build/server",
                "device/build/microwakeword-android/libtater_microwakeword.so",
                "device/build/onnxruntime/armeabi-v7a/libonnxruntime.so",
                "device/build/microwakeword-testdata/stop.tflite",
                "device/build/microwakeword-testdata/melspectrogram.onnx",
                "device/build/microwakeword-testdata/embedding_model.onnx",
                "device/internal/wakeword/microwakeword/models/hey_tater.tflite",
                "device/internal/wakeword/microwakeword/models/hey_tater.json",
                "device/internal/wakeword/microwakeword/models/hey_tater.oww.onnx",
                "device/internal/wakeword/microwakeword/models/hey_tater.oww.json",
                "device/internal/wakeword/microwakeword/models/hey_tater.wake-bundle.json",
                "device/internal/wakeword/microwakeword/models/stop.json",
                "factory/biscuit/install.sh",
                "factory/biscuit/install.py",
                "factory/biscuit/README.md",
                "factory/biscuit/tools/tater_emos_build.py",
                "factory/biscuit/payload/start_server.sh",
                "emos/build/init",
                "emos/build/init32",
                "emos/build/wpa/wpa_supplicant",
                "emos/build/wpa/wpa_cli",
                "emos/build/wpa/em-wifi",
                "emos/build/bb/busybox",
                "emos/build/bb/busybox-1.0.tar.bz2",
                "emos/build/bb/busybox-LICENSE",
                "emos/tools/build-busybox.sh",
                "LICENSE",
                "NOTICE.md",
            )
            for relative in inputs:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((relative + "\n").encode())
            self._write_emos_elf_fixtures(root)

            with (
                mock.patch.object(package_echo_release, "REPO", root),
                mock.patch.object(package_echo_release, "TARGETS", targets),
            ):
                package_echo_release.build(version, "biscuit", output)

            archive_path = output / f"tater-echo-biscuit-{version}-factory.tar.gz"
            prefix = f"tater-echo-biscuit-{version}-factory"
            with tarfile.open(archive_path, "r:gz") as archive:
                names = set(archive.getnames())
                for name in (
                    "hey_tater.tflite",
                    "hey_tater.json",
                    "hey_tater.oww.onnx",
                    "hey_tater.oww.json",
                    "hey_tater.wake-bundle.json",
                    "libonnxruntime.so",
                    "melspectrogram.onnx",
                    "embedding_model.onnx",
                ):
                    self.assertIn(f"{prefix}/payload/{name}", names)
                manifest = json.load(archive.extractfile(f"{prefix}/bundle-manifest.json"))
            for name in (
                "hey_tater.tflite",
                "hey_tater.oww.onnx",
                "hey_tater.wake-bundle.json",
            ):
                self.assertIn(f"payload/{name}", manifest["files"])

    def test_radar_reuses_the_verified_emos_factory_contract(self) -> None:
        version = "v9.8.7"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "repo"
            output = Path(temporary) / "release"
            targets = root / "targets/targets.json"
            targets.parent.mkdir(parents=True)
            targets.write_text(json.dumps({"targets": {"radar": {
                "display_name": "Echo 2",
                "factory_install": True,
                "ota": True,
                "status": "experimental",
            }}}))
            for relative in (
                "factory/radar/install.sh",
                "factory/radar/README.md",
                "factory/biscuit/install.py",
                "factory/biscuit/tools/tater_emos_build.py",
                "factory/biscuit/payload/start_server.sh",
                "emos/tools/build-busybox.sh",
                "LICENSE",
                "NOTICE.md",
            ):
                self._copy(root, relative)
            for relative in (
                "device/build/server",
                "device/build/microwakeword-android/libtater_microwakeword.so",
                "device/build/onnxruntime/armeabi-v7a/libonnxruntime.so",
                "device/build/microwakeword-testdata/stop.tflite",
                "device/build/microwakeword-testdata/melspectrogram.onnx",
                "device/build/microwakeword-testdata/embedding_model.onnx",
                "device/internal/wakeword/microwakeword/models/hey_tater.tflite",
                "device/internal/wakeword/microwakeword/models/hey_tater.json",
                "device/internal/wakeword/microwakeword/models/hey_tater.oww.onnx",
                "device/internal/wakeword/microwakeword/models/hey_tater.oww.json",
                "device/internal/wakeword/microwakeword/models/hey_tater.wake-bundle.json",
                "device/internal/wakeword/microwakeword/models/stop.json",
                "emos/build/init",
                "emos/build/init32",
                "emos/build/wpa/wpa_supplicant",
                "emos/build/wpa/wpa_cli",
                "emos/build/wpa/em-wifi",
                "emos/build/bb/busybox",
                "emos/build/bb/busybox-1.0.tar.bz2",
                "emos/build/bb/busybox-LICENSE",
            ):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((relative + "\n").encode())
            self._write_emos_elf_fixtures(root)

            with (
                mock.patch.object(package_echo_release, "REPO", root),
                mock.patch.object(package_echo_release, "TARGETS", targets),
            ):
                package_echo_release.build(version, "radar", output)

            archive_path = output / f"tater-echo-radar-{version}-factory.tar.gz"
            extract = Path(temporary) / "extract"
            with tarfile.open(archive_path, "r:gz") as archive:
                archive.extractall(extract)
            factory = extract / f"tater-echo-radar-{version}-factory"
            self.assertTrue((factory / "payload/init").is_file())
            self.assertTrue((factory / "payload/init32").is_file())
            result = subprocess.run(
                [str(factory / "install.sh"), "--verify-bundle"],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"Tater Echo Firmware {version} bundle verified", result.stdout)
            manifest = json.loads((output / "firmware-manifest.json").read_text())
            self.assertEqual(manifest["targets"]["radar"]["status"], "experimental")

    def test_rejects_a_host_busybox_before_packaging(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            busybox = Path(temporary) / "busybox"
            busybox.write_bytes(b"\xca\xfe\xba\xbe" + bytes(60))
            with self.assertRaisesRegex(SystemExit, "BusyBox must be a little-endian 32-bit ARM ELF"):
                package_echo_release.require_elf(
                    busybox,
                    package_echo_release.ELFCLASS32,
                    package_echo_release.EM_ARM,
                    "BusyBox",
                )

    def test_crown_uses_shared_show_factory_with_target_specific_boot(self) -> None:
        version = "v2.3.0"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "repo"
            output = Path(temporary) / "release"
            targets = root / "targets/targets.json"
            targets.parent.mkdir(parents=True)
            targets.write_text(json.dumps({"targets": {"crown": {
                "display_name": "Echo Show 8 1st Gen",
                "factory_install": True,
                "ota": True,
                "status": "experimental",
            }}}))

            for relative in (
                "factory/checkers-linux/install.sh",
                "factory/checkers-linux/install.py",
                "factory/checkers-linux/tools/techo5/install-show.py",
                "factory/checkers-linux/tools/techo5/techo5lib.py",
                "factory/checkers-linux/tools/techo5/LICENSE",
                "factory/checkers-linux/tools/techo5/boot-logo/cronos-wordmark.bin.b64",
                "factory/crown-linux/README.md",
                "linux/checkers/provision_console.py",
                "linux/checkers/brand_boot.py",
                "LICENSE",
                "NOTICE.md",
            ):
                self._copy(root, relative)

            build = root / "device/build"
            build.mkdir(parents=True)
            (build / "tater-echo").write_bytes(b"crown-daemon")
            (build / "tater-show-linux").write_bytes(b"crown-screen")
            (build / "tater-camera").write_bytes(b"crown-camera")
            (build / "tater-rescue-fbprobe").write_bytes(b"arm-renderer")
            runtime = build / "microwakeword-linux/libtater_microwakeword.so"
            runtime.parent.mkdir(parents=True)
            runtime.write_bytes(b"crown-wake-runtime")
            ort = build / "onnxruntime-linux-armv7/libonnxruntime.so"
            ort.parent.mkdir(parents=True)
            ort.write_bytes(b"crown-onnx-runtime")
            rootfs = build / f"tater-crown-rootfs-{version}.tar.gz"
            rootfs.write_bytes(b"crown-rootfs")
            boot = root / "linux/checkers/inputs/techo5-boot-crown-v1.0.1.img"
            boot.parent.mkdir(parents=True)
            boot.write_bytes(b"pinned-crown-boot")

            def brand(command, **_kwargs):
                self.assertEqual(["--target", "crown"], command[2:4])
                Path(command[-1]).write_bytes(b"branded-crown-boot")
                return subprocess.CompletedProcess(command, 0)

            with (
                mock.patch.object(package_echo_release, "REPO", root),
                mock.patch.object(package_echo_release, "TARGETS", targets),
                mock.patch.object(package_echo_release.subprocess, "run", side_effect=brand),
                mock.patch.dict(os.environ, {
                    "TATER_CROWN_ROOTFS": str(rootfs),
                    "TATER_CROWN_BOOT_IMAGE": str(boot),
                    "TATER_LINUX_ROOTFS": "",
                    "TATER_LINUX_BOOT_IMAGE": "",
                }),
            ):
                artifacts = package_echo_release.build(version, "crown", output)

            names = {path.name for path in artifacts}
            self.assertIn(f"tater-echo-crown-{version}-factory.tar.gz", names)
            self.assertIn(f"tater-echo-crown-{version}-ota.tar.gz", names)
            self.assertIn(f"tater-echo-crown-{version}-ota-bridge.tar.gz", names)
            with tarfile.open(
                output / f"tater-echo-crown-{version}-ota.tar.gz", "r:gz"
            ) as archive:
                self.assertEqual(
                    set(archive.getnames()),
                    {"manifest.json", "tater-echo", "tater-show", "tater-camera"},
                )
            with tarfile.open(
                output / f"tater-echo-crown-{version}-ota-bridge.tar.gz", "r:gz"
            ) as archive:
                self.assertEqual(
                    set(archive.getnames()), {"manifest.json", "tater-echo", "tater-show"}
                )
            release_manifest = json.loads((output / "firmware-manifest.json").read_text())
            self.assertEqual(release_manifest["targets"]["crown"]["ota_migrations"], [{
                "id": "linux-app-components-v1",
                "required_capability": "ota_bundle_components_version",
                "minimum_capability_version": 1,
                "bridge_artifact": "ota_bridge",
                "final_artifact": "ota",
            }])
            extract = Path(temporary) / "extract"
            with tarfile.open(
                output / f"tater-echo-crown-{version}-factory.tar.gz", "r:gz"
            ) as archive:
                prefix = f"tater-echo-crown-{version}-factory"
                self.assertEqual(
                    b"branded-crown-boot",
                    archive.extractfile(f"{prefix}/payload/boot.img").read(),
                )
                self.assertIn("Echo Show 8", archive.extractfile(
                    f"{prefix}/README.md").read().decode())
                logo = archive.extractfile(
                    f"{prefix}/tools/techo5/boot-logo/cronos-wordmark.bin"
                ).read()
                self.assertEqual(package_echo_release.SHOW_BOOT_LOGO_SIZE, len(logo))
                self.assertEqual(
                    package_echo_release.SHOW_BOOT_LOGO_SHA256,
                    hashlib.sha256(logo).hexdigest(),
                )
                manifest = json.load(archive.extractfile(f"{prefix}/bundle-manifest.json"))
                archive.extractall(extract)
            self.assertEqual("crown", manifest["target"])
            self.assertEqual("tater-linux", manifest["base_os"])
            verified = subprocess.run(
                [str(extract / prefix / "install.sh"), "--verify-bundle"],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(0, verified.returncode, verified.stderr)
            self.assertIn(f"Bundle verified: Crown Tater Linux {version}", verified.stdout)

    def test_rook_factory_and_ota_bundle_verify_end_to_end(self) -> None:
        version = "v2.1.0"
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / "repo"
            output = Path(temporary) / "release"
            targets = root / "targets/targets.json"
            targets.parent.mkdir(parents=True)
            targets.write_text(json.dumps({"targets": {"rook": {
                "display_name": "Echo Spot",
                "factory_install": True,
                "ota": True,
                "status": "hardware-tested",
            }}}))

            for relative in (
                "factory/rook-linux/install.sh",
                "factory/rook-linux/install.py",
                "factory/rook-linux/README.md",
                "factory/checkers-linux/tools/techo5/techo5lib.py",
                "factory/checkers-linux/tools/techo5/LICENSE",
                "LICENSE",
                "NOTICE.md",
            ):
                self._copy(root, relative)

            build = root / "device/build/rook"
            build.mkdir(parents=True)
            (build / "tater-echo").write_bytes(b"rook-daemon-v2.1.0")
            (build / "tater-show").write_bytes(b"rook-screen-v2.1.0")
            (build / "tater-camera").write_bytes(b"rook-camera-v2.1.0")
            runtime = build / "microwakeword/libtater_microwakeword.so"
            runtime.parent.mkdir(parents=True)
            runtime.write_bytes(b"rook-wake-runtime-v2.1.0")
            ort = root / "device/build/onnxruntime-linux-armv7/libonnxruntime.so"
            ort.parent.mkdir(parents=True)
            ort.write_bytes(b"rook-onnx-runtime-v2.1.0")
            self._rook_boot(build / "tater-rook-boot.img")
            rootfs = build / f"tater-rook-rootfs-{version}.tar.gz"
            self._rook_rootfs(rootfs, version)

            with (
                mock.patch.object(package_echo_release, "REPO", root),
                mock.patch.object(package_echo_release, "TARGETS", targets),
                mock.patch.dict(os.environ, {
                    "TATER_ROOK_ROOTFS": str(rootfs),
                    "TATER_ROOK_BOOT_IMAGE": "",
                }),
            ):
                artifacts = package_echo_release.build(version, "rook", output)

            names = {path.name for path in artifacts}
            self.assertIn(f"tater-echo-rook-{version}-factory.tar.gz", names)
            self.assertIn(f"tater-echo-rook-{version}-ota.tar.gz", names)
            self.assertIn(f"tater-echo-rook-{version}-ota-bridge.tar.gz", names)
            self.assertIn(f"tater-echo-rook-{version}-wake-runtime.so", names)
            self.assertIn(f"tater-echo-rook-{version}-onnxruntime.so", names)

            manifest = json.loads((output / "firmware-manifest.json").read_text())
            self.assertEqual(manifest["version"], version)
            self.assertEqual(set(manifest["targets"]), {"rook"})
            self.assertEqual(manifest["targets"]["rook"]["status"], "hardware-tested")

            factory_archive = output / f"tater-echo-rook-{version}-factory.tar.gz"
            extract = Path(temporary) / "extract"
            with tarfile.open(factory_archive, "r:gz") as archive:
                installer = archive.getmember(f"tater-echo-rook-{version}-factory/install.sh")
                self.assertEqual(installer.mode & 0o777, 0o755)
                archive.extractall(extract)
            factory = extract / f"tater-echo-rook-{version}-factory"
            result = subprocess.run(
                [str(factory / "install.sh"), "--verify-bundle"],
                capture_output=True,
                text=True,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"Verified Rook factory bundle {version}", result.stdout)

            with tarfile.open(output / f"tater-echo-rook-{version}-ota.tar.gz", "r:gz") as archive:
                self.assertEqual(
                    set(archive.getnames()),
                    {"manifest.json", "tater-echo", "tater-show", "tater-camera"},
                )
            with tarfile.open(
                output / f"tater-echo-rook-{version}-ota-bridge.tar.gz", "r:gz"
            ) as archive:
                self.assertEqual(
                    set(archive.getnames()), {"manifest.json", "tater-echo", "tater-show"}
                )
            self.assertEqual(manifest["targets"]["rook"]["ota_migrations"], [{
                "id": "linux-app-components-v1",
                "required_capability": "ota_bundle_components_version",
                "minimum_capability_version": 1,
                "bridge_artifact": "ota_bridge",
                "final_artifact": "ota",
            }])


if __name__ == "__main__":
    unittest.main()
