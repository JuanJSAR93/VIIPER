#!/usr/bin/env python3
"""Start one isolated VIIPER server and exercise every available gamepad.

The script uses only the Python standard library. It creates one private bus,
opens the real VIIPER input streams, sends neutral/press/release states for
every exposed button plus analog controls, attaches each device through
usbip-win2, and shuts the server down through server/shutdown.

Xbox One/Series is included on that same bus through VIIPER's authenticated
retained-registration API. Its protected USB/IP alias remains distinct from
the numeric bus/device address. Use --skip-xboxone only when the native attach
prerequisite is unavailable and the generic matrix must still be collected.
"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
import re
import shutil
import socket
import struct
import subprocess
import sys
import time
import zlib
from pathlib import Path
from typing import NoReturn


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_USB_PORT = 3251
DEFAULT_API_PORT = 3252
SEND_DELAY = 0.035

PROFILE_INFO = {
    "xbox360": {
        "manufacturer": "©Microsoft Corporation",
        "product": "VIIPER Xbox 360 Controller",
        "vid": "0x045e",
        "pid": "0x028e",
        "uid": "296013F",
    },
    "dualshock4": {
        "manufacturer": "Sony Interactive Entertainment",
        "product": "VIIPER DualShock 4 Controller",
        "vid": "0x054c",
        "pid": "0x09cc",
        "uid": "1111020BF619A500",
    },
    "dualsense": {
        "manufacturer": "Sony Interactive Entertainment",
        "product": "VIIPER DualSense Wireless Controller",
        "vid": "0x054c",
        "pid": "0x0ce6",
        "uid": "E55700GTD1190A500",
    },
    "ns2pro": {
        "manufacturer": "Nintendo",
        "product": "VIIPER Switch 2 Pro Controller",
        "vid": "0x057e",
        "pid": "0x2069",
        "uid": "VIIPER-NS2PRO-00",
    },
    "xboxone": {
        "manufacturer": "©Microsoft Corporation",
        "product": "VIIPER Xbox One Controller",
        "vid": "0x045e",
        "pid": "0x02ea",
        "uid": "dinámico; incluye DeviceID y serial retenido",
    },
    "xboxseries": {
        "manufacturer": "©Microsoft Corporation",
        "product": "VIIPER Xbox Series X|S Controller",
        "vid": "0x045e",
        "pid": "0x0b12",
        "uid": "dinámico; incluye DeviceID y serial retenido",
    },
}


def die(message: str) -> "NoReturn":
    raise RuntimeError(message)


def find_executable(explicit: str | None, names: list[Path | str]) -> Path:
    if explicit:
        candidate = Path(explicit).expanduser().resolve()
        if candidate.is_file():
            return candidate
        die(f"No se encontró el ejecutable: {candidate}")
    for item in names:
        candidate = Path(item)
        if candidate.is_file():
            return candidate.resolve()
    for name in [str(item) for item in names if isinstance(item, str)]:
        found = shutil.which(name)
        if found:
            return Path(found).resolve()
    die("No se encontró el ejecutable requerido")


def resolve_xboxone_client(
    args: argparse.Namespace, viiper: Path | None = None
) -> tuple[Path, list[str]]:
    """Resolve the integrated feeder, with legacy external-client support."""
    if args.xboxone_client:
        return find_executable(args.xboxone_client, [
            ROOT / "viiper-xboxone-client.exe",
            ROOT.parent / "lab" / "viiper-xboxone-client.exe",
            "viiper-xboxone-client.exe",
        ]), []
    if viiper is None:
        viiper = find_executable(args.viiper, [
            ROOT / "viiper.exe",
            ROOT.parents[2] / "outputs" / "viiper.exe",
            ROOT.parent / "outputs" / "viiper.exe",
            "viiper.exe",
        ])
    return viiper, ["xboxone-client"]


def api_request(host: str, port: int, command: str, timeout: float = 5.0) -> dict:
    with socket.create_connection((host, port), timeout=timeout) as conn:
        conn.settimeout(timeout)
        conn.sendall(command.encode("utf-8") + b"\0")
        chunks: list[bytes] = []
        while True:
            try:
                chunk = conn.recv(65536)
            except socket.timeout:
                break
            if not chunk:
                break
            chunks.append(chunk)
    raw = b"".join(chunks).decode("utf-8", errors="replace").strip()
    if not raw:
        return {}
    result = json.loads(raw.splitlines()[0])
    if isinstance(result, dict) and result.get("status", 200) >= 400:
        die(f"API {result.get('status')}: {result.get('detail', raw)}")
    return result


def open_stream(host: str, port: int, bus_id: int, dev_id: str) -> socket.socket:
    stream = socket.create_connection((host, port), timeout=5.0)
    stream.settimeout(5.0)
    stream.sendall(f"bus/{bus_id}/{dev_id}\0".encode("ascii"))
    return stream


def frame_xbox360(buttons: int = 0, lt: int = 0, rt: int = 0,
                  lx: int = 0, ly: int = 0, rx: int = 0, ry: int = 0) -> bytes:
    return struct.pack("<IBBhhhh6s", buttons, lt, rt, lx, ly, rx, ry, b"\0" * 6)


def frame_ds4(buttons: int = 0, dpad: int = 0, l2: int = 0, r2: int = 0,
             lx: int = 0, ly: int = 0, rx: int = 0, ry: int = 0,
             touch: bool = False) -> bytes:
    return struct.pack(
        "<bbbbHBBBHHBHHBhhhhhh",
        lx, ly, rx, ry, buttons, dpad, l2, r2,
        960, 471, int(touch), 960, 471, 0,
        0, 0, 0, 0, 0, -5023,
    )


def frame_dualsense(buttons: int = 0, dpad: int = 0, l2: int = 0, r2: int = 0,
                    lx: int = 0, ly: int = 0, rx: int = 0, ry: int = 0,
                    touch: bool = False) -> bytes:
    return struct.pack(
        "<bbbbIBBBHHBHHBhhhhhh",
        lx, ly, rx, ry, buttons, dpad, l2, r2,
        960, 471, int(touch), 960, 471, 0,
        0, 0, 0, 0, 0, -5023,
    )


def frame_ns2pro(buttons: int = 0, lx: int = 0x800, ly: int = 0x800,
                 rx: int = 0x800, ry: int = 0x800,
                 ax: int = 0, ay: int = 0, az: int = 0,
                 gx: int = 0, gy: int = 0, gz: int = 0) -> bytes:
    return struct.pack("<IHHHHhhhhhh", buttons, lx, ly, rx, ry,
                       ax, ay, az, gx, gy, gz)


def frame_dualsense_v5(payload: bytes, sequence: int) -> bytes:
    header_without_crc = struct.pack("<4sBBHI", b"VPCM", 5, 1,
                                     len(payload), sequence)
    crc = zlib.crc32(header_without_crc[4:] + payload) & 0xFFFFFFFF
    return header_without_crc + struct.pack("<I", crc) + payload


def button_frames(kind: str) -> list[tuple[str, bytes]]:
    xbox = [
        ("dpad-up", 0x0001), ("dpad-down", 0x0002),
        ("dpad-left", 0x0004), ("dpad-right", 0x0008),
        ("start", 0x0010), ("back", 0x0020),
        ("left-stick", 0x0040), ("right-stick", 0x0080),
        ("left-bumper", 0x0100), ("right-bumper", 0x0200),
        ("guide", 0x0400), ("A", 0x1000), ("B", 0x2000),
        ("X", 0x4000), ("Y", 0x8000),
    ]
    ds4 = [
        ("square", 0x0010), ("cross", 0x0020), ("circle", 0x0040),
        ("triangle", 0x0080), ("L1", 0x0100), ("R1", 0x0200),
        ("L2-click", 0x0400), ("R2-click", 0x0800),
        ("share", 0x1000), ("options", 0x2000),
        ("L3", 0x4000), ("R3", 0x8000), ("PS", 0x0001),
        ("touchpad-click", 0x0002),
    ]
    ds5 = [
        ("square", 0x00000010), ("cross", 0x00000020),
        ("circle", 0x00000040), ("triangle", 0x00000080),
        ("L1", 0x00000100), ("R1", 0x00000200),
        ("L2-click", 0x00000400), ("R2-click", 0x00000800),
        ("create", 0x00001000), ("options", 0x00002000),
        ("L3", 0x00004000), ("R3", 0x00008000),
        ("PS", 0x00010000), ("touchpad-click", 0x00020000),
        ("mute", 0x00040000),
    ]
    ns2 = [
        ("B", 1 << 0), ("A", 1 << 1), ("Y", 1 << 2), ("X", 1 << 3),
        ("R", 1 << 4), ("ZR", 1 << 5), ("plus", 1 << 6),
        ("right-stick", 1 << 7), ("dpad-down", 1 << 8),
        ("dpad-right", 1 << 9), ("dpad-left", 1 << 10),
        ("dpad-up", 1 << 11), ("L", 1 << 12), ("ZL", 1 << 13),
        ("minus", 1 << 14), ("left-stick", 1 << 15),
        ("home", 1 << 16), ("capture", 1 << 17),
        ("GR", 1 << 18), ("GL", 1 << 19), ("C", 1 << 20),
        ("headset", 1 << 21),
    ]
    values = {"xbox360": xbox, "dualshock4": ds4,
              "dualsense": ds5, "ns2pro": ns2}[kind]
    result: list[tuple[str, bytes]] = []
    for name, mask in values:
        if kind == "xbox360":
            result.append((name, frame_xbox360(buttons=mask)))
        elif kind == "dualshock4":
            result.append((name, frame_ds4(buttons=mask)))
        elif kind == "dualsense":
            result.append((name, frame_dualsense(buttons=mask)))
        else:
            result.append((name, frame_ns2pro(buttons=mask)))
    if kind in ("dualshock4", "dualsense"):
        for name, mask in [("dpad-up", 1), ("dpad-down", 2),
                           ("dpad-left", 4), ("dpad-right", 8)]:
            builder = frame_ds4 if kind == "dualshock4" else frame_dualsense
            result.append((name, builder(dpad=mask)))
    return result


def motion_frames(kind: str) -> list[tuple[str, bytes]]:
    if kind == "xbox360":
        return [("triggers-full", frame_xbox360(lt=255, rt=255)),
                ("sticks-extremes", frame_xbox360(lx=32767, ly=-32768,
                                                   rx=16384, ry=-16384))]
    if kind == "dualshock4":
        return [("triggers-full", frame_ds4(l2=255, r2=255)),
                ("sticks-extremes", frame_ds4(lx=127, ly=-128,
                                               rx=64, ry=-64)),
                ("touch-active", frame_ds4(touch=True))]
    if kind == "dualsense":
        return [("triggers-full", frame_dualsense(l2=255, r2=255)),
                ("sticks-extremes", frame_dualsense(lx=127, ly=-128,
                                                    rx=64, ry=-64)),
                ("touch-active", frame_dualsense(touch=True))]
    return [("sticks-extremes", frame_ns2pro(lx=0, ly=0xFFF,
                                               rx=0xFFF, ry=0)),
            ("motion", frame_ns2pro(ax=1000, ay=-1000, az=500,
                                    gx=250, gy=-250, gz=100))]


def usbip_ports(usbip: Path, port: int) -> set[int]:
    result = subprocess.run([str(usbip), "-t", str(port), "port"],
                            capture_output=True, text=True, timeout=5)
    return {int(value) for value in re.findall(r"Port\s+(\d+): device in use",
                                               result.stdout)}


def attach_device(usbip: Path, usb_port: int, bus_id: str) -> tuple[bool, str]:
    result = subprocess.run(
        [str(usbip), "-t", str(usb_port), "attach", "-r", "127.0.0.1",
         "-b", bus_id, "--once"],
        capture_output=True, text=True, timeout=15,
    )
    text = (result.stdout + result.stderr).strip()
    return result.returncode == 0, text


def send_sequence(kind: str, stream: socket.socket) -> tuple[int, list[dict]]:
    sequence = 1
    tests: list[dict] = []

    def send(name: str, payload: bytes, phase: str) -> None:
        nonlocal sequence
        try:
            if kind == "dualsense":
                stream.sendall(frame_dualsense_v5(payload, sequence))
                sequence += 1
            else:
                stream.sendall(payload)
            tests.append({"name": name, "phase": phase, "status": "PASS"})
        except OSError as error:
            tests.append({"name": name, "phase": phase, "status": "FAIL",
                          "detail": str(error)})
            raise
        time.sleep(SEND_DELAY)

    neutral = {
        "xbox360": frame_xbox360(),
        "dualshock4": frame_ds4(),
        "dualsense": frame_dualsense(),
        "ns2pro": frame_ns2pro(),
    }[kind]
    send("neutral", neutral, "neutral")
    states = button_frames(kind) + motion_frames(kind)
    for name, payload in states:
        send(name, payload, "press" if name in dict(button_frames(kind)) else "motion")
        if name in dict(button_frames(kind)):
            send(name, neutral, "release")
    return len(states), tests


def extract_uid(device: dict, fallback: str) -> str:
    specific = device.get("deviceSpecific") or {}
    for key in ("serial_number", "serialNumber", "serial", "deviceID", "deviceId"):
        value = specific.get(key)
        if value not in (None, ""):
            return str(value)
    return fallback


def md_value(value: object) -> str:
    if isinstance(value, (dict, list)):
        return json.dumps(value, ensure_ascii=False, sort_keys=True)
    text = str(value)
    return text.replace("|", "\\|").replace("\n", "<br>")


def write_report(path: Path, report: dict) -> None:
    lines = [
        "# Reporte de prueba de mandos virtuales VIIPER",
        "",
        f"- **Inicio:** `{report.get('startedAt', '')}`",
        f"- **Fin:** `{datetime.now(timezone.utc).isoformat()}`",
        f"- **Resultado global:** **{report.get('result', 'UNKNOWN')}**",
        f"- **VIIPER:** `{report.get('viiper', '')}`",
        f"- **usbip.exe:** `{report.get('usbip', '')}`",
        "",
        "Este reporte proviene de una ejecución real. Cada estado fue escrito en "
        "el stream de VIIPER; `usbipImported` e `inputStreamActive` son los "
        "indicadores observados por `server/status`. En Xbox One, `usbipBusId` "
        "muestra la dirección numérica del bus para compararla con los demás; "
        "el alias retenido real se conserva como `usbipExportAlias`.",
        "",
        "## Resumen",
        "",
        "| Mando | Tipo | Resultado | Botones/controles | USB/IP | Stream |",
        "|---|---|---|---:|---|---|",
    ]
    for entry in report.get("controllers", []):
        tests = entry.get("tests", [])
        passed = sum(test.get("status") == "PASS" for test in tests)
        total = len(tests)
        identity = entry.get("identity", {})
        lines.append(
            f"| {entry.get('name', '')} | `{entry.get('type', '')}` | "
            f"**{entry.get('result', 'FAIL')}** | {passed}/{total} | "
            f"{identity.get('usbipImported', 'n/d')} | "
            f"{identity.get('inputStreamActive', 'n/d')} |"
        )

    for entry in report.get("controllers", []):
        identity = entry.get("identity", {})
        lines.extend(["", f"## {entry.get('name', '')}", "", "### Identidad", "",
                      "| Campo | Valor |", "|---|---|"])
        for key in (
            "type", "busId", "devId", "vid", "pid", "manufacturer", "product",
            "description", "uid", "serial", "deviceID", "numericBusDevice",
            "usbipBusId", "usbipExportAlias", "usbipPort",
            "deviceSpecific",
            "usbipImported", "inputStreamActive", "result",
        ):
            if key in identity:
                lines.append(f"| `{key}` | {md_value(identity[key])} |")
        if entry.get("error"):
            lines.extend(["", f"**Error:** `{md_value(entry['error'])}`"])

        lines.extend(["", "### Botones y controles", "",
                      "| Control | Fase | Resultado | Detalle |",
                      "|---|---|---|---|"])
        for test in entry.get("tests", []):
            lines.append(
                f"| `{md_value(test.get('name', ''))}` | "
                f"{md_value(test.get('phase', ''))} | "
                f"**{md_value(test.get('status', 'FAIL'))}** | "
                f"{md_value(test.get('detail', ''))} |"
            )
        lines.extend(["", "### Respuestas y estado observados", "", "```json"])
        lines.append(json.dumps({
            "createResponse": entry.get("createResponse"),
            "statusSnapshot": entry.get("statusSnapshot"),
            "clientOutput": entry.get("clientOutput"),
        }, ensure_ascii=False, indent=2, sort_keys=True))
        lines.extend(["```", ""])

    if report.get("error"):
        lines.extend(["## Error de ejecución", "", f"`{md_value(report['error'])}`", ""])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines), encoding="utf-8")


def find_status_device(status: dict, bus_id: int, dev_id: str) -> dict:
    for bus in status.get("buses", []):
        if bus.get("busId") != bus_id:
            continue
        for device in bus.get("devices", []):
            if str(device.get("devId")) == str(dev_id):
                return device
    return {}


def run_generic_matrix(args: argparse.Namespace, report: dict) -> int:
    viiper = find_executable(args.viiper, [
        ROOT / "viiper.exe",
        ROOT.parents[2] / "outputs" / "viiper.exe",
        ROOT.parent / "outputs" / "viiper.exe",
        "viiper.exe",
    ])
    usbip = find_executable(args.usbip, [
        Path(r"C:\Program Files\USBip\usbip.exe"), "usbip.exe", "usbip"
    ])
    report["viiper"] = str(viiper)
    report["usbip"] = str(usbip)
    combined = not args.skip_xboxone
    if combined and args.no_attach:
        die("--no-attach no es compatible con Xbox One/Series; use --skip-xboxone")
    env = os.environ.copy()
    env["PATH"] = str(usbip.parent) + os.pathsep + env.get("PATH", "")
    server_args = [
        str(viiper), "server", "--usb.addr=" +
        ("0.0.0.0:" if combined else "127.0.0.1:") + str(args.usb_port),
        "--api.addr=127.0.0.1:" + str(args.api_port),
        "--api.auto-attach-local-client=" + ("true" if combined else "false"),
    ]
    if combined:
        server_args.extend([
            "--api.auto-attach-windows-native=false",
            "--usb.retained-import-authority-id=1",
        ])
    server = subprocess.Popen(server_args, env=env,
                              stdout=subprocess.DEVNULL,
                              stderr=subprocess.STDOUT)
    streams: list[socket.socket] = []
    attached_ports: set[int] = set()
    bus_id: int | None = None
    try:
        for _ in range(60):
            try:
                status = api_request("127.0.0.1", args.api_port, "server/status")
                if status.get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.2)
        else:
            die("VIIPER no abrió el API; revise el ejecutable y los puertos")

        bus_id = int(api_request("127.0.0.1", args.api_port, "bus/create")["busId"])
        print(f"[INFO] Bus virtual creado: {bus_id}")
        specs = [
            ("xbox360", "xbox360"),
            ("dualshock4", "dualshock4"),
            ("dualsense", "dualsensegamepadv5"),
            ("ns2pro", "ns2pro"),
        ]
        before_ports = usbip_ports(usbip, args.usb_port)
        for label, device_type in specs:
            entry = {
                "name": label,
                "type": device_type,
                "result": "FAIL",
                "tests": [],
                "identity": dict(PROFILE_INFO[label]),
            }
            report["controllers"].append(entry)
            try:
                response = api_request(args.host, args.api_port,
                                       f'bus/{bus_id}/add {{"type":"{device_type}"}}')
                dev_id = str(response["devId"])
                entry["createResponse"] = response
                entry["identity"].update({
                    "busId": bus_id,
                    "devId": dev_id,
                    "usbipBusId": response.get("usbipBusId", f"{bus_id}-{dev_id}"),
                    "uid": extract_uid(response, PROFILE_INFO[label]["uid"]),
                })
                stream = open_stream(args.host, args.api_port, bus_id, dev_id)
                streams.append(stream)
                sent, tests = send_sequence(label, stream)
                entry["tests"].extend(tests)
                mounted = args.no_attach
                attach_text = "omitido (--no-attach)"
                if combined:
                    time.sleep(0.4)
                    new_ports = usbip_ports(usbip, args.usb_port) - before_ports
                    attached_ports.update(new_ports)
                    mounted = bool(new_ports)
                    entry["identity"]["usbipPort"] = sorted(new_ports)
                    attach_text = "auto-attach usbip.exe"
                elif not args.no_attach:
                    ok, attach_text = attach_device(
                        usbip, args.usb_port,
                        response.get("usbipBusId", f"{bus_id}-{dev_id}"),
                    )
                    time.sleep(0.4)
                    mounted = ok
                    if ok:
                        new_ports = usbip_ports(usbip, args.usb_port) - before_ports
                        attached_ports.update(new_ports)
                        entry["identity"]["usbipPort"] = sorted(new_ports)
                status = api_request(args.host, args.api_port, "server/status")
                live = find_status_device(status, bus_id, dev_id)
                entry["statusSnapshot"] = status
                entry["identity"].update({
                    "vid": live.get("vid", entry["identity"].get("vid")),
                    "pid": live.get("pid", entry["identity"].get("pid")),
                    "deviceSpecific": live.get("deviceSpecific", {}),
                    "usbipImported": live.get("usbipImported", False),
                    "inputStreamActive": live.get("inputStreamActive", False),
                })
                if not args.no_attach and not mounted:
                    print(f"[WARN] {label}: no se confirmó attach USB/IP", file=sys.stderr)
                entry["identity"]["description"] = entry["identity"]["product"]
                entry["result"] = "PASS" if (
                    all(test.get("status") == "PASS" for test in entry["tests"])
                    and live.get("inputStreamActive") is True
                    and (args.no_attach or live.get("usbipImported") is True)
                ) else "FAIL"
                print(f"[{entry['result']}] {label}: {sent} estados enviados; "
                      f"stream={live.get('inputStreamActive')}; "
                      f"usbipImported={live.get('usbipImported')}; {attach_text}")
                before_ports = usbip_ports(usbip, args.usb_port)
                time.sleep(0.1)
            except Exception as error:
                entry["error"] = str(error)
                raise

        if combined:
            run_xboxone_client_on_server(args, report, args.api_port, bus_id,
                                         viiper)
        return 0
    finally:
        for stream in streams:
            try:
                stream.close()
            except OSError:
                pass
        for port in sorted(attached_ports):
            subprocess.run([str(usbip), "detach", "-p", str(port)],
                           capture_output=True, text=True, timeout=10)
        if bus_id is not None:
            try:
                api_request(args.host, args.api_port,
                            f"bus/remove {bus_id}", timeout=3)
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
        if server.poll() is None:
            try:
                api_request(args.host, args.api_port, "server/shutdown", timeout=5)
            except (OSError, RuntimeError, json.JSONDecodeError):
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    print("[WARN] El servidor no confirmó el cierre interno", file=sys.stderr)
        print("[INFO] VIIPER cerrado mediante server/shutdown")


def parse_xbox_client_tests(output: str) -> list[dict]:
    tests: list[dict] = []
    if "broker: estado neutral aceptado" in output:
        tests.append({"name": "neutral", "phase": "neutral", "status": "PASS"})
    for line in output.splitlines():
        button = re.search(r"broker: button (.+) (pressed|released)", line)
        if button:
            tests.append({
                "name": button.group(1),
                "phase": "press" if button.group(2) == "pressed" else "release",
                "status": "PASS",
                "detail": line.strip(),
            })
        control = re.search(r"broker: control (.+) aceptado", line)
        if control:
            tests.append({"name": control.group(1), "phase": "motion",
                          "status": "PASS", "detail": line.strip()})
    return tests


def run_xboxone_client_on_server(
    args: argparse.Namespace, report: dict, api_port: int, bus_id: int,
    viiper: Path,
) -> None:
    xbox_client, xbox_client_args = resolve_xboxone_client(args, viiper)
    key_file = Path(os.environ.get("APPDATA", "")) / "VIIPER" / "viiper.key.txt"
    if not key_file.is_file():
        die(f"No se encontró la clave de VIIPER: {key_file}")

    profile_key = args.xbox_profile
    entry = {
        "name": "Xbox One/Series",
        "type": profile_key,
        "result": "FAIL",
        "tests": [],
        "identity": dict(PROFILE_INFO[profile_key]),
    }
    report["controllers"].append(entry)
    report["xboxoneClient"] = str(xbox_client)
    report["xboxoneClientCommand"] = xbox_client_args or [str(xbox_client)]
    client = subprocess.Popen(
        [str(xbox_client), *xbox_client_args,
         "--addr", f"127.0.0.1:{api_port}",
         "--key-file", str(key_file), "--bus-id", str(bus_id),
         "--profile", profile_key,
         "--input-test", "--hold-seconds", "1"],
        stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    status = {}
    best_status = {}
    best_status_score = (-1, -1)
    deadline = time.monotonic() + 60
    while client.poll() is None and time.monotonic() < deadline:
        try:
            status = api_request("127.0.0.1", api_port, "server/status")
            score = (status.get("activeStreams", 0),
                     status.get("activeImports", 0))
            if score > best_status_score:
                best_status = status
                best_status_score = score
        except (OSError, RuntimeError, json.JSONDecodeError):
            pass
        time.sleep(0.1)
    if client.poll() is None:
        client.kill()
        output, _ = client.communicate(timeout=5)
        die("La prueba Xbox One/Series excedió 60 segundos:\n" + output)
    output, _ = client.communicate(timeout=5)
    output = output.strip()
    entry["clientOutput"] = output
    entry["tests"] = parse_xbox_client_tests(output)
    entry["statusSnapshot"] = best_status or status
    persona = re.search(
        r'persona creada: bus=(\d+) dev=(\S+) vid=([0-9A-Fa-f]+) '
        r'pid=([0-9A-Fa-f]+) producto="([^"]+)" '
        r'deviceID=([0-9A-Fa-f]+) serial=(\S+) usbip=(\S+)', output)
    if persona:
        entry["identity"].update({
            "busId": int(persona.group(1)),
            "devId": persona.group(2),
            "numericBusDevice": f"{persona.group(1)}-{persona.group(2)}",
            "vid": "0x" + persona.group(3).lower(),
            "pid": "0x" + persona.group(4).lower(),
            "product": persona.group(5),
            "description": persona.group(5),
            "deviceID": "0x" + persona.group(6).lower(),
            "serial": persona.group(7),
            "uid": persona.group(7),
            "usbipBusId": f"{persona.group(1)}-{persona.group(2)}",
            "usbipExportAlias": persona.group(8),
        })
    attach = re.search(r"attach nativo aceptado: .* port=(\d+)", output)
    if attach:
        entry["identity"]["usbipPort"] = [int(attach.group(1))]
    observed_status = best_status or status
    live = find_status_device(observed_status, bus_id,
                               entry["identity"].get("devId", ""))
    entry["identity"].update({
        "usbipImported": live.get("usbipImported", False),
        "inputStreamActive": live.get("inputStreamActive", False),
        "deviceSpecific": live.get("deviceSpecific", {}),
    })
    entry["result"] = "PASS" if (
        client.returncode == 0 and entry["tests"] and
        all(test.get("status") == "PASS" for test in entry["tests"])
    ) else "FAIL"
    if entry["result"] != "PASS":
        die("La prueba Xbox One/Series falló:\n" + output)
    print("[PASS] xboxone/xboxseries: matriz autenticada en el bus compartido")


def run_xboxone_matrix(args: argparse.Namespace, report: dict) -> int:
    """Run the retained, authenticated Xbox One/Series path in isolation."""
    viiper = find_executable(args.viiper, [
        ROOT / "viiper.exe",
        ROOT.parents[2] / "outputs" / "viiper.exe",
        ROOT.parent / "outputs" / "viiper.exe",
        "viiper.exe",
    ])
    xbox_client, xbox_client_args = resolve_xboxone_client(args, viiper)
    report["viiper"] = str(viiper)
    report["xboxoneClient"] = str(xbox_client)
    report["xboxoneClientCommand"] = xbox_client_args or [str(xbox_client)]
    key_file = Path(os.environ.get("APPDATA", "")) / "VIIPER" / "viiper.key.txt"
    if not key_file.is_file():
        die(f"No se encontró la clave de VIIPER: {key_file}")

    entry = {
        "name": "Xbox One/Series",
        "type": "xboxone",
        "result": "FAIL",
        "tests": [],
        "identity": dict(PROFILE_INFO["xboxone"]),
    }
    report["controllers"].append(entry)

    usb_port = args.usb_port + 2
    api_port = args.api_port + 2
    env = os.environ.copy()
    server_args = [
        # The Windows native/fallback attach resolves the host through the
        # active LAN adapter, so the isolated USB/IP listener must not be
        # restricted to loopback. The API remains loopback-only below.
        str(viiper), "server", "--usb.addr=0.0.0.0:" + str(usb_port),
        "--api.addr=127.0.0.1:" + str(api_port),
        "--api.auto-attach-local-client=true",
        "--usb.retained-import-authority-id=1",
    ]
    server = subprocess.Popen(server_args, env=env,
                              stdout=subprocess.DEVNULL,
                              stderr=subprocess.STDOUT)
    try:
        for _ in range(60):
            try:
                status = api_request("127.0.0.1", api_port, "server/status")
                if status.get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.2)
        else:
            die("VIIPER no abrió el API Xbox One/Series")

        print("[INFO] Ejecutando la matriz autenticada Xbox One/Series...")
        client = subprocess.Popen(
            [str(xbox_client), *xbox_client_args,
             "--addr", f"127.0.0.1:{api_port}",
             "--key-file", str(key_file), "--input-test", "--hold-seconds", "1"],
            env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True,
        )
        status = {}
        best_status = {}
        best_status_score = (-1, -1)
        deadline = time.monotonic() + 60
        while client.poll() is None and time.monotonic() < deadline:
            try:
                status = api_request("127.0.0.1", api_port, "server/status")
                score = (status.get("activeStreams", 0),
                         status.get("activeImports", 0))
                if score > best_status_score:
                    best_status = status
                    best_status_score = score
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
            time.sleep(0.1)
        if client.poll() is None:
            client.kill()
            output, _ = client.communicate(timeout=5)
            die("La prueba Xbox One/Series excedió 60 segundos:\n" + output)
        output, _ = client.communicate(timeout=5)
        output = output.strip()
        entry["clientOutput"] = output
        entry["tests"] = parse_xbox_client_tests(output)
        entry["statusSnapshot"] = best_status or status
        persona = re.search(
            r'persona creada: bus=(\d+) dev=(\S+) vid=([0-9A-Fa-f]+) '
            r'pid=([0-9A-Fa-f]+) producto="([^"]+)" '
            r'deviceID=([0-9A-Fa-f]+) serial=(\S+) usbip=(\S+)', output)
        if persona:
            entry["identity"].update({
                "busId": int(persona.group(1)),
                "devId": persona.group(2),
                "vid": "0x" + persona.group(3).lower(),
                "pid": "0x" + persona.group(4).lower(),
                "product": persona.group(5),
                "description": persona.group(5),
                "deviceID": "0x" + persona.group(6).lower(),
                "serial": persona.group(7),
                "uid": persona.group(7),
                "usbipBusId": persona.group(8),
            })
        attach = re.search(r"attach nativo aceptado: .* port=(\d+)", output)
        if attach:
            entry["identity"]["usbipPort"] = [int(attach.group(1))]
        observed_status = best_status or status
        live = next(iter(observed_status.get("buses", [{}])[0].get("devices", [])), {}) \
            if observed_status.get("buses") else {}
        entry["identity"].update({
            "usbipImported": live.get("usbipImported", False),
            "inputStreamActive": live.get("inputStreamActive", False),
        })
        entry["result"] = "PASS" if (
            client.returncode == 0 and entry["tests"] and
            all(test.get("status") == "PASS" for test in entry["tests"])
        ) else "FAIL"
        if entry["result"] != "PASS":
            die("La prueba Xbox One/Series falló:\n" + output)
        print("[PASS] xboxone/xboxseries: matriz autenticada completada")
        return 0
    except Exception as error:
        entry["error"] = str(error)
        raise
    finally:
        if server.poll() is None:
            try:
                api_request("127.0.0.1", api_port, "server/shutdown", timeout=5)
            except (OSError, RuntimeError, json.JSONDecodeError):
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    print("[WARN] El servidor Xbox no confirmó el cierre interno",
                          file=sys.stderr)
        print("[INFO] VIIPER Xbox One/Series cerrado mediante server/shutdown")


def run(args: argparse.Namespace) -> int:
    report = args._report
    run_generic_matrix(args, report)
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--viiper", help="Ruta a viiper.exe")
    parser.add_argument("--usbip", help="Ruta a usbip.exe")
    parser.add_argument(
        "--xboxone-client",
        help="Ruta opcional a un cliente Xbox externo; por defecto usa viiper.exe xboxone-client",
    )
    parser.add_argument("--xbox-profile", choices=("xboxone", "xboxseries"),
                        default="xboxone",
                        help="Perfil de identidad Xbox que se probará")
    parser.add_argument("--skip-xboxone", action="store_true",
                        help="Omite la matriz autenticada Xbox One/Series")
    parser.add_argument("--no-attach", action="store_true",
                        help="No montar por USB/IP; solo prueba API y streams")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--usb-port", type=int, default=DEFAULT_USB_PORT)
    parser.add_argument("--api-port", type=int, default=DEFAULT_API_PORT)
    parser.add_argument("--report", default="viiper_controller_test_report.md",
                        help="Ruta del reporte Markdown único")
    args = parser.parse_args()
    report_path = Path(args.report).expanduser().resolve()
    report = {
        "startedAt": datetime.now(timezone.utc).isoformat(),
        "result": "FAIL",
        "controllers": [],
    }
    args._report = report
    try:
        result = run(args)
        report["result"] = "PASS" if report["controllers"] and all(
            entry.get("result") == "PASS" for entry in report["controllers"]
        ) else "FAIL"
        return result
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        report["error"] = str(error)
        print(f"[FAIL] {error}", file=sys.stderr)
        return 1
    finally:
        write_report(report_path, report)
        print(f"[REPORT] {report_path}")


if __name__ == "__main__":
    raise SystemExit(main())
