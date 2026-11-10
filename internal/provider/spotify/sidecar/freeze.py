"""Native one-folder build, no runtime installation or interpreter fallback."""
import shutil
import subprocess
import sys
from pathlib import Path
import platform
from importlib.metadata import version

sys.path.insert(0, str(Path(__file__).resolve().parent))
from artifacts import native_target, locked_target, write_manifest, validate_licenses, materialize_links

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[3]
OUTPUT = REPO / "build" / "spotify-helper"


def build():
    target = native_target()
    lock = locked_target(target)
    if platform.python_version() != lock["python"]:
        raise SystemExit("Use the locked Python interpreter; do not cross-freeze.")
    if version("PyInstaller") != "6.22.3" or version("SpotifyScraper") != "3.9.2":
        raise SystemExit("Install requirements.lock in the isolated build environment first.")
    licenses = OUTPUT / "licenses"
    if not (licenses / "inventory.json").is_file():
        raise SystemExit("Run collect_licenses.py against verified artifacts first.")
    validate_licenses(OUTPUT, target, lock)
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
    materialize_links(bundle)
    # Installer/cache records contain build-machine paths, not runtime metadata.
    for metadata in bundle.glob("_internal/*.dist-info"):
        for name in ("uv_cache.json", "RECORD", "INSTALLER", "REQUESTED"):
            (metadata / name).unlink(missing_ok=True)
    shutil.copytree(licenses, bundle / "licenses", dirs_exist_ok=True)
    write_manifest(bundle, target)
    print(bundle)


if __name__ == "__main__":
    build()
