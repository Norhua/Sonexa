import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import artifacts
import package_app
import verify_frozen
from protocol import write_frame
import io

REPO = Path(__file__).resolve().parents[5]
spec = importlib.util.spec_from_file_location("sonexa_build", REPO / "scripts" / "build.py")
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class NativeBuildTests(unittest.TestCase):
    def test_native_target_aliases(self):
        for system, machine, expected in [("win32", "AMD64", "win32-x86_64"), ("win32", "ARM64", "win32-arm64"),
                                           ("linux", "aarch64", "linux-arm64"), ("darwin", "x86_64", "darwin-x86_64")]:
            with self.subTest(expected=expected), patch.object(artifacts.sys, "platform", system), \
                 patch.object(artifacts.platform, "machine", return_value=machine):
                self.assertEqual(artifacts.native_target(), expected)

    def test_all_six_locked_targets_are_native_only(self):
        for target, platform in builder.PLATFORMS.items():
            lock = artifacts.locked_target(target)
            for field in ("sha256", "metadataSha256"):
                self.assertRegex(lock["targets"][target][field], r"^[0-9a-f]{64}$")
            with patch.object(package_app, "native_target", return_value=target):
                self.assertEqual(package_app.wails_target(platform), target)
                with self.assertRaises(ValueError): package_app.wails_target("darwin/universal")
            with patch.object(package_app, "native_target", return_value="other"):
                with self.assertRaises(ValueError): package_app.wails_target(platform)

    def test_bootstrap_help_needs_no_third_party_modules(self):
        # -S omits site-packages, including SpotifyScraper; help must still work.
        result = subprocess.run([sys.executable, "-I", "-S", str(REPO / "scripts" / "build.py"), "--help"],
                                capture_output=True, check=True)
        self.assertIn(b"--offline", result.stdout)

    def test_explicit_wails_path_and_offline_missing_tool(self):
        with tempfile.TemporaryDirectory(prefix="build tools ") as temp:
            tool = Path(temp) / "wails"
            tool.write_text("fixture")
            self.assertEqual(builder.find_wails(str(tool), "go", {}, True), str(tool.resolve()))
            with self.assertRaises(ValueError): builder.find_wails(str(tool / "missing"), "go", {}, True)
            with patch.object(builder.shutil, "which", return_value=None), \
                 patch.object(builder, "run", return_value=subprocess.CompletedProcess([], 0, "\n" + temp + "\n")), \
                 patch.object(builder, "REPO", Path(temp)):
                with self.assertRaises(ValueError): builder.find_wails(None, "go", {}, True)

    def test_windows_npm_avoids_cmd_shell(self):
        with tempfile.TemporaryDirectory(prefix="Node tools ") as temp:
            npm = Path(temp) / "npm.cmd"
            npm.write_text("fixture")
            cli = Path(temp) / "node_modules" / "npm" / "bin" / "npm-cli.js"
            with patch.object(builder.sys, "platform", "win32"), patch.object(builder, "require_tool", return_value="node.exe"):
                with self.assertRaises(ValueError): builder.npm_command(str(npm))
                cli.parent.mkdir(parents=True)
                cli.write_text("fixture")
                self.assertEqual(builder.npm_command(str(npm)), ["node.exe", str(cli.resolve())])

    def test_run_stops_on_failure_and_does_not_use_shell(self):
        with patch.object(builder.subprocess, "run", side_effect=subprocess.CalledProcessError(1, "tool")) as run:
            with self.assertRaises(subprocess.CalledProcessError): builder.run(["tool", "path with spaces"])
            self.assertTrue(run.call_args.kwargs["check"])
            self.assertNotIn("shell", run.call_args.kwargs)

    def test_windows_and_linux_build_paths_and_online_mode(self):
        for target, platform, filename in [("win32-x86_64", "windows/amd64", "sonexa.exe"), ("linux-arm64", "linux/arm64", "sonexa")]:
            with patch.object(package_app, "wails_target", return_value=target), \
                 patch.object(package_app, "validate_manifest"), patch.object(package_app, "assemble") as assemble, \
                 patch.object(package_app.subprocess, "run") as run, \
                 patch.object(package_app.sys, "argv", ["package_app.py", "build", "--platform", platform, "--online", "--tags", "webkit2_41"]), \
                 patch.dict(package_app.os.environ, {"GOPROXY": "https://proxy.golang.org"}):
                package_app.main()
                self.assertEqual(assemble.call_args.args[0], REPO / "build" / "bin" / filename)
                self.assertEqual(run.call_args.kwargs["env"]["GOPROXY"], "https://proxy.golang.org")
                self.assertIn("webkit2_41", run.call_args.args[0])

    @unittest.skipIf(sys.platform == "win32", "symlink creation may require developer mode")
    def test_materialize_only_internal_native_library_links(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "library.so.1").write_bytes(b"fixture")
            link = root / "library.so"
            link.symlink_to("library.so.1")
            artifacts.materialize_links(root)
            self.assertFalse(link.is_symlink())
            self.assertEqual(link.read_bytes(), b"fixture")
            (root / "external").symlink_to(Path(sys.executable).resolve())
            with self.assertRaises(ValueError): artifacts.materialize_links(root)

    def test_portable_verifier_runs_frozen_exe_without_path_tools(self):
        for system, target, name in [("win32", "win32-x86_64", "spotify-helper.exe"), ("linux", "linux-x86_64", "spotify-helper")]:
            with tempfile.TemporaryDirectory() as temp:
                bundle = Path(temp) / "bundle"
                bundle.mkdir()
                (bundle / name).write_bytes(b"fixture")
                frame = io.BytesIO()
                write_frame(frame, {"selfTest": "ok", "frozen": True, "python": "3.12.13", "spotifyScraper": "3.9.2", "rawSeen": 205})
                with patch.object(verify_frozen.sys, "platform", system), \
                     patch.object(verify_frozen, "native_target", return_value=target), \
                     patch.object(verify_frozen, "validate_manifest", return_value={"python": "3.12.13", "spotifyScraper": "3.9.2"}), \
                     patch.object(verify_frozen.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, frame.getvalue(), b"")) as run:
                    verify_frozen.verify(bundle)
                    command = run.call_args.args[0]
                    self.assertEqual(Path(command[0]).name, name)
                    self.assertEqual(command[1:], ["--self-test"])
                    self.assertTrue(run.call_args.kwargs["env"]["PATH"].endswith("no-tools"))
