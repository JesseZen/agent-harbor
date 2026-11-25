"""Responses Lite acceptance against a fixture or the supplied aihub route."""
import json
import os

from e2e_local import MockResponses

MODEL = 'aihub/gpt-6.2-sol'
HEADER = 'X-OpenAI-Internal-Codex-Responses-Lite'
AUTO = 'AH_LITE_AUTO_547'
FIRST = 'AH_LITE_TOOL_548'
SECOND = 'AH_LITE_RESUME_549'
STANDARD = 'AH_STANDARD_550'
OLD_REASONING = 'AQAB' * 10240


class LiteRelay(MockResponses):
    def do_GET(self):
        self.reply(json.dumps({'data': [{'id': 'gpt-6.2-sol', 'object': 'model'}]}).encode(), 'application/json')

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers.get('Content-Length', '0'))))
        inputs = body.get('input', [])
        history = json.dumps(inputs)
        self.seed_reasoning = FIRST in history and SECOND not in history
        self.server.journal.append({
            'old_reasoning': any(item.get('type') == 'reasoning' and item.get('encrypted_content') == OLD_REASONING
                                 for item in inputs),
            'lite': self.headers.get(HEADER) == 'true',
            'context': (body.get('reasoning') or {}).get('context'),
            'additional_tools': any(item.get('type') == 'additional_tools' for item in body.get('input', [])),
            'markers': [marker for marker in (AUTO, FIRST, SECOND, STANDARD) if marker in history],
        })
        self.respond(body)

    def reply(self, body, content_type):
        if content_type == 'text/event-stream' and getattr(self, 'seed_reasoning', False):
            events = [json.loads(line[6:]) for line in body.decode().splitlines() if line.startswith('data: ')]
            reasoning = {'id': 'rs_fixture', 'type': 'reasoning', 'summary': [], 'encrypted_content': OLD_REASONING}
            events[-1]['response']['output'].append(reasoning)
            events.insert(-1, {'type': 'response.output_item.done', 'output_index': 1, 'item': reasoning})
            body = ''.join('event: '+event['type']+'\ndata: '+json.dumps(event)+'\n\n' for event in events).encode()
        super().reply(body, content_type)


def configure(managed, run, root, config, endpoint):
    if config is None:
        run([managed, 'provider', 'add', 'aihub', 'responses='+endpoint,
             'key=fixture-only', 'models=gpt-6.2-sol', 'codex=as-sent'])
        return
    # Copy only the route under test to the fixture's portable data directory.
    entries = json.loads(config.read_text())['providers']
    source, = (entry for entry in entries if entry.get('id') == 'aihub')
    provider = {key: source[key] for key in ('id', 'name', 'key', 'responses')}
    provider.update(models=['gpt-6.2-sol'], codexAsSent=True)
    target = root/'harbor/magpie/bin/data/providers.json'
    pending = target.with_suffix('.fixture')
    fixture = json.loads(target.read_text())
    fixture['providers'].append(provider)
    pending.write_text(json.dumps(fixture))
    pending.chmod(0o600)
    os.replace(pending, target)


def exercise(root, real, relay, create, popup, key, text, screen, records, wait, options_prompt, reviewed_confirm):
    def current(record):
        return next(row for row in records() if row['id'] == record['id'])

    def rollout(record):
        conversation = current(record).get('codex_session_id')
        if not conversation:
            return []
        paths = list((root/'codex/sessions').rglob('*'+conversation+'.jsonl'))
        if len(paths) != 1:
            return []
        return [json.loads(line) for line in paths[0].read_text().splitlines() if line.endswith('}')]

    def includes_reasoning(event):
        return event.get('payload', {}).get('info', {}).get('last_usage_includes_reasoning') is True

    def ask(record, prompt, reply):
        def replies():
            return sum(
                event.get('payload', {}).get('type') == 'message'
                and event['payload'].get('role') == 'assistant'
                and any(reply in part.get('text', '') for part in event['payload'].get('content', []))
                for event in rollout(record)
            )
        previous = replies()
        options_prompt()
        text(prompt);key('Enter')
        wait(lambda: replies() > previous, 'Codex assistant reply')

    auto = create('Automatic Lite', MODEL, 'Automatic')
    assert auto['responses_format'] == 'automatic' and auto['responses_lite_model'] == MODEL
    ask(auto, 'Reply with exactly '+AUTO, AUTO if real else 'HELLO_FROM_ISOLATED_MOCK')
    wait(lambda: any(includes_reasoning(event) for event in rollout(auto)), 'automatic usage includes reasoning')
    if not real:
        assert any(AUTO in entry['markers'] and entry['lite'] for entry in relay.journal)
    standard = create('Standard Responses', MODEL, 'Responses')
    assert standard['responses_format'] == 'responses' and 'responses_lite_model' not in standard
    ask(standard, 'Reply with exactly '+STANDARD, STANDARD if real else 'HELLO_FROM_ISOLATED_MOCK')
    if not real:
        assert any(STANDARD in entry['markers'] and not entry['lite'] for entry in relay.journal)
        assert not any(includes_reasoning(event) for event in rollout(standard))

    lite = create('Explicit Lite', MODEL, 'Responses Lite')
    assert lite['responses_format'] == 'responses-lite' and lite['responses_lite_model'] == MODEL
    if real:
        ask(lite, 'First reason through the least common multiple of 816 and 1428 without tools. '
            'Then run exactly printf AH_LITE_TOOL_548 using the shell tool and answer LITE_TOOL_FINISHED.',
            'LITE_TOOL_FINISHED')
        wait(lambda: any(FIRST in str(event.get('payload', {}).get('output', ''))
                         and event.get('payload', {}).get('type') == 'function_call_output'
                         for event in rollout(lite)), 'shell output in Codex history')
        wait(lambda: any(event.get('payload', {}).get('type') == 'reasoning'
                         and event.get('payload', {}).get('encrypted_content')
                         for event in rollout(lite)), 'retained encrypted reasoning in Codex history')
    else:
        ask(lite, FIRST, 'HELLO_FROM_ISOLATED_MOCK')
        assert any(FIRST in entry['markers'] and entry['lite'] and entry['context'] == 'all_turns'
                   and entry['additional_tools'] for entry in relay.journal), relay.journal
    wait(lambda: any(includes_reasoning(event) for event in rollout(lite)), 'Codex Lite usage includes reasoning')
    before = current(lite)
    reasoning_events = sum(includes_reasoning(event) for event in rollout(lite))
    text('/exit');key('Enter')
    wait(lambda: not current(lite).get('process_group'), 'Lite Codex exited')
    popup();key('/');text(lite['name']);key('Enter');key('Enter')
    screen('Resume conversation');screen('Responses Lite');reviewed_confirm()
    wait(lambda: current(lite).get('process_group') and current(lite)['run_token'] != before['run_token'], 'Lite resumed')
    if real:
        ask(lite, 'Reply with exactly '+SECOND, SECOND)
    else:
        ask(lite, SECOND, 'HELLO_FROM_ISOLATED_MOCK')
        assert any(FIRST in entry['markers'] and SECOND in entry['markers'] and entry['lite'] and entry['old_reasoning']
                   for entry in relay.journal), relay.journal
    after = current(lite)
    for field in ('codex_session_id', 'instance_id', 'magpie_model', 'responses_format', 'responses_lite_model'):
        assert after[field] == before[field], 'recovery changed '+field
    wait(lambda: sum(includes_reasoning(event) for event in rollout(lite)) > reasoning_events,
         'resumed usage includes reasoning')
    print('PASS Responses format selection, signed-out Codex requests, and Lite conversation recovery', flush=True)
