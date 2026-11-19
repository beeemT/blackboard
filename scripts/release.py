#!/usr/bin/env python3
"""Build release archives and a Homebrew formula locally; never publish."""
import gzip
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile


def archive_files(destination, files):
    # Normalize gzip and tar metadata so source mtimes/users do not change bytes.
    with open(destination, "wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for source, name in files:
                    source = Path(source)
                    info = tarfile.TarInfo(name)
                    info.size = source.stat().st_size
                    info.mode = 0o755 if name == "blackboard" else 0o644
                    with source.open("rb") as data:
                        archive.addfile(info, data)


def build(tag):
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("expected a stable release tag vMAJOR.MINOR.PATCH")
    version = tag[1:]
    dist = Path("dist")
    dist.mkdir(exist_ok=True)
    if any(dist.iterdir()):
        raise ValueError("dist must be empty; move previous artifacts before building")
    checksums = []
    hashes = {}
    for target_os in ("darwin", "linux"):
        for arch in ("arm64", "amd64"):
            asset = f"blackboard_{version}_{target_os}_{arch}.tar.gz"
            with tempfile.TemporaryDirectory() as tmp:
                binary = Path(tmp) / "blackboard"
                subprocess.run([
                    "go", "build", "-trimpath", "-buildvcs=false", "-ldflags",
                    f"-s -w -X github.com/beeemT/blackboard/internal/cli.Version={version}",
                    "-o", str(binary), "./cmd/blackboard",
                ], env={**os.environ, "CGO_ENABLED": "0", "GOOS": target_os, "GOARCH": arch}, check=True)
                archive_files(dist / asset, [(binary, "blackboard"), ("LICENSE", "LICENSE"),
                              ("README.md", "README.md"), ("skills/blackboard/SKILL.md", "skills/blackboard/SKILL.md")])
            digest = hashlib.sha256((dist / asset).read_bytes()).hexdigest()
            hashes[target_os, arch] = digest
            checksums.append(f"{digest}  {asset}\n")
    (dist / "checksums.txt").write_text("".join(checksums))
    formula = ["class Blackboard < Formula", '  desc "Local topic coordination registry for coding agents"',
               '  homepage "https://github.com/beeemT/blackboard"', '  license "MIT"', f'  version "{version}"']
    for target_os, condition in (("darwin", "macos"), ("linux", "linux")):
        formula.append(f"  on_{condition} do")
        for arch, condition_arch in (("arm64", "arm"), ("amd64", "intel")):
            formula.extend([f"    on_{condition_arch} do",
                f'      url "https://github.com/beeemT/blackboard/releases/download/{tag}/blackboard_{version}_{target_os}_{arch}.tar.gz"',
                f'      sha256 "{hashes[target_os, arch]}"', "    end"])
        formula.append("  end")
    formula.extend(['  def install', '    bin.install "blackboard"', '    pkgshare.install "skills"', '  end',
                    '  test do', '    assert_match "blackboard", shell_output("#{bin}/blackboard help")',
                    '    assert_match "registry", shell_output("#{bin}/blackboard --db #{testpath}/registry.db status")',
                    '    assert_path_exists pkgshare/"skills/blackboard/SKILL.md"', '  end', 'end', ''])
    (dist / "blackboard.rb").write_text("\n".join(formula))
    print("Built four archives, checksums.txt and blackboard.rb (nothing published)")


if __name__ == "__main__":
    try:
        if len(sys.argv) != 2:
            raise ValueError("usage: python3 scripts/release.py vMAJOR.MINOR.PATCH")
        build(sys.argv[1])
    except (ValueError, subprocess.CalledProcessError) as error:
        sys.exit(str(error))
