#!/usr/bin/env python3
"""Run the real Xbox GIP persona while leaving joy.cpl visible.

This is the GIP/XboxComposite test, not the HID compatibility test in
run_xbox_one_joy_test.py. It opens joy.cpl, starts the real VIIPER server,
creates and attaches one Xbox One/Series persona, repeats the input matrix,
and finally removes the persona. No USBPcap is used.
"""

from __future__ import annotations

import argparse
import ctypes
from ctypes import wintypes as w
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
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


def find_viiper(explicit: str | None) -> Path:
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if path.is_file():
            return path
        raise RuntimeError(f"No se encontró VIIPER: {path}")
    candidates = [
        ROOT / "viiper.exe",
        ROOT / "viiper-gip-xone-base.exe",
        ROOT.parents[1] / "outputs" / "viiper-gip-xone-base.exe",
        ROOT.parents[1] / "outputs" / "viiper.exe",
    ]
    for candidate in candidates:
        if candidate.is_file():
            return candidate.resolve()
    raise RuntimeError("No se encontró viiper.exe ni viiper-gip-xone-base.exe")


def api_request(port: int, command: str) -> dict:
    import socket

    with socket.create_connection(("127.0.0.1", port), timeout=4) as conn:
        conn.settimeout(4)
        conn.sendall(command.encode("utf-8") + b"\0")
        chunks: list[bytes] = []
        while True:
            try:
                chunk = conn.recv(65536)
            except TimeoutError:
                break
            if not chunk:
                break
            chunks.append(chunk)
    raw = b"".join(chunks).decode("utf-8", errors="replace").strip()
    if not raw:
        raise RuntimeError(f"API sin respuesta para {command}")
    result = json.loads(raw.splitlines()[0])
    if result.get("status", 200) >= 400:
        raise RuntimeError(result)
    return result


def pnp_snapshot(profile: str) -> str:
    pid = "02EA" if profile == "xboxone" else "0B12"
    command = (
        "Get-PnpDevice -PresentOnly | "
        f"Where-Object {{ $_.InstanceId -match 'VID_045E&PID_{pid}' }} | "
        "Select-Object Status,Class,FriendlyName,InstanceId | "
        "ConvertTo-Json -Compress"
    )
    try:
        result = subprocess.run(
            ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command],
            capture_output=True, text=True, timeout=3, check=False,
        )
    except subprocess.TimeoutExpired:
        return ""
    return (result.stdout or result.stderr).strip()


def xinput_slots() -> tuple[tuple[int, ...], ...]:
    xinput = ctypes.WinDLL("XInput1_4.dll")
    get_state = xinput.XInputGetState
    get_state.argtypes = [w.DWORD, ctypes.POINTER(XInputState)]
    get_state.restype = w.DWORD
    slots = []
    for index in range(4):
        state = XInputState()
        result = get_state(index, ctypes.byref(state))
        gamepad = state.gamepad
        slots.append((
            index, result, state.packet, gamepad.buttons,
            gamepad.left_trigger, gamepad.right_trigger,
            gamepad.left_x, gamepad.left_y,
            gamepad.right_x, gamepad.right_y,
        ))
    return tuple(slots)


