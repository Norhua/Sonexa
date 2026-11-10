import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import artifacts
import package_app
import prepare_python


class ArtifactTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.target = "darwin-arm64"
        self.lock = artifacts.locked_target(self.target)
        (self.root / "licenses").mkdir()
        text = self.root / "licenses" / "LICENSE.txt"
        text.write_text("synthetic license fixture")
        f = {"path": "LICENSE.txt", "sha256": artifacts.sha256(text)}
        wheel_hash = "8a83af7f6569864f409e406110988adc58ae6d433b9527b4a25be5dfebcd148c"
        inventory = {"target": self.target, "python": self.lock["python"], "pythonBuild": self.lock["build"],
                     "packages": [{"sha256": wheel_hash, "files": [f]}], "pythonLicenses": [f]}
        (self.root / "licenses" / "inventory.json").write_text(json.dumps(inventory))
        (self.root / "spotify-helper").write_bytes(b"synthetic executable")
        artifacts.write_manifest(self.root, self.target)

    def test_exact_manifest_and_license_payload(self):
        artifacts.validate_manifest(self.root, self.target)
        (self.root / "injected.py").write_text("unexpected")
        with self.assertRaises(ValueError): artifacts.validate_manifest(self.root, self.target)
        (self.root / "injected.py").unlink()
        (self.root / "licenses" / "LICENSE.txt").write_text("changed")
        with self.assertRaises(ValueError): artifacts.write_manifest(self.root, self.target)

    def test_missing_resource_and_wrong_target(self):
        (self.root / "spotify-helper").unlink()
        with self.assertRaises(ValueError): artifacts.validate_manifest(self.root, self.target)
        with self.assertRaises(ValueError): artifacts.validate_manifest(self.root, "win32-x86_64")

    def test_escaping_manifest_never_reads_outside(self):
        for relative in ("../secret", "/tmp/secret", "foo\\bar", "a/../b", "C:/secret"):
            with self.subTest(relative=relative), self.assertRaises(ValueError):
                artifacts.resource_path(self.root, relative)

    def test_resource_locations(self):
        binary = self.root / "中文.app" / "Contents" / "MacOS" / "sonexa"
        self.assertEqual(package_app.resource_directory(binary, self.target), self.root / "中文.app" / "Contents" / "Resources" / "spotify-helper")
        binary = self.root / "build" / "bin" / "sonexa"
        self.assertEqual(package_app.resource_directory(binary, self.target), self.root / "build" / "spotify-helper" / "dist" / "spotify-helper")
        binary = self.root / "安装 空格" / "sonexa.exe"
        self.assertEqual(package_app.resource_directory(binary, "win32-x86_64"), binary.parent / "resources" / "spotify-helper")

    def test_no_unverified_or_cross_platform_claim(self):
        with patch.object(package_app, "native_target", return_value="darwin-arm64"):
            with self.assertRaises(ValueError): package_app.wails_target("windows/amd64")
        with patch.object(package_app, "native_target", return_value="linux-x86_64"):
            with self.assertRaises(ValueError): package_app.wails_target("darwin/arm64")

    def test_offline_preparation_does_not_download(self):
        path = self.root / "python.tar.gz"
        digest = hashlib.sha256(b"cached").hexdigest()
        with patch.object(prepare_python.urllib.request, "urlopen", side_effect=AssertionError("network")):
            with self.assertRaises(ValueError): prepare_python.download("https://releases.astral.sh/test", digest, path, True)
            path.write_bytes(b"cached")
            prepare_python.download("https://releases.astral.sh/test", digest, path, True)
            path.write_bytes(b"corrupt")
            with self.assertRaises(ValueError): prepare_python.download("https://releases.astral.sh/test", digest, path, True)

    def test_build_forces_offline_tools_before_assembly(self):
        with patch.object(package_app, "wails_target", return_value=self.target), \
             patch.object(package_app.subprocess, "run") as run, \
             patch.object(package_app, "assemble") as assemble, \
             patch.object(package_app.sys, "argv", ["package_app.py", "build", "--platform", "darwin/arm64", "--helper", str(self.root)]):
            package_app.main()
            environment = run.call_args.kwargs["env"]
            self.assertEqual(environment["GOPROXY"], "off")
            self.assertEqual(environment["GOTOOLCHAIN"], "local")
            self.assertEqual(environment["npm_config_offline"], "true")
            assemble.assert_called_once()

    def test_assembly_validates_before_replacing_old_resources(self):
        binary = self.root / "app" / "Contents" / "MacOS" / "sonexa"
        binary.parent.mkdir(parents=True)
        binary.write_text("main")
        destination = package_app.resource_directory(binary, self.target)
        destination.mkdir(parents=True)
        (destination / "sentinel").write_text("existing")
        with self.assertRaises(ValueError): package_app.assemble(binary, self.root, self.target)
        self.assertEqual((destination / "sentinel").read_text(), "existing")

    def test_assembly_replaces_stale_directory_without_changing_source(self):
        with tempfile.TemporaryDirectory() as output:
            binary = Path(output) / "中文.app" / "Contents" / "MacOS" / "sonexa"
            binary.parent.mkdir(parents=True)
            binary.write_text("main")
            destination = package_app.resource_directory(binary, self.target)
            destination.mkdir(parents=True)
            (destination / "stale-native.dylib").write_text("old")
            before = artifacts.validate_manifest(self.root, self.target)
            with patch.object(package_app.subprocess, "run") as run, patch.object(package_app, "verify") as verify:
                package_app.assemble(binary, self.root, self.target)
                verify.assert_called_once_with(destination)
                self.assertIn(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(binary.parent.parent.parent)], [c.args[0] for c in run.call_args_list])
            self.assertFalse((destination / "stale-native.dylib").exists())
            self.assertEqual(before, artifacts.validate_manifest(self.root, self.target))
            self.assertEqual(before, artifacts.validate_manifest(destination, self.target))
