#!/usr/bin/env python3
"""Isolated real Codex query-recovery acceptance; copied native binaries only."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time, json
from e2e_local import MockResponses, FIRST, SECOND
from fixture_process import stop_codex

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--work-root',required=True);a=p.parse_args()
# Exclusively own the discovery address; never probe an existing service.
class DiscoveryHandler(http.server.BaseHTTPRequestHandler):
 def do_GET(self): self.send_error(404)
 def log_message(self,*args): pass
try:
 discovery=http.server.ThreadingHTTPServer(('127.0.0.1',3425),DiscoveryHandler)
except OSError:
 print('SKIP: standard discovery port is in use; no existing service contacted')
 raise SystemExit(77)
threading.Thread(target=discovery.serve_forever,daemon=True).start()
root=pathlib.Path(tempfile.mkdtemp(prefix='q',dir=a.work_root)).resolve()
env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TERM':'xterm-256color','SHELL':'/bin/sh','LANG':'en_US.UTF-8','NO_COLOR':'1'}
for key,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
 (root/leaf).mkdir(mode=0o700);env[key]=str(root/leaf)
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
(root/'bin').mkdir();shutil.copy2(a.binary,root/'bin/agent-harbor');shutil.copy2(a.herdr,root/'bin/herdr')
native=pathlib.Path(a.codex)
assert native.read_bytes()[:4] in (b'\x7fELF',b'\xcf\xfa\xed\xfe',b'\xca\xfe\xba\xbe'), 'use a native Codex binary, never the npm wrapper'
shutil.copy2(native,root/'bin/codex');(root/'bin/codex').chmod(0o700)
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
def records():return json.loads((root/'h/sessions.json').read_text())
def until(predicate,label,timeout=40):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  if predicate():return
  time.sleep(.2)
 (root/'failure-screen.txt').write_text(capture());raise RuntimeError('timeout '+label)
try:
 run([root/'bin/herdr','integration','install','codex'])
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nread answer\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('First run');wait('codex-cli 0.167.0');wait('herdr 0.9.5')
 key('Enter');wait('Review: local');key('Enter');wait('Harbor ^B a')
 key('C-b','a');wait('HARBOR');key('n');wait('New session')
 key('Enter');wait('Ask Codex');time.sleep(2)
 (root/'initial-screen.txt').write_text(capture())
 if 'hook needs review' in capture() or 'hook needs' in capture():
  key('t');time.sleep(.5);key('Escape');time.sleep(.5)
 text(FIRST);key('Enter')
 time.sleep(1)
 if 't trust all' in capture():
  key('t');time.sleep(.5);key('Escape');time.sleep(.5);text(FIRST);key('Enter')
 until(lambda:any(FIRST in marks for _,marks in server.observed),'first native turn')
 wait('HELLO_FROM_ISOLATED_MOCK')
 until(lambda:records()[0].get('conversation'),'upstream reference')
 rec=records()[0];assert rec['conversation']['reference']['value']==rec['codex_session_id']
 # stop explicitly below
 # Stop requires explicit approval.
 subprocess.run([root/'bin/agent-harbor','stop-session',rec['id']],env=env,input='yes\n',text=True,capture_output=True,check=True)
 until(lambda:records()[0].get('process_group',0)==0,'exit')
 # Existing identity review remains mandatory; bind preserves neutral provenance.
 run([root/'bin/agent-harbor','bind',rec['id'],rec['codex_session_id']])
 rec=records()[0]
 managed=root/'h/herdr/bin/herdr'
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
 print('PASS real interactive Codex query recovery, same native connection and context; Linux amd64',root,flush=True)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 stop_codex(root,env)
 server.shutdown();discovery.shutdown()
 print('diagnostics',root,flush=True)
