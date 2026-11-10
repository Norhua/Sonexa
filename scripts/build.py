#!/usr/bin/env python3
"""Native full build. Downloads happen here, never in the installed application."""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

REPO = Path(__file__).resolve().parents[1]
SIDECAR = REPO / "internal" / "provider" / "spotify" / "sidecar"
sys.path.insert(0, str(SIDECAR))
from artifacts import native_target, locked_target
from package_app import assemble, wails_target
from prepare_python import prepare

PLATFORMS = {"darwin-arm64": "darwin/arm64", "darwin-x86_64": "darwin/amd64",
             "win32-x86_64": "windows/amd64", "win32-arm64": "windows/arm64",
             "linux-x86_64": "linux/amd64", "linux-arm64": "linux/arm64"}


def run(command, *, cwd=REPO, env=None, capture=False):
    print("+", subprocess.list2cmdline([str(arg) for arg in command]), flush=True)
    return subprocess.run([str(arg) for arg in command], cwd=cwd, env=env, check=True,
                          text=True, capture_output=capture)


def require_tool(name):
    executable = shutil.which(name)
    if not executable:
        raise ValueError(f"Missing {name}; install the build prerequisites in build/README.md")
    return executable


def npm_command(npm):
    # Execute npm's JS entry directly on Windows instead of relying on implicit
    # .cmd shell handling/quoting (notably paths containing spaces).
    if sys.platform != "win32":
        return [npm]
    cli = Path(npm).resolve().parent / "node_modules" / "npm" / "bin" / "npm-cli.js"
    if not cli.is_file():
        raise ValueError("Cannot locate npm-cli.js beside npm.cmd; use the standard Node.js/npm installation")
    return [require_tool("node"), str(cli)]


def find_wails(explicit, go, env, offline):
    if explicit:
        path = Path(explicit).expanduser().resolve()
        if not path.is_file():
            raise ValueError(f"Wails executable does not exist: {path}")
        return str(path)
    name = "wails.exe" if sys.platform == "win32" else "wails"
    found = shutil.which("wails")
    if found:
        return found
    paths = run([go, "env", "GOBIN", "GOPATH"], env=env, capture=True).stdout.splitlines()
    candidates = [Path(paths[0]) / name] if paths[0] else []
    candidates.extend(Path(p) / "bin" / name for p in paths[1].split(os.pathsep))
    tools = REPO / "build" / "spotify-helper" / "tools"
    candidates.append(tools / name)
    for candidate in candidates:
        if candidate.is_file():
            return str(candidate.resolve())
    if offline:
        raise ValueError("Wails is missing in offline mode; run an online build first or pass --wails")
    version = re.search(r"github.com/wailsapp/wails/v2\s+(v\S+)", (REPO / "go.mod").read_text())[1]
    tools.mkdir(parents=True, exist_ok=True)
    run([go, "install", f"github.com/wailsapp/wails/v2/cmd/wails@{version}"],
        env=dict(env, GOBIN=str(tools)))
    return str(tools / name)


