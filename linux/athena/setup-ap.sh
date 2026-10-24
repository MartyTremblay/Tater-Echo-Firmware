#!/bin/sh
# Athena's mainline MT7663 exposes one wlan0 interface. Hand it from the
# station supplicant to hostapd while first-boot setup is active.
set -eu

killall udhcpc wpa_supplicant 2>/dev/null || true
n=0
while pidof wpa_supplicant >/dev/null 2>&1; do
    n=$((n+1))
    [ "$n" -lt 20 ] || exit 1
    sleep 1
done
ip addr flush dev wlan0 2>/dev/null || true
ip link set wlan0 up
export TATER_SETUP_AP_IFACE=wlan0
export TATER_SETUP_DHCP_BROADCAST=1
exec /usr/local/libexec/tater-setup-ap-common
