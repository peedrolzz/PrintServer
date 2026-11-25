# PrintServer

A small service that turns a Raspberry Pi 3 into a shared USB printer
server for the point-of-sale terminals in a store. It is meant as a
stop-gap while the customer waits for a proper network printer: cheap,
local, and with just enough security to survive a shared LAN.

PrintServer is MIT licensed and depends on nothing outside the box.
No cloud account, no mobile app, no vendor portal.

## What it does

- Exposes any USB printer plugged into the Pi on TCP ports
  `9100`-`9199`, reachable from the store LAN. Two modes:
  **with token** (HMAC-SHA256 handshake, only the configured PDV can
  print) or **open** (plain raw 9100, like a generic network printer).
- Serves a tiny HTTPS panel for one-time setup. First access is
  `printserver` / `printserver`; the panel forces a password change
  before anything else.
- Boots as a self-contained WiFi hotspot named `PrintServer-XXXX` so
  the operator can configure it from a phone without any cable. The
  hotspot drops when the operator joins a real WiFi network, and comes
  back automatically if that connection fails.

## What it is not

- A managed fleet. Every unit is standalone.
- A product with a logo, labels or stickers. It is a service.
- A full-featured print server. No queuing, no CUPS, no filters. It is
  a thin bridge that writes bytes to `/dev/usb/lp*`.

## Components

| Binary | Role |
|---|---|
| `printserver-portal` | HTTPS panel on 443 (plus a captive HTTP helper on 80). Login, WiFi setup, test print, password change, diagnostics, reboot, bridge mode toggle. |
| `printserver-bridge` | Raw TCP 9100-9199. When token mode is on, requires an HMAC-SHA256 handshake, then writes bytes to `/dev/usb/lp*`. When open mode is on, acts like a plain network printer. |
| `printserver-discover` | One-shot triggered by udev on USB plug. Registers the printer in `printers.json`. |
| `printserver-helper` | Root-owned Unix socket daemon. Accepts a fixed whitelist of system actions (restart bridge, flip mode, reload USB, reboot). The only process that runs as root. |
| `printserver-tool` | Small CLI helpers used by the firstboot scripts. |

Everything else runs as the unprivileged `printserver` system user.

## How a store uses it

### 1. First boot

Flash the SD, insert, power on. On first boot:

- The device picks a random hex ID and sets its hostname to
  `printserver-XXXX.local` (last 4 characters of the ID, matching the
  WiFi SSID).
- Generates a self-signed TLS certificate.
- Writes the initial credentials (`printserver` for the panel and for
  the hotspot WiFi).
- Marks the `first-run` flag so the panel will require a password
  change at login.
- Brings up the hotspot `PrintServer-XXXX`.

### 2. Operator configures the device

From a phone or laptop:

1. Connect to the WiFi `PrintServer-XXXX` using password
   `printserver`.
2. The captive portal opens the panel at `https://10.42.0.1` by
   itself. If it doesn't, type that URL in the browser.
3. Log in with password `printserver`.
4. The panel asks for two new passwords: one for the admin, one for
   the WiFi hotspot. Both have to be at least 8 characters and
   different from `printserver`.
5. Open the WiFi section, pick the store network, enter credentials.
   For 802.1X enterprise networks the certificate is optional (opt-in).
6. The device connects to the chosen WiFi. The hotspot goes down.
7. From any device on the same WiFi, open
   `https://printserver-XXXX.local`.

### 3. Point-of-sale talks to the printer

The PDV sends ESC/POS bytes to `9100-9199` on the Pi's address. In
**token mode** it must send a 60-byte handshake first (magic
`CPJ1` + nonce + timestamp + HMAC-SHA256(token, msg)). In **open
mode** it just opens the socket and writes. The token is in
`/var/lib/printserver/credentials/bridge-token` and is shown in the
panel for the integrator.

### 4. Recovery

Three fallback paths if the operator loses access:

- The watchdog runs every 30 seconds. If the device is in client mode
  and `wlan0` has had no valid IP for two minutes, it goes back to
  hotspot by itself.
- Put the SD in a computer, create an empty file called
  `reset-hotspot` in the boot partition, put it back in the Pi. On
  the next boot the device returns to hotspot with the default WiFi
  password.
- SSH via Ethernet is always available for support; the keys live in
  `/home/printer/.ssh/authorized_keys` and stay out of the hotspot
  interface (nftables drops port 22 on `wlan0`).

## Installing

Prebuilt `.deb` for Raspberry Pi OS Lite 64-bit:

```
sudo apt install ./printserver_<version>_arm64.deb
sudo reboot
```

The postinst creates the `printserver` system user, enables the
systemd units, and makes sure the distro `hostapd` and `dnsmasq` do
not race with the units shipped here. First boot does the rest.

## Building

Requirements: Go 1.22 or newer, `dpkg-deb`.

```
make all        # arm64 binaries in dist/bin/arm64
make deb        # Debian package in dist/pkg
make test       # go test ./...
make ci-scan    # grep-based audit against the tree
```

## Security posture

Just the basics that matter when the Pi sits on a customer LAN:

- Per-device admin password; forced change on first login.
- Argon2id for the admin password hash.
- CSRF double-submit on every form.
- Login rate limit (3 attempts per minute per source IP).
- HSTS, CSP, X-Frame-Options, X-Content-Type-Options,
  Referrer-Policy, Permissions-Policy on every response. No `Server`
  header.
- Session cookie HttpOnly, Secure, SameSite=Strict.
- Captive DNS only resolves `printserver.local` and the captive-portal
  probes for every major OS. Anything else returns REFUSED.
- `nftables` drops TCP/22 on the hotspot interface. SSH is Ethernet
  only.
- Bridge in token mode refuses connections without a valid HMAC
  handshake. In open mode it accepts raw bytes, like any generic
  network printer.
- Every systemd unit is hardened (`ProtectSystem=strict`,
  `NoNewPrivileges`, minimal capability set). Only the helper runs as
  root and it only runs commands from a hard-coded list.
- First boot regenerates the SSH host keys and shreds the cloud-init
  `user-data` left in the boot partition by `rpi-imager`.
- `scripts/audit.sh` reruns the list on a live device.

## Layout

```
cmd/              binaries (portal, bridge, discover, helper, tool)
pkg/              reusable packages
scripts/          firstboot, seal-image, apply-network-config, audit,
                  wifi-scan, wifi-profile, watchdog, recovery
systemd/          service units and timers
config/           default nftables.conf
udev/             USB printer rules
```

## License

MIT. See `LICENSE`.
