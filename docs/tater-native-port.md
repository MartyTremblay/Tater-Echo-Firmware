# Tater-native Echo architecture

Tater Echo Firmware makes supported Echo hardware behave like the other
Tater-native satellites: wake detection and audio handling run locally, while
Tater remains the authority for turns, settings, groups, media, and updates.

## Runtime layout

```text
Echo microphones
  -> target-specific capture and beam selection
  -> echo cancellation and signal conditioning
  -> one or two 16 kHz mono wake beams
       |-> microWakeWord only
       |-> openWakeWord only
       |-> NanoWakeWord CNN or GRU only
       |-> MWW + OWW or MWW + NWW agreement
       |-> winning-beam verifier and trainer pre-roll
       `-> winning-beam STT pre-roll, then the locked live beam

Tater
  -> reply/media audio, timers, settings, LED/display state, and OTA
  -> Echo speaker, controls, LED ring, or native Echo display
```

Microphone capture never waits on inference or networking. Wake inference runs
on its own bounded queue; a discontinuity resets the streaming frontend before
scoring continues. Each array lane retains a bounded timestamped audio ring.
When a lane crosses, its exact post-AEC/high-pass audio is frozen and used by
the optional STT wake verifier, wake-audio trainer upload, and the first two seconds sent to speech
recognition. The live microphone then locks to that same acoustic path.
Degraded and non-array capture safely retains the shared mono pre-roll.

## Wake engine

The runtime accepts the model format produced by `Tater-Wake-Words`:

- 16 kHz mono PCM;
- 30 ms feature windows advanced every 10 ms;
- 40 microfrontend features;
- two feature frames per model invocation;
- retained int8 streaming state; and
- manifest thresholds and sliding-window policy, including `tater_native`
  overrides.

`device/internal/wakeword/microwakeword` validates that contract. The native
TFLite Micro runtime is an ARM library loaded through a versioned C ABI. A
missing or incompatible model/runtime disables local wake without preventing
the rest of the satellite from booting.

Trainer bundles can include microWakeWord, an independent openWakeWord ONNX
classifier, and NanoWakeWord CNN/GRU classifiers. All five Echo targets use the
same pinned Linux/musl ARMv7 ONNX Runtime/XNNPACK path. OWW TFLite is not part
of the release contract.

Tater can select MWW, OWW, or NWW alone, or pair MWW with OWW/NWW for dual
confirmation. Enabled engines run continuously on the same wake beams. In dual
mode their captured crossing timestamps must fall inside a bounded window on
the same lane and physical beam direction before Tater opens the microphone.
Either model may cross first, so agreement adds no replay stage. Single-engine
OWW/NWW modes feed wake arbitration, pre-roll, and the optional STT verifier
directly. They never require both physical beams to agree. An explicitly
enabled but missing or invalid OWW/NWW model fails closed and is visible in
wake telemetry; selecting MWW restores the established MWW-only behavior. The
temporary timer `stop` model stays MWW-only so dismissing a ringing timer
remains immediate.

The built-in Hey Tater choice is a verified matched bundle containing the
current MWW, OWW, NWW-CNN, and NWW-GRU classifiers. It is embedded in the
daemon for OTA-safe installation and also copied directly by every factory
installer. Custom wake words use the same bundle contract, which prevents dual
mode from pairing two different phrases.

Each installation carries:

```text
/data/local/share/tater/microwakeword/
  libtater_microwakeword.so
  libonnxruntime.so
  melspectrogram.onnx
  embedding_model.onnx
  hey_tater.json
  hey_tater.tflite
  hey_tater.oww.json
  hey_tater.oww.onnx
  hey_tater.nww.cnn.onnx
  hey_tater.nww.gru.onnx
  hey_tater.wake-bundle.json
  stop.json
  stop.tflite
