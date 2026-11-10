"""Build-only artifact preparation. Requires Python 3.14 for zstd metadata."""
import argparse
import json
from pathlib import Path
import tarfile
import shutil
import sys
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parent))
from artifacts import native_target, sha256

ROOT = Path(__file__).resolve().parent


def download(url, expected, path, offline=False):
    if not url.startswith("https://releases.astral.sh/"):
        raise ValueError("Unexpected build artifact source")
    if not path.exists():
        if offline:
            raise ValueError(f"Missing cached artifact in offline mode: {path.name}")
        request = urllib.request.Request(url, headers={"User-Agent": "uv/0.13.5"})
        partial = path.with_suffix(path.suffix + ".partial")
        try:
            with urllib.request.urlopen(request, timeout=90) as response, partial.open("wb") as output:
                while chunk := response.read(1024 * 1024):
                    output.write(chunk)
            if sha256(partial) != expected:
                raise ValueError("Build artifact SHA-256 mismatch")
            partial.replace(path)
        finally:
            partial.unlink(missing_ok=True)
    if sha256(path) != expected:
        raise ValueError("Cached build artifact SHA-256 mismatch")


def prepare(destination, offline=False):
    from compression import zstd
    lock = json.loads((ROOT / "python.lock.json").read_text())
    target = native_target()
    if target not in lock["targets"]:
        raise ValueError("This target has no verified Python artifact lock")
    artifact = lock["targets"][target]
    destination.mkdir(parents=True, exist_ok=True)
    archive = destination / "python.tar.gz"
    metadata = destination / "python-full.tar.zst"
    download(artifact["url"], artifact["sha256"], archive, offline)
    download(artifact["metadataUrl"], artifact["metadataSha256"], metadata, offline)
    # Re-extract verified archives instead of trusting an old mutable runtime.
    for name in ("python", "python-metadata"):
        path = destination / name
        if path.is_symlink():
            raise ValueError("Refusing a symlinked Python cache")
        if path.exists():
            shutil.rmtree(path)
    with tarfile.open(archive, "r:gz") as source:
        source.extractall(destination, filter="data")
    with zstd.open(metadata, "rb") as stream, tarfile.open(fileobj=stream, mode="r|") as source:
        for entry in source:
            if not entry.isfile() or not (entry.name == "python/PYTHON.json" or entry.name.startswith("python/licenses/")):
                continue
            relative = Path(entry.name)
            if relative.is_absolute() or ".." in relative.parts:
                raise ValueError("Unsafe metadata path")
            path = destination / "python-metadata" / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(source.extractfile(entry).read())
    interpreter = destination / "python" / ("python.exe" if target.startswith("win32-") else "bin/python3.12")
    print(interpreter)
    return interpreter


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("destination", type=Path)
    parser.add_argument("--offline", action="store_true", help="Only use hash-verified cached archives; never download")
    args = parser.parse_args()
    prepare(args.destination.resolve(), args.offline)
