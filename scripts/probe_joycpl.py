#!/usr/bin/env python3
"""Poll the legacy Windows joy.cpl/MMSystem joystick state."""

from __future__ import annotations

import argparse
import time


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--seconds", type=float, default=30)
    args = parser.parse_args()
    import pygame

    pygame.init()
    pygame.joystick.init()
    count = pygame.joystick.get_count()
    print(f"DIRECTINPUT_DEVICES={count}", flush=True)
    devices = []
    labels_by_index: dict[int, list[str]] = {}
    for index in range(count):
        joystick = pygame.joystick.Joystick(index)
        joystick.init()
        devices.append(joystick)
        labels_by_index[index] = (
            [f"axis{axis}" for axis in range(joystick.get_numaxes())]
            + [f"button{button}" for button in range(joystick.get_numbuttons())]
            + [
                f"hat{hat}.{component}"
                for hat in range(joystick.get_numhats())
                for component in ("x", "y")
            ]
        )
        print(
            f"DIRECTINPUT[{index}] name={joystick.get_name()!r} "
            f"guid={joystick.get_guid()} axes={joystick.get_numaxes()} "
            f"buttons={joystick.get_numbuttons()} hats={joystick.get_numhats()}",
            flush=True,
        )
    previous: dict[int, tuple[float, ...]] = {}
    changes = 0
    deadline = time.monotonic() + args.seconds
    while time.monotonic() < deadline:
        pygame.event.pump()
        for index, joystick in enumerate(devices):
            state = (
                *(round(joystick.get_axis(axis), 3)
                  for axis in range(joystick.get_numaxes())),
                *(float(joystick.get_button(button))
                  for button in range(joystick.get_numbuttons())),
                *(float(value)
                  for hat in range(joystick.get_numhats())
                  for value in joystick.get_hat(hat)),
            )
            if previous.get(index) != state:
                changes += 1
                old_state = previous.get(index)
                if old_state is None:
                    changed = list(zip(labels_by_index[index], state))
                else:
                    changed = [
                        (label, old, new)
                        for label, old, new in zip(
                            labels_by_index[index], old_state, state
                        )
                        if old != new
                    ]
                print(f"JOY_CHANGE[{index}] {changed}", flush=True)
                previous[index] = state
        time.sleep(0.025)
    print(f"JOY_CHANGES={changes}", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
