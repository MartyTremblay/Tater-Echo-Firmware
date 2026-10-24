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
actual=$(git -C "$checkout" rev-parse HEAD)
case "$actual" in
    0f042360fbc22d798930cfd683d251606bfb7412|3062cb0a7b812145248c9b60ce88ee3c6474924d) ;;
    *)
        echo "TECHO5 checkout is $actual, not a pinned Checkers/Crown or Athena platform" >&2
        exit 1
        ;;
esac

command_dir=$checkout/echod/cmd/tater-checkers-camera
mkdir -p "$command_dir"
cp "$(dirname "$0")/camera-helper/main.go" "$command_dir/main.go"
cp "$(dirname "$0")/camera-helper/main_test.go" "$command_dir/main_test.go"
camera_dir=$checkout/echod/internal/hardware/camera
cp "$(dirname "$0")/camera-helper/tater_video_show.go" "$camera_dir/tater_video_show.go"
cp "$(dirname "$0")/camera-helper/tater_video_show_test.go" "$camera_dir/tater_video_show_test.go"
(
    cd "$checkout/echod"
    GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build \
        -trimpath -ldflags '-s -w' -o "$output" ./cmd/tater-checkers-camera
)
