#!/usr/bin/env python3
"""Live Windows validation for the retained Xbox GIP persona.

This test deliberately does not use USBPcap. It validates the real VIIPER
USB/IP path by observing PnP and XInput while the integrated GIP client sends
its input matrix.
"""

from __future__ import annotations

import argparse
import ctypes
from ctypes import wintypes as w
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
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


def api_request(port: int, command: str) -> dict:
    with socket.create_connection(("127.0.0.1", port), timeout=3) as conn:
        conn.settimeout(3)
        conn.sendall(command.encode("utf-8") + b"\0")
        chunks = []
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
        raise RuntimeError(f"API sin respuesta para {command}")
    return json.loads(raw.splitlines()[0])


def powershell_pnp(profile: str) -> str:
    pid = "02EA" if profile == "xboxone" else "0B12"
    command = (
        "Get-PnpDevice -PresentOnly | "
        f"Where-Object {{ $_.InstanceId -match 'VID_045E&PID_{pid}' }} | "
        "Select-Object Status,Class,FriendlyName,InstanceId | "
        "ConvertTo-Json -Compress"
    )
    result = subprocess.run(
        ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command],
        capture_output=True,
        text=True,
        timeout=10,
        check=False,
    )
    return (result.stdout or result.stderr).strip()


def xinput_state() -> tuple[tuple[int, ...], ...]:
    xinput = ctypes.WinDLL("XInput1_4.dll")
    get_state = xinput.XInputGetState
    get_state.argtypes = [w.DWORD, ctypes.POINTER(XInputState)]
    get_state.restype = w.DWORD
    states = []
    for index in range(4):
        state = XInputState()
        result = get_state(index, ctypes.byref(state))
        gamepad = state.gamepad
        states.append((
            index,
            result,
            state.packet,
            gamepad.buttons,
            gamepad.left_trigger,
            gamepad.right_trigger,
            gamepad.left_x,
            gamepad.left_y,
            gamepad.right_x,
            gamepad.right_y,
        ))
    return tuple(states)


def find_file(explicit: str | None, candidates: list[Path]) -> Path:
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if path.is_file():
            return path
        raise RuntimeError(f"No se encontró: {path}")
    for candidate in candidates:
        if candidate.is_file():
            return candidate.resolve()
    raise RuntimeError("No se encontró viiper.exe")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--viiper")
    parser.add_argument("--profile", choices=("xboxone", "xboxseries"), default="xboxone")
    parser.add_argument("--seconds", type=int, default=8)
    parser.add_argument("--usb-port", type=int, default=3401)
    parser.add_argument("--api-port", type=int, default=3402)
    args = parser.parse_args()
    if args.seconds < 1:
        raise RuntimeError("--seconds debe ser positivo")

    viiper = find_file(args.viiper, [ROOT / "viiper.exe", ROOT.parent / "outputs" / "viiper.exe"])
    key = Path(os.environ["APPDATA"]) / "VIIPER" / "viiper.key.txt"
    if not key.is_file():
        raise RuntimeError(f"No se encontró la clave: {key}")

    env = os.environ.copy()
    env["PATH"] = r"C:\Program Files\USBip" + os.pathsep + env.get("PATH", "")
    server = subprocess.Popen(
        [
            str(viiper),
            "server",
            # The Windows auto-attach helper selects an active non-loopback
            # address, so the USB/IP listener must be reachable there.
            f"--usb.addr=0.0.0.0:{args.usb_port}",
            f"--api.addr=127.0.0.1:{args.api_port}",
            f"--key-file={key}",
            "--api.auto-attach-local-client=true",
            "--api.auto-attach-windows-native=true",
            "--usb.retained-import-authority-id=1",
        ],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )
    client = None
    try:
        for _ in range(100):
            try:
                if api_request(args.api_port, "server/status").get("state") == "running":
                    break
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
            time.sleep(0.1)
        else:
            raise RuntimeError("VIIPER no abrió la API")

        client = subprocess.Popen(
            [
                str(viiper),
                "xboxone-client",
                f"--addr=127.0.0.1:{args.api_port}",
                f"--key-file={key}",
                f"--profile={args.profile}",
                "--input-test",
                f"--repeat-input-seconds={args.seconds}",
                "--hold-seconds=0",
            ],
            cwd=ROOT,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        assert client.stdout is not None
        deadline = time.monotonic() + args.seconds + 20
        last_xinput = None
        pnp_seen = False
        while time.monotonic() < deadline and client.poll() is None:
            state = xinput_state()
            if state != last_xinput:
                print(f"XINPUT {state}", flush=True)
                last_xinput = state
            pnp = powershell_pnp(args.profile)
            if pnp and pnp not in ("null", "[]"):
                if not pnp_seen:
                    print(f"PNP {pnp}", flush=True)
                pnp_seen = True
            time.sleep(0.08)

        if client.poll() is None:
            client.terminate()
        client.wait(timeout=10)
        if client.stdout is not None:
            for line in client.stdout:
                print(f"CLIENT {line.rstrip()}", flush=True)
        print(f"CLIENT_EXIT={client.returncode}", flush=True)
        print(f"PNP_SEEN={pnp_seen}", flush=True)
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
        if server.stdout is not None:
            for line in server.stdout:
                print(f"SERVER {line.rstrip()}", flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
