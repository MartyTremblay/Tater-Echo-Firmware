#!/bin/sh
set -eu

repository=${1:?TECHO5 repository is required}
commit=${2:?TECHO5 Athena commit is required}
checkout=/tmp/techo5-athena-inputs
output=/opt/tater/athena-inputs

git clone --filter=blob:none "$repository" "$checkout"
git -C "$checkout" checkout --detach "$commit"
test "$(git -C "$checkout" rev-parse HEAD)" = "$commit"
python3 "$checkout/tools/fetch-inputs.py" --device show --out "$output"

# The store builder needs only the rescue base, packages and static tools.
rm -rf "$output/.fetch" "$output/models"

# mkstore also installs these packages by repository name. Fetch that complete,
# exact armv7 closure now so release builds never consult a live APK index.
apk --no-cache --allow-untrusted fetch \
  --arch armv7 \
  --repositories-file /dev/null \
  --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/main \
  --repository https://dl-cdn.alpinelinux.org/alpine/v3.24/community \
  --recursive \
  --output "$output/apks" \
  dropbear iw wireless-tools e2fsprogs e2fsprogs-extra busybox-extras
for package in \
  dropbear-2026.91-r0.apk \
  iw-6.17-r0.apk \
  wireless-tools-30_pre9-r5.apk \
  e2fsprogs-1.47.4-r0.apk \
  e2fsprogs-extra-1.47.4-r0.apk \
  busybox-extras-1.37.0-r31.apk
do
  test -f "$output/apks/$package" || {
    echo "frozen Athena package version changed: $package" >&2
    exit 1
  }
done
apk index --allow-untrusted -o "$output/apks/APKINDEX.tar.gz" "$output"/apks/*.apk
(
  cd "$output"
  find . -type f ! -name SHA256SUMS -print0 \
    | sort -z \
    | xargs -0 sha256sum > SHA256SUMS
)
rm -rf "$checkout"
