#!/usr/bin/env python3
"""Report how SDL3 sees the currently connected Windows gamepads."""

from __future__ import annotations

import argparse
import ctypes
from ctypes import wintypes as w
import json
from pathlib import Path
import time


AXES = ("leftx", "lefty", "rightx", "righty", "lefttrigger", "righttrigger")
BUTTONS = ("south", "east", "west", "north", "back", "start", "leftstick", "rightstick", "leftshoulder", "rightshoulder", "dpad_up", "dpad_down", "dpad_left", "dpad_right")


def text(value: ctypes.c_char_p | None) -> str:
    return value.decode("utf-8", errors="replace") if value else ""


def configure(dll: ctypes.WinDLL, name: str, result, args=()):
    function = getattr(dll, name)
    function.restype = result
    function.argtypes = list(args)
    return function


def run(path: Path, seconds: float) -> int:
    sdl = ctypes.WinDLL(str(path))
    error = configure(sdl, "SDL_GetError", ctypes.c_char_p)
    init = configure(sdl, "SDL_Init", w.BOOL, (ctypes.c_uint32,))
    quit_sdl = configure(sdl, "SDL_Quit", None)
    if not init(0x00000200 | 0x00002000):  # JOYSTICK | GAMEPAD
        raise RuntimeError(f"SDL_Init: {text(error())}")

    try:
        get_gamepads = configure(sdl, "SDL_GetGamepads", ctypes.POINTER(ctypes.c_uint32),
                                 (ctypes.POINTER(ctypes.c_int),))
        free = configure(sdl, "SDL_free", None, (ctypes.c_void_p,))
        get_name = configure(sdl, "SDL_GetGamepadNameForID", ctypes.c_char_p, (ctypes.c_uint32,))
        get_type = configure(sdl, "SDL_GetGamepadTypeForID", ctypes.c_int, (ctypes.c_uint32,))
        get_real_type = configure(sdl, "SDL_GetRealGamepadTypeForID", ctypes.c_int, (ctypes.c_uint32,))
        type_name = configure(sdl, "SDL_GetGamepadStringForType", ctypes.c_char_p, (ctypes.c_int,))
        get_mapping = configure(sdl, "SDL_GetGamepadMappingForID", ctypes.c_char_p, (ctypes.c_uint32,))
        get_path = configure(sdl, "SDL_GetGamepadPathForID", ctypes.c_char_p, (ctypes.c_uint32,))
        get_vendor = configure(sdl, "SDL_GetGamepadVendorForID", ctypes.c_uint16, (ctypes.c_uint32,))
        get_product = configure(sdl, "SDL_GetGamepadProductForID", ctypes.c_uint16, (ctypes.c_uint32,))
        get_product_version = configure(sdl, "SDL_GetGamepadProductVersionForID", ctypes.c_uint16, (ctypes.c_uint32,))
        get_connection = configure(sdl, "SDL_GetGamepadConnectionState", ctypes.c_int, (ctypes.c_void_p,))
        open_gamepad = configure(sdl, "SDL_OpenGamepad", ctypes.c_void_p, (ctypes.c_uint32,))
        close_gamepad = configure(sdl, "SDL_CloseGamepad", None, (ctypes.c_void_p,))
        update_gamepads = configure(sdl, "SDL_UpdateGamepads", None)
        get_axis = configure(sdl, "SDL_GetGamepadAxis", ctypes.c_int16, (ctypes.c_void_p, ctypes.c_int))
        get_button = configure(sdl, "SDL_GetGamepadButton", w.BOOL, (ctypes.c_void_p, ctypes.c_int))

        def snapshot() -> list[dict]:
            update_gamepads()
            count = ctypes.c_int(0)
            ids = get_gamepads(ctypes.byref(count))
            if not ids:
                return []
            try:
                result = []
                for index in range(count.value):
                    gamepad_id = int(ids[index])
                    handle = open_gamepad(gamepad_id)
                    item = {
                        "instance_id": gamepad_id,
                        "name": text(get_name(gamepad_id)),
                        "type": text(type_name(get_type(gamepad_id))),
                        "real_type": text(type_name(get_real_type(gamepad_id))),
                        "vid": f"{get_vendor(gamepad_id):04X}",
                        "pid": f"{get_product(gamepad_id):04X}",
                        "product_version": int(get_product_version(gamepad_id)),
                        "path": text(get_path(gamepad_id)),
                        "mapping": text(get_mapping(gamepad_id)),
                    }
                    if handle:
                        try:
                            item["connection_state"] = int(get_connection(handle))
                            item["axes"] = {name: int(get_axis(handle, axis)) for axis, name in enumerate(AXES)}
                            item["buttons"] = {name: bool(get_button(handle, button)) for button, name in enumerate(BUTTONS)}
                        finally:
                            close_gamepad(handle)
                    else:
                        item["open_error"] = text(error())
                    result.append(item)
                return result
            finally:
                free(ids)

        first = snapshot()
        print(json.dumps({"dll": str(path), "initial": first}, ensure_ascii=False, indent=2))
        deadline = time.monotonic() + seconds
        seen = set()
        while time.monotonic() < deadline:
            current = snapshot()
            for item in current:
                key = json.dumps({"instance_id": item["instance_id"], "axes": item.get("axes"), "buttons": item.get("buttons")}, sort_keys=True)
                if key not in seen:
                    print(json.dumps({"observation": item}, ensure_ascii=False))
                    seen.add(key)
            time.sleep(0.1)
        return 0
    finally:
        quit_sdl()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dll", type=Path, required=True)
    parser.add_argument("--seconds", type=float, default=10)
    args = parser.parse_args()
    return run(args.dll, args.seconds)


if __name__ == "__main__":
    raise SystemExit(main())
