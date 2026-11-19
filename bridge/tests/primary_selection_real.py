#!/usr/bin/env python3
"""Exact /resume recovery with isolated real Codex and Herdr, plus a stock-client control."""

import argparse
import http.server
import json
import pathlib
import re
import shlex
import shutil
import socket
import subprocess
import tempfile
import threading
import time

from e2e_local import ANSWER, FIRST, SECOND, MockResponses
from fixture_process import stop_codex

THIRD = 'AH_LOCAL_THIRD'
DELEGATE = 'AH_SELECTION_DELEGATE'
WORKER = 'AH_SELECTION_WORKER'


class SubagentResponses(MockResponses):
    def respond(self, body):
        if self.server.spawn_sent or not any(item.get('role') == 'user' and DELEGATE in json.dumps(item)
                                            for item in body.get('input', []) if isinstance(item, dict)):
            return super().respond(body)
        self.server.spawn_sent = True
        self.server.observed.append((self.path, [DELEGATE]))
        events = (
            {'type': 'response.created', 'response': {'id': 'resp_spawn'}},
            {'type': 'response.output_item.done', 'item': {
                'type': 'function_call', 'call_id': 'call_spawn', 'namespace': 'collaboration',
                'name': 'spawn_agent', 'arguments': json.dumps({'message': WORKER, 'task_name': 'harbor_fixture'})}},
            {'type': 'response.completed', 'response': {'id': 'resp_spawn'}},
        )
        self.reply(''.join(f"event: {event['type']}\ndata: {json.dumps(event)}\n\n" for event in events).encode(), 'text/event-stream')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'herdr', 'codex'):
        parser.add_argument('--' + name, required=True, type=pathlib.Path)
    scenario = parser.add_mutually_exclusive_group()
    scenario.add_argument('--stock', action='store_true')
    scenario.add_argument('--quick-exit', action='store_true')
    scenario.add_argument('--consecutive', action='store_true')
    scenario.add_argument('--subagent', action='store_true')
    parser.add_argument('--work-root', default='/tmp', type=pathlib.Path)
    args = parser.parse_args()
    tmux = shutil.which('tmux')
    if tmux is None:
        parser.error('tmux is required')
    root = pathlib.Path(tempfile.mkdtemp(prefix='harbor-selection-', dir=args.work_root)).resolve()
    print('diagnostics', root, flush=True)
    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'TERM': 'xterm-256color', 'SHELL': '/bin/sh',
           'LANG': 'C.UTF-8', 'NO_COLOR': '1', 'AGENT_HARBOR_EXPERIMENTAL_CODEX_PRIMARY_SELECTION': '1'}
    paths = {'HOME': 'home', 'XDG_CONFIG_HOME': 'config', 'XDG_DATA_HOME': 'data', 'XDG_STATE_HOME': 'state',
             'XDG_CACHE_HOME': 'cache', 'XDG_RUNTIME_DIR': 'runtime', 'TMPDIR': 'tmp',
             'CODEX_HOME': 'codex', 'CODEX_SQLITE_HOME': 'sqlite'}
    for key, directory in paths.items():
        path = root / directory
        path.mkdir(mode=0o700)
        env[key] = str(path)
    project = root / 'project'
    project.mkdir(mode=0o700)
    binaries = root / 'bin'
    binaries.mkdir(mode=0o700)
    harbor, herdr, codex = (binaries / name for name in ('agent-harbor', 'herdr', 'codex'))
    for source, destination in zip((args.binary, args.herdr, args.codex), (harbor, herdr, codex)):
        with source.open('rb') as executable:
            assert executable.read(4) in (b'\x7fELF', b'\xcf\xfa\xed\xfe', b'\xca\xfe\xba\xbe'), source
        shutil.copy2(source, destination)
        destination.chmod(0o700)
    workspace = root / 'harbor'
    env.update(AGENT_HARBOR_BRIDGE_HOME=str(workspace), AGENT_HARBOR_HERDR_BIN=str(herdr),
               PATH=str(binaries) + ':' + env['PATH'])

    class NoDiscovery(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_error(404)

        def log_message(self, *_args):
            pass

    discovery = http.server.ThreadingHTTPServer(('127.0.0.1', 0), NoDiscovery)
    discovery_config = root / 'config/magpie'
    discovery_config.mkdir(mode=0o700)
    (discovery_config / 'settings.json').write_text(json.dumps({'port': discovery.server_port}))
    provider = http.server.ThreadingHTTPServer(('127.0.0.1', 0), SubagentResponses if args.subagent else MockResponses)
    provider.observed = []
    provider.spawn_sent = False
    for server in (provider, discovery):
        threading.Thread(target=server.serve_forever, daemon=True).start()

    def run(*command, check=True, environment=None):
        result = subprocess.run(command, env=env if environment is None else environment, cwd=project,
                                text=True, capture_output=True, timeout=45)
        if check and result.returncode:
            raise RuntimeError(f'{command[0]} failed: {result.stdout}{result.stderr}')
        return result.stdout

    def tm(*command, check=True):
        return run(tmux, '-f', '/dev/null', '-S', root / 'tmux.sock', *command, check=check)

    def screen():
        return tm('capture-pane', '-p', '-t', 'fixture:0.0')

    def until(predicate, label):
        deadline = time.monotonic() + 40
        while time.monotonic() < deadline:
            if predicate():
                return
            time.sleep(.15)
        (root / 'failure-screen.txt').write_text(screen())
        raise RuntimeError(f'timed out waiting for {label}')

    def wait(text):
        until(lambda: text in screen(), text)

    def keys(*items):
        tm('send-keys', '-t', 'fixture:0.0', *items)
        time.sleep(.20)

    def text(value):
        keys('-l', value)
        time.sleep(.65)

    def record():
        path = workspace / 'sessions.json'
        records = json.loads(path.read_text()) if path.exists() else []
        assert len(records) <= 1, 'unexpected second Harbor Session'
        return records[0] if records else {}

    def open_fixture():
        tm('new-session', '-d', '-s', 'fixture', '-x', '160', '-y', '45', './launch.sh')
        wait('Harbor ^B a')

    def restart_fixture():
        run(harbor, 'stop')
        tm('kill-session', '-t', 'fixture', check=False)
        open_fixture()

    def recover(marker, reference):
        keys('C-b', 'a')
        wait('Search sessions')
        keys('Enter')
        wait('Harbor › Resume conversation')
        time.sleep(.30)
        keys('Enter')
        wait(marker)
        wait(reference)
        assert 'Search sessions' not in screen(), 'Harbor recovery popup stayed open'

    def select_conversation(marker, reference):
        text('/resume')
        keys('Enter')
        wait('Resume a previous session')
        text(marker)
        keys('Enter')
        wait(reference)
        assert 'Resume a previous session' not in screen(), 'selected conversation stayed a picker row'

    try:
        launcher = project / 'launch.sh'
        launcher.write_text('#!/bin/sh\n' + ''.join(f'export {key}={shlex.quote(value)}\n' for key, value in env.items())
                            + 'cd ' + shlex.quote(str(project)) + '\nexec agent-harbor\n')
        launcher.chmod(0o700)
        versions = [run(binary, '--version').strip() for binary in (codex, herdr, harbor)]
        print('fixture versions', *versions, flush=True)
        profile = json.loads(run(harbor, 'profiles', 'new', 'Selection fixture'))
        run(harbor, 'profiles', 'select', profile['id'])
        client = profile['clients'][0]
        profile_home = pathlib.Path(client['home'])
        (profile_home / 'config.toml').write_text(f'''check_for_update_on_startup = false
cli_auth_credentials_store = "file"
model = "test-model"
model_provider = "fixture"
[features]
hooks = true
multi_agent_v2 = {str(args.subagent).lower()}
[tui]
status_line = ["session-id"]
[model_providers.fixture]
name = "fixture"
base_url = "http://127.0.0.1:{provider.server_port}/v1"
requires_openai_auth = false
wire_api = "responses"
[projects.{json.dumps(str(project))}]
trust_level = "trusted"
''')
        open_fixture()
        keys('C-b', 'a')
        wait('Search sessions')
        keys('n')
        wait('New session')
        keys('C-s')
        until(lambda: any(label in screen() for label in ('Conversation recovery', 'Hooks need review', 'Ask Codex')),
              'initial managed Codex launch')
        if 'Conversation recovery' in screen():
            keys('y')
        until(lambda: 'Ask Codex' in screen() or 'Hooks need review' in screen(), 'Codex startup or hook trust')
        if 'Hooks need review' in screen():
            keys('Down', 'Enter')
        wait('Ask Codex')
        if 't trust all' in screen():
            keys('t', 'Escape')
        text(FIRST)
        keys('Enter')
        if 't trust all' in screen():
            keys('t', 'Escape')
            text(FIRST)
            keys('Enter')
        wait(ANSWER)
        until(lambda: record().get('recovery') == 'ready', 'A accepted through its normal SessionStart hook')
        text('/exit')
        keys('Enter')
        until(lambda: not record().get('process_group'), 'A exited normally')
        saved_a = record()
        first = saved_a['codex_session_id']
        assert saved_a['codex_home'] == str(profile_home) and saved_a['profile']['profile_id'] == profile['id']
        scoped = dict(env, CODEX_HOME=str(profile_home), CODEX_SQLITE_HOME=client['sqlite'])
        seed_command = shlex.join(['/usr/bin/env', '-i', *(f'{key}={value}' for key, value in scoped.items()),
                                   str(codex), '--no-daemon', '-c', 'features.hooks=false']) + '; exec /bin/sleep 90'

        def seed(marker):
            tm('new-session', '-d', '-s', 'seed', '-x', '130', '-y', '36', '-c', project, seed_command)

            def seed_screen():
                return tm('capture-pane', '-p', '-t', 'seed:0.0')

            until(lambda: 'Ask Codex' in seed_screen(), 'independent interactive seed')
            tm('send-keys', '-t', 'seed:0.0', '-l', marker)
            time.sleep(.35)
            tm('send-keys', '-t', 'seed:0.0', 'Enter')
            until(lambda: ANSWER in seed_screen(), f'{marker} history seeded in the same Profile')
            reference = re.search(r'\b[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\b', seed_screen()).group(0)
            tm('send-keys', '-t', 'seed:0.0', '-l', '/exit')
            time.sleep(.35)
            tm('send-keys', '-t', 'seed:0.0', 'Enter')
            until(lambda: 'sleep' in tm('display-message', '-p', '-t', 'seed:0.0', '#{pane_current_command}'), 'seed exited')
            tm('kill-session', '-t', 'seed')
            rollout, = (profile_home / 'sessions').rglob('*' + reference + '.jsonl')
            assert marker in rollout.read_text()
            return reference

        second = seed(SECOND)
        assert first != second
        third = seed(THIRD) if args.consecutive else None
        assert third not in (first, second)
        rollout_a, = (profile_home / 'sessions').rglob('*' + first + '.jsonl')

        restart_fixture()
        requests = len(provider.observed)
        recover(FIRST, first)
        live_a = record()
        assert live_a['run_token'] != saved_a['run_token']
        assert live_a['conversation']['run_token'] != live_a['run_token'], 'A already supplied a fresh SessionStart report'
        assert not live_a['conversation'].get('primary_selection'), 'startup reported an in-TUI switch'
        text('/resume')
        keys('Enter')
        wait('Resume a previous session')
        keys('Escape')
        wait(first)
        assert record()['codex_session_id'] == first, 'cancelled switch changed the saved target'
        select_conversation(SECOND, second)
        assert len(provider.observed) == requests, 'reopening A or selecting B ran a new turn'
        if args.quick_exit:
            tm('send-keys', '-t', 'fixture:0.0', '-l', '/exit')
            wait('/exit')
            tm('send-keys', '-t', 'fixture:0.0', 'Enter')
        (root / 'selected-b.ansi').write_text(tm('capture-pane', '-e', '-p', '-t', 'fixture:0.0'))
        if args.stock:
            time.sleep(2)
            assert record()['codex_session_id'] == first
            assert not record()['conversation'].get('primary_selection')
            print('PASS stock Codex selected B while Harbor retained saved A', flush=True)
            return

        until(lambda: record().get('codex_session_id') == second, 'durably acknowledged B before exit')
        selected = record()
        evidence = selected['conversation']['primary_selection']
        assert (evidence['source'], evidence['reason'], evidence['sequence']) == ('herdr:codex-primary-selection-v1', 'resume', 1)
        assert evidence['previous_id'] == first and evidence['selected_id'] == second
        assert evidence['original']['reference']['value'] == first
        assert selected['recovery'] == 'ready'
        if args.quick_exit:
            until(lambda: not record().get('process_group'), 'quick exit after B selection')
            assert record()['codex_session_id'] == second, 'exit replaced B with a passive report'
        else:
            assert selected['process_group'] == live_a['process_group']
            # Report valid run metadata over the real Herdr socket from this
            # fixture process, which does not own the terminal foreground.
            with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as connection:
                connection.settimeout(2)
                connection.connect(selected['herdr_socket'])
                request = {'id': 'wrong-peer', 'method': 'pane.report_primary_selection', 'params': {
                    'pane_id': selected['herdr_pane'], 'run_token': selected['run_token'], 'reason': 'resume',
                    'sequence': 2, 'previous_id': second, 'selected_id': first}}
                connection.sendall(json.dumps(request).encode() + b'\n')
                with connection.makefile('r') as reply:
                    error = json.loads(reply.readline())['error']
                assert error['code'] == 'primary_selection_not_accepted' and 'foreground' in error['message'], error
            assert record()['conversation']['primary_selection'] == evidence
        target_marker, target = SECOND, second
        if args.consecutive:
            select_conversation(THIRD, third)
            until(lambda: record().get('codex_session_id') == third, 'durably acknowledged C')
            selected = record()
            evidence = selected['conversation']['primary_selection']
            assert evidence['sequence'] == 2 and evidence['previous_id'] == second and evidence['selected_id'] == third
            assert evidence['original']['reference']['value'] == first
            assert selected['process_group'] == live_a['process_group']
            target_marker, target = THIRD, third
        restart_fixture()
        assert record()['codex_session_id'] == target
        recover(target_marker, target)
        recovered = record()
        for field in ('profile', 'working_directory', 'mode', 'codex_home', 'magpie_url', 'magpie_model', 'codex_connection', 'instance_id'):
            assert recovered.get(field) == saved_a.get(field), f'recovery changed {field}'
        assert recovered['codex_session_id'] == target and recovered['run_token'] != selected['run_token']
        assert len(provider.observed) == requests, 'recovery sent a model request'
        if args.consecutive:
            select_conversation(SECOND, second)
            until(lambda: record().get('codex_session_id') == second, 'durably acknowledged B in the new run')
            later = record()['conversation']['primary_selection']
            assert later['run_token'] == recovered['run_token'] and later['sequence'] == 1
            assert later['original']['reference']['value'] == third and later['prior_references'] == [third]
            assert later['previous_run'] == evidence and later['previous_run']['original']['reference']['value'] == first
            restart_fixture()
            recover(SECOND, second)
            assert record()['conversation']['primary_selection'] == later
            assert len(provider.observed) == requests
            print('PASS a new run retained the accepted selection evidence from the previous run', flush=True)
        if args.subagent:
            text(DELEGATE)
            keys('Enter')
            wait(ANSWER)
            text('/subagents')
            keys('Enter')
            wait('Subagents')
            wait('harbor_fixture')
            child_row = next(line for line in screen().splitlines() if 'harbor_fixture' in line and re.search(r'[0-9a-f]{8}-', line))
            child = re.search(r'\b[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\b', child_row).group(0)
            assert child not in (first, target)
            keys('Down', 'Enter')
            until(lambda: child in screen() and 'Subagents' not in screen(), 'real Codex opened the child thread')
            assert record()['codex_session_id'] == target
            assert record()['conversation']['primary_selection'] == recovered['conversation']['primary_selection']
            print('PASS navigating to a real Codex subagent left the primary recovery target unchanged', flush=True)
        history_a = rollout_a.read_text()
        assert FIRST in history_a and SECOND not in history_a and THIRD not in history_a, 'A history was replaced or merged'
        (root / 'recovered.ansi').write_text(tm('capture-pane', '-e', '-p', '-t', 'fixture:0.0'))
        journey = 'A→B→C' if args.consecutive else 'A→B'
        print(f'PASS real {journey}, no turn or Harbor popup during selection, durable target across restart, original Profile, connection and A history', flush=True)
    finally:
        run(harbor, 'stop', check=False)
        tm('kill-server', check=False)
        stop_codex(root, env)
        for server in (provider, discovery):
            server.shutdown()
            server.server_close()


if __name__ == '__main__':
    main()
