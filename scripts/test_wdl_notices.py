"""Offline release-notice regression tests; no Go build or application execution."""

import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
import warnings
import zipfile

import verify_wdl_notices as policy


class WdlNoticesTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.root = Path(__file__).resolve().parents[1]
        cls.notice = policy.verify_source(cls.root)

    def files(self, windows=False):
        result = {
            policy.NOTICE_NAME: self.notice,
            "licenses.txt": b"VIIPER and dependency notices\n" + self.notice,
            "LICENSE.txt": b"GPL fixture",
            "VIIPER-SYSTRAY-NOTICE.md": b"Systray fixture",
            "VIIPER-SYSTRAY-LICENSE.txt": b"Apache fixture",
        }
        if windows:
            receipt = {"LicenseFiles": [
                {"Name": name, "Sha256": hashlib.sha256(data).hexdigest().upper()}
                for name, data in result.items()
            ]}
            result["BUILD-INFO.json"] = json.dumps(receipt).encode()
            result["viiper.exe"] = b"Not an executable: archive fixture only"
        return result

    def check_archive(self, files, tar=False, duplicate=False):
        with tempfile.TemporaryDirectory(prefix="viiper-wdl-notices-") as folder:
            path = Path(folder) / ("fixture.tar.gz" if tar else "fixture.zip")
            if tar:
                with tarfile.open(path, "w:gz") as archive:
                    for name, data in files.items():
                        item = tarfile.TarInfo(name)
                        item.size = len(data)
                        archive.addfile(item, io.BytesIO(data))
                    if duplicate:
                        item = tarfile.TarInfo(policy.NOTICE_NAME)
                        item.size = len(self.notice)
                        archive.addfile(item, io.BytesIO(self.notice))
            else:
                with zipfile.ZipFile(path, "w") as archive:
                    for name, data in files.items():
                        archive.writestr(name, data)
                    if duplicate:
                        with warnings.catch_warnings():
                            warnings.simplefilter("ignore", UserWarning)
                            archive.writestr(policy.NOTICE_NAME, self.notice)
            policy.verify_archive(path, self.notice)

    def test_source_template_keeps_full_notice_and_altered_port_attribution(self):
        policy.verify_source(self.root)

    def test_generated_crlf_is_accepted_without_weakening_full_text(self):
        policy.verify_generated(policy.normalized(self.notice).replace("\n", "\r\n").encode(), self.notice)

    def test_generated_missing_or_altered_license_and_attribution_are_rejected(self):
        for text in (b"", self.notice.replace(b"Cockos", b"Other!"),
                     self.notice.split(b"Upstream:")[0], self.notice.replace(b"LGPL v2", b"LGPL v3")):
            with self.subTest(text=text[:40]), self.assertRaises(ValueError):
                policy.verify_generated(text, self.notice)

    def test_all_binary_and_library_archive_shapes(self):
        for tar, windows in ((True, False), (False, False), (False, True)):
            with self.subTest(tar=tar, windows=windows):
                self.check_archive(self.files(windows), tar=tar)

    def test_missing_or_duplicate_packaged_notice_is_rejected(self):
        for tar in (False, True):
            files = self.files()
            del files[policy.NOTICE_NAME]
            with self.subTest(tar=tar, missing=True), self.assertRaises(ValueError):
                self.check_archive(files, tar=tar)
            with self.subTest(tar=tar, duplicate=True), self.assertRaises(ValueError):
                self.check_archive(self.files(), tar=tar, duplicate=True)

    def test_packaged_notice_and_generated_notice_must_both_match(self):
        for name in (policy.NOTICE_NAME, "licenses.txt"):
            files = self.files()
            files[name] = b"Truncated notice"
            with self.subTest(name=name), self.assertRaises(ValueError):
                self.check_archive(files)

    def test_windows_receipt_must_include_exact_notice_hash(self):
        for fault in ("missing", "wrong", "duplicate"):
            files = self.files(windows=True)
            receipt = json.loads(files["BUILD-INFO.json"])
            entry = receipt["LicenseFiles"][0]
            self.assertEqual(policy.NOTICE_NAME, entry["Name"])
            if fault == "missing":
                receipt["LicenseFiles"].pop(0)
            elif fault == "wrong":
                entry["Sha256"] = "0" * 64
            else:
                receipt["LicenseFiles"].append(entry)
            files["BUILD-INFO.json"] = json.dumps(receipt).encode()
            with self.subTest(fault=fault), self.assertRaises(ValueError):
                self.check_archive(files)


if __name__ == "__main__":
    unittest.main()
