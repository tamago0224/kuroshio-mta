"""Run with: python3 -m unittest discover -s scripts/chaos -p 'test_*.py'."""

from pathlib import Path
import subprocess
import tempfile
import unittest


REPO = Path(__file__).resolve().parents[2]


class DRDrillTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="kuroshio-dr-test-")
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.queue = root / "queue ' quoted"
        self.backup = root / "backup ' quoted"
        self.queue.mkdir()
        (self.queue / "marker").write_text("preserve me")

    def run_drill(self, *flags, backup=None):
        return subprocess.run(
            ["scripts/chaos/run_dr_drill.sh", str(self.queue),
             str(backup or self.backup), *flags],
            cwd=REPO, capture_output=True, text=True,
        )

    def test_dry_run_does_not_write(self):
        result = self.run_drill()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("[dry-run]", result.stdout)
        self.assertFalse(self.backup.exists())
        self.assertEqual((self.queue / "marker").read_text(), "preserve me")

    def test_apply_requires_stopped_acknowledgement(self):
        result = self.run_drill("--apply")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--stopped", result.stderr)
        self.assertFalse(self.backup.exists())
        self.assertEqual((self.queue / "marker").read_text(), "preserve me")

    def test_backup_inside_queue_is_rejected(self):
        for backup in (self.queue, self.queue / "backups"):
            with self.subTest(backup=backup):
                result = self.run_drill("--apply", "--stopped", backup=backup)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("outside the queue", result.stderr)
                self.assertEqual((self.queue / "marker").read_text(), "preserve me")
        self.assertFalse((self.queue / "backups").exists())

    def test_offline_backup_restore_with_quoted_paths(self):
        result = self.run_drill("--apply", "--stopped")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.queue / "marker").read_text(), "preserve me")
        self.assertEqual(len(list(self.backup.glob("*.tar.gz"))), 1)
        self.assertIn("drill_elapsed_seconds=", result.stdout)
        self.assertIn("Manual post-checks:", result.stdout)


if __name__ == "__main__":
    unittest.main()
