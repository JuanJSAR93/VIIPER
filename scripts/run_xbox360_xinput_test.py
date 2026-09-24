#!/usr/bin/env python3
"""Create a real VIIPER Xbox 360 USB/IP device and verify XInput input."""

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

from run_xbox_gip_joy_test import xinput_slots


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


def api_request(port: int, command: str) -> dict:
    with socket.create_connection(("127.0.0.1", port), timeout=5) as conn:
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


def frame(buttons: int = 0, lt: int = 0, rt: int = 0,
          lx: int = 0, ly: int = 0, rx: int = 0, ry: int = 0) -> bytes:
    return struct.pack("<IBBhhhh6s", buttons, lt, rt, lx, ly, rx, ry,
                       b"\0" * 6)


def ports(usbip: Path, usb_port: int) -> set[int]:
    result = subprocess.run([str(usbip), "-t", str(usb_port), "port"],
                            capture_output=True, text=True, timeout=5)
    return {int(value) for value in re.findall(
        r"Port\s+(\d+): device in use", result.stdout)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--viiper")
    parser.add_argument("--usbip")
    parser.add_argument("--seconds", type=int, default=10)
    parser.add_argument("--usb-port", type=int, default=3411)
    parser.add_argument("--api-port", type=int, default=3412)
    args = parser.parse_args()
    viiper = find_file(args.viiper, [
        ROOT / "viiper.exe", ROOT.parents[1] / "outputs" / "viiper.exe",
        ROOT.parent / "outputs" / "viiper.exe", "viiper.exe",
    ])
    usbip = find_file(args.usbip, [
        Path(r"C:\Program Files\USBip\usbip.exe"), "usbip.exe", "usbip",
    ])
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
                if api_request(args.api_port, "server/status").get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.15)
        else:
            raise RuntimeError("VIIPER no abrió el API")

        bus_id = int(api_request(args.api_port, "bus/create")["busId"])
        created = api_request(args.api_port,
                              f'bus/{bus_id}/add {{"type":"xbox360"}}')
        dev_id = str(created["devId"])
        stream = socket.create_connection(("127.0.0.1", args.api_port), timeout=5)
        stream.sendall(f"bus/{bus_id}/{dev_id}\0".encode())
        stream.sendall(frame())
        before = ports(usbip, args.usb_port)
        attach = subprocess.run([
            str(usbip), "-t", str(args.usb_port), "attach", "-r", "127.0.0.1",
            "-b", str(created.get("usbipBusId", f"{bus_id}-{dev_id}")), "--once",
        ], capture_output=True, text=True, timeout=20)
        if attach.returncode != 0:
            raise RuntimeError((attach.stdout + attach.stderr).strip())
        after = ports(usbip, args.usb_port)
        new_ports = sorted(after - before)
        attached_port = new_ports[0] if new_ports else (max(after) if after else 1)
        usbip_bus_id = created.get("usbipBusId", f"{bus_id}-{dev_id}")
        print(f"Creado: xbox360 bus={bus_id} dev={dev_id}")
        print(f"Producto: {created.get('product', 'VIIPER Xbox 360 Controller')}")
        print(f"usbip: {usbip_bus_id} -> puerto local {attached_port}")

        states = [
            ("A", frame(buttons=0x1000)),
            ("B", frame(buttons=0x2000)),
            ("dpad-up", frame(buttons=0x0001)),
            ("left-trigger", frame(lt=255)),
            ("sticks", frame(lx=32767, ly=-32768, rx=16384, ry=-16384)),
        ]
        baseline = xinput_slots()[0]
        dynamic = False
        a_seen = False
        deadline = time.monotonic() + args.seconds
        while time.monotonic() < deadline:
            for name, state in states:
                if time.monotonic() >= deadline:
                    break
                stream.sendall(state)
                hold_until = time.monotonic() + 0.25
                while time.monotonic() < hold_until:
                    current = xinput_slots()[0]
                    if current != baseline and current[1] == 0:
                        dynamic = True
                    if name == "A" and current[3] & 0x1000:
                        a_seen = True
                    time.sleep(0.025)
                stream.sendall(frame())
        time.sleep(0.25)
        stream.sendall(frame())
        print(f"XINPUT_DYNAMIC_STATE_SEEN={dynamic}")
        print(f"XINPUT_A_SEEN={a_seen}")
        print(f"XINPUT_FINAL={xinput_slots()[0]}")
        return 0 if dynamic and a_seen else 1
    finally:
        if stream is not None:
            stream.close()
        if attached_port is not None:
            subprocess.run([str(usbip), "-t", str(args.usb_port), "detach",
                            "--port", str(attached_port)],
                           capture_output=True, text=True, timeout=10)
        try:
            api_request(args.api_port, "server/shutdown")
        except Exception:
            pass
        server.wait(timeout=15)


if __name__ == "__main__":
    raise SystemExit(main())
