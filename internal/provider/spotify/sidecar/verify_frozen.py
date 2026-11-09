"""Offline frozen artifact check on macOS; not native application acceptance."""
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

from protocol import read_frame


def verify(bundle):
    manifest = json.loads((bundle / "manifest.json").read_text())
    for relative, expected in manifest["files"].items():
        if hashlib.sha256((bundle / relative).read_bytes()).hexdigest() != expected:
            raise RuntimeError(f"Resource mismatch: {relative}")
    with tempfile.TemporaryDirectory(prefix="Sonexa 中文 路径 ") as directory:
        root = Path(directory)
        copied = root / "应用 空格" / "spotify-helper"
        shutil.copytree(bundle, copied)
        executable = copied / "spotify-helper"
        before = {p.relative_to(copied).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in copied.rglob("*") if p.is_file()}
        env = {"PATH": str(root / "no-tools"), "HOME": str(root / "empty-home"),
               "TMPDIR": str(root), "PYTHONHOME": str(root / "invalid-python"),
               "PYTHONPATH": str(root / "invalid-modules")}
        if sys.platform != "darwin":
            raise RuntimeError("This sandbox verification is macOS-only; do not claim other platforms.")
        # Deny network, runtime resource writes and external interpreters/libraries.
        # Tests run under the same OS sandbox as the frozen process, not mocks.
        repo = Path(__file__).resolve().parents[4]
        denied = [repo / "tmp" / "_scratch" / "spotify-audit", Path("/nix"),
                  Path.home() / ".local" / "share" / "uv" / "python", Path("/usr/bin/python3")]
        profile = '(version 1) (allow default) (deny network*) '
        profile += '(deny file-read* ' + ' '.join(f'(subpath {json.dumps(str(p))})' for p in denied) + ') '
        profile += f'(deny file-write* (subpath {json.dumps(str(copied))}))'
        process = subprocess.run(["/usr/bin/sandbox-exec", "-p", profile, str(executable), "--self-test"],
                                 env=env, cwd=root, capture_output=True, timeout=20)
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
        subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(executable)], check=True)
        subprocess.run(["/usr/bin/codesign", "--verify", "--strict", str(copied / "_internal" / "libpython3.12.dylib")], check=True)
        print(json.dumps({"handshake": result, "networkDenied": True, "externalPythonDenied": True,
                          "pathToolsAbsent": True, "unicodeSpacePath": True, "resourcesUnchanged": True,
                          "signature": "ad-hoc only, not distribution signing"}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("bundle", type=Path)
    args = parser.parse_args()
    verify(args.bundle.resolve())
