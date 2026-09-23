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
for Xbox One/Series. Its API endpoint is separate from the generic factory
because it supplies the authorized identity, USB profile, product strings, GIP
device identity, and removal capability; it does not require a separate USB
bus. The development feeder is integrated into the main executable as
`viiper.exe xboxone-client`, so no second client binary is required.

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

### Live controller matrix

On Windows with `usbip-win2` installed, the repository includes a real
end-to-end smoke test. It starts an isolated VIIPER server, creates Xbox 360,
DS4, DualSense and Switch 2 Pro devices, sends every mapped button plus analog
and motion states, mounts each one with `usbip.exe`, checks `server/status`,
then adds Xbox One/Series to that same bus through the authenticated retained
path, executes its input matrix, removes the devices and calls
`server/shutdown`:

```powershell
python scripts/test_all_controllers.py --viiper .\viiper.exe --report .\viiper_controller_test_report.md
```

Use `--no-attach` when only the VIIPER API and input streams should be tested,
or `--skip-xboxone` when the native usbip-win2 attach prerequisite is not
available. The Xbox One/Series phase requires the local VIIPER key file. The
Markdown report contains the identity/status snapshot for every controller and
the PASS/FAIL result for every button press, release, trigger, stick, touch or
motion state.

For a same-type fan-out test, use the dedicated harness. By default it runs
four and then eight simultaneous instances of Xbox 360, DualShock 4, DualSense,
Switch 2 Pro and Xbox One/Series. Every instance uses its own input stream and
is checked through the real USB/IP import state:

```powershell
python scripts/test_same_type_fanout.py `
  --viiper .\viiper.exe `
  --usbip "C:\Program Files\USBip\usbip.exe" `
  --counts 4 8 `
  --report .\viiper_fanout_report.md
```

Use `--only-type dualsense` (or another supported type) to isolate one family,
and `--xbox-profile xboxseries` to use the Xbox Series identity. The Xbox
client assigns a distinct primary GIP identity and a distinct retained import
ID to every concurrent persona; this is required for multiple Xbox instances
to remain active on the same VIIPER bus. The harness closes each input stream,
removes the bus through the API, detaches USB/IP ports and calls
`server/shutdown` after every group.

## Uso detallado del sistema

Esta sección describe el flujo completo para ejecutar VIIPER directamente en
Windows. El mando de Xbox One/Series usa el mismo ejecutable, pero conserva una
ruta autenticada porque necesita registrar la identidad GIP, el perfil USB y
los mensajes de activación propios de Xbox. No se necesita un segundo
ejecutable.

### Requisitos

- Windows 10/11 x64.
- `usbip-win2` instalado y su controlador aprobado por Windows.
- Una consola de PowerShell abierta con permisos de administrador para instalar
  o adjuntar dispositivos USB/IP.
- Para Xbox One/Series, la clave local de VIIPER en
  `%APPDATA%\VIIPER\viiper.key.txt`, salvo que se indique otra mediante
  `--key-file`.
- `viiper.exe`, ya sea un binario compilado o el binario de desarrollo.

Comprueba que el cliente USB/IP está disponible antes de iniciar una prueba:

```powershell
usbip.exe port
```

### 1. Iniciar el servidor

Desde la carpeta que contiene `viiper.exe`, inicia el servidor en una ventana
de PowerShell:

```powershell
.\viiper.exe server `
  --usb.addr=0.0.0.0:3241 `
  --api.addr=127.0.0.1:3242 `
  --api.auto-attach-local-client=true `
  --api.auto-attach-windows-native=true
```

El puerto USB/IP es `3241` y la API local de administración es `3242`. La
opción `--api.auto-attach-windows-native=true` permite que VIIPER gestione el
adjunto local cuando la configuración de `usbip-win2` lo permite. Si se usa un
flujo externo con `usbip.exe attach`, puede desactivarse y comprobar el estado
manualmente.

En otra ventana puedes comprobar que el servidor está escuchando:

```powershell
Test-NetConnection 127.0.0.1 -Port 3241
Test-NetConnection 127.0.0.1 -Port 3242
```

### 2. Ejecutar Xbox One o Xbox Series

El cliente integrado se ejecuta como subcomando del mismo `viiper.exe` y debe
conectarse al servidor ya iniciado. El perfil `xboxone` crea:

```text
VID:PID       045E:02EA
Producto      VIIPER Xbox One Controller
```

Ejemplo:

