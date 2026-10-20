# Biscuit and Radar Tater Linux factory installer

This factory bundle replaces the old userspace with Alpine-based Tater Linux.
It is intentionally a reset, not a migration.

> [!IMPORTANT]
> Biscuit and Radar running firmware older than v3.0.0 must use this USB/TWRP
> factory installer once. The pre-v3 base system cannot install the v3 OTA
> format. The reset clears the satellite's Tater settings; later releases use
> the normal Tater OTA flow.

1. Complete the matching amonet unlock and boot the unit into TWRP.
2. Connect USB and extract the matching factory archive.
3. Run `./install.sh --verify-bundle`, then `./install.sh` (macOS/Linux), or
   run `python3 install.py --verify-bundle`, then `python3 install.py`
   (Windows).
4. Type `ERASE` when prompted.
5. Join `Tater-Setup-XXXX`, open `http://192.168.4.1`, and pair the satellite.

The installer checksum-verifies every bundled file and every partition backup,
builds the Linux boot image from that unit's own TWRP header, checks the
board-specific kernel, writes only recovery/cache plus Tater-owned userdata,
and preserves the stock system slots. TWRP is copied into the Linux slot store
so `/usr/local/sbin/to-twrp` remains the recovery route.

An empty or previously erased cache partition is supported. Preflight leaves it
untouched; after backups are verified and the user types `ERASE`, the installer
creates a fresh ext4 cache before installing the Tater Linux A/B rootfs.

Use `./install.sh --verify-bundle` to validate an extracted archive without a
device, or `./install.sh --dry-run` in TWRP to perform all checks and backups
without writing the unit.
