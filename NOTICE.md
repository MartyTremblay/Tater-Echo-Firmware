# Third-party notices

Tater Echo Firmware is MIT licensed. Release bundles also contain open-source
components and platform files under their own terms. Keep this notice and the
license files included in each factory archive with redistributed copies.

## Runtime components

| Component | Used for | License |
|---|---|---|
| SpeexDSP | Acoustic echo cancellation in `device/internal/aec/` | BSD-3-Clause |
| GoTinyAlsa | Native ALSA capture, playback, and mixer access in `device/third_party/GoTinyAlsa/` | BSD-3-Clause |
| TensorFlow Lite Micro, microfrontend, FlatBuffers, gemmlowp, ruy, and kissfft | microWakeWord runtime | Apache-2.0 and BSD-3-Clause |
| Microsoft ONNX Runtime 1.19.2 | openWakeWord and NanoWakeWord inference on all five targets | MIT |
| Eigen e7248b26 | ONNX Runtime build dependency | MPL-2.0 |
| openWakeWord feature models | openWakeWord feature extraction | Apache-2.0 |
| Sendspin player implementation | Synchronized media playback in `device/internal/sendspin/` | MIT |
| EchoLocal Radar ALSA byte-control helper | Writing Radar's byte-valued codec control in `device/internal/bindings/mixer/bytes_server.go` | MIT |

SpeexDSP is copyright Xiph.Org Foundation, Jean-Marc Valin, Analog Devices,
and CSIRO. GoTinyAlsa is copyright binozoworks; the vendored copy is based on
the `wilbowes/GoTinyAlsa` fork and includes local tinyalsa 2.x compatibility
fixes. The Sendspin implementation is copyright Wil Bowes. Applicable notices
are retained in the source tree and release bundles. Detailed microWakeWord
runtime attribution is recorded in
[`device/internal/wakeword/microwakeword/native/THIRD_PARTY_NOTICES.md`](device/internal/wakeword/microwakeword/native/THIRD_PARTY_NOTICES.md).

The small Radar ALSA byte-control layout and name-lookup helper was adapted
from the MIT-licensed EchoLocal Radar work by Vithurshan Selvarajah, based on
EchoLocal copyright Yuri Gelfand. Its notice remains in [`LICENSE`](LICENSE).
Radar's current routes, flat codec policy, software EQ, limiter, and playback
sequencing are Tater-native and do not run the EchoLocal application.

## Echo Linux platform

Tater uses the MIT-licensed [TECHO5](https://github.com/HuskerMinion/techo5)
platform work by HuskerMinion for Linux boot and rescue, device hardware
enablement, A/B rootfs tooling, and camera support. The exact platform source
varies by target:

- Biscuit and Radar use the pinned TECHO5 Dot platform and board kernels.
- Checkers and Crown use the pinned TECHO5 platform, boot images, and camera
  driver.
- Rook uses the pinned TECHO5 Spot platform plus the TECHO5 camera driver.

The build verifies each upstream input by commit and/or SHA-256. Its retained
licenses and upstream notices are embedded in the generated rootfs and factory
archive. The board-specific Linux kernels remain GPL-2.0 and are separate from
this repository's MIT-licensed application code.

## Build dependencies

Native libraries fetched by pinned build scripts retain their upstream terms.
All five targets use the same reduced Linux/musl ARMv7 ONNX Runtime built from
the hash-pinned 1.19.2 source archive. Release builds verify the runtime and all
built-in OWW/NWW classifiers under ARMv7 emulation before packaging. The exact
Eigen revision is also SHA-256 verified and remains under MPL-2.0.

## Wake-word models

Wake-word models retain the license and terms supplied by their publisher.
Adding a model to a release does not relicense it under this repository's MIT
license.

The timer-stop model is published by Kevin Ahrendt and pinned from the
Tater-Wake-Words model collection. Its publisher metadata remains in
`device/internal/wakeword/microwakeword/models/stop.json`.
