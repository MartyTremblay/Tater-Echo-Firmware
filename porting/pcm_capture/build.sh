#!/bin/sh
# Build pcm_capture for a stock ARMv7 Echo. This isolated Android diagnostic
# toolchain is not part of the Tater Linux firmware or release path.
set -eu
ROOT=$(git rev-parse --show-toplevel)
docker build -t tater-stock-android-capture "$ROOT/porting/pcm_capture"
docker run --rm --entrypoint bash \
    -e CGO_LDFLAGS="-Wl,--hash-style=both" \
    -v "$ROOT/porting/pcm_capture":/sdk \
    -v "$ROOT/device/third_party/GoTinyAlsa":/GoTinyAlsa \
    tater-stock-android-capture \
    -c "cd /sdk && go build -tags server -o pcm_capture ."
file "$ROOT/porting/pcm_capture/pcm_capture" 2>/dev/null || true
