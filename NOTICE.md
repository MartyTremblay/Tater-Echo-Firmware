# Third-party notices

Tater Echo Firmware is MIT licensed. The firmware and release bundles also
contain or link the components below under their own terms. Keep this notice
with redistributed binaries and factory bundles.

## Firmware components

| Component | Location | License |
|---|---|---|
| SpeexDSP acoustic echo canceller | `device/internal/aec/` | BSD-3-Clause |
| GoTinyAlsa | `device/third_party/GoTinyAlsa/` | BSD-3-Clause |
| TFLite Micro and microfrontend dependencies | `device/internal/wakeword/microwakeword/native/` build | Apache-2.0 and BSD-3-Clause |
| Microsoft ONNX Runtime 1.19.2 | Biscuit, Radar, Checkers, Crown, and Rook openWakeWord runtime | MIT |
| Eigen e7248b26 | Linux ARMv7 ONNX Runtime build dependency | MPL-2.0 |
| openWakeWord ONNX feature models | Biscuit, Radar, Checkers, Crown, and Rook openWakeWord runtime | Apache-2.0 |
| echolocal Radar hardware research | Radar codec, amplifier-gate, speaker filter, output-route, and ALSA-control integration | MIT (Copyright (c) 2026 Yuri Gelfand; Radar port by Vithurshan Selvarajah) |
| Sendspin player implementation | `device/internal/sendspin/` | MIT (Copyright (c) 2026 Wil Bowes) |
| TECHO5 Dot Biscuit/Radar Linux boot, Wi-Fi/Bluetooth enablement, Radar vendor-audio path handling, and rootfs A/B tooling | `linux/dot/`, `device/internal/bindings/codec/radar_tuning.go`, Radar speaker routing, generated rootfs, Biscuit/Radar factory bundles | MIT |
| TECHO5 Checkers/Crown Linux tooling, camera driver, and direct ALSA speaker path | `linux/checkers/`, `device/internal/checkersalsa/`, generated rootfs, factory bundles | MIT |
| TECHO5 Spot Rook Linux hardware enablement and rescue platform | `linux/rook/`, generated rootfs, Rook factory bundle | MIT |
| Linux kernel in the pinned TECHO5 Checkers and Crown boot images | Checkers/Crown factory bundle `payload/boot.img` | GPL-2.0 |
| Linux kernel in the pinned TECHO5 Spot Rook boot image | Rook factory bundle `payload/boot.img` | GPL-2.0 |
| Linux kernels in the pinned TECHO5 Dot Biscuit/Radar images | Biscuit/Radar factory bundle `payload/kernel.zImage-dtb` | GPL-2.0 |

SpeexDSP is copyright Xiph.Org Foundation, Jean-Marc Valin, Analog Devices,
and CSIRO. The Sendspin implementation is copyright Wil Bowes; its MIT notice
is preserved in `LICENSE`. GoTinyAlsa is copyright binozoworks; this repository uses the
`wilbowes/GoTinyAlsa` fork containing a stream leak fix, with local tinyalsa
2.x compatibility fixes retained in the vendored copy. Checkers and Crown
Linux hardware enablement, rescue/A-B contract, framebuffer research, camera
driver, and direct ALSA speaker path are
derived from TECHO5 by HuskerMinion; its license is embedded in the generated
rootfs and factory bundle and included with the speaker source. Detailed wake-runtime
attribution is in
[`device/internal/wakeword/microwakeword/native/THIRD_PARTY_NOTICES.md`](device/internal/wakeword/microwakeword/native/THIRD_PARTY_NOTICES.md).

Radar's TLV320AIC3204 MFP2 amplifier gate, analog route, vendor speaker filter,
and byte-control integration follow the independently hardware-tested `radar`
branch of echolocal by Vithurshan Selvarajah, based on the MIT-licensed
echolocal project, Copyright (c) 2026 Yuri Gelfand. Its copyright notice is
retained in `LICENSE`.

Rook Linux hardware enablement and rescue behavior derive from TECHO5 Spot by
HuskerMinion. Its MIT license is embedded in the generated Rook rootfs and
factory bundle. The Rook camera helper derives from TECHO5's MIT-licensed
camera driver.

The Checkers and Crown boot images contain Linux 4.9.337 kernels rebuilt by TECHO5
from [Amazon's published kernel source](https://github.com/amazon-oss/android_kernel_amazon_mt8163)
with the Checkers changes described in [TECHO5's kernel notice](https://github.com/HuskerMinion/techo5/blob/main/NOTICE).
Its GPL-2.0 terms are separate from this repository's MIT license.
The Rook boot image likewise contains the TECHO5 Spot Linux 4.9.337 kernel;
its GPL-2.0 terms remain separate from this repository's MIT license.

Biscuit and Radar Linux boot, MediaTek Wi-Fi/Bluetooth enablement, recovery,
and rootfs slot management derive from TECHO5 Dot by HuskerMinion. Its MIT
license and complete upstream notice are embedded in each generated rootfs and
factory bundle. The board-specific Linux kernels retain GPL-2.0 terms.

## Build dependencies

Native runtime libraries fetched by the pinned build scripts retain their
upstream terms. Microsoft ONNX Runtime is distributed under the MIT license;
all five targets use the same reduced Linux/musl ARMv7 runtime from the
hash-pinned 1.19.2 source archive and execute all three OWW models under ARMv7
emulation before packaging it. The build also verifies ONNX Runtime's exact
Eigen revision by SHA-256; Eigen is used under its MPL-2.0 terms.

## Wake-word models

Wake-word models retain the license and terms supplied by their publisher.
Adding a model to a release does not relicense it under this repository's MIT
license.

The timer-stop model is published by Kevin Ahrendt and is pinned from the
Tater-Wake-Words model collection. Its publisher metadata remains in
`device/internal/wakeword/microwakeword/models/stop.json`.
