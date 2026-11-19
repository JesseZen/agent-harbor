#!/usr/bin/env python3
"""Isolated real Codex query-recovery acceptance; copied native binaries only."""
import argparse, getpass, http.server, os, pathlib, shlex, shutil, socket, subprocess, sys, tempfile, threading, time, json
from e2e_local import MockResponses, FIRST, SECOND
from fixture_process import stop_codex

p=argparse.ArgumentParser();p.add_argument('--skip-identity',action='store_true',help='verify explicitly skipped setup cannot enter an invalid recovery confirmation');p.add_argument('--preinstalled',action='store_true',help='prepare hooks in the fixture rather than through Harbor startup');p.add_argument('--enter-recovery',action='store_true',help='test live focus, restored shell, stale review and exact recovery through Enter');p.add_argument('--width',type=int,default=160);p.add_argument('--height',type=int,default=45);p.add_argument('--color',action='store_true');p.add_argument('--magpie',help='explicit Magpie dependency executable');p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--work-root',required=True);a=p.parse_args()
if a.preinstalled and a.skip_identity:p.error('--skip-identity requires startup setup')
# The fixture supplies its own discovery address through Magpie's normal config.
class DiscoveryHandler(http.server.BaseHTTPRequestHandler):
 def do_GET(self): self.send_error(404)
 def log_message(self,*args): pass
try:
 discovery=http.server.ThreadingHTTPServer(('127.0.0.1',0),DiscoveryHandler)
except OSError:
 print('SKIP: standard discovery port is in use; no existing service contacted')
 raise SystemExit(77)
threading.Thread(target=discovery.serve_forever,daemon=True).start()
root=pathlib.Path(tempfile.mkdtemp(prefix='q',dir=a.work_root)).resolve()
env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TERM':'xterm-256color','SHELL':'/bin/sh','LANG':'en_US.UTF-8','NO_COLOR':'1'}
for key,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
 (root/leaf).mkdir(mode=0o700);env[key]=str(root/leaf)
if a.color:
 env.pop('NO_COLOR');env['COLORTERM']='truecolor'
if a.magpie:env['AGENT_HARBOR_MAGPIE_BIN']=str(pathlib.Path(a.magpie).resolve())
(root/'config/magpie').mkdir()
(root/'config/magpie/settings.json').write_text(json.dumps({'port':discovery.server_port}))
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
(root/'bin').mkdir();shutil.copy2(a.binary,root/'bin/agent-harbor');shutil.copy2(a.herdr,root/'bin/herdr')
native=pathlib.Path(a.codex).resolve()
with native.open('rb') as binary:
 assert binary.read(4) in (b'\x7fELF',b'\xcf\xfa\xed\xfe',b'\xca\xfe\xba\xbe'), 'use a native Codex binary, never the npm wrapper'
codex_copy=root/'bin/codex'
if sys.platform=='darwin':
 subprocess.run(['/bin/cp','-c',native,codex_copy],check=True)
else:
 shutil.copy2(native,codex_copy)
codex_copy.chmod(0o700)
(root/'bin/herdr').chmod(0o700)
(root/'bin/agent-harbor').chmod(0o700)
env['PATH']=str(root/'bin')+':'+env['PATH'];env['AGENT_HARBOR_HERDR_BIN']=str(root/'bin/herdr');env['AGENT_HARBOR_BRIDGE_HOME']=str(root/'h')
tmux=shutil.which('tmux');sock=str(root/'tmux.sock')
def run(args,check=True):
 result=subprocess.run(list(map(str,args)),env=env,text=True,capture_output=True)
 if check and result.returncode:raise RuntimeError('command failed '+str(args)+': '+result.stdout+result.stderr)
 return result