```powershell
.\viiper.exe xboxone-client `
  --addr=127.0.0.1:3242 `
  --key-file="$env:APPDATA\VIIPER\viiper.key.txt" `
  --profile=xboxone `
  --input-test `
  --hold-seconds=3
```

Para probar el perfil Xbox Series X|S cambia únicamente el perfil:

```powershell
.\viiper.exe xboxone-client `
  --addr=127.0.0.1:3242 `
  --key-file="$env:APPDATA\VIIPER\viiper.key.txt" `
  --profile=xboxseries `
  --input-test `
  --hold-seconds=3
```

El perfil Xbox Series crea:

```text
VID:PID       045E:0B12
Producto      VIIPER Xbox Series X|S Controller
```

Opciones útiles del cliente:

| Opción | Uso |
| --- | --- |
| `--addr` | Dirección de la API VIIPER, normalmente `127.0.0.1:3242`. |
| `--key-file` | Ruta de la clave autorizada para la sesión Xbox. |
| `--profile` | `xboxone` o `xboxseries`. |
| `--bus-id` | Reutiliza un bus existente; si se omite, el cliente crea uno. |
| `--input-test` | Envía una matriz de botones, sticks, gatillos y estados de entrada. |
| `--hold-seconds` | Tiempo que mantiene activo cada estado de la prueba. |
| `--pause-before-activate` | Pausa antes de activar el dispositivo para inspección manual. |

El cliente Xbox registra la identidad, negocia la activación, abre el stream de
entrada, envía los estados de prueba y elimina exactamente su dispositivo al
terminar. Si se cierra con `Ctrl+C`, vuelve a ejecutar `server/status` y retira
el dispositivo retenido antes de iniciar otra prueba.

### 3. Probar todos los mandos y generar un reporte

El smoke test crea y prueba Xbox 360, DualShock 4, DualSense, Switch 2 Pro y
Xbox One/Series. También comprueba la identidad del dispositivo, el bus, el
puerto USB/IP, el estado de importación y el stream de entrada:

```powershell
python scripts/test_all_controllers.py `
  --viiper .\viiper.exe `
  --report .\viiper_controller_test_report.md
```

El perfil por defecto para la fase Xbox es `xboxone`. Para probar Xbox Series:

```powershell
python scripts/test_all_controllers.py `
  --viiper .\viiper.exe `
  --xbox-profile xboxseries `
  --report .\viiper_controller_test_report_series.md
```

Usa `--skip-xboxone` si sólo quieres probar los cuatro dispositivos genéricos,
`--no-attach` para probar API y streams sin montar USB/IP, o
`--xboxone-client <ruta>` únicamente si necesitas utilizar un cliente Xbox
externo heredado. El cliente integrado es la ruta recomendada.

### 4. Consultar, reiniciar y cerrar el servicio

VIIPER incluye comandos de ciclo de vida internos; no es necesario usar
`taskkill`:

- `server/status`: devuelve listeners, buses, dispositivos, alias USB/IP,
  `usbipImported` y streams de entrada activos.
- `server/restart`: cierra y vuelve a abrir los listeners dentro del mismo
  proceso. Después del reinicio puede ser necesario recrear o reanexar los
  dispositivos.
- `server/shutdown`: cierra ordenadamente API, USB/IP, streams y el proceso
  del servidor.

Las solicitudes de administración se envían a la API local como una ruta
terminada en byte NUL. Para integraciones, consulta
[docs/api/overview.md](docs/api/overview.md); el cliente de prueba ya realiza
estas operaciones automáticamente al finalizar.

### 5. Interpretar la identidad y el bus

Para diferenciar virtuales de físicos en Windows, usa primero
`DEVPKEY_Device_BusReportedDeviceDesc` y busca el texto `VIIPER ... Controller`.
VID/PID, fabricante, padre PnP y servicio son datos de apoyo, no la señal
principal.

En los mandos genéricos el reporte normalmente muestra un bus y puerto
numéricos, por ejemplo `usbipBusId=1-5` y `usbipPort=[5]`. Xbox One/Series
también comparte el bus VIIPER cuando se le pasa `--bus-id`, pero conserva un
alias USB/IP protegido de la forma `x1-...` para su registro autenticado. Ese
alias no significa que se haya creado otro bus ni que el mando esté usando una
identidad de laboratorio; es el identificador interno de la exportación
retenida. El reporte muestra ambos valores para que el bus numérico pueda
compararse con los demás sin perder la identidad real del endpoint Xbox.

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
