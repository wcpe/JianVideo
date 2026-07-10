#!/usr/bin/env python3
"""工具镜像脚本的离线单元测试，不下载大型源码。"""

import hashlib
import importlib.util
import json
import os
import sys
import tarfile
import tempfile
import unittest
import zipfile
from io import BytesIO
from pathlib import Path
from unittest.mock import patch

SCRIPT_ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_ROOT))
import tool_mirror

FIXTURE_SPEC = importlib.util.spec_from_file_location(
    "verify_fixtures", SCRIPT_ROOT / "verify_fixtures.py"
)
verify_fixtures = importlib.util.module_from_spec(FIXTURE_SPEC)
if FIXTURE_SPEC and FIXTURE_SPEC.loader:
    FIXTURE_SPEC.loader.exec_module(verify_fixtures)

DNG_SPEC = importlib.util.spec_from_file_location(
    "generate_dng", SCRIPT_ROOT / "fixtures" / "generate_dng.py"
)
generate_dng = importlib.util.module_from_spec(DNG_SPEC)
if DNG_SPEC and DNG_SPEC.loader:
    DNG_SPEC.loader.exec_module(generate_dng)


class ToolMirrorTest(unittest.TestCase):
    def locked_toolchain(self):
        return {
            "status": "locked",
            "image": {"ImageOS": "test", "ImageVersion": "1"},
            "tools": {"cc": {"path": "/usr/bin/cc", "version": "cc 1"}},
            "packages": {"compiler": "1"},
            "evidence": {
                "workflow_run": "https://github.com/example/repo/actions/runs/1",
                "sha256": "b" * 64,
            },
        }

    def valid_lock(self):
        package = {
            "name": "demo",
            "version": "1.0",
            "license": "MIT",
            "license_file": "LICENSE",
            "archive_name": "demo-1.0.tar.xz",
            "url": "https://example.invalid/archive/" + "c" * 40 + ".tar.xz",
            "sha256": "a" * 64,
            "verification": {
                "method": "immutable_git_commit",
                "repository": "https://example.invalid/demo.git",
                "commit": "c" * 40,
            },
        }
        runners = []
        for index in range(6):
            runners.append({
                "label": f"runner-{index}",
                "id": f"platform-{index}",
                "platform": "test",
                "arch": "test",
                "runner_os": "Test",
                "runner_arch": "TEST",
                "toolchain": self.locked_toolchain(),
            })
        return {
            "schema_version": 2,
            "release": "tools-v1.0.0",
            "source_date_epoch": 1704067200,
            "runners": runners,
            "delegate_fixtures": {
                "status": "locked",
                "manifest": "fixtures/manifest.json",
            },
            "packages": [package],
        }

    def test_valid_lock_passes(self):
        self.assertEqual([], tool_mirror.validate_lock(self.valid_lock()))

    def test_real_lock_has_complete_explicit_source_verification(self):
        data = tool_mirror.load_lock(tool_mirror.DEFAULT_LOCK)
        errors = tool_mirror.validate_lock(
            data, require_toolchains=False, require_fixtures=False
        )
        self.assertEqual([], errors)
        methods = {item["name"]: item["verification"]["method"] for item in data["packages"] if not item.get("optional")}
        self.assertEqual("pgp", methods["ffmpeg"])
        self.assertEqual("pgp", methods["zlib"])
        self.assertEqual("github_attestation", methods["imagemagick"])
        self.assertNotIn("", methods.values())

    def test_real_lock_contains_six_verified_toolchains_and_fixtures(self):
        data = tool_mirror.load_lock(tool_mirror.DEFAULT_LOCK)
        self.assertEqual(6, len(data["runners"]))
        for runner in data["runners"]:
            toolchain = runner["toolchain"]
            self.assertEqual("locked", toolchain["status"])
            self.assertTrue(toolchain["image"])
            self.assertTrue(toolchain["tools"])
            self.assertTrue(toolchain["packages"])
            self.assertTrue(toolchain["evidence"]["sha256"])
        windows = {item["id"]: item for item in data["runners"] if item["platform"] == "windows"}
        self.assertEqual("UCRT64", windows["windows-x86_64"]["toolchain"]["shell"]["msystem"])
        self.assertEqual("CLANGARM64", windows["windows-aarch64"]["toolchain"]["shell"]["msystem"])
        self.assertEqual("locked", data["delegate_fixtures"]["status"])

    def test_missing_verification_method_fails_closed(self):
        data = self.valid_lock()
        data["packages"][0].pop("verification")
        self.assertTrue(any("verification" in error for error in tool_mirror.validate_lock(data)))

    def test_pgp_without_signature_url_fails_closed(self):
        data = self.valid_lock()
        data["packages"][0]["verification"] = {
            "method": "pgp",
            "key_url": "https://example.invalid/key",
            "key_fingerprint": "A" * 40,
            "key_format": "armored_file",
        }
        self.assertTrue(any("signature_url" in error for error in tool_mirror.validate_lock(data)))

    def test_optional_package_is_only_required_when_enabled(self):
        data = self.valid_lock()
        data["packages"].append({
            "name": "optional",
            "optional": True,
            "blocking_reason": "尚未核实",
        })
        self.assertEqual([], tool_mirror.validate_lock(data, False))
        self.assertTrue(tool_mirror.validate_lock(data, True))

    def test_runner_identity_is_bound_to_expected_values(self):
        data = self.valid_lock()
        runner = data["runners"][0]
        tool_mirror.assert_runner_identity(data, runner["label"], "Test", "TEST")
        with self.assertRaises(tool_mirror.MirrorError):
            tool_mirror.assert_runner_identity(data, runner["label"], "Linux", "X64")

    def test_setup_msys_location_and_prefix_are_explicit(self):
        with tempfile.TemporaryDirectory() as temp:
            with patch.dict(os.environ, {"MSYS2_LOCATION": temp}):
                self.assertEqual(
                    Path(temp) / "usr" / "bin" / "bash.exe",
                    tool_mirror.windows_msys_bash(),
                )
        script = tool_mirror.msys_script("CLANGARM64", "command -v cc")
        self.assertIn('/clangarm64/bin:', script)
        self.assertEqual("minimal", tool_mirror.msys_environment("CLANGARM64")["MSYS2_PATH_TYPE"])

    def test_missing_msys_command_does_not_become_a_tool_path(self):
        with tempfile.TemporaryDirectory() as temp:
            bash = Path(temp) / "bash.exe"
            bash.touch()
            result = type("Result", (), {
                "stdout": "",
                "stderr": "/usr/bin/bash: line 1: autoconf: command not found\n",
                "returncode": 127,
            })()
            with patch.object(tool_mirror.subprocess, "run", return_value=result):
                evidence = tool_mirror.capture_msys_tool(
                    bash, "UCRT64", "autoconf", "autoconf --version"
                )
        self.assertEqual("", evidence["path"])
        self.assertEqual(127, evidence["status"])

    def test_missing_msys_bash_is_observable_without_crashing(self):
        runner = {
            "label": "windows-11-arm",
            "id": "windows-aarch64",
            "platform": "windows",
            "arch": "aarch64",
            "runner_os": "Windows",
            "runner_arch": "ARM64",
            "toolchain": {"shell": {"msystem": "CLANGARM64"}},
        }
        with tempfile.TemporaryDirectory() as temp:
            missing_bash = Path(temp) / "missing-bash.exe"
            environment = {"RUNNER_OS": "Windows", "RUNNER_ARCH": "ARM64"}
            with patch.object(tool_mirror, "MSYS_BASH", missing_bash), patch.dict(os.environ, environment):
                evidence = tool_mirror.collect_discovery({"runners": [runner]}, runner["label"])
        for name in ("cc", "c++", "cmake", "make", "pkg-config", "autoconf", "automake", "libtoolize", "tar", "python"):
            self.assertEqual({"path": "", "version": "", "status": 127}, evidence["tool_versions"][name])
        self.assertEqual("CLANGARM64", evidence["environment"]["MSYSTEM"])
        self.assertEqual("minimal", evidence["environment"]["MSYS2_PATH_TYPE"])
        self.assertEqual("unavailable", evidence["package_manager"])
        self.assertEqual({}, evidence["packages"])

    def test_html_key_extraction_requires_exactly_one_armor_block(self):
        armor = "-----BEGIN PGP PUBLIC KEY BLOCK-----\nabc\n-----END PGP PUBLIC KEY BLOCK-----"
        page = f"<html><pre>{armor}</pre></html>".encode()
        self.assertEqual(armor + "\n", tool_mirror.extract_armored_key(page, "html_armored_block").decode())
        with self.assertRaises(tool_mirror.MirrorError):
            tool_mirror.extract_armored_key(page + page, "html_armored_block")

    def test_manifest_is_stable_and_verifiable(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            payload = root / "payload"
            payload.mkdir()
            (payload / "a.txt").write_text("内容", encoding="utf-8")
            output = root / "manifest.json"
            tool_mirror.create_manifest(self.valid_lock(), payload, output)
            item = json.loads(output.read_text(encoding="utf-8"))["files"][0]
            self.assertEqual("a.txt", item["path"])
            self.assertEqual(tool_mirror.sha256_file(payload / "a.txt"), item["sha256"])

    def test_sbom_contains_verification_method(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "sbom.json"
            tool_mirror.create_sbom(self.valid_lock(), output, False)
            component = json.loads(output.read_text(encoding="utf-8"))["components"][0]
            self.assertEqual("demo", component["name"])
            self.assertIn(
                {"name": "jianvideo:upstream-verification", "value": "immutable_git_commit"},
                component["properties"],
            )

    def test_zip_is_deterministic_and_has_normalized_metadata(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            payload = root / "payload"
            (payload / "bin").mkdir(parents=True)
            (payload / "bin" / "tool").write_bytes(b"binary")
            (payload / "notice.txt").write_text("notice", encoding="utf-8")
            first = root / "first.zip"
            second = root / "second.zip"
            data = self.valid_lock()
            tool_mirror.package_payload(data, payload, first)
            tool_mirror.package_payload(data, payload, second)
            self.assertEqual(hashlib.sha256(first.read_bytes()).digest(), hashlib.sha256(second.read_bytes()).digest())
            with zipfile.ZipFile(first) as archive:
                self.assertEqual(["bin/tool", "notice.txt"], archive.namelist())
                entries = {item.filename: item for item in archive.infolist()}
                self.assertEqual((2024, 1, 1, 0, 0, 0), entries["bin/tool"].date_time)
                self.assertEqual(0o755, entries["bin/tool"].external_attr >> 16)
                self.assertEqual(0o644, entries["notice.txt"].external_attr >> 16)

    def test_non_zip_package_is_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            payload = root / "payload"
            payload.mkdir()
            with self.assertRaises(tool_mirror.MirrorError):
                tool_mirror.package_payload(self.valid_lock(), payload, root / "bad.tar.xz")

    def test_extract_rejects_path_traversal(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            archive = root / "bad.tar"
            with tarfile.open(archive, "w") as output:
                info = tarfile.TarInfo("../escape")
                info.size = 1
                output.addfile(info, BytesIO(b"x"))
            with self.assertRaises(tool_mirror.MirrorError):
                tool_mirror.extract_archive(archive, root / "out")

    def test_generated_dng_matches_committed_fixture(self):
        fixture = SCRIPT_ROOT / "fixtures" / "jianvideo-gradient.dng"
        self.assertEqual(generate_dng.build_dng(), fixture.read_bytes())

    def test_fixture_manifest_requires_real_heic_and_raw_sources(self):
        valid = {
            "schema_version": 1,
            "fixtures": [
                {
                    "id": "heic",
                    "kind": "heic",
                    "path": "sample.heic",
                    "sha256": "1" * 64,
                    "source_url": "https://example.invalid/sample.heic",
                    "license": "CC0-1.0",
                    "license_url": "https://example.invalid/license",
                },
                {
                    "id": "raw",
                    "kind": "raw",
                    "path": "sample.dng",
                    "sha256": "2" * 64,
                    "source_url": "https://example.invalid/sample.dng",
                    "license": "CC0-1.0",
                    "license_url": "https://example.invalid/license",
                },
            ],
        }
        self.assertEqual([], verify_fixtures.validate_manifest(valid))
        valid["fixtures"] = valid["fixtures"][:1]
        self.assertTrue(any("RAW" in error for error in verify_fixtures.validate_manifest(valid)))

    def test_jpeg_validation_rejects_fake_output(self):
        self.assertTrue(verify_fixtures.is_jpeg(b"\xff\xd8payload\xff\xd9"))
        self.assertFalse(verify_fixtures.is_jpeg(b"not-jpeg"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
