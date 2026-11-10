"""Launcher safety checks; only temporary fixtures and stub Docker are used."""
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import unittest


@unittest.skipUnless(shutil.which("bash") and shutil.which("flock"), "Linux shell tools required")
class LauncherSafety(unittest.TestCase):
    def fixture(self, directory):
        root = Path(directory)
        shutil.copy2(Path(__file__).resolve().parents[1] / "start.sh", root / "start.sh")
        migrations = root / "backend/db/migrations"
        migrations.mkdir(parents=True)
        (migrations / "00040_fixture.sql").write_text("-- fixture only\n")
        commands = root / "commands"
        commands.mkdir()
        (commands / "docker").write_text("""#!/usr/bin/env bash
case "$1" in
 info) printf '%s' "${GENERATION_FAST_STATE_V1:-}" > "$FIXTURE/literal-value" ;;
 inspect|start) exit 0 ;;
 port) echo '127.0.0.1:5432' ;;
 exec)
   case "$3" in
     pg_isready) exit 0 ;;
     pg_dump) touch "$FIXTURE/backup-attempted"; exit 17 ;;
     psql)
       case "${!#}" in
         *to_regclass*) echo 'goose_db_version' ;;
         *version_id*) echo '35' ;;
         *) echo '1' ;;
       esac ;;
     *) exit 90 ;;
   esac ;;
 *) exit 91 ;;
esac
""")
        (commands / "make").write_text('#!/usr/bin/env bash\ntouch "$FIXTURE/migration-attempted"\n')
        for name in ("go", "node", "npm", "curl", "setsid"):
            (commands / name).write_text("#!/usr/bin/env bash\nexit 0\n")
        for path in commands.iterdir():
            path.chmod(0o700)
        sockets = [socket.socket(), socket.socket()]
        for handle in sockets:
            handle.bind(("127.0.0.1", 0))
        ports = [str(handle.getsockname()[1]) for handle in sockets]
        for handle in sockets:
            handle.close()
        env = os.environ.copy()
        env.pop("GENERATION_FAST_STATE_V1", None)
        env.update(PATH=str(commands) + os.pathsep + env["PATH"], FIXTURE=str(root),
                   HOST_DATABASE_URL="postgres://story:story@127.0.0.1:5432/fixture",
                   STORY_BACKEND_PORT=ports[0], STORY_FRONTEND_PORT=ports[1])
        return root, env

    def test_backup_failure_blocks_migrations(self):
        with tempfile.TemporaryDirectory() as directory:
            root, env = self.fixture(directory)
            result = subprocess.run(["bash", str(root / "start.sh"), "--no-browser"],
                                    env=env, capture_output=True, text=True, timeout=15)
            self.assertNotEqual(result.returncode, 0)
            self.assertTrue((root / "backup-attempted").exists())
            self.assertFalse((root / "migration-attempted").exists())
            self.assertIn("Резервное копирование не удалось", result.stderr)

    def test_dotenv_is_literal_even_without_final_newline(self):
        with tempfile.TemporaryDirectory() as directory:
            root, env = self.fixture(directory)
            literal = f"$(touch {root / 'executed'})"
            (root / ".env").write_text("GENERATION_FAST_STATE_V1=" + literal)
            result = subprocess.run(["bash", str(root / "start.sh"), "--no-browser"],
                                    env=env, capture_output=True, text=True, timeout=15)
            self.assertNotEqual(result.returncode, 0)  # intentional dump failure
            self.assertTrue((root / "backup-attempted").exists())
            self.assertEqual((root / "literal-value").read_text(), literal)
            self.assertFalse((root / "executed").exists())

    def test_busy_port_preserves_existing_listener(self):
        with tempfile.TemporaryDirectory() as directory:
            root, env = self.fixture(directory)
            with socket.socket() as listener:
                listener.bind(("127.0.0.1", int(env["STORY_BACKEND_PORT"])))
                listener.listen(1)
                result = subprocess.run(["bash", str(root / "start.sh"), "--no-browser"],
                                        env=env, capture_output=True, text=True, timeout=15)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("занят", result.stderr)
                self.assertFalse((root / "backup-attempted").exists())
                self.assertEqual(listener.getsockname()[1], int(env["STORY_BACKEND_PORT"]))


if __name__ == "__main__":
    unittest.main()
