#!/usr/bin/env python3
"""Verify the local WDL derivative's notices without building or extracting files."""

import argparse
import hashlib
import json
from pathlib import Path
import tarfile
import zipfile


NOTICE_NAME = "VIIPER-WDL-NOTICE.txt"
UPSTREAM_REVISION = "8f4d783de745126ac8c201455dc30818c8613324"
MAX_NOTICE_BYTES = 4 * 1024 * 1024


def normalized(data):
    return data.decode("utf-8-sig").replace("\r\n", "\n").strip()


def verify_generated(data, notice):
    if normalized(notice) not in normalized(data):
        raise ValueError("Generated notices omit or alter the complete WDL notice/port attribution")


def verify_source(root):
    notice = (root / "docs/licenses/WDL-resampler.txt").read_bytes()
    text = normalized(notice)
    required = (
        "Copyright (C) 2010 and later Cockos Incorporated",
        "3. This notice may not be removed or altered from any source distribution.",
        "You may also distribute this software under the LGPL v2 or later.",
        "Pinned revision: " + UPSTREAM_REVISION,
        "is an altered, fixed-rate Go port,\nnot the original WDL resampler.",
    )
    if not all(item in text for item in required):
        raise ValueError("The source WDL notice lacks its license, pinned origin, or altered-port attribution")
    verify_generated((root / "scripts/licenses.tpl").read_bytes(), notice)
    source = (root / "device/dualsense/rear_sinc64.go").read_text(encoding="utf-8")
    if UPSTREAM_REVISION not in source or "altered" not in source or "docs/licenses/WDL-resampler.txt" not in source:
        raise ValueError("The altered source must retain its origin and full-notice reference")
    return notice


def verify_archive(path, notice):
    # Read only bounded notice/receipt entries, never the binary or archive paths on disk.
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as archive:
            entries = archive.infolist()
            names = [item.filename for item in entries]

            def read(name):
                selected = [item for item in entries if item.filename == name]
                if len(selected) != 1 or selected[0].is_dir() or selected[0].file_size > MAX_NOTICE_BYTES:
                    raise ValueError("Missing, duplicate, or oversized notice/receipt: " + name)
                return archive.read(selected[0])

            verify_archive_entries(names, read, notice)
    else:
        with tarfile.open(path, "r:gz") as archive:
            entries = archive.getmembers()
            names = [item.name for item in entries]

            def read(name):
                selected = [item for item in entries if item.name == name]
                if len(selected) != 1 or not selected[0].isfile() or selected[0].size > MAX_NOTICE_BYTES:
                    raise ValueError("Missing, duplicate, or oversized notice/receipt: " + name)
                with archive.extractfile(selected[0]) as stream:
                    return stream.read(MAX_NOTICE_BYTES + 1)

            verify_archive_entries(names, read, notice)


def verify_archive_entries(names, read, notice):
    if read(NOTICE_NAME) != notice:
        raise ValueError("Packaged WDL notice differs from the source notice")
    verify_generated(read("licenses.txt"), notice)
    if "viiper.exe" in names or "BUILD-INFO.json" in names:
        receipt = json.loads(read("BUILD-INFO.json"))
        entries = receipt["LicenseFiles"]
        recorded = {}
        for entry in entries:
            name = entry["Name"]
            if name in recorded:
                raise ValueError("Duplicate notice in BUILD-INFO: " + name)
            recorded[name] = entry["Sha256"].upper()
        required = {"licenses.txt", "LICENSE.txt", "VIIPER-SYSTRAY-NOTICE.md", "VIIPER-SYSTRAY-LICENSE.txt", NOTICE_NAME}
        if not required.issubset(recorded):
            raise ValueError("BUILD-INFO omits a shipped license/notice hash")
        for name, expected in recorded.items():
            if hashlib.sha256(read(name)).hexdigest().upper() != expected:
                raise ValueError("Packaged notice hash differs from BUILD-INFO: " + name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--generated", type=Path)
    parser.add_argument("--archive", type=Path)
    args = parser.parse_args()
    notice = verify_source(args.root)
    if args.generated:
        verify_generated(args.generated.read_bytes(), notice)
    if args.archive:
        verify_archive(args.archive, notice)
    print("PASS: complete WDL source, altered-port attribution, and requested release notices")


if __name__ == "__main__":
    main()
