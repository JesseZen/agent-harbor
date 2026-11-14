#!/usr/bin/env python3
"""Isolated pinned-native-Codex delete and writer-lock capability experiment."""
import sys
sys.dont_write_bytecode = True
import argparse
import fcntl
import http.server
import json
import os
import pathlib
import shutil
import signal
import subprocess
import tempfile
import threading
import time
from e2e_local import MockResponses
from fixture_process import stop_codex


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--codex', required=True)
    args = parser.parse_args()
    source = pathlib.Path(args.codex).resolve()
    with source.open('rb') as f:
        if f.read(4) not in (b'\x7fELF', b'\xcf\xfa\xed\xfe', b'\xfe\xed\xfa\xcf', b'\xca\xfe\xba\xbe'):
            raise RuntimeError('a native executable is required')
    root = pathlib.Path(tempfile.mkdtemp(prefix='harbor02-capabilities-')).resolve()
    env = {'PATH': '/usr/bin:/bin', 'TERM': 'xterm-256color', 'LANG': 'C.UTF-8'}
    for key, leaf in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'),
                      ('XDG_DATA_HOME', 'data'), ('XDG_STATE_HOME', 'state'),
                      ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime'),
                      ('TMPDIR', 'tmp'), ('CODEX_HOME', 'codex'),
                      ('CODEX_SQLITE_HOME', 'sqlite')]:
        directory = root / leaf
        directory.mkdir(mode=0o700)
        env[key] = str(directory)
    (root / 'bin').mkdir()
    (root / 'project').mkdir()
    native = root / 'bin/codex'
    shutil.copy2(source, native)
    entered, release = threading.Event(), threading.Event()

    class Relay(MockResponses):
        def do_POST(self):
            entered.set()
            if not release.wait(30):
                self.send_error(504)
                return
            super().do_POST()

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Relay)
    server.observed = []
    threading.Thread(target=server.serve_forever, daemon=True).start()
    config = ('check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\n'
              'model_provider = "fixture"\nmodel = "test-model"\n'
              '[model_providers.fixture]\nname = "fixture"\n'
              f'base_url = "http://127.0.0.1:{server.server_port}/v1"\n'
              'wire_api = "responses"\nrequires_openai_auth = false\nsupports_websockets = false\n')
    (root / 'codex/config.toml').write_text(config)
    proc = None
    result = {'root': str(root)}
    try:
        version = subprocess.check_output([native, '--version'], env=env, text=True).strip()
        if version != 'codex-cli 0.167.0':
            raise RuntimeError('expected Codex 0.167.0')
        result['version'] = version
        with (root / 'exec.log').open('w') as log:
            proc = subprocess.Popen([native, 'exec', '--skip-git-repo-check', '--json',
                                     'Reply with HELLO_FROM_ISOLATED_MOCK'],
                                    cwd=root / 'project', env=env, stdout=log, stderr=log,
                                    start_new_session=True)
            if not entered.wait(30):
                raise RuntimeError('Codex did not contact isolated relay; see exec.log')
            locks = list((root / 'codex/thread-writer-locks').glob('*.lock'))
            result['writer_locks_seen'] = len(locks)
            result['writer_locks_held'] = []
            for lock in locks:
                with lock.open('rb') as stream:
                    try:
                        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        held = False
                        fcntl.flock(stream, fcntl.LOCK_UN)
                    except BlockingIOError:
                        held = True
                    result['writer_locks_held'].append(held)
            release.set()
            if proc.wait(timeout=40) != 0:
                raise RuntimeError('Codex exec failed; see exec.log')
        ids = [event['thread_id'] for line in (root / 'exec.log').read_text().splitlines()
               if line.startswith('{') for event in [json.loads(line)]
               if event.get('type') == 'thread.started']
        if len(ids) != 1:
            raise RuntimeError('expected one conversation')
        conversation = ids[0]
        rollouts = list((root / 'codex/sessions').rglob('*' + conversation + '*.jsonl'))
        if len(rollouts) != 1:
            raise RuntimeError('expected one persisted rollout before delete')
        deletion = subprocess.run([native, 'delete', '--force', conversation], env=env,
                                  cwd=root / 'project', capture_output=True, timeout=30)
        result['delete_exit'] = deletion.returncode
        result['rollout_removed'] = not rollouts[0].exists()
        # An exec lock experiment is insufficient evidence for interactive TUI locks.
        result['interactive_writer_lock_verified'] = False
        result['interactive_delete_verified'] = False
    finally:
        release.set()
        if proc is not None and proc.poll() is None:
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.wait()
        stop_codex(root, env)
        server.shutdown()
        server.server_close()
        print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
