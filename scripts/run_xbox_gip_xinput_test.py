#!/usr/bin/env python3
"""Mount one native VIIPER Xbox GIP persona and verify it through XInput.

Examples:
    python scripts/run_xbox_gip_xinput_test.py --profile one
    python scripts/run_xbox_gip_xinput_test.py --profile series

The test creates an isolated server, injects each XInput-visible control,
verifies the values returned by Windows' XInput API, exercises rumble in the
opposite direction, then detaches and closes VIIPER via server/shutdown.
"""

from __future__ import annotations

import argparse
import ctypes
from ctypes import wintypes as w
import json
from pathlib import Path
import re
import socket
import struct
import subprocess
import time


ROOT = Path(__file__).resolve().parents[1]


class XInputGamepad(ctypes.Structure):
    _fields_ = [
        ("buttons", w.WORD),
        ("left_trigger", w.BYTE),
        ("right_trigger", w.BYTE),
        ("left_x", ctypes.c_short),
        ("left_y", ctypes.c_short),
        ("right_x", ctypes.c_short),
        ("right_y", ctypes.c_short),
    ]


class XInputState(ctypes.Structure):
    _fields_ = [("packet", w.DWORD), ("gamepad", XInputGamepad)]


class XInputVibration(ctypes.Structure):
    _fields_ = [("left_motor", w.WORD), ("right_motor", w.WORD)]


def api_request(port: int, command: str, timeout: float = 5.0) -> dict:
    with socket.create_connection(("127.0.0.1", port), timeout=timeout) as conn:
        conn.settimeout(timeout)
        conn.sendall(command.encode("ascii") + b"\0")
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
    response = json.loads(raw.splitlines()[0])
    if response.get("status", 200) >= 400:
        raise RuntimeError(response)
    return response


def input_frame(buttons: int = 0, left_trigger: int = 0, right_trigger: int = 0,
                left_x: int = 0, left_y: int = 0, right_x: int = 0,
                right_y: int = 0) -> bytes:
    return struct.pack("<HHHhhhh", buttons, left_trigger, right_trigger,
                       left_x, left_y, right_x, right_y)


def buttons_for_xinput(source_buttons: int) -> int:
    """Translate the public VIIPER input bits into XInput's button mask."""
    mapping = (
        (1 << 0, 0x0001), (1 << 1, 0x0002), (1 << 2, 0x0004),
        (1 << 3, 0x0008), (1 << 4, 0x0010), (1 << 5, 0x0020),
        (1 << 6, 0x0040), (1 << 7, 0x0080), (1 << 8, 0x0100),
        (1 << 9, 0x0200), (1 << 11, 0x1000), (1 << 12, 0x2000),
        (1 << 13, 0x4000), (1 << 14, 0x8000),
    )
    return sum(target for source, target in mapping if source_buttons & source)


def usbip_ports(usbip: Path, server_port: int) -> set[int]:
    result = subprocess.run([str(usbip), "-t", str(server_port), "port"],
                            capture_output=True, text=True, timeout=5)
    return {int(port) for port in re.findall(r"Port\s+(\d+): device in use",
                                             result.stdout)}


def xinput_api() -> tuple[object, object]:
    dll = ctypes.WinDLL("XInput1_4.dll")
    get_state = dll.XInputGetState
    get_state.argtypes = [w.DWORD, ctypes.POINTER(XInputState)]
    get_state.restype = w.DWORD
    set_state = dll.XInputSetState
    set_state.argtypes = [w.DWORD, ctypes.POINTER(XInputVibration)]
    set_state.restype = w.DWORD
    return get_state, set_state


def all_xinput_states(get_state: object) -> list[tuple[int, int, XInputState]]:
    states = []
    for slot in range(4):
        state = XInputState()
        result = get_state(slot, ctypes.byref(state))
        states.append((slot, result, state))
    return states


def first_matching_slot(get_state: object, predicate: object) -> int | None:
    for slot, result, state in all_xinput_states(get_state):
        if result == 0 and predicate(state.gamepad):
            return slot
    return None


def wait_for_slot(get_state: object, timeout: float) -> int:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for slot, result, _ in all_xinput_states(get_state):
            if result == 0:
                return slot
        time.sleep(0.1)
    raise RuntimeError("Windows no expuso un slot XInput en el tiempo esperado")


