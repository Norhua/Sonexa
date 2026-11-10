"""Frozen helper smoke test; macOS additionally uses an OS sandbox."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from artifacts import native_target, validate_manifest
from protocol import read_frame


def verify(bundle, deny_paths=()):
    manifest = validate_manifest(bundle, native_target())
    with tempfile.TemporaryDirectory(prefix="Sonexa 中文 路径 ") as directory:
        root = Path(directory)
        copied = root / "应用 空格" / "spotify-helper"
        shutil.copytree(bundle, copied)
        executable = copied / ("spotify-helper.exe" if sys.platform == "win32" else "spotify-helper")
        before = {p.relative_to(copied).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in copied.rglob("*") if p.is_file()}
        env = {"PATH": str(root / "no-tools"), "HOME": str(root / "empty-home"),
               "TMPDIR": str(root), "TMP": str(root), "TEMP": str(root),
               "PYTHONHOME": str(root / "invalid-python"),
               "PYTHONPATH": str(root / "invalid-modules")}
        # Windows needs OS DLL/temporary-directory locations, not PATH Python.
        for key in ("SystemRoot", "WINDIR", "SYSTEMROOT"):
            if key in os.environ:
                env[key] = os.environ[key]
        command = [str(executable), "--self-test"]
        sandboxed = sys.platform == "darwin"
        # Deny network, runtime resource writes and external interpreters/libraries.
        # On other targets the helper's audit hook/fixture transport is the
        # offline guard, not an OS-level sandbox or clean-machine acceptance.
        repo = Path(__file__).resolve().parents[4]
        denied = [repo / "tmp" / "_scratch" / "spotify-audit", Path("/nix"),
                  Path.home() / ".local" / "share" / "uv" / "python", Path("/usr/bin/python3"),
                  Path(sys.prefix).resolve(), Path(sys.base_prefix).resolve(), *deny_paths]
        profile = '(version 1) (allow default) (deny network*) '
        profile += '(deny file-read* ' + ' '.join(f'(subpath {json.dumps(str(p))})' for p in denied) + ') '
        profile += f'(deny file-write* (subpath {json.dumps(str(copied))}))'
        if sandboxed:
            command = ["/usr/bin/sandbox-exec", "-p", profile, *command]
        process = subprocess.run(command, env=env, cwd=root, capture_output=True, timeout=30)
        if process.returncode != 0:
            raise RuntimeError(f"Frozen self-test failed: {process.stderr[:4096]!r}; {process.stdout[:4096]!r}")
        stream = io.BytesIO(process.stdout)
        result = read_frame(stream)
        if stream.read() or result.get("selfTest") != "ok" or not result.get("frozen"):
            raise RuntimeError("Invalid frozen handshake")
        assert result["python"] == manifest["python"]
        assert result["spotifyScraper"] == manifest["spotifyScraper"]
        assert result["rawSeen"] == 205
        after = {p.relative_to(copied).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                 for p in copied.rglob("*") if p.is_file()}
        if before != after:
            raise RuntimeError("Runtime modified application resources")
        if sandboxed:
            subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(executable)], check=True)
            subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(copied / "_internal" / "libpython3.12.dylib")], check=True)
        print(json.dumps({"handshake": result, "osSandbox": sandboxed, "externalPythonDenied": sandboxed,
                          "pathToolsAbsent": True, "unicodeSpacePath": True, "resourcesUnchanged": True,
                          "signature": "ad-hoc only" if sandboxed else "unsigned",
                          "nativeUIAccepted": False}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("bundle", type=Path)
    parser.add_argument("--deny-path", action="append", default=[], type=Path, help="Additional build/cache path denied to the helper")
    args = parser.parse_args()
    verify(args.bundle.resolve(), [p.resolve() for p in args.deny_path])
