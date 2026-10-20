#!/bin/sh
# Biscuit/Radar's MediaTek driver exposes its AP through the vendor control
# device. Writing A creates a dedicated ap0 on the AP-capable P2P wiphy; wlan0
# itself is station-only and rejects the standard nl80211 type change.
set -eu
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

[ ! -s /data/local/etc/tater/native.json ] || exit 0
if [ -s /run/tater-setup-ssid ] && [ -s /run/tater-hostapd.pid ] && \
   [ -s /run/tater-dnsmasq.pid ] && \
   kill -0 "$(cat /run/tater-hostapd.pid)" 2>/dev/null && \
   kill -0 "$(cat /run/tater-dnsmasq.pid)" 2>/dev/null; then
    exit 0
fi

for pid in $(pidof wpa_supplicant 2>/dev/null || true); do
    kill "$pid" 2>/dev/null || true
done
sleep 1
[ -w /dev/wmtWifi ] || exit 1
if ! iw dev ap0 info 2>/dev/null | grep -q 'type AP'; then
    printf A > /dev/wmtWifi
fi
attempt=0
until iw dev ap0 info 2>/dev/null | grep -q 'type AP'; do
    attempt=$((attempt+1))
    [ "$attempt" -lt 20 ] || exit 1
    sleep 1
done

TATER_SETUP_AP_IFACE=ap0 TATER_SETUP_DHCP_BROADCAST=1 \
    /usr/local/sbin/tater-setup-ap-base
