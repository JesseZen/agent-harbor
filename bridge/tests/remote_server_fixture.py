#!/usr/bin/env python3
"""Synthetic server half of remote_workspace.py. Uses only its owned root."""
import http.server
import json
import os
import pathlib
import shlex
import subprocess
import sys
import threading
import time

from e2e_local import MockResponses

root = pathlib.Path(__file__).resolve().parent


def environment():
    return json.loads((root / 'environment.json').read_text())


def run(*args, check=True):
    result = subprocess.run(list(map(str, args)), env=environment(), cwd=root / 'project',
                            capture_output=True, text=True, timeout=40)
    if check and result.returncode:
        (root / 'last-command-error.txt').write_text(result.stdout + result.stderr)
        raise RuntimeError(result.stdout + result.stderr)
    return result


def main():
    mode = sys.argv[1]
    if mode == 'provider':
        class Provider(MockResponses):
            def do_POST(self):
                (root / 'request-started').write_text('synthetic remote request')
                time.sleep(12)
                super().do_POST()
                (root / 'request-completed').write_text('synthetic response completed')

        server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Provider)
        server.observed = []
        (root / 'provider.json').write_text(json.dumps({'port': server.server_port, 'pid': os.getpid()}))
        server.serve_forever()
    elif mode == 'setup':
        env = {'PATH': str(root / 'bin') + ':/usr/local/bin:/usr/bin:/bin',
               'TERM': 'xterm-256color', 'SHELL': '/bin/sh', 'LANG': 'C.UTF-8', 'NO_COLOR': '1'}
        for key, leaf in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'),
                          ('XDG_STATE_HOME', 'state'), ('XDG_DATA_HOME', 'data'),
                          ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime'),
                          ('TMPDIR', 'tmp'), ('CODEX_HOME', 'codex'), ('CODEX_SQLITE_HOME', 'sqlite')]:
            (root / leaf).mkdir(mode=0o700)
            env[key] = str(root / leaf)
        (root / 'project').mkdir(mode=0o700)
        (root / 'harbor').mkdir(mode=0o700)
        env['AGENT_HARBOR_BRIDGE_HOME'] = str(root / 'harbor')
        (root / 'environment.json').write_text(json.dumps(env))
        # This initializes the owned state marker even before Herdr is reachable.
        run(root / 'bin/agent-harbor', 'sessions', '--json', check=False)
        # Completed fixture onboarding prevents probing any user discovery service.
        (root / 'harbor/onboarding.done').write_text('v1\n')
        with (root / 'provider.log').open('w') as log:
            subprocess.Popen([sys.executable, __file__, 'provider'], env=env,
                             stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        deadline = time.monotonic() + 10
        while not (root / 'provider.json').exists():
            if time.monotonic() > deadline:
                raise RuntimeError('provider failed to start')
            time.sleep(.1)
        port = json.loads((root / 'provider.json').read_text())['port']
        (root / 'codex/config.toml').write_text(
            'check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\n'
            'model = "test-model"\nmodel_provider = "remote-fixture"\n'
            '[features]\nhooks = true\n'
            '[model_providers.remote-fixture]\nname = "Synthetic remote"\n'
            f'base_url = "http://127.0.0.1:{port}/v1"\n'
            'requires_openai_auth = false\nwire_api = "responses"\n'
            '[projects.' + json.dumps(str(root / 'project')) + ']\ntrust_level = "trusted"\n')
        run(root / 'bin/herdr', 'integration', 'install', 'codex')
        launcher = root / 'remote-harbor'
        launcher.write_text('#!/bin/sh\n' + ''.join('export ' + k + '=' + shlex.quote(v) + '\n'
                            for k, v in env.items()) + 'cd ' + shlex.quote(str(root / 'project')) +
                            '\nexec ' + shlex.quote(str(root / 'bin/agent-harbor')) + ' "$@"\n')
        launcher.chmod(0o700)
        print(run(root / 'bin/agent-harbor', 'remote', 'prepare', '--native').stdout, end='')
    elif mode == 'snapshot':
        # Reading the application view reconciles official Herdr observations.
        run(root / 'bin/agent-harbor', 'sessions', '--json')
        records = root / 'harbor/sessions.json'
        rows = json.loads(records.read_text()) if records.exists() else []
        processes = {}
        for row in rows:
            pid = row.get('process_group', 0)
            try:
                # /proc start ticks distinguish a live process from a stale record or reused PID.
                fields = pathlib.Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
                if fields[0] != 'Z':
                    processes[str(pid)] = {'group': int(fields[2]), 'start_ticks': fields[19]}
            except FileNotFoundError:
                pass
        print(json.dumps({'records': rows, 'processes': processes,
                          'request_started': (root / 'request-started').exists(),
                          'request_completed': (root / 'request-completed').exists()}))
    elif mode == 'replace-identity':
        path = root / 'harbor/remote-context.json'
        content = path.read_bytes()
        (root / 'original-context').write_bytes(content)
        data = json.loads(content)
        data['id'] = '0' * 32
        path.write_text(json.dumps(data))
    elif mode == 'restore-identity':
        (root / 'harbor/remote-context.json').write_bytes((root / 'original-context').read_bytes())
    elif mode == 'stop':
        if (root / 'environment.json').exists():
            run(root / 'bin/agent-harbor', 'stop', check=False)
        info = root / 'provider.json'
        if info.exists():
            provider = json.loads(info.read_text())
            # This PID was created by this fixture, with this exact script.
            cmdline = pathlib.Path(f"/proc/{provider['pid']}/cmdline")
            if cmdline.exists() and str(root).encode() in cmdline.read_bytes():
                os.kill(provider['pid'], 15)
    else:
        raise ValueError(mode)


if __name__ == '__main__':
    main()
