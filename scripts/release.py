#!/usr/bin/env python3
"""Build six portable archives and SHA-256 checksums using only Python stdlib."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
TARGETS = [(system, arch) for system in ("darwin", "linux", "windows") for arch in ("amd64", "arm64")]


def run(*args, **kwargs):
    return subprocess.run(args, cwd=ROOT, check=True, **kwargs)


def main():
    if len(sys.argv) != 2 or not re.fullmatch(r"v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", sys.argv[1]):
        raise SystemExit("usage: python3 scripts/release.py vMAJOR.MINOR.PATCH")
    version = sys.argv[1]
    dist = ROOT / "dist"
    dist.mkdir(exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0")
    flags = ["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", f"-s -w -X main.version={version}"]
    sums = []
    with tempfile.TemporaryDirectory(prefix="masky-release-") as temporary:
        stage = Path(temporary)
        host = stage / ("host.exe" if os.name == "nt" else "host")
        run(*flags, "-o", str(host), "./cmd/masky", env=env)
        common = {name: (ROOT / name).read_bytes() for name in ("README.md", "LICENSE", "NOTICE")}
        for shell, name in [("bash", "masky.bash"), ("zsh", "_masky"), ("fish", "masky.fish"), ("powershell", "masky.ps1")]:
            common["completions/" + name] = run(str(host), "completion", shell, capture_output=True).stdout
        for system, arch in TARGETS:
            binary = "masky.exe" if system == "windows" else "masky"
            target = stage / binary
            run(*flags, "-o", str(target), "./cmd/masky", env=dict(env, GOOS=system, GOARCH=arch))
            files = dict(common, **{binary: target.read_bytes()})
            suffix = ".zip" if system == "windows" else ".tar.gz"
            archive = dist / f"masky_{version}_{system}_{arch}{suffix}"
            if system == "windows":
                with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as z:
                    for name, data in sorted(files.items()):
                        info = zipfile.ZipInfo(name, date_time=(2026, 1, 1, 0, 0, 0))
                        info.compress_type = zipfile.ZIP_DEFLATED
                        info.external_attr = (0o755 if name == binary else 0o644) << 16
                        z.writestr(info, data)
            else:
                with archive.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as gz, tarfile.open(fileobj=gz, mode="w") as tar:
                    for name, data in sorted(files.items()):
                        info = tarfile.TarInfo(name)
                        info.size, info.mode, info.mtime = len(data), (0o755 if name == binary else 0o644), 0
                        tar.addfile(info, io.BytesIO(data))
            sums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
            print(archive.name, flush=True)
    (dist / "checksums.txt").write_text("".join(sorted(sums)), encoding="utf-8")


if __name__ == "__main__":
    main()
