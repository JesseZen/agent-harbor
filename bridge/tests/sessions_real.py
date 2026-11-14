#!/usr/bin/env python3
"""Real interactive native Codex notify capture and history continuity."""
import sys
sys.dont_write_bytecode = True
import argparse
import fcntl
import http.server
import json
import os
import pathlib
import shutil
import socket as network_socket
import subprocess
import tempfile
import threading
import time
from e2e_local import MockResponses, FIRST, SECOND, ANSWER
from fixture_process import stop_codex


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--codex', required=True)
    parser.add_argument('--binary', required=True)
    parser.add_argument('--magpie')
    args = parser.parse_args()
    root = pathlib.Path(tempfile.mkdtemp(prefix='harbor02-interactive-')).resolve()
    env = {'PATH': '/usr/bin:/bin', 'TERM': 'xterm-256color', 'LANG': 'C.UTF-8', 'SHELL': '/bin/sh'}
    for key, leaf in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'),
                      ('XDG_STATE_HOME', 'state'), ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime'),
                      ('TMPDIR', 'tmp'), ('CODEX_HOME', 'codex'), ('CODEX_SQLITE_HOME', 'sqlite')]:
        directory = root / leaf
        directory.mkdir(mode=0o700)
        env[key] = str(directory)
    (root / 'bin').mkdir()
    (root / 'project').mkdir()
    source = pathlib.Path(args.codex).resolve()
    if source.read_bytes()[:4] not in (b'\x7fELF', b'\xcf\xfa\xed\xfe'):
        raise RuntimeError('native Codex required')
    shutil.copy2(source, root / 'bin/codex')
    shutil.copy2(args.binary, root / 'bin/agent-harbor')
    env['PATH'] = str(root / 'bin') + ':' + env['PATH']
    env['AGENT_HARBOR_BRIDGE_HOME'] = str(root / 'harbor')
    binary = root / 'bin/agent-harbor'
    tmux = shutil.which('tmux')
    if not tmux:
        raise RuntimeError('tmux required')
    socket = str(root / 'tmux.sock')
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), MockResponses)
    server.observed = []
    extra_servers = []
    managed_started = False
    threading.Thread(target=server.serve_forever, daemon=True).start()
    chain = root / 'bin/original-notify'
    chain.write_text('#!/bin/sh\nprintf notified >> "$HOME/original-notify-called"\n')
    chain.chmod(0o700)
    config = ('check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\n'
              'model_provider = "fixture"\nmodel = "test-model"\n'
              f'notify = [{json.dumps(str(chain))}]\n'
              f'[projects.{json.dumps(str(root / "project"))}]\ntrust_level = "trusted"\n'
              '[model_providers.fixture]\nname = "fixture"\n'
              f'base_url = "http://127.0.0.1:{server.server_port}/v1"\n'
              'wire_api = "responses"\nrequires_openai_auth = false\nsupports_websockets = false\n'
              '[tui]\nscreen_reader_detection_done = true\n[tui.model_availability_nux]\n"gpt-6.1-sol" = 1\n')
    (root / 'codex/config.toml').write_text(config)

    def run(command, **kw):
        return subprocess.run([str(c) for c in command], env=env, text=True,
                              capture_output=True, check=True, timeout=15, **kw)

    def tm(*args):
        return run([tmux, '-S', socket, *args]).stdout

    def screen():
        return tm('capture-pane', '-p', '-t', 'test')

    def wait(predicate, description):
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(.15)
        (root / 'failure-screen.txt').write_text(screen())
        raise RuntimeError('timed out: ' + description)

    def records():
        return json.loads(run([binary, 'sessions', '--json']).stdout)

    try:
        assert run([root / 'bin/codex', '--version']).stdout.strip() == 'codex-cli 0.167.0', 'pinned version mismatch'
        tm('-f', '/dev/null', 'new-session', '-d', '-s', 'test', '-x', '130', '-y', '38',
           binary, 'new', '--dir', root / 'project', '--name', 'native-test')
        tm('set-option', '-g', 'remain-on-exit', 'on')
        wait(lambda: 'Ask Codex' in screen(), 'native Codex prompt')
        tm('send-keys', '-t', 'test', '-l', FIRST)
        time.sleep(.3)
        tm('send-keys', '-t', 'test', 'Enter')
        wait(lambda: records()[0].get('conversation_id_source') == 'captured', 'captured main ID')
        original = records()[0]
        assert not original.get('candidate_ids'), 'side thread became a candidate'
        wait(lambda: (root / 'home/original-notify-called').exists(), 'chained notify')
        conversation = original['codex_session_id']
        writer_lock = root / 'codex/thread-writer-locks' / (conversation + '.lock')
        def lock_held():
            with writer_lock.open('rb') as stream:
                try:
                    fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    fcntl.flock(stream, fcntl.LOCK_UN)
                    return False
                except BlockingIOError:
                    return True
        assert lock_held(), 'interactive writer lock was not held'

        # This test does not enable writer locks or delete-history capability.
        stopped = subprocess.run([binary, 'stop-session', original['id']], env=env,
                                 input='yes\n', text=True, capture_output=True, timeout=15)
        if stopped.returncode:
            raise RuntimeError('stop-session refused: ' + stopped.stdout + stopped.stderr)
        wait(lambda: not records()[0].get('process_group'), 'recorded exit')
        assert not lock_held(), 'interactive writer lock remained held after exit'
        print('VERIFIED: main TUI UUID flock held while open, released after stop', flush=True)
        tm('new-session', '-d', '-s', 'resume', '-x', '130', '-y', '38', binary, 'resume', original['id'])
        # Use a separate target for confirmation and resumed interaction.
        wait_resume = time.monotonic() + 30
        while 'Type yes to resume' not in tm('capture-pane', '-p', '-t', 'resume'):
            if time.monotonic() > wait_resume:
                raise RuntimeError('resume confirmation absent')
            time.sleep(.1)
        tm('send-keys', '-t', 'resume', 'yes', 'Enter')
        deadline = time.monotonic() + 40
        while 'Ask Codex' not in tm('capture-pane', '-p', '-t', 'resume'):
            if time.monotonic() > deadline:
                raise RuntimeError('resumed prompt absent')
            time.sleep(.1)
        tm('send-keys', '-t', 'resume', '-l', SECOND)
        time.sleep(.3)
        tm('send-keys', '-t', 'resume', 'Enter')
        wait(lambda: any(FIRST in marks and SECOND in marks for _, marks in server.observed), 'history continuity')
        assert records()[0]['codex_session_id'] == conversation, 'conversation ID changed'
        assert (root / 'codex/config.toml').read_text().split('[tui]')[0] == config.split('[tui]')[0], 'notify or provider config changed'
        if args.magpie:
            shutil.copy2(args.magpie, root / 'bin/magpie')
            env['AGENT_HARBOR_MAGPIE_BIN'] = str(root / 'bin/magpie')
            env['MAGPIE_NO_ACCOUNTS'] = '1'
            def free_port():
                with network_socket.socket() as sock:
                    sock.bind(('127.0.0.1', 0))
                    return sock.getsockname()[1]
            run([binary, 'magpie', 'start', '--port', str(free_port()), '--web-port', str(free_port())])
            managed_started = True
            managed = root / 'harbor/magpie/bin/magpie'
            # Magpie speaks chat completions to each separate mock relay.
            from e2e_package import MockResponses as ChatRelay
            for label in ('a', 'b'):
                relay = http.server.ThreadingHTTPServer(('127.0.0.1', 0), ChatRelay)
                relay.observed, relay.events = [], []
                extra_servers.append(relay)
                threading.Thread(target=relay.serve_forever, daemon=True).start()
                run([managed, 'provider', 'add', 'relay-' + label,
                     f'url=http://127.0.0.1:{relay.server_port}/v1', 'key=fixture-only', 'models=test-model'])
                deadline = time.monotonic() + 20
                while 'relay-' + label + '/test-model' not in run([binary, 'models', '--instance', 'managed', '--json']).stdout:
                    if time.monotonic() > deadline: raise RuntimeError('new provider did not enter catalog')
                    time.sleep(.2)
                tm('new-session', '-d', '-s', label, '-x', '130', '-y', '38', binary, 'new',
                   '--dir', root / 'project', '--connection', 'magpie', '--instance', 'managed',
                   '--model', 'relay-' + label + '/test-model', '--name', 'magpie-' + label)
                deadline = time.monotonic() + 30
                while 'Type yes to start' not in tm('capture-pane', '-p', '-t', label):
                    if time.monotonic() > deadline: raise RuntimeError('Magpie confirmation absent')
                    time.sleep(.1)
                tm('send-keys', '-t', label, 'yes', 'Enter')
                deadline = time.monotonic() + 40
                while 'Ask Codex' not in tm('capture-pane', '-p', '-t', label):
                    if time.monotonic() > deadline: raise RuntimeError('Magpie Codex prompt absent')
                    time.sleep(.1)
                marker = FIRST if label == 'a' else 'AH_RELAY_B'
                tm('send-keys', '-t', label, '-l', marker)
                time.sleep(.3)
                tm('send-keys', '-t', label, 'Enter')
                deadline = time.monotonic() + 40
                while not any(marker in marks for _, marks in relay.observed):
                    if time.monotonic() > deadline: raise RuntimeError('selected relay not reached')
                    time.sleep(.1)
            assert not any('AH_RELAY_B' in marks for _, marks in extra_servers[0].observed)
            assert not any(FIRST in marks for _, marks in extra_servers[1].observed)
            # All three live sessions share neither connection intent nor conversation ID.
            wait(lambda: len(records()) == 3 and all(r.get('codex_session_id') for r in records()), 'three captured sessions')
            saved = {r['id']: (r['codex_session_id'], r.get('magpie_model')) for r in records()}
            for r in records():
                if r['mode'] == 'magpie':
                    observed = json.loads(run([binary, 'route', r['id']]).stdout)
                    assert observed['observed'], 'real Magpie route not observed'
                    if observed.get('main'):
                        assert observed['main']['model'] == r['magpie_model'], 'main route differs from selected mock provider'

            for r in records():
                stopped = subprocess.run([binary, 'stop-session', r['id']], env=env, input='yes\n',
                                         text=True, capture_output=True, timeout=15)
                if stopped.returncode: raise RuntimeError('stop refused: ' + stopped.stdout + stopped.stderr)
            # Seed a recorded historical identity, then require an explicit,
            # auditable override against the still-real, verified gateway.
            state_path = root / 'harbor/sessions.json'
            historical = json.loads(state_path.read_text())
            overridden_id = next(r['id'] for r in historical if r['mode'] == 'magpie')
            for r in historical:
                if r['id'] == overridden_id:
                    r['instance_snapshot']['config_dir'] = str(root / 'previous-configuration')
            temporary = state_path.with_suffix('.test-new')
            temporary.write_text(json.dumps(historical))
            temporary.chmod(0o600)
            temporary.replace(state_path)
            refused = subprocess.run([binary, 'resume', overridden_id], env=env, input='yes\n',
                                     capture_output=True, text=True, timeout=15)
            assert refused.returncode and 'instance changed' in refused.stderr, 'changed identity did not refuse recovery'
            run([binary, 'magpie', 'stop'])
            magpie_record = next(r for r in records() if r['mode'] == 'magpie')
            refused = subprocess.run([binary, 'resume', magpie_record['id']], env=env,
                                     input='yes\n', capture_output=True, text=True, timeout=15)
            assert refused.returncode and 'gateway offline' in refused.stderr, 'offline gateway did not refuse recovery'
            run([binary, 'magpie', 'start'])
            for index, r in enumerate(records()):
                target = 'restart-' + str(index)
                resume_args = ['resume']
                if r['id'] == overridden_id:
                    resume_args += ['--override-instance', 'fixture historical configuration reviewed']
                tm('new-session', '-d', '-s', target, '-x', '130', '-y', '38', binary, *resume_args, r['id'])
                deadline = time.monotonic() + 30
                while 'Type yes to resume' not in tm('capture-pane', '-p', '-t', target):
                    if time.monotonic() > deadline: raise RuntimeError('restart confirmation absent')
                    time.sleep(.1)
                tm('send-keys', '-t', target, 'yes', 'Enter')
                deadline = time.monotonic() + 40
                while 'Ask Codex' not in tm('capture-pane', '-p', '-t', target):
                    if time.monotonic() > deadline: raise RuntimeError('restart prompt absent')
                    time.sleep(.1)
            assert saved == {r['id']: (r['codex_session_id'], r.get('magpie_model')) for r in records()}
            overridden = next(r for r in records() if r['id'] == overridden_id)
            assert overridden['overrides'][-1]['reason'] == 'fixture historical configuration reviewed'
            print('PASS: two real Magpie destinations plus native, distinct captures, gateway restart, exact resumes')
        print('PASS: interactive capture, side-thread filtering, chained notify, exact resume, history continuity')
    except Exception as exc:
        print(type(exc).__name__ + ': ' + str(exc), flush=True)
        raise
    finally:
        subprocess.run([tmux, '-S', socket, 'kill-server'], env=env, capture_output=True)
        stop_codex(root, env)
        if managed_started:
            subprocess.run([binary, 'magpie', 'stop'], env=env, capture_output=True, timeout=20)
        for relay in extra_servers:
            relay.shutdown()
            relay.server_close()
        server.shutdown()
        server.server_close()
        print('diagnostics:', root)


if __name__ == '__main__':
    main()
