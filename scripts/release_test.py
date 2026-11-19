"""Contract checks for the no-publish packaging command."""
import importlib.util
import tempfile
import os
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def test_invalid_tags_rejected_before_writes(self):
        for tag in ("1.0.0", "v1.0", "v01.0.0", "v1.0.0-rc1", "v1.2.3;echo bad", "v1.2.3\n"):
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.build(tag)

    def test_archive_metadata_is_deterministic(self):
        import tarfile
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "binary"
            source.write_bytes(b"test binary")
            first, second = root / "first.tar.gz", root / "second.tar.gz"
            release.archive_files(first, [(source, "blackboard")])
            os.utime(source, (1700000000, 1700000000))
            source.chmod(0o600)
            release.archive_files(second, [(source, "blackboard")])
            self.assertEqual(first.read_bytes(), second.read_bytes())
            with tarfile.open(first) as archive:
                info = archive.getmember("blackboard")
                self.assertEqual((info.uid, info.gid, info.mtime, info.mode), (0, 0, 0, 0o755))
                self.assertEqual(archive.extractfile(info).read(), b"test binary")

    def test_published_asset_guard(self):
        import json
        from unittest.mock import patch
        workflow = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"
        block = workflow.read_text().split('          python3 - <<\'PYTHON\'\n', 1)[1].split("\n          PYTHON", 1)[0]
        code = "\n".join(line[10:] for line in block.splitlines())
        cwd = os.getcwd()
        with tempfile.TemporaryDirectory() as tmp:
            try:
                os.chdir(tmp)
                Path("dist").mkdir()
                Path("dist/blackboard_1.0.0_linux_arm64.tar.gz").write_bytes(b"archive")
                Path("dist/checksums.txt").write_bytes(b"checksums")
                def run_case(existing, remote, fails=False):
                    Path("releases.json").write_text(json.dumps([[{"tag_name": "v1.0.0", "assets": [{"name": name} for name in existing]}]]))
                    def download(args, check):
                        if fails:
                            raise release.subprocess.CalledProcessError(1, args)
                        name = args[args.index("--pattern") + 1]
                        (Path(args[args.index("--dir") + 1]) / name).write_bytes(remote[name])
                    with patch.dict(os.environ, {"TAG": "v1.0.0", "GITHUB_REPOSITORY": "beeemT/blackboard"}), patch("subprocess.run", side_effect=download) as run:
                        exec(code, {})
                        return run.call_count
                self.assertEqual(run_case([], {}), 0)  # New release.
                archive = "blackboard_1.0.0_linux_arm64.tar.gz"
                self.assertEqual(run_case([archive], {archive: b"archive"}), 1)  # Partial upload.
                self.assertEqual(run_case([archive, "checksums.txt"], {archive: b"archive", "checksums.txt": b"checksums"}), 2)
                with self.assertRaises(SystemExit):
                    run_case([archive], {archive: b"different"})
                with self.assertRaises(release.subprocess.CalledProcessError):
                    run_case([archive], {}, fails=True)
            finally:
                os.chdir(cwd)

    def test_tap_version_guard(self):
        import contextlib
        import io
        workflow = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"
        text = workflow.read_text()
        block = text.split("            UPDATE=$(python3 - <<'PYTHON'\n", 1)[1].split("\n          PYTHON", 1)[0]
        code = "\n".join(line[10:] for line in block.splitlines())
        with tempfile.TemporaryDirectory() as tmp:
            current, generated = Path(tmp) / "current", Path(tmp) / "generated"
            code = code.replace("/tmp/tap/Formula/blackboard.rb", str(current)).replace("dist/blackboard.rb", str(generated))
            for old, new, want in [("1.2.3", "1.2.2", "skip"), ("1.2.3", "1.2.3", "update"),
                                   ("1.2.3", "1.10.0", "update"), ("2.0.0", "1.99.0", "skip")]:
                with self.subTest(old=old, new=new):
                    current.write_text(f'  version "{old}"\n')
                    generated.write_text(f'  version "{new}"\n')
                    out = io.StringIO()
                    with contextlib.redirect_stdout(out):
                        exec(code, {})
                    self.assertTrue(out.getvalue().strip().endswith(want))
            current.write_text("unrecognized")
            with self.assertRaises(SystemExit):
                exec(code, {})

    def test_previous_artifacts_not_overwritten(self):
        cwd = os.getcwd()
        with tempfile.TemporaryDirectory() as tmp:
            try:
                os.chdir(tmp)
                Path("dist").mkdir()
                Path("dist/previous").write_text("keep")
                with self.assertRaises(ValueError):
                    release.build("v1.2.3")
                self.assertEqual(Path("dist/previous").read_text(), "keep")
            finally:
                os.chdir(cwd)


if __name__ == "__main__":
    unittest.main()
