"""Explicit offline Wails resource assembly. Never installs Python or downloads."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent))
from artifacts import native_target, locked_target, validate_manifest, write_manifest
from verify_frozen import verify

REPO = Path(__file__).resolve().parents[4]
DEFAULT_HELPER = REPO / "build" / "spotify-helper" / "dist" / "spotify-helper"


def wails_target(value):
    targets = {"darwin/arm64": "darwin-arm64", "darwin/amd64": "darwin-x86_64",
               "windows/amd64": "win32-x86_64", "windows/arm64": "win32-arm64",
               "linux/amd64": "linux-x86_64", "linux/arm64": "linux-arm64"}
    if value not in targets:
        raise ValueError("Unsupported Wails target")
    target = targets[value]
    locked_target(target)
    if target != native_target():
        raise ValueError("Assemble and self-test on the native target; cross-build is not supported")
    return target


def resource_directory(binary, target):
    if target.startswith("darwin-") and binary.parent.name == "MacOS" and binary.parent.parent.name == "Contents":
        return binary.parent.parent / "Resources" / "spotify-helper"
    if binary.parent.name == "bin" and binary.parent.parent.name == "build":
        return binary.parent.parent / "spotify-helper" / "dist" / "spotify-helper"
    return binary.parent / "resources" / "spotify-helper"


def is_macho(path):
    with path.open("rb") as stream:
        return stream.read(4) in (b"\xcf\xfa\xed\xfe", b"\xce\xfa\xed\xfe", b"\xfe\xed\xfa\xcf", b"\xca\xfe\xba\xbe", b"\xca\xfe\xba\xbf")


def assemble(binary, helper, target):
    if not binary.is_file() or binary.is_symlink():
        raise ValueError("Expected a built native application binary")
    destination = resource_directory(binary, target)
    validate_manifest(helper, target)
    if target.startswith("darwin-"):
        arch = target.removeprefix("darwin-")
        subprocess.run(["/usr/bin/lipo", "-verify_arch", arch, str(binary)], check=True)
        subprocess.run(["/usr/bin/lipo", "-verify_arch", arch, str(helper / "spotify-helper")], check=True)
    if destination.is_symlink():
        raise ValueError("Refusing a symlinked resource destination")
    if destination.resolve() != helper.resolve():
        destination.parent.mkdir(parents=True, exist_ok=True)
        # Prepare the entire directory first. Never merge old native libraries
        # or keep files removed by an upgrade's new manifest.
        with tempfile.TemporaryDirectory(prefix=".spotify-stage-", dir=destination.parent) as temp:
            staged = Path(temp) / "spotify-helper"
            shutil.copytree(helper, staged)
            validate_manifest(staged, target)
            if destination.exists():
                shutil.rmtree(destination)
            staged.replace(destination)
    if target.startswith("darwin-"):
        # Development/ad-hoc only. No identities, keychains or notarisation are
        # accessed. Release signing needs its own approved acceptance procedure.
        for path in sorted(destination.rglob("*")):
            if path.is_file() and is_macho(path):
                subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(path)], check=True)
        write_manifest(destination, target)  # signing changes hashes
        subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(binary)], check=True)
        if binary.parent.name == "MacOS" and binary.parent.parent.name == "Contents":
            app = binary.parent.parent.parent
            subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", str(app)], check=True)
            subprocess.run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(app)], check=True)
        verify(destination)
    else:
        # No platform is silently accepted without a lock and offline verifier.
        raise ValueError("This native target still needs a no-external-Python verifier")
    validate_manifest(destination, target)
    print(json.dumps({"binary": str(binary), "helper": str(destination), "target": target,
                      "signature": "ad-hoc only", "nativeUIAccepted": False}, ensure_ascii=False))


def main():
    parser = argparse.ArgumentParser()
    sub = parser.add_subparsers(dest="action", required=True)
    for action in ("build", "assemble", "verify", "check"):
        p = sub.add_parser(action)
        p.add_argument("--platform", required=True)
        p.add_argument("--helper", type=Path, default=DEFAULT_HELPER)
        if action in ("assemble", "verify"):
            p.add_argument("--binary", type=Path, required=True)
        if action == "build":
            p.add_argument("--wails", default="wails", help="Existing Wails v2 CLI; never auto-installed")
    args = parser.parse_args()
    target = wails_target(args.platform)
    helper = args.helper.resolve()
    if args.action == "verify":
        binary = args.binary.absolute()
        if not binary.is_file() or binary.is_symlink():
            raise ValueError("Expected a built native application binary")
        destination = resource_directory(binary, target)
        validate_manifest(destination, target)
        if target.startswith("darwin-"):
            verify(destination)
            if binary.parent.name == "MacOS":
                subprocess.run(["/usr/bin/codesign", "--verify", "--deep", "--strict", str(binary.parent.parent.parent)], check=True)
        return
    validate_manifest(helper, target)
    if args.action == "check":
        print("Locked helper resources and licenses verified:", target)
        return
    if args.action == "build":
        # Explicit entry, not a global hook: an ordinary Wails build can still
        # build QQ/local-only development without a prepared Spotify runtime.
        environment = dict(os.environ, GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local",
                           npm_config_offline="true", npm_config_audit="false", npm_config_fund="false")
        subprocess.run([args.wails, "build", "-platform", args.platform, "-m", "-nosyncgomod", "-trimpath"],
                       cwd=REPO, env=environment, check=True)
        config = json.loads((REPO / "wails.json").read_text())
        name = config["outputfilename"]
        binary = REPO / "build" / "bin" / f"{name}.app" / "Contents" / "MacOS" / name
    else:
        binary = args.binary.absolute()
    assemble(binary, helper, target)


if __name__ == "__main__":
    main()
