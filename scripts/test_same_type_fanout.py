#!/usr/bin/env python3
"""Emula grupos homogéneos de mandos VIIPER y verifica cada instancia.

Cada grupo usa un servidor y un bus reales. Para los dispositivos genéricos se
crean N dispositivos del mismo tipo, se abren N streams, se envía la matriz
completa de entradas y se monta cada dispositivo mediante usbip-win2. Xbox
One/Series usa N procesos autenticados del cliente integrado.
"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
import re
import socket
import subprocess
import sys
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from scripts import test_all_controllers as base  # noqa: E402


GENERIC_SPECS = (
    ("xbox360", "xbox360"),
    ("dualshock4", "dualshock4"),
    ("dualsense", "dualsensegamepadv5"),
    ("ns2pro", "ns2pro"),
)


def wait_server(api_port: int) -> None:
    for _ in range(100):
        try:
            status = base.api_request("127.0.0.1", api_port, "server/status")
            if status.get("state") == "running":
                return
        except (OSError, RuntimeError, json.JSONDecodeError):
            time.sleep(0.1)
    raise RuntimeError(f"VIIPER no abrió el API en 127.0.0.1:{api_port}")


def bus_devices(status: dict, bus_id: int) -> list[dict]:
    for bus in status.get("buses", []):
        if bus.get("busId") == bus_id:
            return list(bus.get("devices", []))
    return []


def status_for(status: dict, bus_id: int, dev_id: str) -> dict:
    return base.find_status_device(status, bus_id, dev_id)


def launch_server(
    viiper: Path, usbip: Path, usb_port: int, api_port: int
) -> subprocess.Popen:
    args = [
        str(viiper),
        "server",
        f"--usb.addr=0.0.0.0:{usb_port}",
        f"--api.addr=127.0.0.1:{api_port}",
        "--api.auto-attach-local-client=true",
        "--api.auto-attach-windows-native=false",
        "--usb.retained-import-authority-id=1",
    ]
    env = os.environ.copy()
    env["PATH"] = str(usbip.parent) + os.pathsep + env.get("PATH", "")
    return subprocess.Popen(
        args,
        env=env,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.STDOUT,
    )


def new_entry(name: str, kind: str) -> dict:
    return {
        "name": name,
        "type": kind,
        "result": "FAIL",
        "tests": [],
        "identity": dict(base.PROFILE_INFO[kind]),
    }


def wait_generic_imports(
    api_port: int,
    bus_id: int,
    entries: list[dict],
    usbip: Path,
    usb_port: int,
    baseline_ports: set[int],
    attached_ports: set[int],
) -> dict:
    """Wait for auto-attach and use usbip-win2 as a real fallback attach."""
    last_status: dict = {}
    for _ in range(120):
        last_status = base.api_request("127.0.0.1", api_port, "server/status")
        if all(
            status_for(last_status, bus_id, str(entry["identity"]["devId"])).get(
                "inputStreamActive"
            )
            and status_for(last_status, bus_id, str(entry["identity"]["devId"])).get(
                "usbipImported"
            )
            for entry in entries
        ):
            return last_status
        time.sleep(0.1)

    # Auto-attach may be disabled by a local usbip-win2 policy. Attach every
    # still-unimported device explicitly, then verify the server state again.
    for entry in entries:
        live = status_for(last_status, bus_id, str(entry["identity"]["devId"]))
        if live.get("usbipImported") is True:
            continue
        ok, detail = base.attach_device(
            usbip,
            usb_port,
            str(entry["identity"].get("usbipBusId", "")),
        )
        entry["attachFallback"] = detail
        if not ok:
            entry["attachFallbackFailed"] = True
        time.sleep(0.2)

    for _ in range(120):
        last_status = base.api_request("127.0.0.1", api_port, "server/status")
        current_ports = base.usbip_ports(usbip, usb_port) - baseline_ports
        attached_ports.update(current_ports)
        if all(
            status_for(last_status, bus_id, str(entry["identity"]["devId"])).get(
                "inputStreamActive"
            )
            and status_for(last_status, bus_id, str(entry["identity"]["devId"])).get(
                "usbipImported"
            )
            for entry in entries
        ):
            return last_status
        time.sleep(0.1)
    return last_status


def run_generic_group(
    viiper: Path,
    usbip: Path,
    kind: str,
    device_type: str,
    count: int,
    usb_port: int,
    api_port: int,
) -> dict:
    group = {
        "type": kind,
        "count": count,
        "result": "FAIL",
        "controllers": [],
    }
    server = launch_server(viiper, usbip, usb_port, api_port)
    streams: list[socket.socket] = []
    attached_ports: set[int] = set()
    bus_id: int | None = None
    baseline_ports = base.usbip_ports(usbip, usb_port)
    cleanup_warnings: list[str] = []
    try:
        wait_server(api_port)
        bus_id = int(base.api_request("127.0.0.1", api_port, "bus/create")["busId"])
        group["busId"] = bus_id
        print(f"[INFO] {kind} x{count}: bus={bus_id}")

        for index in range(1, count + 1):
            entry = new_entry(f"{kind} #{index}", kind)
            group["controllers"].append(entry)
            response = base.api_request(
                "127.0.0.1",
                api_port,
                f'bus/{bus_id}/add {{"type":"{device_type}"}}',
            )
            if "devId" not in response:
                raise RuntimeError(
                    f"respuesta inesperada al crear {kind} #{index}: {response}"
                )
            dev_id = str(response["devId"])
            entry["createResponse"] = response
            entry["identity"].update(
                {
                    "busId": bus_id,
                    "devId": dev_id,
                    "usbipBusId": response.get("usbipBusId", f"{bus_id}-{dev_id}"),
                    "uid": base.extract_uid(response, entry["identity"]["uid"]),
                }
            )
            streams.append(base.open_stream("127.0.0.1", api_port, bus_id, dev_id))

        for entry, stream in zip(group["controllers"], streams):
            sent, tests = base.send_sequence(kind, stream)
            entry["tests"] = tests
            entry["statesSent"] = sent

        status = wait_generic_imports(
            api_port,
            bus_id,
            group["controllers"],
            usbip,
            usb_port,
            baseline_ports,
            attached_ports,
        )
        group["statusSnapshot"] = status
        current_ports = base.usbip_ports(usbip, usb_port) - baseline_ports
        attached_ports.update(current_ports)

        for entry in group["controllers"]:
            live = status_for(status, bus_id, str(entry["identity"]["devId"]))
            entry["identity"].update(
                {
                    "vid": live.get("vid", entry["identity"].get("vid")),
                    "pid": live.get("pid", entry["identity"].get("pid")),
                    "deviceSpecific": live.get("deviceSpecific", {}),
                    "usbipImported": live.get("usbipImported", False),
                    "inputStreamActive": live.get("inputStreamActive", False),
                    "description": entry["identity"]["product"],
                }
            )
            entry["result"] = "PASS" if (
                len(entry["tests"]) > 0
                and all(test.get("status") == "PASS" for test in entry["tests"])
                and entry["identity"]["inputStreamActive"] is True
                and entry["identity"]["usbipImported"] is True
            ) else "FAIL"
            print(
                f"[{entry['result']}] {entry['name']}: "
                f"{len(entry['tests'])} estados; "
                f"stream={entry['identity']['inputStreamActive']}; "
                f"usbipImported={entry['identity']['usbipImported']}"
            )
        group["result"] = "PASS" if all(
            entry["result"] == "PASS" for entry in group["controllers"]
        ) else "FAIL"
        return group
    finally:
        # Capture ports even when creation or input testing fails before the
        # normal post-matrix snapshot. This prevents a partial group from
        # leaving a real usbip-win2 import attached.
        try:
            attached_ports.update(base.usbip_ports(usbip, usb_port) - baseline_ports)
        except (OSError, subprocess.SubprocessError):
            cleanup_warnings.append("no se pudo consultar usbip port durante la limpieza")
        for stream in streams:
            try:
                stream.close()
            except OSError:
                pass
        for port in sorted(attached_ports):
            try:
                result = subprocess.run(
                    [str(usbip), "detach", "-p", str(port)],
                    capture_output=True,
                    text=True,
                    timeout=10,
                )
                if result.returncode != 0:
                    cleanup_warnings.append(
                        f"usbip detach port {port} devolvió {result.returncode}: "
                        f"{(result.stdout + result.stderr).strip()}"
                    )
            except subprocess.TimeoutExpired:
                cleanup_warnings.append(
                    f"usbip detach port {port} excedió 10 segundos"
                )
        if bus_id is not None:
            try:
                base.api_request("127.0.0.1", api_port, f"bus/remove {bus_id}", timeout=3)
            except (OSError, RuntimeError, json.JSONDecodeError):
                cleanup_warnings.append("bus/remove no respondió")
        if server.poll() is None:
            try:
                base.api_request("127.0.0.1", api_port, "server/shutdown", timeout=5)
            except (OSError, RuntimeError, json.JSONDecodeError):
                cleanup_warnings.append("server/shutdown no respondió")
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                cleanup_warnings.append("el servidor no terminó tras server/shutdown")
                server.terminate()
                try:
                    server.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait(timeout=5)
        if cleanup_warnings:
            group["cleanupWarnings"] = cleanup_warnings
        print(f"[INFO] Grupo {kind} x{count} cerrado")


def parse_xbox_output(output: str, profile: str) -> dict:
    entry = new_entry("Xbox One/Series", profile)
    entry["clientOutput"] = output.strip()
    entry["tests"] = base.parse_xbox_client_tests(output)
    persona = re.search(
        r'persona creada: bus=(\d+) dev=(\S+) vid=([0-9A-Fa-f]+) '
        r'pid=([0-9A-Fa-f]+) producto="([^"]+)" '
        r'deviceID=([0-9A-Fa-f]+) serial=(\S+) usbip=(\S+)',
        output,
    )
    if persona:
        entry["identity"].update(
            {
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
            }
        )
    attach = re.search(r"attach nativo aceptado: .* port=(\d+)", output)
    if attach:
        entry["identity"]["usbipPort"] = [int(attach.group(1))]
    return entry


def run_xbox_group(
    viiper: Path,
    usbip: Path,
    profile: str,
    key_file: Path,
    count: int,
    usb_port: int,
    api_port: int,
) -> dict:
    group = {
        "type": profile,
        "count": count,
        "result": "FAIL",
        "controllers": [],
    }
    server = launch_server(viiper, usbip, usb_port, api_port)
    bus_id: int | None = None
    clients: list[subprocess.Popen] = []
    try:
        wait_server(api_port)
        bus_id = int(base.api_request("127.0.0.1", api_port, "bus/create")["busId"])
        group["busId"] = bus_id
        print(f"[INFO] {profile} x{count}: bus={bus_id}")
        for _ in range(count):
            clients.append(
                subprocess.Popen(
                    [
                        str(viiper),
                        "xboxone-client",
                        "--addr",
                        f"127.0.0.1:{api_port}",
                        "--key-file",
                        str(key_file),
                        "--bus-id",
                        str(bus_id),
                        "--profile",
                        profile,
                        "--input-test",
                        "--hold-seconds",
                        "5",
                    ],
                    stdout=subprocess.PIPE,
                    stderr=subprocess.STDOUT,
                    text=True,
                )
            )

        best_status: dict = {}
        best_score = (-1, -1, -1)
        deadline = time.monotonic() + max(45, count * 15)
        while time.monotonic() < deadline and any(client.poll() is None for client in clients):
            try:
                status = base.api_request("127.0.0.1", api_port, "server/status")
                devices = bus_devices(status, bus_id)
                score = (
                    len(devices),
                    sum(bool(device.get("usbipImported")) for device in devices),
                    sum(bool(device.get("inputStreamActive")) for device in devices),
                )
                if score > best_score:
                    best_score = score
                    best_status = status
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
            time.sleep(0.1)

        if any(client.poll() is None for client in clients):
            diagnostic: list[str] = []
            for client in clients:
                if client.poll() is None:
                    client.kill()
                try:
                    output, _ = client.communicate(timeout=10)
                except subprocess.TimeoutExpired:
                    output = "<sin salida: el proceso no terminó tras kill>"
                diagnostic.append(output.strip())
            raise RuntimeError(
                f"{profile} x{count}: una sesión Xbox excedió el tiempo límite\n"
                + "\n--- cliente ---\n".join(diagnostic)
            )

        devices = bus_devices(best_status, bus_id)
        for index, client in enumerate(clients, start=1):
            output, _ = client.communicate(timeout=10)
            entry = parse_xbox_output(output, profile)
            entry["name"] = f"{profile} #{index}"
            dev_id = str(entry["identity"].get("devId", ""))
            live = next((device for device in devices if str(device.get("devId")) == dev_id), {})
            entry["statusSnapshot"] = best_status
            entry["identity"].update(
                {
                    "usbipImported": live.get("usbipImported", False),
                    "inputStreamActive": live.get("inputStreamActive", False),
                    "deviceSpecific": live.get("deviceSpecific", {}),
                }
            )
            entry["result"] = "PASS" if (
                client.returncode == 0
                and len(entry["tests"]) == 36
                and all(test.get("status") == "PASS" for test in entry["tests"])
                and entry["identity"]["usbipImported"] is True
                and entry["identity"]["inputStreamActive"] is True
            ) else "FAIL"
            group["controllers"].append(entry)
            print(
                f"[{entry['result']}] {entry['name']}: "
                f"{len(entry['tests'])}/36 estados; "
                f"stream={entry['identity']['inputStreamActive']}; "
                f"usbipImported={entry['identity']['usbipImported']}"
            )
        group["statusSnapshot"] = best_status
        group["result"] = "PASS" if all(
            entry["result"] == "PASS" for entry in group["controllers"]
        ) else "FAIL"
        return group
    finally:
        for client in clients:
            if client.poll() is None:
                client.kill()
                client.wait(timeout=5)
        if bus_id is not None:
            try:
                base.api_request("127.0.0.1", api_port, f"bus/remove {bus_id}", timeout=3)
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
        if server.poll() is None:
            try:
                base.api_request("127.0.0.1", api_port, "server/shutdown", timeout=5)
            except (OSError, RuntimeError, json.JSONDecodeError):
                pass
            try:
                server.wait(timeout=10)
            except subprocess.TimeoutExpired:
                server.terminate()
                server.wait(timeout=5)
        print(f"[INFO] Grupo {profile} x{count} cerrado")


def write_report(path: Path, report: dict) -> None:
    lines = [
        "# Prueba de fan-out de mandos virtuales VIIPER",
        "",
        f"- **Inicio:** `{report['startedAt']}`",
        f"- **Fin:** `{datetime.now(timezone.utc).isoformat()}`",
        f"- **Resultado global:** **{report['result']}**",
        f"- **VIIPER:** `{report['viiper']}`",
        f"- **usbip.exe:** `{report['usbip']}`",
        "",
        "Esta prueba crea varias instancias reales del mismo tipo en un bus VIIPER "
        "compartido. Cada instancia abre su stream, recibe la matriz de entradas "
        "y se verifica mediante `server/status` y `usbip-win2`.",
        "",
        "## Resumen por grupo",
        "",
        "| Tipo | Cantidad | Bus | Mandos PASS | Resultado |",
        "|---|---:|---:|---:|---|",
    ]
    for group in report["groups"]:
        passed = sum(entry.get("result") == "PASS" for entry in group["controllers"])
        lines.append(
            f"| `{group['type']}` | {group['count']} | {group.get('busId', 'n/d')} | "
            f"{passed}/{group['count']} | **{group['result']}** |"
        )
    for group in report["groups"]:
        lines.extend(
            [
                "",
                f"## {group['type']} x{group['count']}",
                "",
                f"Bus VIIPER: `{group.get('busId', 'n/d')}`",
                "",
                "| Instancia | DevId | Producto | VID:PID | USB/IP | Stream | Tests | Resultado |",
                "|---|---|---|---|---|---|---:|---|",
            ]
        )
        for entry in group["controllers"]:
            identity = entry.get("identity", {})
            passed = sum(test.get("status") == "PASS" for test in entry.get("tests", []))
            total = len(entry.get("tests", []))
            lines.append(
                f"| {entry['name']} | `{identity.get('devId', 'n/d')}` | "
                f"{base.md_value(identity.get('product', 'n/d'))} | "
                f"`{identity.get('vid', 'n/d')}:{identity.get('pid', 'n/d')}` | "
                f"{identity.get('usbipImported', 'n/d')} | "
                f"{identity.get('inputStreamActive', 'n/d')} | {passed}/{total} | "
                f"**{entry.get('result', 'FAIL')}** |"
            )
        for entry in group["controllers"]:
            identity = entry.get("identity", {})
            lines.extend(
                [
                    "",
                    f"### {entry['name']}",
                    "",
                    "#### Identidad y estado",
                    "",
                    "| Campo | Valor |",
                    "|---|---|",
                ]
            )
            for key in (
                "type",
                "busId",
                "devId",
                "vid",
                "pid",
                "manufacturer",
                "product",
                "description",
                "uid",
                "serial",
                "deviceID",
                "numericBusDevice",
                "usbipBusId",
                "usbipExportAlias",
                "usbipPort",
                "usbipImported",
                "inputStreamActive",
                "deviceSpecific",
            ):
                if key in identity:
                    lines.append(f"| `{key}` | {base.md_value(identity[key])} |")
            lines.extend(
                [
                    "",
                    "#### Todos los botones y controles",
                    "",
                    "| Control | Fase | Resultado | Detalle |",
                    "|---|---|---|---|",
                ]
            )
            for test in entry.get("tests", []):
                lines.append(
                    f"| `{base.md_value(test.get('name', ''))}` | "
                    f"{base.md_value(test.get('phase', ''))} | "
                    f"**{base.md_value(test.get('status', 'FAIL'))}** | "
                    f"{base.md_value(test.get('detail', ''))} |"
                )
            lines.extend(["", "#### Salida/estado capturado", "", "```json"])
            lines.append(
                json.dumps(
                    {
                        "createResponse": entry.get("createResponse"),
                        "statusSnapshot": entry.get("statusSnapshot"),
                        "clientOutput": entry.get("clientOutput"),
                    },
                    ensure_ascii=False,
                    indent=2,
                    sort_keys=True,
                )
            )
            lines.extend(["```", ""])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--viiper", required=True, help="Ruta a viiper.exe")
    parser.add_argument("--usbip", help="Ruta a usbip.exe")
    parser.add_argument("--key-file", help="Clave VIIPER para Xbox One/Series")
    parser.add_argument(
        "--xbox-profile",
        choices=("xboxone", "xboxseries"),
        default="xboxone",
        help="Perfil Xbox que se prueba en los grupos autenticados",
    )
    parser.add_argument(
        "--counts",
        nargs="+",
        type=int,
        default=[4, 8],
        help="Cantidades homogéneas; por defecto: 4 8",
    )
    parser.add_argument(
        "--only-type",
        choices=("xbox360", "dualshock4", "dualsense", "ns2pro", "xboxone"),
        help="Ejecuta únicamente un tipo",
    )
    parser.add_argument("--usb-port", type=int, default=3261)
    parser.add_argument("--api-port", type=int, default=3262)
    parser.add_argument("--report", required=True, help="Reporte Markdown de salida")
    args = parser.parse_args()

    if any(count < 1 or count > 8 for count in args.counts):
        parser.error("cada cantidad debe estar entre 1 y 8")

    viiper = base.find_executable(args.viiper, [])
    usbip = base.find_executable(args.usbip, [Path(r"C:\Program Files\USBip\usbip.exe"), "usbip.exe"])
    key_file = Path(args.key_file).expanduser() if args.key_file else (
        Path(os.environ.get("APPDATA", "")) / "VIIPER" / "viiper.key.txt"
    )
    if not key_file.is_file():
        parser.error(f"no se encontró la clave VIIPER: {key_file}")

    selected_generic = list(GENERIC_SPECS)
    if args.only_type:
        selected_generic = [spec for spec in GENERIC_SPECS if spec[0] == args.only_type]
    include_xbox = args.only_type in (None, "xboxone")
    report = {
        "startedAt": datetime.now(timezone.utc).isoformat(),
        "result": "FAIL",
        "viiper": str(viiper),
        "usbip": str(usbip),
        "counts": args.counts,
        "xboxProfile": args.xbox_profile,
        "groups": [],
    }

    try:
        group_index = 0
        for count in args.counts:
            for kind, device_type in selected_generic:
                group_index += 1
                report["groups"].append(
                    run_generic_group(
                        viiper,
                        usbip,
                        kind,
                        device_type,
                        count,
                        args.usb_port + group_index * 4,
                        args.api_port + group_index * 4,
                    )
                )
            if include_xbox:
                group_index += 1
                report["groups"].append(
                    run_xbox_group(
                        viiper,
                        usbip,
                        args.xbox_profile,
                        key_file,
                        count,
                        args.usb_port + group_index * 4,
                        args.api_port + group_index * 4,
                    )
                )
        report["result"] = "PASS" if report["groups"] and all(
            group.get("result") == "PASS" for group in report["groups"]
        ) else "FAIL"
        return 0 if report["result"] == "PASS" else 1
    except (OSError, RuntimeError, subprocess.SubprocessError) as error:
        report["error"] = str(error)
        print(f"[FAIL] {error}", file=sys.stderr)
        return 1
    finally:
        write_report(Path(args.report).expanduser().resolve(), report)
        print(f"[REPORT] {Path(args.report).expanduser().resolve()}")


if __name__ == "__main__":
    raise SystemExit(main())