def tm(*args):return run([tmux,'-S',sock,*args]).stdout
def capture():return tm('capture-pane','-p','-t','app:0.0')
def capture_ansi(label,width=None):
 screen=tm('capture-pane','-e','-p','-t','app:0.0')
 for original,public in ((socket.gethostname().split('.')[0],'fixture-host'),(getpass.getuser()+'$','fixture$')):
  screen=screen.replace(original,public)
 (root/f'{label}-{width or a.width}.ansi').write_text(screen)
def wait(text):
 end=time.time()+20
 while time.time()<end:
  out=capture()
  if text in out:return out
  time.sleep(.2)
 raise RuntimeError('missing '+text+'\n'+capture())

server=http.server.ThreadingHTTPServer(('127.0.0.1',0),MockResponses);server.observed=[]
threading.Thread(target=server.serve_forever,daemon=True).start()
(root/'project').mkdir()
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\ncli_auth_credentials_store = "file"\nmodel = "test-model"\nmodel_provider = "fixture"\n[features]\nhooks = true\n[model_providers.fixture]\nname = "fixture"\nbase_url = "http://127.0.0.1:'+str(server.server_port)+'/v1"\nrequires_openai_auth = false\nwire_api = "responses"\n[projects.'+json.dumps(str(root/'project'))+']\ntrust_level = "trusted"\n')
def key(*keys):tm('send-keys','-t','app:0.0',*keys);time.sleep(.25)
def text(value):key('-l',value);time.sleep(.8)
def records():
 path=root/'h/sessions.json'
 return json.loads(path.read_text()) if path.exists() else []
def until(predicate,label,timeout=40):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  if predicate():return
  time.sleep(.2)
 (root/'failure-screen.txt').write_text(capture());raise RuntimeError('timeout '+label)
