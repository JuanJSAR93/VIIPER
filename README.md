<img src="docs/viiper.svg" align="right" width="128" alt="VIIPER logo" />

# VIIPER

[![Build](https://github.com/hbashton/VIIPER/actions/workflows/snapshots.yml/badge.svg)](https://github.com/hbashton/VIIPER/actions)
[![Release](https://img.shields.io/github/v/release/hbashton/VIIPER?include_prereleases&sort=semver)](https://github.com/hbashton/VIIPER/releases)
[![License](https://img.shields.io/github/license/hbashton/VIIPER)](LICENSE.txt)

**Virtual Input over IP EmulatoR**

VIIPER is a userspace virtual USB device framework built on USBIP. This fork
provides the backend used by the hbashton DS4Windows project for native virtual
controller output, including DualSense audio, haptics, and microphone work.

This repository is forked from [Alia5/VIIPER](https://github.com/Alia5/VIIPER)
and incorporates the protocol, USB-audio, device-identity, and lifecycle work
used by [hbashton/DS4Windows](https://github.com/hbashton/DS4Windows). This fork
is maintained at [JuanJSAR93/VIIPER](https://github.com/JuanJSAR93/VIIPER).

> **Windows releases from this fork are x64 only.** x86 Windows and x86
> DS4Windows builds are not compatible with VIIPER. Use 64-bit Windows and the
> x64 DS4Windows package.

## Install for DS4Windows

The simplest and recommended path is through a VIIPER-capable DS4Windows build:

1. Download the newest VIIPER pre-release from
   [hbashton/DS4Windows Releases](https://github.com/hbashton/DS4Windows/releases).
2. Open **DS4Windows > Settings**.
3. Under **VIIPER Virtual Controller Support**, click **Install / Repair VIIPER**.
4. Accept the administrator prompt and restart Windows if `usbip-win2` was installed or updated.
5. In a profile, choose an output such as **DualSense**.

DS4Windows installs the exact bundled VIIPER build to
`%ProgramFiles%\DS4Windows\VIIPER\viiper.exe`, installs or safely replaces the
pinned USB-IP package when necessary, registers startup, and verifies the
binary hash, driver files, USB-IP ABI, and local VIIPER API before reporting
Ready. Portable DS4Windows uses this same protected backend.

## Standalone Windows setup is developer-only

The former PowerShell installer created a separate LocalAppData/HKCU owner that
could disagree with DS4Windows about VIIPER, USB-IP, startup, and reboot state.
It is therefore fail-closed by default. End users should use the signed
DS4Windows installer or its built-in **Install / Repair VIIPER** action.

Developers deliberately testing VIIPER outside the managed DS4Windows contract
must download the repository, inspect the script, set
`VIIPER_DEVELOPER_STANDALONE=1`, and pass `-DeveloperStandalone`. That mode is
not a supported DS4Windows installation path.

You can also download `viiper.exe` manually from the
[latest hbashton release](https://github.com/hbashton/VIIPER/releases/latest).
VIIPER itself is portable, but virtual devices on Windows still require the
[`usbip-win2`](https://github.com/vadimgrn/usbip-win2) kernel driver.

## What this fork adds

### DS4Windows controller backends

VIIPER can expose the following virtual USB devices for DS4Windows:

- Xbox 360 controller
- DualShock 4
- DualSense
- DualSense Edge
- Nintendo Switch 2 Pro Controller

The generic VIIPER keyboard and mouse devices remain available to other feeder
applications.

### Explicit VIIPER device identity

VIIPER devices publish their own product strings. Applications running on
Windows can use `DEVPKEY_Device_BusReportedDeviceDesc` to distinguish a VIIPER
device from a physical controller that uses the same protocol or VID/PID.

| Device | Product string | Development identity or evidence |
| --- | --- | --- |
| Xbox 360 | `VIIPER Xbox 360 Controller` | Xbox/XInput-compatible virtual USB device |
| DualShock 4 | `VIIPER DualShock 4 Controller` | DS4-compatible virtual USB device |
| DualSense | `VIIPER DualSense Wireless Controller` | DS5-compatible virtual USB device |
| Switch 2 Pro | `VIIPER Switch 2 Pro Controller` | Switch 2 Pro-compatible virtual USB device |
| Xbox One/Series | `VIIPER Xbox One Controller` or an authorized profile string | Explicit retained USB/IP development path |

The product string is the primary signal. The PnP parent and service can be
used as corroborating evidence, but they may vary with Windows, the installed
driver, and the USB topology. Do not identify virtual devices using VID/PID
alone. See [Windows device identification](docs/VIIPER_DEVICE_IDENTIFICATION.md)
for a PowerShell example.

### Xbox One and Xbox Series retained path

The repository contains an explicit, authenticated retained USB/IP composition
for Xbox One/Series development. It is intentionally separate from the
generic device factory: the caller supplies the authorized identity, USB
profile, product strings, GIP device identity, and removal capability. The
development client is available at
[`cmd/viiper-xboxone-client`](cmd/viiper-xboxone-client/main.go).

This path supports the VIIPER broker protocol for semantic input, canonical
feedback acknowledgements, USB/IP import, and exact registration removal. It is
not a claim that every Xbox One or Series firmware, Windows binding, or physical
controller has been validated. See the [Xbox One API notes](docs/api/xboxone-exact-removal.md)
and the [Xbox One provenance record](device/xboxone/PROVENANCE.md).

### Native DualSense input

The virtual DualSense and DualSense Edge paths carry:

- Face, shoulder, system, and mute buttons
- Sticks and analog triggers
- Touchpad click and two-finger coordinates
- Gyroscope and accelerometer reports
- DualSense Edge Fn buttons and back paddles
- Battery and controller metadata used by the USB identity

### Output reports and adaptive triggers

VIIPER returns host output to DS4Windows instead of reducing every command to
generic rumble. Extended DualSense streams preserve:

- The native USB HID output report `0x02`
- Lightbar, player LED, mute LED, and rumble state
- Native-spaced left and right adaptive-trigger effect blocks
- Optional Bluetooth haptics report `0x32`
- Combined Bluetooth state, haptics, and speaker report `0x36`

This lets DS4Windows forward game-authored adaptive-trigger commands and other
DualSense output to a compatible physical controller.

### Advanced haptics and speaker audio

The virtual DualSense includes the USB Audio Class interfaces expected by games.
The hbashton fork implements USBIP isochronous packet descriptors, completion
pacing, and audio-interface state so those endpoints can carry real data.

With the matching DS4Windows bridge:

- A game can open the virtual DualSense playback endpoint.
- DualSense haptics samples are converted into the Bluetooth haptics lane.
- Speaker samples can be forwarded to the physical controller speaker over Bluetooth.
- Haptics and speaker data share the combined Bluetooth report without one path starving the other.

### Microphone input

The microphone-capable DualSense, DualSense Edge, and DualShock 4 device types
expose virtual Windows recording endpoints. The framed feeder protocol accepts
PCM microphone frames separately from controller input state, and the USBIP
ISO-IN path supplies them to Windows.

In the DS4Windows integration, microphone audio follows this path:

1. The physical Bluetooth DualSense or DualShock 4 supplies its encoded
   microphone frames.
2. DS4Windows decodes and conditions the signal.
3. DS4Windows converts the PCM to the emulated controller's native format and
   sends it to VIIPER.
4. VIIPER presents that PCM through the selected virtual controller's recording
   endpoint.

Transport framing and microphone data are deliberately isolated from HID input
reports. This prevents audio bytes from being interpreted as controller buttons,
keyboard commands, or mouse movement.

## Architecture

```text
Physical controller
        |
        | HID input, audio, and feedback
        v
DS4Windows feeder
        |
        | local framed TCP API
        v
VIIPER userspace USB device
        |
        | USBIP
        v
usbip-win2 virtual host controller
        |
        v
Windows, games, and audio services
```

VIIPER does not emulate a Bluetooth radio and does not make the virtual device
appear wirelessly paired. The game sees a native-style USB controller. DS4Windows
is responsible for translating and forwarding supported feedback between that
virtual USB device and the physical USB or Bluetooth controller.

## Privacy and update behavior

VIIPER has no built-in update checker, update dialog, self-update installer, or
outbound telemetry client. The server does not contact GitHub or another vendor
service to check for updates. Updates are performed by replacing the binary or
by the application that embeds VIIPER, such as DS4Windows.

This is independent of any update behavior implemented by a feeder application.
See [server configuration](docs/cli/configuration.md) for the privacy and
configuration details.

## Requirements

### Windows

- Windows 10 or Windows 11 x64
- [`usbip-win2`](https://github.com/vadimgrn/usbip-win2)
- A VIIPER executable matching the protocol used by your feeder application
- Administrator approval for driver installation and startup registration

The upstream hbashton release channel prioritizes Windows x64 and DS4Windows.
The underlying VIIPER project remains cross-platform, but binaries and features
available from this fork may differ from upstream.

### Linux development

Linux uses the kernel USBIP client and `vhci-hcd` module. Package names vary by
distribution; common starting points are `linux-tools-generic` on Ubuntu and
`usbip` on Arch Linux.

## Server and API

The standalone `viiper` executable exposes a lightweight TCP API for bus and
device management. Management requests are null-terminated path/payload messages,
while active devices use persistent binary streams for low-latency input and
feedback.

Localhost feeder applications can create a bus, add a device, open its stream,
send input state, and receive output feedback. VIIPER handles USB descriptors,
USBIP requests, and device attachment.

See:

- [API overview](docs/api/overview.md)
- [DualSense protocol](docs/devices/dualsense.md)
- [DualShock 4 protocol](docs/devices/dualshock4.md)
- [Xbox 360 protocol](docs/devices/xbox360.md)
- [Switch 2 Pro protocol](docs/devices/ns2pro.md)
- [libVIIPER overview](docs/libviiper/overview.md)

### Server lifecycle controls

The production server exposes internal lifecycle commands through the same API:

| Command | Response | Behavior |
| --- | --- | --- |
| `server/status` | `{"server":"VIIPER", "state":"running", ...}` | Reports listeners, buses, devices, active USB/IP imports, and active input streams. |
| `server/restart` | `{"accepted":true,"action":"restart"}` | Gracefully closes the current listeners and starts a fresh server inside the same process. |
| `server/shutdown` | `{"accepted":true,"action":"shutdown"}` | Gracefully closes the API and USB/IP servers and exits the server command. |

Management requests are null-terminated. For example, a client sends the path
`server/status` followed by a NUL byte. Localhost authentication is optional by
default; remote API connections require authentication. Set
`--api.require-local-host-auth=true` when localhost control must also be
authenticated.

`server/restart` is an in-process lifecycle operation; it does not require
`taskkill`. Existing USB/IP connections and the current virtual topology are
closed during the restart and are not persisted automatically. Recreate or
reattach devices after a restart when the feeder does not do so for you.

The `usbipImported` field in `server/status` is server-side evidence that a
USB/IP client has an active import connection. It confirms the server's mount
state, but does not by itself prove that the remote Windows Plug and Play
stack has completed enumeration.

The implementation has been validated with the Windows `usbip.exe` client:
`server/restart` returned to a running listener in the same process,
`server/shutdown` closed the listeners internally, and an authorized VIIPER
Xbox One test device changed from `usbipImported: false` to
`usbipImported: true` after a real `usbip.exe attach`. A separate Xbox 360
test also reported its active USB/IP import and input stream.

See the complete [API overview](docs/api/overview.md) and the [server command
reference](docs/cli/server.md).

## Build from source

### Prerequisites

- [Go](https://go.dev/) 1.26 or newer
- [just](https://github.com/casey/just), recommended
- USBIP support for the target operating system
- A C compiler only when building `libVIIPER`

### Build

```powershell
git clone https://github.com/JuanJSAR93/VIIPER.git
cd VIIPER
just build Release
```

The Windows executable is written to `dist/viiper-windows-amd64.exe`. Useful
development commands include:

```powershell
just test
go test ./...
go run ./cmd/viiper codegen
```

The Windows release build generates the version resource from
[`versioninfo.json`](versioninfo.json), including [`viiper.ico`](viiper.ico).
For a direct development binary:

```powershell
go build -o viiper.exe ./cmd/viiper
```

Run `just build Release` when the version metadata and Windows icon must be
regenerated as part of the build.

Client bindings are generated for TypeScript, C#, C++, and Rust. Run code
generation whenever a public device-state or feedback contract changes, then
build the client examples before publishing the change.

## Troubleshooting

- **DS4Windows says VIIPER is unavailable:** run **Install / Repair VIIPER** and
  restart Windows if `usbip-win2` was just installed.
- **No virtual controller appears:** confirm `viiper.exe server` is running and
  that the USBIP driver is installed.
- **Need to verify whether a device is mounted:** query `server/status` and
  inspect the device's `usbipImported` field. Confirm the client-side port with
  `usbip.exe port` when using usbip-win2.
- **The server is stuck:** use `server/restart` for an in-process restart or
  `server/shutdown` for a graceful close. These commands close owned listeners
  and imports without relying on `taskkill`.
- **A virtual device must be distinguished from a physical one:** read
  `DEVPKEY_Device_BusReportedDeviceDesc` and match the `VIIPER ... Controller`
  product string. See [Windows device identification](docs/VIIPER_DEVICE_IDENTIFICATION.md).
- **Several stale controllers appear:** stop DS4Windows and VIIPER, start VIIPER
  once, then start DS4Windows. Report repeatable lifecycle bugs with both logs.
- **DualSense audio or microphone endpoints are missing:** use matching
  hbashton DS4Windows and VIIPER releases; older upstream VIIPER builds do not
  contain the same extended device types.
- **Input becomes corrupted while the microphone is active:** stop the test and
  report the DS4Windows log plus the VIIPER log. Microphone-capable streams must
  use the framed protocol and should never pass audio transport bytes into HID state.

Report backend issues at
[hbashton/VIIPER Issues](https://github.com/hbashton/VIIPER/issues). Report
controller mapping or DS4Windows UI issues at
[hbashton/DS4Windows Issues](https://github.com/hbashton/DS4Windows/issues).

## License

The VIIPER server and core are licensed under GPL-3.0-or-later. Generated client
libraries retain their documented MIT licensing. See [`LICENSE.txt`](LICENSE.txt)
and the individual client packages for details.

## Credits

VIIPER was created by Peter Repukat and the Alia5/VIIPER contributors. This fork
builds on that architecture for DS4Windows. It also depends on the USBIP project,
`usbip-win2`, and controller/audio protocol research shared by SDL, SAxense,
DualSense reverse-engineering projects, and the wider open-source community.
