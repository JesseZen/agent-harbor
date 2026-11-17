#!/usr/bin/env python3
"""Isolated Harbor UI / native Codex / real Magpie acceptance.
Only copied executables and fixture-owned processes, ports and homes are used.
"""
import sys
sys.dont_write_bytecode = True
import argparse, io, tomllib, http.server, json, os, pathlib, re, shlex, shutil, socket, subprocess, tempfile, threading, time
from e2e_package import MockResponses as ChatRelay
from e2e_local import MockResponses as NativeRelay
from fixture_process import stop_codex
from fixture_mcp import MCPProbe
from fixture_requests import evidence
from fixture_negative_ui import exercise as negative_ui


def main():
    p=argparse.ArgumentParser()
    p.add_argument('--full-state',action='store_true',help='include unmanaged client and live refresh in full acceptance')
    p.add_argument('--replacement',action='store_true')
    p.add_argument('--live-state-probe',action='store_true',help='exercise automatic popup refresh and reconnect')
    p.add_argument('--state-probe',action='store_true',help='stop after real runtime state comparison')
    p.add_argument('--renamed-codex',action='store_true',help='exercise native Codex symlink with a nonstandard target basename')
    p.add_argument('--recovery-review',action='store_true',help='replay an unavailable final identity observation and test one-action recovery plus retry')
    p.add_argument('--profiles',action='store_true',help='run the Profile A/B UI, runtime and recovery journey with isolated native clients')
    p.add_argument('--session-names',action='store_true',help='exercise one-time Codex title capture and manual Herdr display names')
    p.add_argument('--model-picker',action='store_true',help='exercise Magpie models, Codex effort and profile recovery')
    p.add_argument('--experimental-codex-accounts',action='store_true',help='start eligible Codex sessions through the experimental owned App Server')
    p.add_argument('--experimental-account-recovery',choices=['native','magpie'],help='exercise same-conversation recovery with the experimental App Server')
    for name in ('binary','herdr','magpie','codex'): p.add_argument('--'+name,required=True)
    p.add_argument('--codex-auth',help='native codex-auth when testing an unpackaged Harbor executable')
    p.add_argument('--onboarding',choices=['local','managed','external'],default='local')
    p.add_argument('--mcp', help='built herdr-mcp dist/index.js; enables exact-target MCP acceptance')
    a=p.parse_args()
    root=pathlib.Path(tempfile.mkdtemp(prefix='harbor03-ui-',dir='/tmp')).resolve()
    root.chmod(0o700)
    print('diagnostics',root,flush=True)
    env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TERM':'xterm-256color','SHELL':'/bin/sh','LANG':'C.UTF-8','NO_COLOR':'1','MAGPIE_NO_ACCOUNTS':'1'}
    if a.session_names:
        env.pop('NO_COLOR');env['COLORTERM']='truecolor'
    for key,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
        (root/leaf).mkdir(mode=0o700);env[key]=str(root/leaf)
    for leaf in ('bin','project'): (root/leaf).mkdir(mode=0o700)
    for name,source in [('agent-harbor',a.binary),('herdr',a.herdr),('magpie',a.magpie),('codex',a.codex)]:
        if name=='codex': assert pathlib.Path(source).read_bytes()[:4] in (b'\x7fELF',b'\xcf\xfa\xed\xfe'), 'native Codex required'
        if name=='codex' and a.renamed_codex:
            target=root/'codex-renamed-native'
            shutil.copy2(source,target);target.chmod(0o700)
            (root/'bin'/name).symlink_to(target)
        else:
            shutil.copy2(source,root/'bin'/name);(root/'bin'/name).chmod(0o700)
    env.update(PATH=str(root/'bin')+':'+env['PATH'],AGENT_HARBOR_BRIDGE_HOME=str(root/'harbor'),AGENT_HARBOR_HERDR_BIN=str(root/'bin/herdr'),AGENT_HARBOR_MAGPIE_BIN=str(root/'bin/magpie'))
    auth=pathlib.Path(a.codex_auth) if a.codex_auth else pathlib.Path(a.binary).resolve().with_name('codex-auth')
    if auth.is_file():
        shutil.copy2(auth,root/'bin/codex-auth');(root/'bin/codex-auth').chmod(0o700)
    if a.replacement:env['AGENT_HARBOR_REPLACEMENT_CANDIDATE']='1'
    if a.experimental_codex_accounts:
        env['AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS']='1'
        env['OPENAI_API_KEY']='sk-harbor-fixture'
    binary=root/'bin/agent-harbor'; tmux=shutil.which('tmux'); sock=str(root/'tmux.sock')
    servers=[]
    def run(args,check=True,**kwargs):
        return subprocess.run(list(map(str,args)),env=env,text=True,capture_output=True,check=check,timeout=40,**kwargs)
    def tm(*args):return run([tmux,'-S',sock,*args]).stdout
    def capture():return tm('capture-pane','-p','-t','app:0.0')
    def wait(predicate,label,timeout=50):
        end=time.monotonic()+timeout
        while time.monotonic()<end:
            value=predicate()
            if value:return value
            time.sleep(.15)
        (root/'failure-screen.txt').write_text(capture())
        raise RuntimeError('timeout: '+label+'\n'+capture())
    def screen(text):return wait(lambda: text in capture(),text)
    def key(*keys):tm('send-keys','-t','app:0.0',*keys);time.sleep(.15)
    def text(value):tm('send-keys','-t','app:0.0','-l',str(value));time.sleep(.6)
    def popup():
        # Search placeholders disappear while a query is present. Detect the
        # Sessions heading so a retained popup is not mistaken for a closed one.
        wait(lambda: 'Harbor › Working' not in capture(), 'prior popup action settled')
        if 'Harbor · Profile:' not in capture():key('C-b','a')
        wait(lambda: 'Harbor · Profile:' in capture() or 'Harbor · Accounts' in capture(), 'management popup visible')
        if 'Harbor · Profile:' not in capture():click('Sessions')
        screen('Harbor · Profile:')
    def click(label):
        import unicodedata
        lines=capture().splitlines()
        # Action captions can also name the page; footer controls come last.
        y=max(i for i,line in enumerate(lines) if label in line)
        prefix=lines[y][:lines[y].index(label)]
        x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
        tm('send-keys','-t','app:0.0','-l',f'\x1b[<0;{x};{y+1}M\x1b[<0;{x};{y+1}m');time.sleep(.3)
    def reviewed_confirm():
        # Enter directly confirms; retain the key/click penetration guard.
        time.sleep(.25);key('Enter')
    def records():
        path=root/'harbor/sessions.json'
        return json.loads(path.read_text()) if path.exists() else []
    def port():
        with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
    def server(handler):
        class RecordingHandler(handler):
            def do_POST(self):
                raw=self.rfile.read(int(self.headers.get('Content-Length','0')))
                if evidence(raw)['current']==['AH_LIVE_STATUS']:time.sleep(4)
                self.rfile=io.BytesIO(raw)
                marks=[m for m in ('AH_LOCAL_FIRST','AH_LOCAL_SECOND','AH_RELAY_B','AH_RESUME_B','AH_LOCAL_C','AH_RESUME_C','AH_EXTERNAL_STATUS','AH_LIVE_STATUS') if m.encode() in raw]
                event=evidence(raw)
                event['session']=self.headers.get('session-id','')
                event['model']=json.loads(raw).get('model','')
                event['effort']=json.loads(raw).get('reasoning_effort','')
                self.server.journal.append(event)
                super().do_POST()
                self.server.observed.append((self.path,marks))
        s=http.server.ThreadingHTTPServer(('127.0.0.1',0),RecordingHandler);s.observed=[];s.events=[];s.journal=[]
        threading.Thread(target=s.serve_forever,daemon=True).start();servers.append(s);return s
    class NoGateway(http.server.BaseHTTPRequestHandler):
        def do_GET(self):self.send_error(404)
        def log_message(self,*args):pass
    discovery=None;started=False;external=None
    try:
        # Never probe the user's standard discovery service.
        try:discovery=http.server.ThreadingHTTPServer(('127.0.0.1',3425),NoGateway)
        except OSError:print('SKIP discovery port occupied',flush=True);return 77
        threading.Thread(target=discovery.serve_forever,daemon=True).start()
        relay_a,relay_b,native=server(ChatRelay),server(ChatRelay),server(MCPProbe if a.mcp else NativeRelay)
        native.tool_output,native.tool_error='',''
        if a.session_names:native.automatic_title='修复登录页面'
        original_notify=root/'bin/original-notify'
        original_notify.write_text('#!/bin/sh\nprintf notified >> "$HOME/original-notify-called"\n')
        original_notify.chmod(0o700)
        def notify_count():
            path=root/'home/original-notify-called'
            return path.stat().st_size if path.exists() else 0
        config=('check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\nmodel = "test-model"\nmodel_provider = "fixture"\n'
                'notify = ['+json.dumps(str(original_notify))+']\n'
                '[model_providers.fixture]\nname = "Fixture native"\nbase_url = "http://127.0.0.1:'+str(native.server_port)+'/v1"\nrequires_openai_auth = false\nwire_api = "responses"\n'
                '[projects.'+json.dumps(str(root/'project'))+']\ntrust_level = "trusted"\n')
        if a.replacement and not a.session_names:
            config+='[features]\nhooks = true\n'
            run([root/'bin/herdr','integration','install','codex'])
        if a.mcp:
            node=shutil.which('node');assert node, 'node required'
            env['HERDR_BIN']=str(root/'harbor/herdr/bin/herdr')
            config+='[mcp_servers.herdr]\ncommand = '+json.dumps(node)+'\nargs = ['+json.dumps(str(pathlib.Path(a.mcp).resolve()))+']\n'
        (root/'codex/config.toml').write_text(config)
        # Explicit signed-out API connections use the fixture gateway credential.
        # A synthetic API key cannot stand in for a confirmed ChatGPT account.
        # Experimental App Server account tests retain their separate key path.
        if a.experimental_codex_accounts:
            run([root/'bin/codex','login','--with-api-key'],input='sk-harbor-fixture\n')
        else:
            run([root/'bin/codex','logout'])
        launcher=root/'launch.sh'
        launcher.write_text('#!/bin/sh\n'+''.join('export '+k+'='+shlex.quote(v)+'\n' for k,v in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nread answer\n')
        launcher.chmod(0o700)
        def open_workspace():tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
        if a.onboarding=='external':
            import urllib.request
            ext=root/'external';ext.mkdir(mode=0o700)
            shutil.copy2(root/'bin/magpie',ext/'magpie');(ext/'.portable').write_text('fixture')
            ext_env=env.copy()
            for k,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data-root'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
                (ext/leaf).mkdir(mode=0o700);ext_env[k]=str(ext/leaf)
            (ext/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
            # Portable settings disable all unrelated background work.
            (ext/'data').mkdir();(ext/'data/settings.json').write_text(json.dumps({'noStats':True,'noAutoUpdate':True,'noAutoDownload':True}))
            address='127.0.0.1:'+str(port());env['MAGPIE_ADDR']=address;ext_env['MAGPIE_ADDR']=address
            external=subprocess.Popen([ext/'magpie','serve'],env=ext_env,stdout=open(ext/'serve.log','w'),stderr=subprocess.STDOUT)
            def ready_external():
                try:
                    with urllib.request.urlopen('http://'+address+'/',timeout=.5) as response:return json.load(response).get('name')=='magpie'
                except Exception:return False
            wait(ready_external,'external gateway')
            # Rewrite the launcher after setting the discovery address.
            launcher.write_text('#!/bin/sh\n'+''.join('export '+k+'='+shlex.quote(v)+'\n' for k,v in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nread answer\n')
        versions={name:run([root/'bin'/name,'--version']).stdout.strip() for name in ('codex','herdr','magpie')}
        print('fixture versions',versions,flush=True)
        open_workspace();screen('Harbor ^B a')
        # Gateway configuration is a fixture prerequisite. Daily session creation
        # and recovery below are driven through Harbor's actual popup.
        if not started:run([binary,'magpie','start','--port',port(),'--web-port',port()]);started=True
        managed=root/'harbor/magpie/bin/magpie'
        for label,relay in [('a',relay_a),('b',relay_b)]:
            run([managed,'provider','add','relay-'+label,'url=http://127.0.0.1:'+str(relay.server_port)+'/v1','key=fixture-only','models=test-model'])
            if a.model_picker:run([managed,'model','spec','relay-'+label+'/test-model','--reasoning','low-high'])
        run([managed,'group','add','ui-group','models=relay-a/test-model','routing=rotate','stays=off'])
        if a.model_picker:run([managed,'group','set','ui-group','levels=low,medium,high'])
        wait(lambda:'relay-b/test-model' in run([binary,'models','--instance','managed','--json']).stdout,'catalog readiness')
        def create(name,model=None):
            expected_name=name or ('project · magpie' if model else 'project · local')
            popup();key('n');screen('New session')
            if model:
                click('Connection');screen('Choose instance and model');click('Choose instance and model');click('Select');screen('Choose instance')
                key('/');text('managed');key('Enter');key('Enter');screen('Model or routing group')
                key('/');text(model);key('Enter')
                if 'Acknowledge & apply' in capture():click('Acknowledge & apply')
                else:click('Apply selection')
                screen('New session')
            else:
                click('Connection');screen('Choose instance and model');click('Native');click('Select');screen('New session')
            click('Name');screen('Session name');text(name);click('Apply');screen('New session')
            click('Create')
            wait(lambda:any(label in capture() for label in ('Ask Codex','Hooks need review','Conversation recovery')), 'Codex prompt or identity setup')
            if 'Conversation recovery' in capture():
                key('s' if a.session_names else 'y')
                wait(lambda:'Ask Codex' in capture() or 'Hooks need review' in capture(), 'Codex prompt or hook review')
            if 'Hooks need review' in capture():
                key('Down','Enter')
            screen('Ask Codex');time.sleep(1)
            wait(lambda:any(r['name']==expected_name and r.get('process_group',0)>0 for r in records()),'running '+expected_name)
            rec=next(r for r in records() if r['name']==expected_name)
            assert rec.get('herdr_tab') and rec.get('herdr_pane'),rec
            def listed():
                result=json.loads(run([root/'harbor/herdr/bin/herdr','agent','list']).stdout)['result']
                return any(row['pane_id']==rec['herdr_pane'] for row in result['agents'])
            wait(listed,'popup-created session in official Herdr agent.list')
            # Before the first turn Codex may not yet report a conversation.
            # The foreground run must still appear once and be focusable,
            # without claiming that its conversation identity is established.
            def fresh_target():
                views=json.loads(run([binary,'sessions','--json']).stdout)
                same=[v for v in views if v.get('herdr_pane')==rec['herdr_pane']]
                if len(same)!=1 or same[0]['id']!=rec['id'] or same[0].get('external'):
                    return None
                view=same[0]
                if not view['running'] or not (view['runtime']['associated'] or view['runtime'].get('run_verified')):
                    return None
                return view
            fresh=wait(fresh_target,'one focusable fresh managed target '+name)
            if not fresh['runtime'].get('observed_reference'):
                assert not fresh['runtime']['associated'], 'missing conversation identity was claimed as verified'
            result=run([binary,'focus',rec['id']],check=False)
            assert result.returncode==0, 'fresh managed target could not be focused: '+result.stderr
            return rec
        def turn(marker,relay):
            if a.replacement and 'hook needs' in capture():
                key('t');time.sleep(.5);key('Escape')
            text(marker);key('Enter')
            if a.replacement:
                time.sleep(1)
                if 't trust all' in capture():
                    key('t');time.sleep(.5);key('Escape');time.sleep(.5);text(marker);key('Enter')
            wait(lambda:any(marker in marks for _,marks in relay.observed),'provider marker '+marker)
            if not a.session_names:
                wait(lambda:any(r.get('codex_session_id') for r in records()),'conversation capture')
            screen('HELLO_FROM_ISOLATED_MOCK');time.sleep(1)
            assert any(e['kind']=='main' and marker in e['current'] for e in relay.journal), 'main request evidence absent'
            for other in servers:
                if other is not relay:
                    assert not any(marker in e['current'] for e in other.journal), 'main marker reached wrong upstream'
        def choose_model(name,effort,temporary=False):
            def navigate(rows,target):
                selected=next((i for i,line in enumerate(rows) if line.lstrip().startswith(('›','>'))),None)
                assert selected is not None, 'current picker selection is unavailable: '+repr(rows)
                for _ in range(abs(target-selected)):key('Down' if target>selected else 'Up')
            text('/model');key('Enter');screen('Select Model and Effort')
            menu=capture();rows=[line.split('│')[-1] for line in menu.splitlines() if re.match(r'^\s*[›>]?\s*\d+\.',line.split('│')[-1])]
            provider,model=name.split('/',1)
            target=next((i for i,line in enumerate(rows) if model+' · '+provider in line),None)
            assert target is not None, 'model missing from Codex picker: '+name+'\n'+menu
            navigate(rows,target)
            key('Enter');screen(effort)
            menu=capture();levels=[line.split('│')[-1] for line in menu.splitlines() if re.match(r'^\s*[›>]?\s*\d+\.',line.split('│')[-1])]
            target=next((i for i,line in enumerate(levels) if effort.lower() in line.lower()),None)
            assert target is not None, 'effort missing from Codex picker: '+effort+'\n'+menu
            navigate(levels,target)
            key('s' if temporary else 'Enter')
            screen('Model changed to')
        if a.profiles:
            assert a.replacement, 'Profile recovery acceptance requires the Herdr query candidate'
            def profile_rows():return json.loads(run([binary,'profiles','list']).stdout)
            def profile_select(name):
                popup();key('p');screen('Harbor · Profiles');click(name);key('Enter');screen('Harbor · Profile:');key('q')
            for name in ('PF Alpha','PF Beta'):
                popup();key('p');screen('Harbor · Profiles');key('n');screen('Create isolated Profile');text(name);key('Enter');screen('Profile saved');key('Escape');key('q')
            profiles={p['name']:p for p in profile_rows()}
            for name in ('PF Alpha','PF Beta'):
                client=profiles[name]['clients'][0]
                # Explicitly authored synthetic fixture settings. No user data,
                # grant, history or registry is imported from another Profile.
                home=pathlib.Path(client['home']);home.joinpath('config.toml').write_text(config)
                # API-only recovery uses an explicitly signed-out client. A synthetic
                # OpenAI key is not a confirmed ChatGPT account for --skip-api.
                assert not home.joinpath('auth.json').exists()
                # Harbor prepares the scoped identity integration on first launch.
            profile_select('PF Alpha')
            first=create('PF-A-one','relay-a/test-model');turn('AH_LOCAL_FIRST',relay_a)
            if a.recovery_review:
                before=next(r for r in records() if r['id']==first['id'])
                run([binary,'stop']);tm('kill-session','-t','app')
                open_workspace();screen('Harbor ^B a')
                popup();key('/');text('PF-A-one');key('Enter');key('c');screen('Close inspected shell');reviewed_confirm();time.sleep(.5)
                if 'Harbor · Profile:' in capture():key('q')
                # Replay the user's persisted final-read failure using the exact
                # reference captured from this fixture's real first turn. No ID,
                # Profile, connection or exit evidence is invented or changed.
                rows=records();saved=next(r for r in rows if r['id']==first['id'])
                saved['conversation']['status']='observation_unavailable';saved['recovery']='needs_review'
                (root/'harbor/sessions.json').write_text(json.dumps(rows))
                run([binary,'magpie','stop']);started=False
                popup();key('/');text('PF-A-one');key('Enter');key('Enter');screen('Resume conversation')
                assert 'Use recorded conversation' not in capture(),'separate acceptance step still required'
                (root/'recovery-before-click.txt').write_text(capture())
                time.sleep(.3)  # pass the popup's input-penetration guard
                click('Resume conversation')
                wait(lambda:len(next(r for r in records() if r['id']==first['id']).get('reassociations',[]))==1,'saved reference reviewed by Resume')
                screen('Resume conversation');time.sleep(.5)
                failed=capture();(root/'recovery-retry-screen.txt').write_text(failed)
                assert '↵ Resume conversation' in failed and '›↵' not in failed,'failure lost the direct Resume shortcut'
                assert 'conversation observation needs review' not in failed,'same identity review dead end'
                run([binary,'magpie','start']);started=True
                click('Resume conversation');screen('Ask Codex');time.sleep(1);turn('AH_LOCAL_SECOND',relay_a)
                wait(lambda:any('AH_LOCAL_FIRST' in marks and 'AH_LOCAL_SECOND' in marks for _,marks in relay_a.observed),'same conversation after combined review and retry')
                current=next(r for r in records() if r['id']==first['id'])
                assert all(current.get(k)==before.get(k) for k in ('profile','codex_home','codex_session_id','instance_id','mode','codex_connection'))
                assert len(current.get('reassociations',[]))==1,'retry repeated identity acceptance'
                print('PASS one-action saved-reference recovery, real gateway failure with Resume focus retained, same-page retry and exact original Magpie conversation',root,flush=True)
                return 0
            profile_select('PF Beta')
            second=create('PF-B-one','relay-b/test-model');turn('AH_RELAY_B',relay_b)
            profile_select('PF Alpha')
            third=create('PF-A-two');turn('AH_LOCAL_C',native)
            wait(lambda:len(records())==3 and all(r.get('codex_session_id') for r in records()),'three Profile-bound Conversations')
            snapshot={r['name']:dict(r) for r in records()}
            a1,a2,b1=snapshot['PF-A-one'],snapshot['PF-A-two'],snapshot['PF-B-one']
            assert a1['profile']==a2['profile'] and a1['profile']!=b1['profile'],snapshot
            assert a1['codex_home']==a2['codex_home']!=b1['codex_home']
            assert len({r['codex_session_id'] for r in records()})==3
            b_auth=pathlib.Path(profiles['PF Beta']['clients'][0]['home'])/'auth.json'
            b_before=b_auth.read_bytes() if b_auth.exists() else None;b_pid=b1['process_group']
            result=run([binary,'stop-session',a1['id']],check=False,input='yes\n')
            assert result.returncode==0,result.stdout+result.stderr
            wait(lambda:next(r for r in records() if r['id']==a1['id']).get('process_group',0)==0,'A stopped before recovery')
            profile_select('PF Beta')
            # Recovery must use A even while B is the visible default.
            wrapper=root/'harbor/herdr/bin/herdr'
            def a_conversation_released():
                agents=json.loads(run([wrapper,'agent','list']).stdout)['result']['agents']
                return not any((agent.get('agent_session') or {}).get('value')==a1['codex_session_id'] for agent in agents)
            wait(a_conversation_released,'Herdr releases stopped A Conversation',timeout=15)
            popup();key('/');text('PF-A-one');key('Enter');key('Enter');screen('Resume conversation');key('a');key('Enter')
            screen('Ask Codex');time.sleep(1);turn('AH_LOCAL_SECOND',relay_a)
            wait(lambda:any('AH_LOCAL_FIRST' in marks and 'AH_LOCAL_SECOND' in marks for _,marks in relay_a.observed),'A recovered original history')
            resumed=next(r for r in records() if r['id']==a1['id'])
            assert resumed['codex_session_id']==a1['codex_session_id'] and resumed['profile']==a1['profile']
            assert resumed['instance_id']==a1['instance_id'] and resumed['codex_home']==a1['codex_home']
            assert (b_auth.read_bytes() if b_auth.exists() else None)==b_before and next(r for r in records() if r['id']==b1['id'])['process_group']==b_pid
            for name,other_marker in [('PF Alpha','AH_RELAY_B'),('PF Beta','AH_LOCAL_FIRST')]:
                home=pathlib.Path(profiles[name]['clients'][0]['home'])
                assert any(home.joinpath('sessions').rglob('*.jsonl')), 'Profile history absent'
                assert all(other_marker not in path.read_text() for path in home.joinpath('sessions').rglob('*.jsonl')), 'history crossed Profiles'
                assert list(pathlib.Path(profiles[name]['clients'][0]['sqlite']).glob('*.sqlite')), 'Profile SQLite absent'
            # Separate Conversations within A share its settings/history root,
            # but never each other's messages.
            for record,forbidden in [(a1,'AH_LOCAL_C'),(a2,'AH_LOCAL_FIRST')]:
                files=list(pathlib.Path(record['codex_home']).joinpath('sessions').rglob('*'+record['codex_session_id']+'.jsonl'))
                assert files and all(forbidden not in path.read_text() for path in files), 'same-Profile Conversations merged'
            # A restart must retain the binding, connection and exact reference.
            for record in records():
                run([binary,'stop-session',record['id']],input='yes\n')
            run([binary,'stop']);run([binary,'magpie','stop']);started=False
            tm('kill-session','-t','app')
            run([binary,'magpie','start']);started=True
            open_workspace();screen('Harbor ^B a')
            for visible,name,marker,relay,first_marker in [('PF Alpha','PF-B-one','AH_RESUME_B',relay_b,'AH_RELAY_B'),('PF Beta','PF-A-one','AH_RESUME_C',relay_a,'AH_LOCAL_FIRST')]:
                profile_select(visible)
                popup();key('/');text(name);key('Enter');key('Enter');screen('Resume conversation');key('a');key('Enter')
                screen('Ask Codex');time.sleep(1);turn(marker,relay)
                wait(lambda:any(first_marker in marks and marker in marks for _,marks in relay.observed),'post-restart original history '+name)
                current=next(r for r in records() if r['name']==name);prior=snapshot[name]
                assert all(current.get(k)==prior.get(k) for k in ('profile','codex_home','codex_session_id','instance_id','mode','codex_connection')), 'restart changed Profile/Conversation/connection'
            # A changed Profile default is only a future-session reference.
            run([binary,'profiles','default',profiles['PF Alpha']['id'],'managed'])
            assert next(r for r in records() if r['id']==a1['id'])['instance_id']==a1['instance_id']
            (root/'profile-acceptance.json').write_text(json.dumps({'passed':True,'profiles':{k:v['id'] for k,v in profiles.items()},'sessions':{r['name']:{'id':r['id'],'profile':r['profile'],'conversation':r['codex_session_id']} for r in records()},'b_process_preserved':True,'b_auth_preserved':True,'restart_recovery':True},indent=2))
            print('PASS Profile A/B creation, concurrent real clients, separate history/SQLite, same-Profile Conversations and exact A/B recovery across workspace/gateway restart while the other Profile is selected',root,flush=True)
            return 0
        if a.session_names:
            rec=create('')
            turn('AH_LOCAL_FIRST',native)
            def current():return next(r for r in records() if r['id']==rec['id'])
            wait(lambda:current()['name']=='修复登录页面' and not current().get('auto_name_pending'),'one-time native Codex title capture')
            assert getattr(native,'title_calls',0)>0,'Codex automatic title generation was not exercised'
            assert not current().get('conversation') and not current().get('codex_session_id'),'display title manufactured Conversation identity'
            wrapper=root/'harbor/herdr/bin/herdr'
            def labels(name):
                pane=json.loads(run([wrapper,'pane','get',rec['herdr_pane']]).stdout)['result']['pane']
                tab=json.loads(run([wrapper,'tab','get',rec['herdr_tab']]).stdout)['result']['tab']
                return pane.get('label')==name and tab.get('label')==name
            assert labels('修复登录页面'),'automatic title not displayed in Herdr'
            text('/rename Later Codex title');key('Enter');time.sleep(2)
            assert current()['name']=='修复登录页面' and labels('修复登录页面'),'later title overwrote first capture'
            run([binary,'rename',rec['id'],'用户手动名称'])
            assert labels('用户手动名称'),'manual rename not displayed in Herdr'
            wait(lambda:'用户手动名称' in capture(),'renamed title rendered by Herdr client')
            agent=next(x for x in json.loads(run([wrapper,'agent','list']).stdout)['result']['agents'] if x['pane_id']==rec['herdr_pane'])
            assert agent.get('display_agent')=='codex','CLI handle replaced the visible client type'
            (root/'session-name-screen.ansi').write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
            config=tomllib.loads((root/'harbor/herdr/config/herdr.toml').read_text())
            rows=config['ui']['sidebar']['agents']['rows']
            assert rows[0][1]['bold'] and rows[0][1]['dim'] is False and rows[2][0]['fg']=='#9399b2','missing visual hierarchy'
            print('PASS real Codex automatic title → one-time Harbor/Herdr display name without hooks or inferred Conversation identity, Unicode manual rename and native sidebar styles',root,flush=True)
            return 0
        one=create('UI-A','ui-group');notify_before=notify_count();turn('AH_LOCAL_FIRST',relay_a)
        if a.model_picker:
            config_before=(root/'codex/config.toml').read_bytes()
            choose_model('relay-a/test-model','High')
            profile_a=root/'codex'/('agent-harbor-'+one['id']+'.config.toml')
            saved_a=tomllib.loads(profile_a.read_text())
            assert saved_a['model']=='relay-a/test-model' and saved_a['model_reasoning_effort']=='high',saved_a
            assert (root/'codex/config.toml').read_bytes()==config_before,'picker changed the native settings'
        wait(lambda:notify_count()>notify_before,'existing notify after first managed turn')
        key('C-b','m');screen('Magpie instances');key('Enter');screen('Review connection · Manage providers');reviewed_confirm()
        screen('1 agents')
        # Magpie's agent picker writes client settings. In a managed instance
        # those writes must not escape into the native Codex configuration.
        native_before_management=(root/'codex/config.toml').read_bytes()
        key('Enter');screen('Codex › model');text('relay-b/test-model');key('Enter');screen('1 agents');screen('model → relay-b/test-model')
        assert (root/'codex/config.toml').read_bytes()==native_before_management, 'managed Magpie changed native Codex settings'
        managed_client_config=root/'harbor/magpie/home/config.toml'
        wait(lambda:managed_client_config.is_file() and tomllib.loads(managed_client_config.read_text()).get('model')=='relay-b/test-model','managed Magpie client setting was applied')
        key('2');screen('relay-a');(root/'management.txt').write_text(capture());key('q');screen('Magpie instances')
        status=json.loads(run([binary,'magpie','status','--json']).stdout)
        (root/'management-status.json').write_text(json.dumps(status,indent=2))
        assert 'verified' in json.dumps(status), 'management UI stopped the gateway'
        key('Escape')
        wait(lambda:'UI-A' in capture() and ('Idle' in capture() or 'Done' in capture()),'live sessions after standalone Magpie popup')
        key('r');screen('UI-A');(root/'magpie-return-sessions.txt').write_text(capture());key('q')
        print('PASS contextual Magpie management isolates native settings, leaves gateway running and returns to live sessions',flush=True)
        two=create('UI-B','relay-b/test-model');notify_before=notify_count();turn('AH_RELAY_B',relay_b)
        if a.model_picker:
            profile_b=root/'codex'/('agent-harbor-'+two['id']+'.config.toml')
            before_b=profile_b.read_bytes();choose_model('relay-a/test-model','High',temporary=True)
            assert profile_b.read_bytes()==before_b,'session-only choice changed the session default'
            assert tomllib.loads(profile_a.read_text())==saved_a,'one session changed another session profile'
        wait(lambda:notify_count()>notify_before,'existing notify after second managed turn')
        three=create('UI-Native');notify_before=notify_count();turn('AH_LOCAL_C',native)
        if a.experimental_codex_accounts:
            current=next(r for r in records() if r['name']=='UI-Native')
            assert current.get('codex_server_pid') and current.get('codex_server_socket_identity'), 'native session has no owned App Server'
        wait(lambda:notify_count()>notify_before,'existing notify after native turn')
        wait(lambda:len(records())==3 and all(r.get('codex_session_id') for r in records()),'three captured IDs')
        if a.replacement:
            assert all(r.get('conversation',{}).get('status')=='observed' for r in records()), 'identity must come from official observations'
            assert not list((root/'harbor').glob('notify-*.json')), 'replacement prepared legacy notify'
        saved_references={r['name']:r.get('conversation',{}).get('reference') for r in records()}
        saved={r['name']:(r['codex_session_id'],r.get('instance_id'),r.get('magpie_model')) for r in records()}
        assert len({v[0] for v in saved.values()})==3
        assert not any('AH_RELAY_B' in marks for _,marks in relay_a.observed)
        assert not any('AH_LOCAL_FIRST' in marks for _,marks in relay_b.observed)
        # The active local pane's management shortcut lists instances explicitly.
        key('C-b','m');screen('Magpie instances');key('q')
        # All per-pane sidebar intent tokens must be set in real Herdr.
        herdr_env=env.copy()
        # Use the installed wrapper; it addresses only this managed workspace.
        wrapper=root/'harbor/herdr/bin/herdr'
        panes=json.loads(run([wrapper,'pane','list']).stdout)
        (root/'panes.json').write_text(json.dumps(panes,indent=2))
        pane_rows=panes['result']['panes']
        for r in records():
            pane=next(x for x in pane_rows if x['pane_id']==r['herdr_pane'])
            token=pane.get('tokens',{}).get('harbor','')
            expected=('Local Codex' if r['mode']=='local' else
                      'Magpie managed / Codex' if r.get('codex_connection')=='caller' else
                      'Magpie managed / '+r['magpie_model'])
            assert expected in token,pane
        assert 'routing group' in run([binary,'models','--instance','managed','--json']).stdout, 'real group label missing'
        (root/'connections-before.json').write_text(json.dumps(saved,indent=2))
        print('PASS three interactive UI-created Codex sessions, separate Magpie providers and native connection',flush=True)
        upstream=json.loads(run([wrapper,'agent','list']).stdout)['result']['agents']
        observed=json.loads(run([binary,'sessions','--json']).stdout)
        comparisons=[]
        for rec in records():
            row=next(x for x in upstream if x['pane_id']==rec['herdr_pane'])
            view=next(x for x in observed if x['id']==rec['id'])
            assert view['runtime']['associated'], 'live owned client is unassociated'
            assert view['runtime']['activity']==row['agent_status'], 'Harbor activity differs from Herdr'
            assert view['runtime']['terminal_id']==row['terminal_id']
            detail=json.loads(run([binary,'show',rec['id']]).stdout)
            assert detail['runtime']['activity']==view['runtime']['activity']
            comparisons.append({'client':rec['cli'],'herdr':row['agent_status'],'harbor':view['runtime']['activity']})
        (root/'runtime-comparison.json').write_text(json.dumps(comparisons,indent=2))
        print('PASS real Herdr agent.list matches Harbor CLI list/show for three native clients',flush=True)
        if a.experimental_account_recovery:
            assert a.experimental_codex_accounts and a.replacement, 'account recovery requires both experimental flags'
            name,first,marker,relay=({'native':('UI-Native','AH_LOCAL_C','AH_RESUME_C',native),
                                      'magpie':('UI-A','AH_LOCAL_FIRST','AH_LOCAL_SECOND',relay_a)}[a.experimental_account_recovery])
            before=next(r for r in records() if r['name']==name)
            popup();key('/');text(name);key('Tab');key('s');screen('Stop process');reviewed_confirm()
            wait(lambda:not next(r for r in records() if r['name']==name).get('process_group'),'experimental session stopped')
            def herdr_released_conversation():
                agents=json.loads(run([wrapper,'agent','list']).stdout)['result']['agents']
                return not any((agent.get('agent_session') or {}).get('value')==before['codex_session_id'] for agent in agents)
            wait(herdr_released_conversation,'Herdr releases stopped conversation',timeout=15)
            if 'Harbor · Profile:' in capture():key('q')
            popup();key('/');text(name);key('Enter');screen('Resume conversation');key('a');key('Enter')
            screen('Ask Codex');turn(marker,relay)
            wait(lambda:any(first in marks and marker in marks for _,marks in relay.observed),'experimental recovery history')
            after=next(r for r in records() if r['name']==name)
            assert after['codex_session_id']==before['codex_session_id'] and after.get('codex_server_pid') and after['codex_server_pid']!=before['codex_server_pid']
            print('PASS experimental App Server resumes the same conversation and history:',name,flush=True)
        external_session_id=None
        if a.full_state:
            previous={x['pane_id'] for x in json.loads(run([wrapper,'pane','list']).stdout)['result']['panes']}
            run([wrapper,'pane','split',three['herdr_pane'],'--direction','down','--focus','--cwd',root/'project'])
            manual=next(x['pane_id'] for x in json.loads(run([wrapper,'pane','list']).stdout)['result']['panes'] if x['pane_id'] not in previous)
            run([wrapper,'pane','run',manual,'codex --no-daemon -c check_for_update_on_startup=false'])
            # Another visible pane can already contain the same prompt. Wait
            # for this pane, and verify focus before sending terminal input.
            wait(lambda:'Ask Codex' in run([wrapper,'pane','read',manual]).stdout,'manual external Codex prompt')
            wait(lambda:any(x['pane_id']==manual for x in json.loads(run([wrapper,'agent','list']).stdout)['result']['agents']),'manual external Codex detected')
            focus=run([wrapper,'agent','focus',manual],check=False)
            assert focus.returncode==0, 'manual external focus failed: '+focus.stderr
            wait(lambda:any(x['pane_id']==manual and x.get('focused') for x in json.loads(run([wrapper,'pane','list']).stdout)['result']['panes']),'manual external pane focused')
            turn('AH_EXTERNAL_STATUS',native)
            def external_ready():
                views=json.loads(run([binary,'sessions','--json']).stdout)
                return next((x for x in views if x.get('external') and x['herdr_pane']==manual and x['runtime'].get('observed_reference')),None)
            extview=wait(external_ready,'manual external client in authoritative runtime')
            external_session_id=extview['runtime']['observed_reference']['value']
            assert extview['mode']=='unknown' and len(records())==3,'external client adopted'
            assert run([binary,'rename',extview['id'],'forbidden'],check=False).returncode!=0
            focus_result=run([binary,'focus',extview['id']],check=False)
            assert focus_result.returncode==0,focus_result.stdout+focus_result.stderr+json.dumps(extview['runtime'])
            upstream_external=next(x for x in json.loads(run([wrapper,'agent','list']).stdout)['result']['agents'] if x['pane_id']==manual)
            assert extview['runtime']['activity']==upstream_external['agent_status']
            run([wrapper,'agent','focus',three['herdr_pane']])
            print('PASS ordinary terminal manually launched external Codex is observable, focusable and read-only',flush=True)
        if a.live_state_probe or a.full_state:
            popup();key('/');text('UI-Native');key('Tab');screen('Idle')
            run([wrapper,'agent','prompt',three['herdr_pane'],'AH_LIVE_STATUS'])
            started_refresh=time.monotonic()
            wait(lambda:'Working' in capture(),'automatic working state',timeout=3)
            working_delay=time.monotonic()-started_refresh
            wait(lambda:'Idle' in capture() or 'Done' in capture(),'automatic completion state',timeout=9)
            sockpath=pathlib.Path(next(r for r in records() if r['name']=='UI-Native')['herdr_socket'])
            offline=sockpath.with_name(sockpath.name+'.fixture-offline')
            sockpath.rename(offline)
            try:
                wait(lambda:'discovery unavailable' in capture(),'automatic disconnect unknown',timeout=4)
                wait(lambda:next(r for r in records() if r['id']==three['id']).get('conversation',{}).get('status')=='association_lost','identity observer sees interrupted transport',timeout=6)
            finally:offline.rename(sockpath)
            wait(lambda:'discovery unavailable' not in capture() and ('Idle' in capture() or 'Done' in capture()),'automatic reconnect',timeout=6)
            assert 'UI-Native' in capture(),'refresh lost search or selection'
            (root/'live-state.json').write_text(json.dumps({'working_delay_seconds':working_delay,'reconnected':True}))
            key('q')
            print('PASS automatic real-client working/completion and owned socket disconnect/reconnect with selection retained',flush=True)
        if (a.state_probe or a.live_state_probe) and not a.full_state:return 0

        # Exit each CLI through Harbor's stop review and restart both Harbor and
        # its gateway. Conversation records must remain exact.
        # A server restart while CLIs are live exercises Herdr's actual shell
        # restoration, rather than closing all tabs before the restart.
        run([binary,'stop']);run([binary,'magpie','stop']);started=False
        if external:assert external.poll() is None, 'Harbor stopped external user-managed gateway'
        tm('kill-session','-t','app')
        run([binary,'magpie','start']);started=True
        open_workspace();screen('Harbor ^B a')
        # Reproduce opening the review from the very restored pane being
        # closed. Its popup may be terminated by that close request.
        run([wrapper,'tab','focus',one['herdr_tab']])
        wait(lambda:any(p['pane_id']==one['herdr_pane'] and p.get('focused') for p in json.loads(run([wrapper,'pane','list']).stdout)['result']['panes']),'restored pane focused before review')
        popup();key('/');text('UI-A');key('Tab');key('c');screen('Close inspected shell')
        key('Escape');key('c');screen('Close inspected shell');reviewed_confirm();time.sleep(.5)
        wait(lambda:next(r for r in records() if r['name']=='UI-A').get('ended_evidence'),'reviewed exit saved when closing its own popup')
        if 'Harbor · Profile:' in capture():key('q')
        print('PASS restored idle shell review, cancellation and explicit close',flush=True)
        for ended_name in ['UI-B','UI-Native']:
            popup();key('/');text(ended_name);key('Tab');key('c');screen('Close inspected shell');reviewed_confirm();time.sleep(.5)
            wait(lambda:next(r for r in records() if r['name']==ended_name).get('ended_evidence'),'reviewed exit '+ended_name)
            if 'Harbor · Profile:' in capture():key('q')
        print('PASS explicit restored-shell end evidence for all three original runs',flush=True)

        if a.replacement:
            assert not any(r['running'] for r in json.loads(run([binary,'sessions','--json']).stdout)), 'restart silently recovered a client'
            for rec in records():
                assert rec['conversation']['reference']==saved_references[rec['name']], 'restart changed full reference'
                if rec['conversation']['status']!='observed':
                    run([binary,'reassociate',rec['id'],json.dumps(saved_references[rec['name']]),'Reviewed original synthetic conversation after abrupt isolated workspace restart; retain exact native or Magpie intent'],input='yes\n')
            print('PASS restart retains exact references; unresolved final observations explicitly reviewed through public reassociation',flush=True)
        (root/'provider-a-restore.json').write_bytes((root/'harbor/magpie/bin/data/providers.json').read_bytes())
        if a.replacement:
            from replacement_negative_ui import exercise
            exercise(root,binary,managed,run,popup,key,text,screen,capture,records,wait,saved)
            audit_before={r['name']:r.get('reassociations',[]) for r in records()}
            (root/'audit-before-recovery.json').write_text(json.dumps(audit_before,indent=2))
        else:negative_ui(root,binary,managed,run,popup,key,text,screen,capture,records,wait,saved)
        assert {r['name']:(r['codex_session_id'],r.get('instance_id'),r.get('magpie_model')) for r in records()}==saved
        for name,marker,relay,first in [('UI-A','AH_LOCAL_SECOND',relay_a,'AH_LOCAL_FIRST'),('UI-B','AH_RESUME_B',relay_b,'AH_RELAY_B'),('UI-Native','AH_RESUME_C',native,'AH_LOCAL_C')]:
            popup();key('/');text(name);key('Enter');key('Enter');screen('Resume conversation');screen(name);
            if a.model_picker and name=='UI-A':screen('relay-a/test-model');screen('high')
            if a.replacement:
                screen('Current login and other writers: unverified.')
                key('i');screen('Details')
                if name=='UI-Native':
                    screen('Native route or login may have changed')
                    screen('A provider name does not verify its destination')
                assert not next(r for r in json.loads(run([binary,'sessions','--json']).stdout) if r['name']==name)['running'], 'Details launched before confirmation'
                key('Escape');screen('Resume conversation');key('Up')
            key('Enter');screen('Ask Codex');time.sleep(1);turn(marker,relay)
            wait(lambda:any(first in marks and marker in marks for _,marks in relay.observed),'history '+name)
            rec=next(r for r in records() if r['name']==name)
            expected_model='relay-a/test-model' if a.model_picker and name=='UI-A' else saved[name][2]
            assert (rec['codex_session_id'],rec.get('instance_id'),rec.get('magpie_model'))==(saved[name][0],saved[name][1],expected_model)
            if a.model_picker and name=='UI-A':
                assert any(e['kind']=='main' and marker in e['current'] and e['effort']=='high' for e in relay.journal), 'saved effort was not used after recovery'
            if a.replacement:
                wait(lambda:next(r for r in records() if r['name']==name).get('conversation',{}).get('status')=='observed','fresh official recovery observation '+name)
                recovered=next(r for r in records() if r['name']==name)
                assert recovered['conversation']['reference']==saved_references[name]
                assert recovered.get('reassociations',[])==audit_before[name],'recovery lost review audit'
            print('PASS resumed',name,flush=True)
        current_config=tomllib.loads((root/'codex/config.toml').read_text());current_config.pop('tui',None)
        if a.replacement:
            hook_state=current_config.pop('hooks',{})
            assert set(hook_state).issubset({'state'}), 'hook definitions unexpectedly changed'
        assert current_config==tomllib.loads(config), 'native connection config changed'
        if a.mcp:
            text('AH_MCP_PROBE');key('Enter')
            screen('Allow the herdr MCP server');key('Enter')
            wait(lambda:native.tool_output or native.tool_error,'real Codex MCP call')
            assert not native.tool_error and 'pane' in str(native.tool_output).lower(), 'Codex MCP discovery failed'
            parsed=native.tool_output
            # Every UI-created live pane must be visible through Codex's actual call.
            for rec in records():assert rec['herdr_pane'] in json.dumps(parsed), 'Codex MCP targeted another workspace'
            (root/'codex-mcp-output.json').write_text(json.dumps(parsed,indent=2))
            print('PASS real interactive Codex calls MCP and discovers this workspace panes',flush=True)
            node=shutil.which('node')
            assert node, 'node required for MCP acceptance'
            mcp_env=dict(env,HERDR_BIN=str(wrapper),HERDR_CONFIG_PATH=str(root/'harbor/herdr/config/herdr.toml'))
            for env_key,leaf in [('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime')]:
                mcp_env[env_key]=str(root/'harbor/herdr'/leaf)
            mcp=subprocess.Popen([node,str(pathlib.Path(a.mcp).resolve())],env=mcp_env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(root/'mcp-stderr.log','w'),text=True)
            def request(mid,method,params):
                import select
                mcp.stdin.write(json.dumps({'jsonrpc':'2.0','id':mid,'method':method,'params':params})+'\n');mcp.stdin.flush()
                assert select.select([mcp.stdout],[],[],30)[0], 'MCP response timeout'
                response=json.loads(mcp.stdout.readline())
                assert response.get('id')==mid and not response.get('error') and not response.get('result',{}).get('isError'),response
                return response
            try:
                request(1,'initialize',{'protocolVersion':'2025-03-26','capabilities':{},'clientInfo':{'name':'harbor-ui-acceptance','version':'1'}})
                mcp.stdin.write(json.dumps({'jsonrpc':'2.0','method':'notifications/initialized'})+'\n');mcp.stdin.flush()
                for rec in records():
                    run([binary,'rename',rec['id'],rec['name']])
                    named=json.loads(run([wrapper,'agent','get',rec['herdr_agent_name']]).stdout)
                    assert rec['herdr_pane'] in json.dumps(named), 'session name targets a different pane'
                agents=request(2,'tools/call',{'name':'herdr_agent_list','arguments':{}})
                discovered=json.loads(agents['result']['content'][0]['text'])['result']['agents']
                (root/'mcp-agents.json').write_text(json.dumps(agents,indent=2))
                for index,(name,marker,relay) in enumerate([('UI-A','AH_LOCAL_FIRST',relay_a),('UI-B','AH_RELAY_B',relay_b)]):
                    rec=next(r for r in records() if r['name']==name)
                    match=next(x for x in discovered if x['pane_id']==rec['herdr_pane'])
                    # Pinned Herdr exposes pane/tab identity, not Codex UUID.
                    assert match['tab_id']==rec['herdr_tab'], 'MCP tab mismatch'
                    connection_label='Codex' if rec.get('codex_connection')=='caller' else rec['magpie_model']
                    assert match.get('tokens',{}).get('harbor')=='Magpie managed / '+connection_label, 'MCP connection mismatch'
                    before=sum(marker in marks for _,marks in relay.observed)
                    request(10+index,'tools/call',{'name':'herdr_relay','arguments':{'target':rec['herdr_agent_name'],'text':marker}})
                    wait(lambda:sum(marker in marks for _,marks in relay.observed)>before,'MCP send '+name)
                    read=request(20+index,'tools/call',{'name':'herdr_agent_read','arguments':{'target':rec['herdr_agent_name']}})
                    assert marker in json.dumps(read), 'MCP read missing marker'
                print('PASS MCP exact pane/tab and connection discovery, targeted send and read',flush=True)
            finally:
                mcp.terminate();mcp.wait(timeout=10)
        # Retired history actions explain native tools and preserve the conversation.
        popup();key('/');text('UI-B');key('Tab');key('d');screen('Client history');key('Enter');key('q')
        assert any(r['name']=='UI-B' for r in records())
        popup();key('/');text('UI-B');key('Tab');key('s');screen('Stop process');reviewed_confirm();time.sleep(.5)
        if 'Harbor · Profile:' in capture():key('q')
        if a.replacement:
            stopped=json.loads(run([binary,'sessions','--json']).stdout)
            (root/'after-ui-stop.json').write_text(json.dumps(stopped,indent=2))
            rec=next(r for r in stopped if r['name']=='UI-B')
            if rec['running']:
                # Review again through the public CLI if the UI review became
                # stale while official identity observations were still arriving.
                result=run([binary,'stop-session',rec['id']],check=False,input='yes\n')
                (root/'explicit-stop-retry.txt').write_text(result.stdout+result.stderr)
                assert result.returncode==0, 'explicit stop retry failed'
            wait(lambda:not next(r for r in json.loads(run([binary,'sessions','--json']).stdout) if r['name']=='UI-B')['running'],'stopped UI-B before removal')
        history_before = {str(p): p.read_bytes() for p in (root/'codex/sessions').rglob('*'+saved['UI-B'][0]+'*') if p.is_file()}
        assert history_before
        popup();key('/');text('UI-B');key('Tab');key('x');screen('Remove record');reviewed_confirm()
        if a.replacement:
            # A visible busy response must never be bypassed by touching the lock.
            # Record only fixture lock holders, then retry the same public action.
            rec=next((r for r in records() if r['name']=='UI-B'),None)
            if rec:
                lock=root/'harbor'/('running-'+rec['id']+'.lock')
                if fuser:=shutil.which('fuser'):
                    holders=run([fuser,lock],check=False)
                    (root/'record-removal-lock-holders.txt').write_text(holders.stdout+holders.stderr)
                wait(lambda:run([binary,'remove',rec['id']],check=False,input='yes\n').returncode==0,'public record removal after runner cleanup',timeout=25)
        wait(lambda:all(r['name']!='UI-B' for r in records()),'record removal')
        assert all(pathlib.Path(p).read_bytes()==data for p,data in history_before.items())
        if 'Harbor · Profile:' in capture():key('q')
        assert sum(e['kind']=='auxiliary-title' for relay in servers for e in relay.journal)>0, 'no real auxiliary request exercised'
        for label,relay,name in [('A',relay_a,'UI-A'),('B',relay_b,'UI-B'),('C',native,'UI-Native')]:
            assert all(e['session'] in ({saved[name][0],external_session_id} if label=='C' else {saved[name][0]}) for e in relay.journal if e['kind']=='main'), 'main request thread differs from recorded conversation'
            (root/('requests-'+label+'.json')).write_text(json.dumps(relay.journal,indent=2))
            assert not any(e['kind']=='unclassified' and e['history'] for e in relay.journal), 'marked request could not be classified'
        print('PASS structured main/auxiliary classification and main markers absent from wrong upstreams',flush=True)
        (root/'acceptance.json').write_text(json.dumps({'passed':True,'sessions':saved,'native_config_preserved':current_config==tomllib.loads(config)},indent=2))
        print('PASS UI resumes preserve conversations, connections and upstream history across Herdr/Magpie restart',root,flush=True)
    finally:
        for r in records():
            run([binary,'stop-session',r['id']],check=False,input='yes\n')
        run([binary,'stop'],check=False)
        if started:run([binary,'magpie','stop'],check=False)
        if tmux:run([tmux,'-S',sock,'kill-server'],check=False)
        stop_codex(root,env)
        if external:
            external.terminate();external.wait(timeout=10);stop_codex(root/'external',ext_env)
        for i,s in enumerate(servers):
            (root/('request-evidence-'+str(i)+'.json')).write_text(json.dumps(s.journal,indent=2))
            s.shutdown();s.server_close()
        if discovery:discovery.shutdown();discovery.server_close()
    return 0
if __name__=='__main__':sys.exit(main())
