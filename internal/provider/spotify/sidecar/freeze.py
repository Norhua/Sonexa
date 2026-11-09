"""Native one-folder build, no runtime installation or interpreter fallback."""
import hashlib
import json
from pathlib import Path
import platform
import shutil
import subprocess
import sys
from importlib.metadata import version

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[3]
OUTPUT = REPO / "build" / "spotify-helper"


def build():
    lock = json.loads((ROOT / "python.lock.json").read_text())
    target = f"{sys.platform}-{platform.machine()}"
    if target not in lock["targets"] or platform.python_version() != lock["python"]:
        raise SystemExit("No validated Python lock for this target; do not cross-freeze.")
    if version("PyInstaller") != "6.22.3" or version("SpotifyScraper") != "3.9.2":
        raise SystemExit("Install requirements.lock in the isolated build environment first.")
    licenses = OUTPUT / "licenses"
    if not (licenses / "inventory.json").is_file():
        raise SystemExit("Run collect_licenses.py against verified artifacts first.")
    work = OUTPUT / "work"
    work.mkdir(parents=True, exist_ok=True)
    subprocess.run([
        sys.executable, "-I", "-m", "PyInstaller", "--noconfirm", "--clean", "--onedir",
        "--name", "spotify-helper", "--distpath", str(OUTPUT / "dist"),
        "--workpath", str(work), "--specpath", str(work),
        "--paths", str(ROOT), "--copy-metadata", "SpotifyScraper",
        "--add-data", f"{ROOT / 'tests' / 'fixtures'}:tests/fixtures",
        "--exclude-module", "tkinter", "--exclude-module", "pip",
        str(ROOT / "helper.py"),
    ], check=True, cwd=ROOT)
    bundle = OUTPUT / "dist" / "spotify-helper"
    # Installer/cache records contain build-machine paths, not runtime metadata.
    for metadata in bundle.glob("_internal/*.dist-info"):
        for name in ("uv_cache.json", "RECORD", "INSTALLER", "REQUESTED"):
            (metadata / name).unlink(missing_ok=True)
    shutil.copytree(licenses, bundle / "licenses", dirs_exist_ok=True)
    files = {}
    for path in sorted(bundle.rglob("*")):
        if path.is_file() and path.name != "manifest.json":
            files[path.relative_to(bundle).as_posix()] = hashlib.sha256(path.read_bytes()).hexdigest()
    manifest = {"protocol": 1, "target": target, "python": lock["python"], "pythonBuild": lock["build"],
                "spotifyScraper": version("SpotifyScraper"), "pyinstaller": version("PyInstaller"),
                "files": files}
    (bundle / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(bundle)


if __name__ == "__main__":
    build()
