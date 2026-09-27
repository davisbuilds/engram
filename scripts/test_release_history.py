"""Exercise syntax controls and actual Git history traversal."""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from check_release_history import check_range, valid_subject


class ReleaseHistoryTest(unittest.TestCase):
    def test_subjects(self):
        for subject in ["feat: add version", "fix(cli)!: change output", "chore(main): release 0.1.0", "revert: remove change"]:
            with self.subTest(subject=subject):
                self.assertTrue(valid_subject(subject))
        for subject in ["Add version", "fix:", "fix: ", "feat(): no scope", "fix: ok\nmalicious", "fixup! fix: change"]:
            with self.subTest(subject=subject):
                self.assertFalse(valid_subject(subject))

    def test_retained_history_excludes_merges(self):
        with tempfile.TemporaryDirectory() as directory:
            def git(*args):
                return subprocess.check_output(["git", "-C", directory, *args], text=True).strip()

            git("init", "-q", "--initial-branch=base")
            git("config", "user.email", "test@example.com")
            git("config", "user.name", "Test")
            git("config", "commit.gpgsign", "false")
            git("config", "tag.gpgsign", "false")
            git("commit", "--allow-empty", "-qm", "Historic nonconventional subject")
            base = git("rev-parse", "HEAD")
            git("checkout", "-qb", "feature")
            git("commit", "--allow-empty", "-qm", "feat: add version")
            valid = git("rev-parse", "HEAD")
            git("commit", "--allow-empty", "-qm", "Invalid retained change")
            git("checkout", "-qb", "main", base)
            git("merge", "--no-ff", "-qm", "Merge feature", "feature")
            previous = Path.cwd()
            try:
                os.chdir(directory)
                Path("release-please-config.json").write_text(json.dumps({"bootstrap-sha": base}))
                manifest = Path(".release-please-manifest.json")
                manifest.write_text(json.dumps({".": "0.0.0"}))
                self.assertEqual(check_range(base, git("rev-parse", "HEAD")), ["Invalid retained change"])
                self.assertEqual(check_range(base, valid), [])
                self.assertEqual(check_range(base, base), [])
                for invalid in ["", "0" * 40, "HEAD", "g" * 40]:
                    with self.subTest(revision=invalid), self.assertRaises(ValueError):
                        check_range(invalid, valid)
                    with self.subTest(head=invalid), self.assertRaises(ValueError):
                        check_range(base, invalid)
                with self.assertRaises(subprocess.CalledProcessError):
                    check_range("f" * 40, valid)
                script = previous / "scripts" / "check_release_history.py"
                env = dict(os.environ)
                env.pop("PR_TITLE", None)
                def run(*args):
                    return subprocess.run([sys.executable, str(script), *args], env=env, capture_output=True, text=True)
                self.assertEqual(run(base, valid).returncode, 0)
                self.assertNotEqual(run(base, git("rev-parse", "HEAD")).returncode, 0)
                failed_head = git("rev-parse", "HEAD")
                git("commit", "--allow-empty", "-qm", "fix: subsequent valid push")
                good_head = git("rev-parse", "HEAD")
                self.assertEqual(check_range(failed_head, good_head, push=True), [])
                self.assertNotEqual(run(failed_head, good_head).returncode, 0)
                git("tag", "-a", "v0.1.0", "-m", "Actual release boundary", failed_head)
                manifest.write_text(json.dumps({".": "0.1.0"}))
                self.assertEqual(run(failed_head, good_head).returncode, 0)
                # Tagged heads may have no unreleased commits; the push itself
                # still needs a nonempty forward range.
                git("tag", "v0.2.0", good_head)
                manifest.write_text(json.dumps({".": "0.2.0"}))
                self.assertEqual(run(failed_head, good_head).returncode, 0)
                # Proposed manifest versions without a tag use bootstrap.
                manifest.write_text(json.dumps({".": "0.3.0"}))
                self.assertNotEqual(run(failed_head, good_head).returncode, 0)
                for malformed in ["invalid", "01.2.3", "1.02.3", "1.2.03", "1.2.3-01", "1.2.3-alpha..beta", "1.2.3+build..id", "1.2.3-", "1.2.3+", "1.2.3\n", None]:
                    with self.subTest(version=malformed):
                        manifest.write_text(json.dumps({".": malformed}))
                        self.assertNotEqual(run(base, valid).returncode, 0)
                for semver in ["0.0.0", "1.2.3", "1.2.3-0", "1.2.3-alpha.1", "1.2.3-0alpha", "1.2.3+01.build", "1.2.3-alpha-1+build.01"]:
                    with self.subTest(version=semver):
                        manifest.write_text(json.dumps({".": semver}))
                        self.assertEqual(run(base, valid).returncode, 0)
                manifest.unlink()
                self.assertNotEqual(run(base, valid).returncode, 0)
                manifest.write_text(json.dumps({".": "0.0.0"}))
                self.assertNotEqual(run("0" * 40, valid).returncode, 0)
                self.assertNotEqual(run(base, base).returncode, 0)
                self.assertNotEqual(run(valid, base).returncode, 0)
                git("checkout", "-qb", "diverged", base)
                git("commit", "--allow-empty", "-qm", "fix: other branch")
                diverged = git("rev-parse", "HEAD")
                self.assertNotEqual(run(valid, diverged).returncode, 0)
                Path("release-please-config.json").write_text(json.dumps({"bootstrap-sha": diverged}))
                self.assertNotEqual(run(base, valid).returncode, 0)
            finally:
                os.chdir(previous)


if __name__ == "__main__":
    unittest.main()
