#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
    echo "usage: $0 <techo5 checkout> <output>" >&2
    exit 2
fi

checkout=$1
output_dir=$(dirname "$2")
mkdir -p "$output_dir"
output=$(cd "$output_dir" && pwd)/$(basename "$2")
expected=0f042360fbc22d798930cfd683d251606bfb7412
actual=$(git -C "$checkout" rev-parse HEAD)
[ "$actual" = "$expected" ] || {
    echo "TECHO5 checkout is $actual, expected $expected" >&2
    exit 1
}

command_dir=$checkout/echod/cmd/tater-rook-camera
mkdir -p "$command_dir"
cp "$(dirname "$0")/camera-helper/main.go" "$command_dir/main.go"
cp "$(dirname "$0")/camera-helper/main_test.go" "$command_dir/main_test.go"
camera_dir=$checkout/echod/internal/hardware/camera
cp "$(dirname "$0")/camera-helper/tater_video_spot.go" "$camera_dir/tater_video_spot.go"
cp "$(dirname "$0")/camera-helper/tater_video_spot_test.go" "$camera_dir/tater_video_spot_test.go"

# TECHO5 normally develops every second sensor frame because its standard path
# expands each 640x480 Bayer frame before scaling it. Tater's echo-low path
# develops directly to 320x180, so it is cheap enough to consume every sensor
# frame. Patch the pinned checkout only for this build and always put it back.
camera_driver=$camera_dir/camera_spot.go
driver_backup=$(mktemp "${TMPDIR:-/tmp}/tater-rook-camera.XXXXXX")
cp "$camera_driver" "$driver_backup"
restore_driver() {
    cp "$driver_backup" "$camera_driver"
    rm -f "$driver_backup"
}
trap restore_driver EXIT HUP INT TERM
matches=$(grep -c '^[[:space:]]*convertEvery = 2$' "$camera_driver" || true)
[ "$matches" -eq 1 ] || {
    echo "expected exactly one TECHO5 convertEvery = 2 setting, found $matches" >&2
    exit 1
}
sed 's/^\([[:space:]]*\)convertEvery = 2$/\1convertEvery = 1/' "$driver_backup" > "$camera_driver"
(
    cd "$checkout/echod"
    GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build \
        -tags spot -trimpath -ldflags '-s -w' -o "$output" ./cmd/tater-rook-camera
)
