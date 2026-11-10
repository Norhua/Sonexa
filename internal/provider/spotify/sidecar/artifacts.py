"""Build-only artifact contract shared by freezing, assembly and verification."""
import hashlib
import json
from pathlib import Path, PurePosixPath
import platform
import sys

ROOT = Path(__file__).resolve().parent
PROTOCOL = 1
SCRAPER = "3.9.2"
PYINSTALLER = "6.22.3"


def native_target():
    arch = {"amd64": "x86_64", "aarch64": "arm64"}.get(platform.machine().lower(), platform.machine().lower())
    return f"{sys.platform}-{arch}"


def locked_target(target):
    lock = json.loads((ROOT / "python.lock.json").read_text())
    if target not in lock["targets"]:
        raise ValueError(f"No verified Python artifact lock for {target}; do not cross-freeze or claim support")
    return lock


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def resource_path(root, relative):
    p = PurePosixPath(relative)
    if not relative or p.is_absolute() or ".." in p.parts or "\\" in relative or str(p) != relative or ":" in relative:
        raise ValueError("Unsafe artifact path")
    path = root / relative
    if not path.resolve().is_relative_to(root.resolve()):
        raise ValueError("Artifact path escapes its resource directory")
    return path


def materialize_links(root):
    """PyInstaller on Linux may alias bundled .so files with symlinks.

    Distribute only regular files, consistent with the runtime hash contract.
    Never copy a link to an external system/build-machine library.
    """
    links = [p for p in root.rglob("*") if p.is_symlink()]
    for path in links:
        resolved = path.resolve(strict=True)
        if not resolved.is_relative_to(root.resolve()) or not resolved.is_file():
            raise ValueError("Frozen symlink is not an internal regular file")
        data, mode = resolved.read_bytes(), resolved.stat().st_mode
        path.unlink()
        path.write_bytes(data)
        path.chmod(mode)


def file_hashes(root):
    files = {}
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError("Symlinks are not supported in the frozen artifact contract")
        if path.is_file() and path != root / "manifest.json":
            files[path.relative_to(root).as_posix()] = sha256(path)
    return files


def validate_licenses(root, target, lock):
    inventory = json.loads((root / "licenses" / "inventory.json").read_text(encoding="utf-8"))
    if (inventory.get("target"), inventory.get("python"), inventory.get("pythonBuild")) != (target, lock["python"], lock["build"]):
        raise ValueError("License inventory target/version mismatch")
    if not inventory.get("packages") or not inventory.get("pythonLicenses"):
        raise ValueError("Full package and Python license texts are required")
    requirements = (ROOT / "requirements.lock").read_text()
    for package in inventory["packages"]:
        if f"sha256:{package['sha256']}" not in requirements or not package.get("files"):
            raise ValueError("Unverified package license inventory")
    for entry in inventory["pythonLicenses"] + [f for p in inventory["packages"] for f in p["files"]]:
        path = resource_path(root / "licenses", entry["path"])
        if sha256(path) != entry["sha256"]:
            raise ValueError("License text mismatch")


def write_manifest(root, target):
    lock = locked_target(target)
    validate_licenses(root, target, lock)
    manifest = {"protocol": PROTOCOL, "target": target, "python": lock["python"], "pythonBuild": lock["build"],
                "spotifyScraper": SCRAPER, "pyinstaller": PYINSTALLER, "files": file_hashes(root)}
    (root / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    return validate_manifest(root, target)


def validate_manifest(root, target):
    lock = locked_target(target)
    manifest = json.loads((root / "manifest.json").read_text())
    expected = {"protocol": PROTOCOL, "target": target, "python": lock["python"], "pythonBuild": lock["build"],
                "spotifyScraper": SCRAPER, "pyinstaller": PYINSTALLER}
    if any(manifest.get(k) != v for k, v in expected.items()) or not manifest.get("files"):
        raise ValueError("Helper manifest target/version mismatch")
    name = "spotify-helper.exe" if target.startswith("win32-") else "spotify-helper"
    if name not in manifest["files"]:
        raise ValueError("Missing helper executable")
    for relative in manifest["files"]:
        resource_path(root, relative)
    if file_hashes(root) != manifest["files"]:
        raise ValueError("Missing, changed or unlisted helper resource")
    validate_licenses(root, target, lock)
    return manifest