try:
 codex_version=run([root/'bin/codex','--version']).stdout.strip()
 herdr_version=run([root/'bin/herdr','--version']).stdout.strip()
 if a.preinstalled:run([root/'bin/herdr','integration','install','codex'])
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nread answer\n')
 launcher.chmod(0o700)
 def open_workspace():
  tm('new-session','-d','-s','app','-x',str(a.width),'-y',str(a.height),str(launcher))
  wrapper=root/'h/herdr/bin/herdr'
  until(lambda:wrapper.exists() and run([wrapper,'pane','list'],check=False).returncode==0,'managed workspace')
 open_workspace()
 key('C-b','a');wait('Search sessions');key('n');wait('New session')
 key('C-s')
 if not a.preinstalled:
  wait('Conversation recovery')
  tabs_before=run([root/'h/herdr/bin/herdr','tab','list']).stdout
  key('Escape');wait('New session')
  assert not records(), 'cancelled setup published a session record'
  assert run([root/'h/herdr/bin/herdr','tab','list']).stdout==tabs_before, 'cancelled setup opened a terminal'
  (root/'cancelled-setup.ansi').write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
  key('C-s');wait('Conversation recovery');key('s' if a.skip_identity else 'y')
 until(lambda:'Ask Codex' in capture() or 'Hooks need review' in capture(),'Codex startup or hook trust')
 if 'Hooks need review' in capture():key('Down','Enter')
 wait('Ask Codex');time.sleep(2)
 (root/'initial-screen.txt').write_text(capture())
 if 'hook needs review' in capture() or 'hook needs' in capture():
  key('t');time.sleep(.5);key('Escape');time.sleep(.5)
 text(FIRST);key('Enter')
 time.sleep(1)
 if 't trust all' in capture():
  key('t');time.sleep(.5);key('Escape');time.sleep(.5);text(FIRST);key('Enter')
 until(lambda:any(FIRST in marks for _,marks in server.observed),'first native turn')
 wait('HELLO_FROM_ISOLATED_MOCK')
 if a.skip_identity:
  assert not records()[0].get('conversation'), 'skipped setup inferred identity'
  text('/exit');key('Enter');until(lambda:records()[0].get('process_group',0)==0,'exit without identity')
  key('C-b','a');wait('Search sessions');key('Enter');time.sleep(.5)
  page=wait('Conversation identity was not captured')
  assert 'Resume conversation' not in page and 'Recheck' in page, 'offered impossible recovery'
  (root/'missing-identity-screen.txt').write_text(page)
  print('PASS skipped identity setup preserves missing reference and blocks invalid UI recovery before confirmation',codex_version,herdr_version,root,flush=True)
  raise SystemExit(0)
 until(lambda:records()[0].get('conversation'),'upstream reference')
 rec=records()[0];assert rec['conversation']['reference']['value']==rec['codex_session_id']
 managed=root/'h/herdr/bin/herdr'
 if a.enter_recovery:
  def panes():return json.loads(run([managed,'pane','list']).stdout)['result']['panes']
  def focused(pane):return any(value['pane_id']==pane and value.get('focused') for value in panes())
  def popup():key('C-b','a');wait('Search sessions')
  def select():key('Enter');wait('Resume conversation');time.sleep(.3)
  current_rec=records()[0]
  other_tab=next(value['tab_id'] for value in panes() if value['pane_id']!=rec['herdr_pane'])
  run([managed,'tab','focus',other_tab]);until(lambda:not focused(rec['herdr_pane']),'unrelated tab focus')
  popup();until(lambda: any(label in capture() for label in ('↵ Terminal','↵ Enter terminal')),'visible terminal action');capture_ansi('live-sessions')
  views=json.loads(run([root/'bin/agent-harbor','sessions','--json']).stdout)
  assert len(views)==1 and views[0]['id']==rec['id'], 'live run appeared more than once'
  key('s');wait('Stop process');wait('The conversation stays resumable');capture_ansi('stop-review')
  key('Escape');wait('Search sessions')
  key('Enter');until(lambda:focused(rec['herdr_pane']) and 'Search sessions' not in capture(),'Enter focuses original pane')
  assert records()[0]['conversation']['reference']==current_rec['conversation']['reference']
  print('PASS Enter focused the one live run from a different tab',flush=True)
  run([root/'bin/agent-harbor','stop']);tm('kill-session','-t','app');open_workspace()
  until(lambda:any(value['pane_id']==rec['herdr_pane'] for value in panes()),'restored original shell')
  before=next(value for value in panes() if value['pane_id']==rec['herdr_pane'])
  run([managed,'pane','run',rec['herdr_pane'],'/bin/sleep 30'])
  popup();key('Enter');wait('Recovery blocked');wait('View old terminal');capture_ansi('blocked-recovery')
  key('o');until(lambda:focused(rec['herdr_pane']) and 'Search sessions' not in capture(),'view verified old pane')
  key('C-c')
  run([managed,'tab','focus',rec['herdr_tab']]);popup();select();wait('The old shell stays open')
  capture_ansi('restored-recovery');key('Escape');wait('Search sessions')
  assert not records()[0].get('ended_evidence'), 'cancel wrote end evidence'
  select()
  config=root/'codex/config.toml';original=config.read_bytes()
  endpoint=f'127.0.0.1:{server.server_port}'.encode()
  assert endpoint in original
  config.write_bytes(original.replace(endpoint,b'127.0.0.1:1',1))
  key('Enter');wait('Recovery blocked');wait('Recheck');capture_ansi('stale-recovery')
  assert not records()[0].get('ended_evidence'), 'stale confirmation wrote end evidence'
  assert b'hooks = true' in original
  config.write_bytes(original.replace(b'hooks = true',b'hooks = false',1))
  blocked_record=records()[0];blocked_panes={value['pane_id'] for value in panes()}
  key('r');wait('Codex recovery hooks are disabled');wait('Recheck');capture_ansi('blocked-hooks')
  assert records()[0]==blocked_record and {value['pane_id'] for value in panes()}==blocked_panes, 'hook check launched or changed the session'
  config.write_bytes(original)
  key('r');wait('Resume conversation');time.sleep(.3);key('Enter')
  until(lambda:records()[0]['herdr_pane']!=rec['herdr_pane'] and focused(records()[0]['herdr_pane']),'recovery pane focused')
  wait(FIRST);wait('Ask Codex');time.sleep(1)
  text(SECOND);key('Enter');until(lambda:any(FIRST in marks and SECOND in marks for _,marks in server.observed),'exact recovered context')
  recovered=records()[0]
  assert recovered['codex_session_id']==rec['codex_session_id'] and recovered['profile']==rec['profile'] and recovered['mode']==rec['mode']
  assert recovered['herdr_pane']!=rec['herdr_pane'] and any(value['terminal_id']==before['terminal_id'] for value in panes()), 'original shell was closed or replaced'
  text('/exit');key('Enter');until(lambda:records()[0].get('process_group',0)==0,'recovered client exit')
  state_path=root/'h/sessions.json';altered=records()[0]
  reported=altered['conversation']['reference'].copy()
  original={'source':'herdr:codex','agent':'codex','kind':'id','value':'11111111-1111-4111-8111-111111111111'}
  altered['conversation'].update(reference=original,observed_reference=reported,status='conflict')
  altered.update(codex_session_id=original['value'],reported_codex_session_id=reported['value'],recovery='needs_review')
  state_path.write_text(json.dumps([altered]))
  popup();key('Enter');wait('Choose recovery target');wait('Originally saved');wait('Last reported');capture_ansi('conflict-recovery')
  time.sleep(.3);key('Enter');wait('Choose recovery target');assert records()==[altered], 'Enter without a choice wrote a target'
  key('Down');wait('> Last reported')
  if a.width==160:
   tm('resize-window','-t','app:0','-x','60','-y','24')
   until(lambda: all(len(line.rstrip())<=60 for line in capture().splitlines()) and '> Last reported' in capture(),'selected target at 60 columns')
   wait('↑↓/jk select');capture_ansi('conflict-resized',60)
   tm('resize-window','-t','app:0','-x',str(a.width),'-y',str(a.height));wait('> Last reported')
  key('Enter');wait(SECOND);wait('Ask Codex')
  final=records()[0]
  assert final['codex_session_id']==reported['value'] and final['profile']==rec['profile'] and final['mode']==rec['mode']
  assert final['reassociations'][-1]['original_record']['conversation']['reference']==original
  print('PASS replayed legacy conflict required a choice and resumed the last reported real Codex history',flush=True)
  print('PASS restored shell retained, cancellation left record unchanged, stale endpoint and disabled hooks blocked recovery, exact Native conversation and Profile recovered',codex_version,herdr_version,root,flush=True)
  raise SystemExit(0)
 # Normal client exit must preserve the fresh official identity.
 text('/exit');key('Enter')
 until(lambda:records()[0].get('process_group',0)==0,'exit')
 assert records()[0]['conversation']['status']=='observed', 'normal exit lost official identity'
 rec=records()[0]
 # Launch the public development command inside the managed workspace.
 script=root/'query.sh'
 script.write_text('#!/bin/sh\nprintf "accept\\nyes\\n" | '+shlex.quote(str(root/'bin/agent-harbor'))+' resume --development-query '+rec['id']+'\n')
 script.chmod(0o700)
 (root/'panes.json').write_text(run([managed,'pane','list']).stdout)
 panes=json.loads((root/'panes.json').read_text())['result']['panes']
 assert len(panes)==1
 run([managed,'pane','run',panes[0]['pane_id'],str(script)])
 wait(FIRST)
 text(SECOND);key('Enter')
 until(lambda:any(FIRST in marks and SECOND in marks for _,marks in server.observed),'exact recovered context')
 assert records()[0]['codex_session_id']==rec['codex_session_id']
 print('PASS startup identity preparation, normal exit and real Codex query recovery, same native connection and context',codex_version,herdr_version,root,flush=True)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 stop_codex(root,env)
 server.shutdown();discovery.shutdown()
 print('diagnostics',root,flush=True)
