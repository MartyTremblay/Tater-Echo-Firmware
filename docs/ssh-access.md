# Tater Linux SSH access

Every Tater Linux satellite image includes a small Dropbear SSH service for
maintenance when USB serial is unavailable. It accepts public keys only. There
is no SSH password, default credential, or embedded Tater key, and the service
does not listen until the owner provisions at least one public key.

SSH is independent of the network-selection policy. It listens on port 22 on
the device's available network interfaces, so the same login works over Wi-Fi
or USB Ethernet. If an active session uses Wi-Fi when Ethernet takes over, that
session will disconnect with the Wi-Fi interface; reconnect to the wired IP.

## Provision before the Ethernet test

If the computer does not already have a key, create one on the computer:

```sh
ssh-keygen -t ed25519
```

While USB serial is still available, copy the single **public** key line from
the computer (normally `~/.ssh/id_ed25519.pub`) and run this on the satellite:

```sh
tater-ssh-authorize add 'ssh-ed25519 AAAA... your-computer'
tater-ssh-authorize status
```

The command also accepts one public key on standard input. Never copy the
private key to the satellite. Provisioning creates
`/data/tater-linux/ssh/authorized_keys` with owner-only permissions. The SSH
supervisor then creates a persistent Ed25519 host key in the same directory and
starts listening. Both files survive a normal Tater rootfs update.

From the computer, connect using the address from the router's DHCP lease list
or the address previously shown by `ip -4 addr`:

```sh
ssh root@DEVICE_IP
```

The image hostname is `tater-biscuit`, `tater-radar`, `tater-checkers`,
`tater-crown`, `tater-athena`, or `tater-rook`. A router may make that hostname
resolvable, but Tater Linux does not currently promise `.local` discovery, so
the DHCP address is the reliable first test.

## Ethernet diagnostics over SSH

These commands cover the same evidence normally collected over USB serial:

```sh
cat /run/tater-usb-ethernet.state
tater-usb-inventory --include-logs
ip address
ip route
tail -f /data/tater-linux/sshd.log
```

`/run/tater-sshd.state` reports whether SSH is waiting for a key or active. The
listener is restricted to the dedicated `tater-ssh` login group, whose only
member is root. Password authentication and local and remote port forwarding
are disabled. Provisioned keys also disable agent and X11 forwarding, while
leaving the interactive shell and PTY available. Authentication failures and server diagnostics go to
`/data/tater-linux/sshd.log`.

## Disable or restore access

To stop accepting new SSH connections without deleting the keys:

```sh
tater-ssh-authorize disable
```

This moves the authorized key file aside and stops the listener. A connection
that already has its own Dropbear child may remain until it is closed. BusyBox
init restarts the supervisor in its disabled waiting state.

Restore the saved keys and listener with:

```sh
tater-ssh-authorize enable
```

Other useful commands are `tater-ssh-authorize list`,
`tater-ssh-authorize path`, and `tater-ssh-authorize status`. If a persistent
host key is corrupt, startup fails closed and reports the error instead of
silently replacing the device identity.

SSH is part of the Linux root filesystem. Installing only an application OTA
does not add it to an older image; use a factory or full Linux update that
contains this rootfs first.
