"""Stop only Codex daemon processes belonging to an isolated fixture."""
import json
import os
import pathlib
import signal
import subprocess


def stop_codex(root, env):
    root = pathlib.Path(root)
    state = root / "codex/app-server-daemon"
    updater = state / "daemon-updater.pid"
    if updater.exists():
        pid = json.loads(updater.read_text()).get("pid")
        if isinstance(pid, int) and pid > 1:
            result = subprocess.run(["ps", "-p", str(pid), "-o", "command="], capture_output=True, text=True)
            command = result.stdout.strip()
            expected = str(root / "codex/packages/app-server-daemon/releases") + "/"
            if result.returncode == 0:
                if not command.startswith(expected) or " app-server daemon pid-update-loop" not in command:
                    raise RuntimeError("fixture updater PID does not identify its own Codex process")
                os.kill(pid, signal.SIGTERM)
    if (state / "daemon.pid").exists():
        subprocess.run([root / "bin/codex", "app-server", "daemon", "stop"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30, check=True)