```

Wake engines, models, optional companion classifiers, sounds, sensitivity,
verification policy, and trainer behavior can change live from Tater. A little
over three seconds of each candidate beam remains available to the trainer,
verifier, and STT paths. The final two seconds protect the first command word
while the server acknowledges and arbitrates a wake heard by multiple rooms.

## Target integrations

### Biscuit and Radar

Biscuit and Radar boot Tater Linux from recovery on the pinned TECHO5 Dot
hardware platform. The TWRP factory installer saves verified copies of the
unit's boot-critical partitions, combines the unit's TWRP boot header with the
target-matched Linux kernel, writes that image to recovery, and installs the
initial A/B rootfs under cache. The stock system remains available as the
firmware and audio-coefficient donor; it is not the running userspace.

The seven-mic capture, hardware playback reference, two-lane beamformed wake
frontend, seven-microphone steered speech beam, and audio boundaries are documented in
[`biscuit-hardware.md`](biscuit-hardware.md).

### Checkers and Crown

Checkers and Crown run a native Alpine rootfs on target-matched, pinned TECHO5
kernel/rescue platforms. Tater owns audio and BLE directly; `tater-show` owns
framebuffer and touch, and a loopback-only camera helper owns the MediaTek ISP.
The former system partition is an A/B rootfs store, persistent Tater state
remains on userdata, and TWRP stays in recovery.

Checkers' two live microphone channels are level-calibrated and coherently
combined. Crown uses four microphones and four steering directions. Their
winning beam and pre-roll carry through verification, training, and STT.
Continued chat acquires from the user's speech onset, then freezes steering so
competing dialogue cannot pull pickup toward a new direction.

See [`checkers.md`](checkers.md), [`crown.md`](crown.md), and their matching
factory installer guides.

### Rook

Rook (the 2017 Echo Spot) runs Tater Linux with a native 480×480 round display,
touch, BLE presence, and a loopback-only camera helper. Its measured PCM22
layout has four microphones followed by two playback-loopback channels. The
microphones form a compact square in front-right, rear-right, rear-left,
front-left channel order.

Rook continuously evaluates four geometry-correct fractional-delay beams and
scores the two strongest with independent microWakeWord state. The accepted
wake beam is held for STT; continued chat reacquires from its own speech onset
through the same path. Its unlocked four-mic mix and four steered paths keep
independent AEC state, and microphone-health fallback excludes failed channels
without inventing a bearing.

See [`../linux/rook/README.md`](../linux/rook/README.md) and
[`../factory/rook-linux/README.md`](../factory/rook-linux/README.md).

## Setup and credentials

An unconfigured Echo starts `Tater-Setup-XXXX`. Its captive portal stores Wi-Fi
and a one-time Tater pairing code. USB pairing is also available during factory
installation. After the first accepted hello, the permanent device credential
is written atomically with mode `0600` and used on later boots.

The native bootstrap lives under `/data/local/etc/tater`. A satellite can
return to setup through Tater or with the supported physical reset gesture:
five quick Mute/Action presses followed by holding the sixth for five seconds.
The same gesture remains active while the captive setup screen is already
running, so submitting bad Wi-Fi or pairing information cannot trap the unit.
Pairing and Wi-Fi state live outside both rootfs and application slots.

## Audio and device behavior

The native protocol supports:

- local wake, pre-roll, continued conversation, barge-in, intercom, optional
  STT wake verification, and trainer samples;
- TTS, announcements, timers, volume, mute, and live wake sounds;
- a discoverable Sendspin v1 player with PCM/FLAC decoding, encrypted Music
  Assistant sessions, Tater's plaintext transport profile, persistent
  left/right stereo-pair routing, clock correction, and underrun recovery;
- native TTS overlays, local audio scenes, and sample-counted music ducking;
- Tater-selected LED/display animations and tool progress; and
- bounded BLE advertisement batches for Tater presence; and
- active BLE GATT discovery, reads, writes, notifications, indications,
  disconnects, and persistent authenticated six-digit-PIN bonds for services
  such as Meshtastic Core. The PIN is not stored, unpairing removes the bond,
  and all control stays inside the authenticated native satellite connection.

Capabilities are negotiated explicitly. Unknown fields are ignored safely so
mixed firmware versions can remain online during rollout.

### Native display dashboard

Checkers and Crown share one responsive dashboard renderer sized for their
960×480 and 1280×800 panels. Rook uses a dedicated round-safe 480×480 layout
with a large centered clock and weather hero, larger room readings, and the
assistant's live state beside the room name. Every screen target keeps Tater's
selected display theme and provides Home, Climate, Music, Voice, Sat Chat,
Room Vision, and device-status surfaces; Calendar is not part of the native
satellite UI.

Tater supplies a compact `display.home` snapshot built from its normalized
integration device registry. Climate controls send room/category/action
requests back to Tater, which validates them and routes them through the owning
integration. The display therefore has no Home Assistant-specific service or
entity logic. Camera imagery remains event-driven through the existing
Awareness notification path rather than becoming a continuously loaded camera
dashboard.

Awareness popups are deliberately modal but never sticky: the first touch
clears the local frame, returns to Home, acknowledges the event sequence, and
temporarily suppresses the remainder of that notification burst. The paired
touch-up is consumed so it cannot accidentally activate a Home control.

Screen targets advertise `screen_notifications_version: 2` for the generic
`display.notification` card contract. Senders may provide `display_label` for
the card heading and `image_fit` as `contain` or `cover`; otherwise the renderer
derives a readable heading from `kind` and applies a layout-appropriate image
fit. `title`, `description`, `source`, priority and TTL remain generic metadata,
while `image_data_b64` plus `image_content_type` carries an inline image through
the existing authenticated satellite connection. Unknown kinds and fields stay
safe, so new Verbas and cores can reuse the card without a firmware-specific
layout or protocol change.

## OTA

Every target uses the same fast application transaction on writable `/data`:
it verifies a deterministic bundle, stages the files into the inactive slot,
then switches the shared active link atomically. Biscuit and Radar bundles
contain the daemon; screen targets also contain the renderer and optional
camera helper. Repeated fast exits or a failed health window restores the
preceding application slot. The immutable Linux rootfs changes only through
USB/recovery when the platform itself changes.

The artifact contract is documented in
[`firmware-releases.md`](firmware-releases.md).

## Validation

Host tests cover the Tater protocol, wake frontend/model golden vectors, audio
state machines, OTA rollback, installer guards, Tater Linux rootfs/factory
assembly, native screen/input packages, Wi-Fi setup, and BLE HCI behavior.
GitHub Actions cross-builds every target and assembles the target-matched Linux
rootfs and installer artifacts.

Hardware release checks still include wake and first-word retention, barge-in,
Sendspin group synchronization, BLE presence, physical recovery gestures, clean/cold
boots, a successful OTA, and one deliberate rollback.

## Guardrails

- Never block capture or playback timing on inference or networking.
- Treat models, configuration, downloads, and manifests as untrusted input.
- Keep partition writes target-specific, verified, and recoverable.
- Do not copy boot layouts, mixer settings, mic maps, or root images between
  Echo codenames.
- Compiler or boot-tool changes require a real-device recovery test before a
  release.
