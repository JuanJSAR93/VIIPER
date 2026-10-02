#!/usr/bin/env python3
"""Small Windows XInput polling helper shared by controller tests."""

from __future__ import annotations

import ctypes
from ctypes import wintypes as w


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


def xinput_slots() -> tuple[tuple[int, ...], ...]:
    """Return one snapshot for each of the four XInput user slots."""
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
