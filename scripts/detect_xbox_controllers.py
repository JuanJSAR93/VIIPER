#!/usr/bin/env python3
"""Inspect Xbox 360/One/Series identity, driver binding and input APIs on Windows.

VID/PID is reported as evidence, not as proof of a physical controller.  The
classification also considers the Windows devnode, service, parent, serial,
BusReportedDeviceDesc and hardware IDs so VIIPER/HIDMaestro devices can be
separated from an unmarked official device.

Examples::

    python scripts/detect_xbox_controllers.py
    python scripts/detect_xbox_controllers.py --format json
    python scripts/detect_xbox_controllers.py --no-directinput
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from typing import Any


PROFILES: dict[tuple[str, str], dict[str, str]] = {
    ("045E", "028E"): {
        "profile": "xbox360",
        "official_name": "Xbox 360 Controller",
        "driver": "xusb22/xusb21",
    },
    ("045E", "02D1"): {
        "profile": "xbox-one-original",
        "official_name": "Xbox One Controller (Model 1537)",
        "driver": "dc1-controller/GIP",
    },
    ("045E", "02EA"): {
        "profile": "xbox-one-s",
        "official_name": "Xbox One S Controller (Model 1708)",
        "driver": "dc1-controller/GIP",
    },
    ("045E", "0B12"): {
        "profile": "xbox-series-xs",
        "official_name": "Xbox Series X|S Controller (Model 1914)",
        "driver": "dc1-controller/GIP",
    },
    ("045E", "0B13"): {
        "profile": "xbox-series-xs-bt",
        "official_name": "Xbox Series X|S Controller (Bluetooth)",
        "driver": "xinputhid/GIP",
    },
}


VID_PID_RE = re.compile(
    r"(?:VID_|VID&)([0-9A-F]{4,6}).*?(?:PID_|PID&)([0-9A-F]{4,6})",
    re.I,
)


def _as_list(value: Any) -> list[Any]:
    if value is None:
        return []
    return value if isinstance(value, list) else [value]


def _text(value: Any) -> str:
    if isinstance(value, (list, tuple)):
        return " ".join(_text(item) for item in value)
    return str(value or "")


def _first(properties: dict[str, Any], key: str, fallback: Any = "") -> Any:
    value = properties.get(key, fallback)
    values = _as_list(value)
    return values[0] if values else fallback


def _powershell_devices() -> list[dict[str, Any]]:
    """Return present Xbox-related devnodes and selected DEVPKEY properties."""
    if sys.platform != "win32":
        return []

    command = r'''
$known = '045E&PID_028E|045E&PID_02D1|045E&PID_02EA|045E&PID_0B12|045E&PID_0B13'
$devices = Get-CimInstance Win32_PnPEntity -ErrorAction SilentlyContinue |
  Where-Object { $_.ConfigManagerErrorCode -eq 0 }
$propertyKeys = @(
  'DEVPKEY_Device_BusReportedDeviceDesc', 'DEVPKEY_Device_DeviceDesc',
  'DEVPKEY_Device_Parent', 'DEVPKEY_Device_ContainerId',
  'DEVPKEY_Device_EnumeratorName', 'DEVPKEY_Device_LocationInfo'
)
$result = foreach ($device in $devices) {
  $label = "{0} {1} {2} {3} {4} {5} {6}" -f `
    $device.PNPDeviceID, $device.Name, $device.PNPClass, $device.ClassGuid,
    $device.HardwareID, $device.CompatibleID,
    $device.Service
  if (($label -notmatch $known) -and ($label -notmatch '(?i)VIIPER|HIDMaestro|Xbox|XInput')) {
    continue
  }
  $properties = @{}
  Get-PnpDeviceProperty -InstanceId $device.PNPDeviceID -KeyName $propertyKeys -ErrorAction SilentlyContinue |
    ForEach-Object { $properties[$_.KeyName] = $_.Data }
  [pscustomobject]@{
    Status = if ($device.Status) { $device.Status } else { 'OK' }
    Class = $device.PNPClass
    ClassGuid = $device.ClassGuid
    FriendlyName = $device.Name
    InstanceId = $device.PNPDeviceID
    BusReportedDeviceDesc = $properties['DEVPKEY_Device_BusReportedDeviceDesc']
    DeviceDesc = $properties['DEVPKEY_Device_DeviceDesc']
    HardwareIds = if ($device.HardwareID) { $device.HardwareID } else { $properties['DEVPKEY_Device_HardwareIds'] }
    CompatibleIds = if ($device.CompatibleID) { $device.CompatibleID } else { $properties['DEVPKEY_Device_CompatibleIds'] }
    Service = if ($device.Service) { $device.Service } else { $properties['DEVPKEY_Device_Service'] }
    Manufacturer = if ($device.Manufacturer) { $device.Manufacturer } else { $properties['DEVPKEY_Device_Manufacturer'] }
    Parent = $properties['DEVPKEY_Device_Parent']
    ContainerId = $properties['DEVPKEY_Device_ContainerId']
    EnumeratorName = $properties['DEVPKEY_Device_EnumeratorName']
    LocationInfo = $properties['DEVPKEY_Device_LocationInfo']
  }
}
@($result) | ConvertTo-Json -Depth 8 -Compress
'''
    completed = subprocess.run(
        ["powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", command],
        check=False,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    if completed.returncode != 0:
        raise RuntimeError(completed.stderr.strip() or "PowerShell no pudo enumerar los dispositivos")
    output = completed.stdout.strip()
    if not output:
        return []
    decoded = json.loads(output)
    return decoded if isinstance(decoded, list) else [decoded]


def _vid_pid(instance_id: str) -> tuple[str, str] | None:
    match = VID_PID_RE.search(instance_id)
    if not match:
        return None
    # Bluetooth LE instance IDs commonly encode the Microsoft vendor as
    # VID&02045E (local manufacturer prefix + 045E), unlike USB's VID_045E.
    vid = match.group(1).upper()[-4:]
    pid = match.group(2).upper()[-4:]
    return vid, pid


def _classify(device: dict[str, Any]) -> dict[str, Any]:
    instance_id = _text(device.get("InstanceId"))
    vid_pid = _vid_pid(instance_id)
    profile = PROFILES.get(vid_pid or ("", "")) or {}

    evidence_fields = {
        "friendlyName": device.get("FriendlyName", ""),
        "busReportedDeviceDesc": device.get("BusReportedDeviceDesc", ""),
        "deviceDesc": device.get("DeviceDesc", ""),
        "hardwareIds": device.get("HardwareIds", []),
        "compatibleIds": device.get("CompatibleIds", []),
        "service": device.get("Service", ""),
        "manufacturer": device.get("Manufacturer", ""),
        "parent": device.get("Parent", ""),
        "containerId": device.get("ContainerId", ""),
        "enumeratorName": device.get("EnumeratorName", ""),
        "locationInfo": device.get("LocationInfo", ""),
        "instanceId": instance_id,
    }
    searchable = " ".join(_text(value) for value in evidence_fields.values()).lower()

    if "hidmaestro" in searchable:
        origin = "HIDMaestro virtual"
        confidence = "alta"
        reason = "aparecen marcas HIDMaestro en las propiedades del devnode"
    elif "viiper" in searchable or "usbip" in searchable:
        origin = "VIIPER virtual"
        confidence = "alta"
        reason = "aparecen marcas VIIPER/USBIP en las propiedades del devnode"
    elif "vigembus" in searchable or "nefarius" in searchable:
        origin = "ViGEmBus virtual"
        confidence = "alta"
        reason = "aparecen marcas ViGEmBus/Nefarius en las propiedades del devnode"
    elif profile:
        origin = "oficial/no marcado (probablemente físico)"
        confidence = "media"
        reason = "VID/PID oficial sin una marca de software; el VID/PID solo no demuestra origen físico"
    else:
        origin = "desconocido"
        confidence = "baja"
        reason = "no coincide con un perfil conocido ni con una marca de origen"

    return {
        "profile": profile.get("profile", "unknown"),
        "officialName": profile.get("official_name", "unknown"),
        "expectedDriver": profile.get("driver", "unknown"),
        "vid": vid_pid[0] if vid_pid else "",
        "pid": vid_pid[1] if vid_pid else "",
        "origin": origin,
        "confidence": confidence,
        "reason": reason,
        "status": device.get("Status", ""),
        "class": device.get("Class", ""),
        "friendlyName": device.get("FriendlyName", ""),
        "instanceId": instance_id,
        "service": device.get("Service", ""),
        "busReportedDeviceDesc": device.get("BusReportedDeviceDesc", ""),
        "deviceDesc": device.get("DeviceDesc", ""),
        "manufacturer": device.get("Manufacturer", ""),
        "hardwareIds": _as_list(device.get("HardwareIds")),
        "compatibleIds": _as_list(device.get("CompatibleIds")),
        "parent": device.get("Parent", ""),
        "containerId": device.get("ContainerId", ""),
        "enumeratorName": device.get("EnumeratorName", ""),
        "locationInfo": device.get("LocationInfo", ""),
    }


def _xinput_snapshot() -> list[dict[str, Any]]:
    if sys.platform != "win32":
        return []
    try:
        from xinput_probe import xinput_slots
    except (ImportError, OSError):
        return []
    try:
        slots = xinput_slots()
    except (OSError, AttributeError):
        return []
    fields = (
        "slot", "result", "packet", "buttons", "leftTrigger", "rightTrigger",
        "leftX", "leftY", "rightX", "rightY",
    )
    return [dict(zip(fields, snapshot)) for snapshot in slots]


def _directinput_snapshot() -> list[dict[str, Any]]:
    if sys.platform != "win32":
        return []
    try:
        import pygame
    except ImportError:
        return []
    pygame.init()
    pygame.joystick.init()
    devices = []
    try:
        for index in range(pygame.joystick.get_count()):
            joystick = pygame.joystick.Joystick(index)
            joystick.init()
            item = {
                "index": index,
                "name": joystick.get_name(),
                "guid": joystick.get_guid(),
                "axes": joystick.get_numaxes(),
                "buttons": joystick.get_numbuttons(),
                "hats": joystick.get_numhats(),
            }
            if hasattr(joystick, "get_instance_id"):
                item["instanceId"] = joystick.get_instance_id()
            devices.append(item)
    finally:
        pygame.joystick.quit()
        pygame.quit()
    return devices


def _markdown(report: dict[str, Any]) -> str:
    lines = [
        "# Detección de mandos Xbox",
        "",
        "Este informe combina PnP/USB, XInput y DirectInput. `origin` no se decide por VID/PID solamente.",
        "",
        "## Devnodes PnP",
        "",
    ]
    devices = report["pnp"]
    if not devices:
        lines.append("No hay devnodes Xbox 360/One/Series presentes o PowerShell no devolvió propiedades.")
    for index, device in enumerate(devices, 1):
        lines.extend([
            f"### {index}. {device['profile']} — {device['origin']} ({device['confidence']})",
            "",
            f"- Perfil oficial esperado: `{device['officialName']}`",
            f"- VID:PID: `{device['vid']}:{device['pid']}`",
            f"- FriendlyName: `{device['friendlyName']}`",
            f"- BusReportedDeviceDesc: `{_text(device['busReportedDeviceDesc'])}`",
            f"- DeviceDesc: `{_text(device['deviceDesc'])}`",
            f"- Servicio: `{_text(device['service'])}`",
            f"- Clase/estado: `{device['class']}` / `{device['status']}`",
            f"- InstanceId: `{device['instanceId']}`",
            f"- Parent: `{_text(device['parent'])}`",
            f"- ContainerId: `{_text(device['containerId'])}`",
            f"- Evidencia: {device['reason']}",
            f"- Hardware IDs: `{_text(device['hardwareIds'])}`",
            "",
        ])

    lines.extend(["## XInput", ""])
    xinput = report["xinput"]
    if xinput:
        lines.append("```json")
        lines.append(json.dumps(xinput, ensure_ascii=False, indent=2))
        lines.append("```")
    else:
        lines.append("No hay slots XInput activos o no se pudo cargar XInput1_4.dll.")

    lines.extend(["", "## DirectInput / joy.cpl", ""])
    directinput = report["directinput"]
    if directinput:
        lines.append("| Índice | Nombre | GUID | Ejes | Botones | Hats | Instance ID |")
        lines.append("|---:|---|---|---:|---:|---:|---:|")
        for device in directinput:
            lines.append(
                f"| {device['index']} | `{device['name']}` | `{device['guid']}` | "
                f"{device['axes']} | {device['buttons']} | {device['hats']} | "
                f"`{device.get('instanceId', '')}` |"
            )
    else:
        lines.append("No hay dispositivos DirectInput visibles o pygame no está instalado.")
    lines.extend([
        "",
        "## Interpretación",
        "",
        "- `VIIPER virtual`/`HIDMaestro virtual` es una clasificación por evidencia del devnode.",
        "- `oficial/no marcado` significa que no se encontró una marca de software; no prueba por sí solo que sea físico.",
        "- XInput no expone de forma fiable el VID/PID ni el origen físico/virtual; debe correlacionarse con PnP.",
    ])
    return "\n".join(lines) + "\n"


def main() -> int:
    if hasattr(sys.stdout, "reconfigure"):
        sys.stdout.reconfigure(encoding="utf-8", errors="replace")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--format", choices=("markdown", "json"), default="markdown")
    parser.add_argument("--no-xinput", action="store_true", help="no consultar los cuatro slots XInput")
    parser.add_argument("--no-directinput", action="store_true", help="no inicializar pygame/joy.cpl")
    args = parser.parse_args()

    try:
        pnp = [_classify(device) for device in _powershell_devices()]
    except (RuntimeError, json.JSONDecodeError, OSError) as error:
        print(f"Error enumerando PnP: {error}", file=sys.stderr)
        pnp = []

    report = {
        "pnp": pnp,
        "xinput": [] if args.no_xinput else _xinput_snapshot(),
        "directinput": [] if args.no_directinput else _directinput_snapshot(),
    }
    if args.format == "json":
        print(json.dumps(report, ensure_ascii=False, indent=2, default=str))
    else:
        print(_markdown(report), end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
