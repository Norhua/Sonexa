"""Build-time license inventory from hash-locked wheels and PBS metadata.

Does not import third-party build catalogs or run code from archives.
"""
import argparse
import email
import hashlib
import json
from pathlib import Path
import re
import shutil
import zipfile


def digest(data):
    return hashlib.sha256(data).hexdigest()


def collect(wheelhouse, python_metadata, output):
    output.mkdir(parents=True, exist_ok=True)
    lock = Path(__file__).with_name("requirements.lock").read_text()
    entries = []
    for wheel in sorted(wheelhouse.glob("*.whl")):
        wheel_hash = digest(wheel.read_bytes())
        if f"sha256:{wheel_hash}" not in lock:
            raise ValueError(f"Wheel is not hash-locked: {wheel.name}")
        with zipfile.ZipFile(wheel) as archive:
            metadata = next(n for n in archive.namelist() if re.fullmatch(r"[^/]+\.dist-info/METADATA", n))
            meta = email.message_from_bytes(archive.read(metadata))
            paths = [n for n in archive.namelist() if re.search(r"(?i)(license|copying|copyright)", Path(n).name)]
            if not paths:
                raise ValueError(f"Missing license: {wheel.name}")
            licenses = []
            for name in paths:
                data = archive.read(name)
                # Preserve vendored package provenance instead of flattening names.
                relative = Path("packages") / wheel.stem / name
                dest = output / relative
                dest.parent.mkdir(parents=True, exist_ok=True)
                dest.write_bytes(data)
                licenses.append({"path": relative.as_posix(), "sha256": digest(data)})
            entries.append({"name": meta["Name"], "version": meta["Version"],
                            "license": meta["License-Expression"] or meta["License"] or "See license file",
                            "wheel": wheel.name, "sha256": wheel_hash, "files": licenses})
    pbs = json.loads((python_metadata / "PYTHON.json").read_text())
    native = []
    for name, variants in sorted(pbs["build_info"]["extensions"].items()):
        for variant in variants:
            if variant.get("license_paths"):
                native.append({"module": name, "licenses": variant["licenses"],
                               "paths": variant["license_paths"], "links": variant.get("links", [])})
    python_licenses = []
    for path in sorted((python_metadata / "licenses").glob("*")):
        dest = output / "python" / path.name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, dest)
        python_licenses.append({"path": str(dest.relative_to(output)), "sha256": digest(path.read_bytes())})
    inventory = {"python": pbs["python_version"], "pythonBuild": "20260807", "target": "darwin-arm64",
                 "packages": entries, "pythonLicenses": python_licenses, "nativeModules": native}
    (output / "inventory.json").write_text(json.dumps(inventory, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--wheelhouse", type=Path, required=True)
    parser.add_argument("--python-metadata", type=Path, required=True,
                        help="python/ from the SHA-256-verified full PBS archive")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    collect(args.wheelhouse, args.python_metadata, args.output)
