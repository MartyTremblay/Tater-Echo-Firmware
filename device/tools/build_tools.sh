#!/bin/sh
set -eu

repo=$(git rev-parse --show-toplevel)
device=$repo/device
image=${TATER_FIRMWARE_BUILDER_IMAGE:-$(python3 "$repo/tools/platform_lock.py" builder-reference)}
cache=${TATER_BUILD_CACHE:-/tmp/tater-diagnostic-tools-cache}
mkdir -p "$device/build" "$cache/go-build" "$cache/go-mod"

docker run --rm --platform linux/arm/v7 \
    -e GOTOOLCHAIN=local \
    -v "$repo:/src" \
    -v "$device/third_party/GoTinyAlsa:/GoTinyAlsa" \
    -v "$cache/go-build:/root/.cache/go-build" \
    -v "$cache/go-mod:/root/go/pkg/mod" \
    -w /src/device "$image" sh -lc '
set -eu
for tool in capture_mics bf_capture; do
    cd /src/device/tools/$tool
    go build -trimpath -tags server -o /src/device/build/$tool .
done
cd /src/device
CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 \
    go build -trimpath -tags server -o build/mixer_probe ./tools/mixer_probe
chmod 0755 build/capture_mics build/bf_capture build/mixer_probe
'

file "$device/build/capture_mics" "$device/build/bf_capture" "$device/build/mixer_probe"
for tool in capture_mics bf_capture mixer_probe; do
    file "$device/build/$tool" | grep -q '32-bit.*ARM'
done
echo "Tater Linux diagnostic tools are in $device/build"