def build(args):
    if sys.version_info < (3, 14):
        raise ValueError("Build bootstrap requires Python 3.14+ (zstd archive metadata); the app needs no Python")
    target = native_target()
    locked_target(target)
    platform = args.platform or PLATFORMS[target]
    wails_target(platform)  # reject cross-freezing before downloading anything
    go, npm = require_tool("go"), npm_command(require_tool("npm"))
    environment = dict(os.environ, npm_config_audit="false", npm_config_fund="false")
    if args.offline:
        environment.update(GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", npm_config_offline="true")
    wails = find_wails(args.wails, go, environment, args.offline)
    version = run([wails, "version"], env=environment, capture=True).stdout
    if not re.search(r"\bv2\.", version):
        raise ValueError("Wails v2 is required")
    # Build-time Python injection must not affect the private environment.
    for key in list(environment):
        if key.upper().startswith(("PYTHON", "_PYI_", "PYINSTALLER_")):
            environment.pop(key)
    cache = (args.cache or REPO / "build" / "spotify-helper" / "cache" / target).resolve()
    python = prepare(cache, args.offline)
    wheelhouse = cache / "wheelhouse"
    wheelhouse.mkdir(parents=True, exist_ok=True)
    if not args.offline:
        run([python, "-I", "-m", "pip", "--isolated", "download", "--require-hashes", "--only-binary=:all:",
             "--index-url", "https://pypi.org/simple", "-r", SIDECAR / "requirements.lock", "-d", wheelhouse], env=environment)
    venv = cache / "build-venv"
    run([python, "-I", "-m", "venv", "--clear", venv], env=environment)
    interpreter = venv / ("Scripts/python.exe" if target.startswith("win32-") else "bin/python")
    run([interpreter, "-I", "-m", "pip", "--isolated", "install", "--no-index", "--find-links", wheelhouse,
         "--require-hashes", "--only-binary=:all:", "-r", SIDECAR / "requirements.lock"], env=environment)
    run([interpreter, "-I", "-m", "pip", "--isolated", "check"], env=environment)
    run([interpreter, "-I", SIDECAR / "collect_licenses.py", "--wheelhouse", wheelhouse,
         "--python-metadata", cache / "python-metadata" / "python",
         "--output", REPO / "build" / "spotify-helper" / "licenses"], env=environment)
    run([interpreter, "-I", SIDECAR / "freeze.py"], env=environment)
    helper = REPO / "build" / "spotify-helper" / "dist" / "spotify-helper"
    run([interpreter, "-I", SIDECAR / "verify_frozen.py", helper], env=environment)
    run([go, "mod", "download"], env=environment)
    run([*npm, "ci", "--no-audit", "--no-fund"], cwd=REPO / "frontend", env=environment)
    tags = args.tags
    if target.startswith("linux-") and not tags:
        pkgconfig = require_tool("pkg-config")
        if subprocess.run([pkgconfig, "--exists", "webkit2gtk-4.1"], check=False).returncode == 0:
            tags = "webkit2_41"
    command = [interpreter, "-I", SIDECAR / "package_app.py", "build", "--platform", platform, "--wails", wails]
    if not args.offline:
        command.append("--online")
    if tags:
        command.extend(["--tags", tags])
    run(command, env=environment)
    name = json.loads((REPO / "wails.json").read_text())["outputfilename"]
    release = REPO / "build" / "release" / platform.replace("/", "-")
    release.mkdir(parents=True, exist_ok=True)
    if target.startswith("darwin-"):
        source, destination = REPO / "build" / "bin" / f"{name}.app", release / f"{name}.app"
        if destination.exists():
            shutil.rmtree(destination)
        shutil.copytree(source, destination)
    else:
        destination = release / name
        if destination.exists():
            shutil.rmtree(destination)
        destination.mkdir()
        binary_name = name + (".exe" if target.startswith("win32-") else "")
        binary = destination / binary_name
        shutil.copy2(REPO / "build" / "bin" / binary_name, binary)
        assemble(binary, helper, target)
    print(f"\nComplete application: {destination}\nDistribute this ENTIRE app/directory, not just its executable.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--offline", action="store_true", help="Use only previously downloaded build caches")
    parser.add_argument("--cache", type=Path, help="Native Python archive/wheel cache directory")
    parser.add_argument("--platform", choices=PLATFORMS.values(), help="Must match the native build environment")
    parser.add_argument("--wails", help="Existing Wails v2 executable (auto-detected/installed otherwise)")
    parser.add_argument("--tags", default="", help="Wails Go build tags; Linux WebKit 4.1 auto-detected by default")
    args = parser.parse_args()
    try:
        build(args)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Build failed: {error}\nSee build/README.md. Do not distribute outputs from this failed run.\n")


if __name__ == "__main__":
    main()
