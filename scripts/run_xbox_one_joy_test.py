#!/usr/bin/env python3
"""Create a real VIIPER Xbox HID gamepad and exercise it for joy.cpl.

This deliberately uses xboxonehid/xboxserieshid instead of the retained
xboxone-client GIP persona. The latter is for XInput/XboxComposite and is not
enumerated by the legacy Windows game-controller panel.
"""

from __future__ import annotations

import argparse
import json
import re
import shutil
import socket
import struct
import subprocess
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


def find_file(explicit: str | None, candidates: list[Path | str]) -> Path:
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if path.is_file():
            return path
        raise RuntimeError(f"No se encontró: {path}")
    for candidate in candidates:
        path = Path(candidate)
        if path.is_file():
            return path.resolve()
    for name in candidates:
        if isinstance(name, str):
            found = shutil.which(name)
            if found:
                return Path(found).resolve()
    raise RuntimeError("No se encontró el ejecutable requerido")


def api_request(host: str, port: int, command: str) -> dict:
    with socket.create_connection((host, port), timeout=5) as conn:
        conn.settimeout(5)
        conn.sendall(command.encode() + b"\0")
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
        raise RuntimeError(f"API sin respuesta: {command}")
    result = json.loads(raw.splitlines()[0])
    if result.get("status", 200) >= 400:
        raise RuntimeError(result)
    return result


def frame(buttons: int = 0, left_trigger: int = 0,
          right_trigger: int = 0, left_x: int = 0, left_y: int = 0,
          right_x: int = 0, right_y: int = 0) -> bytes:
    # buttons:u16, LT:u16, RT:u16, LX:i16, LY:i16, RX:i16, RY:i16
    return struct.pack("<HHHhhhh", buttons, left_trigger, right_trigger,
                       left_x, left_y, right_x, right_y)


def used_usbip_ports(usbip: Path, usb_port: int) -> set[int]:
    result = subprocess.run([str(usbip), "-t", str(usb_port), "port"],
                            capture_output=True, text=True, timeout=5)
    return {int(value) for value in re.findall(
        r"Port\s+(\d+): device in use", result.stdout)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--viiper", help="ruta a viiper-gip-xone-base.exe")
    parser.add_argument("--usbip", help="ruta a usbip.exe")
    parser.add_argument("--profile", choices=("xboxone", "xboxseries"),
                        default="xboxone")
    parser.add_argument("--seconds", type=int, default=30)
    parser.add_argument("--usb-port", type=int, default=3291)
    parser.add_argument("--api-port", type=int, default=3292)
    args = parser.parse_args()
    if args.seconds < 1:
        raise RuntimeError("--seconds debe ser positivo")

    viiper = find_file(args.viiper, [
        ROOT / "viiper-gip-xone-base.exe",
        ROOT.parents[1] / "outputs" / "viiper-gip-xone-base.exe",
        ROOT.parent / "outputs" / "viiper-gip-xone-base.exe",
        "viiper-gip-xone-base.exe",
    ])
    usbip = find_file(args.usbip, [
        Path(r"C:\Program Files\USBip\usbip.exe"), "usbip.exe", "usbip",
    ])
    device_type = "xboxonehid" if args.profile == "xboxone" else "xboxserieshid"
    server = subprocess.Popen([
        str(viiper), "server", f"--usb.addr=127.0.0.1:{args.usb_port}",
        f"--api.addr=127.0.0.1:{args.api_port}",
        "--api.auto-attach-local-client=false",
    ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    stream: socket.socket | None = None
    attached_port: int | None = None
    try:
        for _ in range(80):
            try:
                if api_request("127.0.0.1", args.api_port,
                               "server/status").get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.15)
        else:
            raise RuntimeError("VIIPER no abrió el API")

        bus_id = int(api_request("127.0.0.1", args.api_port,
                                 "bus/create")["busId"])
        created = api_request(
            "127.0.0.1", args.api_port,
            f'bus/{bus_id}/add {{"type":"{device_type}"}}')
        dev_id = str(created["devId"])
        stream = socket.create_connection(("127.0.0.1", args.api_port), timeout=5)
        stream.sendall(f"bus/{bus_id}/{dev_id}\0".encode())
        stream.sendall(frame())

        bus_id_usbip = created.get("usbipBusId", f"{bus_id}-{dev_id}")
        before = used_usbip_ports(usbip, args.usb_port)
        attach = subprocess.run([
            str(usbip), "-t", str(args.usb_port), "attach", "-r", "127.0.0.1",
            "-b", str(bus_id_usbip), "--once",
        ], capture_output=True, text=True, timeout=20)
        if attach.returncode != 0:
            raise RuntimeError((attach.stdout + attach.stderr).strip())
        after = used_usbip_ports(usbip, args.usb_port)
        new_ports = sorted(after - before)
        attached_port = new_ports[0] if new_ports else (max(after) if after else 1)

        print(f"Creado: {device_type} bus={bus_id} dev={dev_id}")
        print(f"VID:PID HID: {created.get('vid')}:{created.get('pid')}")
        print(f"Producto: VIIPER Xbox {'One' if args.profile == 'xboxone' else 'Series X|S'} Controller")
        print(f"usbip: {bus_id_usbip} -> puerto local {attached_port}")
        print("Abre joy.cpl y selecciona 'Dispositivo de juego compatible con HID'.")
        print("La prueba repetirá botones, triggers y sticks durante "
              f"{args.seconds} segundos.")

        sequence = [
            ("A", frame(buttons=1 << 11)),
            ("B", frame(buttons=1 << 12)),
            ("X", frame(buttons=1 << 13)),
            ("Y", frame(buttons=1 << 14)),
            ("dpad-up", frame(buttons=1 << 0)),
            ("dpad-down", frame(buttons=1 << 1)),
            ("dpad-left", frame(buttons=1 << 2)),
            ("dpad-right", frame(buttons=1 << 3)),
            ("menu", frame(buttons=1 << 4)),
            ("view", frame(buttons=1 << 5)),
            ("left-stick-click", frame(buttons=1 << 6)),
            ("right-stick-click", frame(buttons=1 << 7)),
            ("left-bumper", frame(buttons=1 << 8)),
            ("right-bumper", frame(buttons=1 << 9)),
            ("left-trigger", frame(left_trigger=1023)),
            ("right-trigger", frame(right_trigger=1023)),
            ("left-stick", frame(left_x=-32768, left_y=32767)),
            ("right-stick", frame(right_x=32767, right_y=-32768)),
            ("guide", frame(buttons=1 << 10)),
            ("share", frame(buttons=1 << 15)),
        ]
        neutral = frame()
        deadline = time.monotonic() + args.seconds
        while time.monotonic() < deadline:
            for name, state in sequence:
                if time.monotonic() >= deadline:
                    break
                stream.sendall(state)
                print(f"  {name}", flush=True)
                time.sleep(0.18)
                stream.sendall(neutral)
                time.sleep(0.08)
        stream.sendall(neutral)
        print("Prueba HID finalizada; se deja el estado neutral.")
        return 0
    finally:
        if stream is not None:
            stream.close()
        if attached_port is not None:
            subprocess.run([str(usbip), "-t", str(args.usb_port), "detach",
                            "--port", str(attached_port)],
                           capture_output=True, text=True, timeout=10)
        try:
            api_request("127.0.0.1", args.api_port, "server/shutdown")
        except Exception:
            pass
        server.wait(timeout=15)


if __name__ == "__main__":
    raise SystemExit(main())
