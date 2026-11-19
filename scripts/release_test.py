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