def wait_for_injected_slot(get_state: object, timeout: float) -> int:
    """Find the VIIPER slot, without confusing it with a physical controller."""
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for slot, result, state in all_xinput_states(get_state):
            gamepad = state.gamepad
            if (result == 0 and gamepad.buttons & 0x1000 and
                    gamepad.left_trigger == 255 and gamepad.right_trigger == 128 and
                    gamepad.left_x == 12345 and gamepad.left_y == -23456 and
                    gamepad.right_x == -12000 and gamepad.right_y == 3000):
                return slot
        time.sleep(0.05)
    raise RuntimeError("no se pudo identificar el slot XInput inyectado por VIIPER")


def read_feedback(stream: socket.socket, timeout: float) -> bytes:
    deadline = time.monotonic() + timeout
    data = b""
    while len(data) < 2 and time.monotonic() < deadline:
        try:
            data += stream.recv(2 - len(data))
        except socket.timeout:
            pass
    return data


def run(args: argparse.Namespace) -> int:
    profile = {"one": "xboxone-gip", "series": "xboxseries-gip"}[args.profile]
    viiper = Path(args.viiper) if args.viiper else ROOT / "build" / "viiper.exe"
    usbip = Path(args.usbip) if args.usbip else Path(r"C:\Program Files\USBip\usbip.exe")
    if not viiper.is_file():
        raise RuntimeError(f"No se encontró VIIPER: {viiper}")
    if not usbip.is_file():
        raise RuntimeError(f"No se encontró usbip.exe: {usbip}")

    get_state, set_state = xinput_api()
    server = subprocess.Popen([
        str(viiper), "server", f"--usb.addr=127.0.0.1:{args.usb_port}",
        f"--api.addr=127.0.0.1:{args.api_port}",
        "--api.auto-attach-local-client=false",
    ], cwd=ROOT, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    stream: socket.socket | None = None
    attached_port: int | None = None
    try:
        for _ in range(100):
            try:
                if api_request(args.api_port, "server/status", 1).get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.1)
        else:
            raise RuntimeError("VIIPER no abrió su API")

        bus_id = int(api_request(args.api_port, "bus/create")["busId"])
        created = api_request(args.api_port,
                              f'bus/{bus_id}/add {json.dumps({"type": profile})}')
        dev_id = str(created["devId"])
        stream = socket.create_connection(("127.0.0.1", args.api_port), timeout=5)
        stream.settimeout(0.2)
        stream.sendall(f"bus/{bus_id}/{dev_id}\0".encode("ascii"))
        stream.sendall(input_frame())

        before = usbip_ports(usbip, args.usb_port)
        result = subprocess.run([
            str(usbip), "-t", str(args.usb_port), "attach", "-r", "127.0.0.1",
            "-b", created.get("usbipBusId", f"{bus_id}-{dev_id}"), "--once",
        ], capture_output=True, text=True, timeout=20)
        if result.returncode != 0:
            raise RuntimeError((result.stdout + result.stderr).strip())
        new_ports = usbip_ports(usbip, args.usb_port) - before
        attached_port = next(iter(new_ports)) if new_ports else None

        # First wait for GIP/XInput activation, then identify our own slot using an
        # input vector unlikely to be mirrored by a physical controller.
        wait_for_slot(get_state, args.ready_timeout)
        stream.sendall(input_frame(buttons=1 << 11, left_trigger=1023,
                                   right_trigger=512, left_x=12345, left_y=-23456,
                                   right_x=-12000, right_y=3000))
        slot = wait_for_injected_slot(get_state, args.ready_timeout)
        stream.sendall(input_frame())
        print(f"XInput listo: perfil={profile}, slot={slot}")

        buttons = [
            ("DPad Up", 1 << 0, 0x0001), ("DPad Down", 1 << 1, 0x0002),
            ("DPad Left", 1 << 2, 0x0004), ("DPad Right", 1 << 3, 0x0008),
            ("Menu", 1 << 4, 0x0010), ("View", 1 << 5, 0x0020),
            ("Left Stick", 1 << 6, 0x0040), ("Right Stick", 1 << 7, 0x0080),
            ("LB", 1 << 8, 0x0100), ("RB", 1 << 9, 0x0200),
            ("A", 1 << 11, 0x1000), ("B", 1 << 12, 0x2000),
            ("X", 1 << 13, 0x4000), ("Y", 1 << 14, 0x8000),
        ]
        for label, source_bit, expected_bit in buttons:
            stream.sendall(input_frame(buttons=source_bit))
            time.sleep(0.14)
            matched = first_matching_slot(
                get_state, lambda state, bit=expected_bit: state.buttons & bit)
            stream.sendall(input_frame())
            if matched != slot:
                raise RuntimeError(f"{label}: no llegó al slot {slot}")
            print(f"PASS botón {label}")

        stream.sendall(input_frame(left_trigger=1023, right_trigger=512,
                                   left_x=12345, left_y=-23456,
                                   right_x=-12000, right_y=3000))
        time.sleep(0.16)
        state = XInputState()
        if get_state(slot, ctypes.byref(state)) != 0:
            raise RuntimeError("se perdió el slot XInput durante la prueba de ejes")
        gamepad = state.gamepad
        if not (gamepad.left_trigger == 255 and 120 <= gamepad.right_trigger <= 130 and
                gamepad.left_x == 12345 and gamepad.left_y == -23456 and
                gamepad.right_x == -12000 and gamepad.right_y == 3000):
            raise RuntimeError(
                "ejes inesperados: "
                f"LT={gamepad.left_trigger}, RT={gamepad.right_trigger}, "
                f"LX={gamepad.left_x}, LY={gamepad.left_y}, "
                f"RX={gamepad.right_x}, RY={gamepad.right_y}")
        stream.sendall(input_frame())
        print("PASS triggers y sticks")

        vibration = XInputVibration(32768, 16384)
        if set_state(slot, ctypes.byref(vibration)) != 0:
            raise RuntimeError("XInputSetState rechazó la vibración")
        feedback = read_feedback(stream, 2)
        set_state(slot, ctypes.byref(XInputVibration()))
        if len(feedback) != 2 or not (45 <= feedback[0] <= 55 and 20 <= feedback[1] <= 30):
            raise RuntimeError(f"feedback de vibración inesperado: {list(feedback)}")
        print(f"PASS vibración: izquierda={feedback[0]}%, derecha={feedback[1]}%")

        if args.stress_seconds > 0:
            deadline = time.monotonic() + args.stress_seconds
            cycles = 0
            stress_buttons = (1 << 11, 1 << 12, 1 << 13, 1 << 14,
                              1 << 8, 1 << 9, 1 << 0, 1 << 3)
            while time.monotonic() < deadline:
                sign = 1 if cycles % 2 == 0 else -1
                source_button = stress_buttons[cycles % len(stress_buttons)]
                stream.sendall(input_frame(
                    buttons=source_button, left_trigger=(cycles * 97) % 1024,
                    right_trigger=(cycles * 53) % 1024, left_x=sign * 20000,
                    left_y=-sign * 17000, right_x=sign * 14000,
                    right_y=-sign * 11000))
                time.sleep(0.08)
                state = XInputState()
                if (get_state(slot, ctypes.byref(state)) != 0 or
                        not state.gamepad.buttons & buttons_for_xinput(source_button)):
                    raise RuntimeError("el dispositivo se desconectó durante el estrés")
                if cycles % 4 == 0:
                    vibration = XInputVibration(65535 if sign > 0 else 16384,
                                                16384 if sign > 0 else 65535)
                    if set_state(slot, ctypes.byref(vibration)) != 0:
                        raise RuntimeError("la vibración desconectó el slot XInput")
                cycles += 1
                time.sleep(0.08)
            stream.sendall(input_frame())
            set_state(slot, ctypes.byref(XInputVibration()))
            print(f"PASS estrés: {cycles} ciclos durante {args.stress_seconds:.0f} s")
        print("PASS: XInput y feedback GIP verificados")
        return 0
    finally:
        if args.diagnostics:
            try:
                status = api_request(args.api_port, "server/status", 3)
                print("DIAGNÓSTICO=" + json.dumps(status, ensure_ascii=False))
            except Exception as error:
                print(f"DIAGNÓSTICO no disponible: {error}")
        if stream is not None:
            stream.close()
        if attached_port is not None:
            subprocess.run([str(usbip), "-t", str(args.usb_port), "detach",
                            "--port", str(attached_port)], capture_output=True,
                           text=True, timeout=10)
        try:
            api_request(args.api_port, "server/shutdown", 3)
        except Exception:
            pass
        try:
            server.wait(timeout=15)
        except subprocess.TimeoutExpired:
            server.kill()
            server.wait()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=("one", "series"), required=True)
    parser.add_argument("--viiper")
    parser.add_argument("--usbip")
    parser.add_argument("--usb-port", type=int, default=3531)
    parser.add_argument("--api-port", type=int, default=3532)
    parser.add_argument("--ready-timeout", type=float, default=20.0,
                        help="espera máxima de la activación GIP/XInput de Windows")
    parser.add_argument("--stress-seconds", type=float, default=30.0,
                        help="duración de la prueba de entradas/vibración repetidas")
    parser.add_argument("--diagnostics", action="store_true",
                        help="imprime server/status antes de desmontar el dispositivo")
    return run(parser.parse_args())


if __name__ == "__main__":
    raise SystemExit(main())