def drain_process_output(process: subprocess.Popen[str], prefix: str,
                         sink: list[str]) -> threading.Thread | None:
    """Consume a child stdout continuously so diagnostics cannot back-pressure it."""
    stream = process.stdout
    if stream is None:
        return None

    def worker() -> None:
        try:
            for line in stream:
                clean = line.rstrip()
                sink.append(clean)
                print(f"{prefix} {clean}", flush=True)
        finally:
            stream.close()

    thread = threading.Thread(target=worker, name=f"drain-{prefix.lower()}",
                              daemon=True)
    thread.start()
    return thread


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("profile", nargs="?", choices=("xboxone", "xboxseries"),
                        default="xboxone",
                        help="xboxone o xboxseries; por defecto xboxone")
    parser.add_argument("--viiper", help=argparse.SUPPRESS)
    parser.add_argument("--seconds", type=int, default=30,
                        help="duración de la matriz de entrada")
    parser.add_argument("--matrix-once", action="store_true",
                        help="envía la matriz una sola vez; evita saturar la cola GIP")
    parser.add_argument("--dynamic-pattern",
                        choices=("a-pulse", "dpad-pulse", "trigger-ramp", "stick-sweep", "mixed"),
                        help="patrón GIP temporizado para medir cambios individuales")
    parser.add_argument("--pattern-seconds", type=int, default=8,
                        help="duración del patrón GIP temporizado")
    parser.add_argument("--pattern-delay-seconds", type=int, default=0,
                        help="espera antes del primer estado dinámico")
    parser.add_argument("--retained-input-service-ms", type=int, choices=(0, 1, 2, 4), default=0,
                        help="cadencia experimental del IN retenido")
    parser.add_argument("--raw-log", help="archivo local para registrar paquetes USB/IP")
    parser.add_argument("--hold-seconds", type=int, default=0,
                        help="mantiene el dispositivo montado después de la matriz")
    parser.add_argument("--usb-port", type=int, default=3401)
    parser.add_argument("--api-port", type=int, default=3402)
    args = parser.parse_args()
    if args.seconds < 1:
        raise RuntimeError("--seconds debe ser positivo")

    viiper = find_viiper(args.viiper)
    key = Path(os.environ["APPDATA"]) / "VIIPER" / "viiper.key.txt"
    if not key.is_file():
        raise RuntimeError(f"No se encontró la clave: {key}")

    # joy.cpl is a preinstalled Windows control-panel app. It is intentionally
    # left open so the user can inspect the Test tab after this script ends.
    subprocess.Popen(["control.exe", "joy.cpl"])
    print("joy.cpl abierto; selecciona 'Control Xbox One' o 'Control Xbox' "
          "si Windows lo muestra.", flush=True)

    env = os.environ.copy()
    env["PATH"] = (r"C:\Program Files\USBip" + os.pathsep +
                   env.get("PATH", ""))
    server_args = [
        str(viiper), "server",
        f"--usb.addr=0.0.0.0:{args.usb_port}",
        f"--api.addr=127.0.0.1:{args.api_port}",
        f"--key-file={key}",
        "--api.auto-attach-local-client=true",
        "--api.auto-attach-windows-native=true",
        "--usb.retained-import-authority-id=1",
        f"--usb.retained-input-service-ms={args.retained_input_service_ms}",
        "--usb.endpoint-diagnostics=true",
    ]
    if args.raw_log:
        server_args.extend(["--log.level=debug", f"--log.raw-file={Path(args.raw_log).resolve()}"])
    server = subprocess.Popen(server_args, cwd=ROOT, env=env, stdout=subprocess.PIPE,
       stderr=subprocess.STDOUT, text=True, bufsize=1)
    server_output: list[str] = []
    server_output_thread = drain_process_output(server, "SERVER", server_output)
    client = None
    pnp_seen = False
    dynamic_xinput = False
    baseline_xinput = xinput_slots()
    initially_disconnected = {slot[0] for slot in baseline_xinput if slot[1] != 0}
    first_virtual_connected: dict[int, tuple[int, ...]] = {}
    virtual_slots: set[int] = set()
    last_xinput = None
    try:
        for _ in range(100):
            try:
                if api_request(args.api_port, "server/status").get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                time.sleep(0.1)
        else:
            raise RuntimeError("VIIPER no abrió la API")

        hold_seconds = args.hold_seconds
        if args.matrix_once and hold_seconds == 0:
            hold_seconds = 5
        client_args = [
            str(viiper), "xboxone-client",
            f"--addr=127.0.0.1:{args.api_port}",
            f"--key-file={key}",
            f"--profile={args.profile}",
            "--input-test",
            f"--hold-seconds={hold_seconds}",
        ]
        if args.dynamic_pattern:
            client_args.append(f"--dynamic-pattern={args.dynamic_pattern}")
            client_args.append(f"--dynamic-pattern-secs={args.pattern_seconds}")
            client_args.append(f"--dynamic-pattern-delay={args.pattern_delay_seconds}")
        if not args.matrix_once:
            client_args.insert(-1, f"--repeat-input-seconds={args.seconds}")
        client = subprocess.Popen(client_args, cwd=ROOT, env=env, stdout=subprocess.PIPE,
           stderr=subprocess.STDOUT, text=True, bufsize=1)
        client_output: list[str] = []
        client_output_thread = drain_process_output(client, "CLIENT", client_output)

        observation_seconds = max(20, hold_seconds + 15)
        if args.dynamic_pattern:
            observation_seconds = max(
                observation_seconds,
                args.pattern_delay_seconds + args.pattern_seconds + hold_seconds + 15,
            )
        deadline = time.monotonic() + observation_seconds
        while time.monotonic() < deadline and client.poll() is None:
            current = xinput_slots()
            if current != last_xinput:
                print(f"XINPUT {current}", flush=True)
                last_xinput = current
            for slot in current:
                index = slot[0]
                if index in initially_disconnected and slot[1] == 0:
                    virtual_slots.add(index)
                    if index not in first_virtual_connected:
                        first_virtual_connected[index] = slot
                        print(f"XINPUT_VIRTUAL_SLOT_CANDIDATE={index} state={slot}", flush=True)
                    elif slot != first_virtual_connected[index]:
                        dynamic_xinput = True
            pnp = pnp_snapshot(args.profile)
            if pnp and pnp not in ("null", "[]"):
                if not pnp_seen:
                    print(f"PNP {pnp}", flush=True)
                pnp_seen = True
            time.sleep(0.08)

        if client.poll() is None:
            client.terminate()
        client.wait(timeout=10)
        print(f"CLIENT_EXIT={client.returncode}", flush=True)
        print(f"PNP_SEEN={pnp_seen}", flush=True)
        print(f"XINPUT_VIRTUAL_SLOTS={sorted(virtual_slots)}", flush=True)
        print(f"XINPUT_DYNAMIC_STATE_SEEN={dynamic_xinput}", flush=True)
        print("joy.cpl queda abierto para inspección manual.", flush=True)
        return 0 if client.returncode == 0 and pnp_seen else 1
    finally:
        if client is not None and client.poll() is None:
            client.terminate()
            client.wait(timeout=10)
        if server.poll() is None:
            try:
                api_request(args.api_port, "server/shutdown")
            except Exception as exc:
                print(f"SERVER_SHUTDOWN_WARNING={exc}", file=sys.stderr)
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                server.terminate()
                server.wait(timeout=5)


if __name__ == "__main__":
    raise SystemExit(main())
