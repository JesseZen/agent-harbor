#!/usr/bin/env python3
"""Isolated real Codex query-recovery acceptance; copied native binaries only."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time, json, platform
from e2e_local import MockResponses, FIRST, SECOND
from fixture_process import stop_codex

p=argparse.ArgumentParser();p.add_argument('--replacement',action='store_true');p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--claude',required=True);p.add_argument('--work-root',required=True);a=p.parse_args()
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
env['AGENT_HARBOR_IDENTITY_CANDIDATE']='1'
if a.replacement:env['AGENT_HARBOR_REPLACEMENT_CANDIDATE']='1'
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


# Independent native Claude fixture: synthetic API key, local Messages service.
shutil.copy2(a.claude,root/'bin/claude');(root/'bin/claude').chmod(0o700)
(root/'claude').mkdir()
env.update(CLAUDE_CONFIG_DIR=str(root/'claude'),DISABLE_AUTOUPDATER='1',CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC='1',ANTHROPIC_API_KEY='harbor-synthetic-fixture')
class Messages(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  body=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
  marks=json.dumps(body.get('messages',[]))
  self.server.observed.append(marks)
  if self.path.endswith('/count_tokens'):
   result=json.dumps({'input_tokens':10}).encode();self.send_response(200);self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(result);return
  message={'id':'msg_fixture','type':'message','role':'assistant','model':'claude-sonnet-4-6','content':[{'type':'text','text':'HARBOR_CLAUDE_REPLY'}],'stop_reason':'end_turn','stop_sequence':None,'usage':{'input_tokens':10,'output_tokens':4}}
  self.send_response(200)
  if body.get('stream'):
   self.send_header('Content-Type','text/event-stream');self.end_headers()
   events=[('message_start',{'type':'message_start','message':dict(message,content=[],stop_reason=None)}),('content_block_start',{'type':'content_block_start','index':0,'content_block':{'type':'text','text':''}}),('content_block_delta',{'type':'content_block_delta','index':0,'delta':{'type':'text_delta','text':'HARBOR_CLAUDE_REPLY'}}),('content_block_stop',{'type':'content_block_stop','index':0}),('message_delta',{'type':'message_delta','delta':{'stop_reason':'end_turn','stop_sequence':None},'usage':{'output_tokens':4}}),('message_stop',{'type':'message_stop'})]
   for event,data in events:self.wfile.write(('event: '+event+'\ndata: '+json.dumps(data)+'\n\n').encode())
  else:
   self.send_header('Content-Type','application/json');self.end_headers();self.wfile.write(json.dumps(message).encode())
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Messages);server.observed=[]
threading.Thread(target=server.serve_forever,daemon=True).start()
env['ANTHROPIC_BASE_URL']='http://127.0.0.1:'+str(server.server_port)
(root/'project').mkdir()
(root/'claude/.claude.json').write_text(json.dumps({'hasCompletedOnboarding':True,'theme':'dark','projects':{str(root/'project'):{'hasTrustDialogAccepted':True}}}))
def key(*keys):tm('send-keys','-t','app:0.0',*keys);time.sleep(.5)
def text(value):key('-l',value)
def records():return json.loads((root/'h/sessions.json').read_text()) if (root/'h/sessions.json').exists() else []
def until(predicate,label,timeout=40):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  if predicate():return
  time.sleep(.2)
 (root/'failure-screen.txt').write_text(capture());raise RuntimeError('timeout '+label)
def claude_ready(stage):
 # Observe the actual interactive client, not elapsed startup time. Slow native
 # startup must never receive a conversation marker as a prompt answer.
 deadline=time.monotonic()+90
 while time.monotonic()<deadline:
  view=capture()
  if 'Do you want to use this API key?' in view:
   (root/(stage+'-api-key-screen.txt')).write_text(view)
   key('Up');key('Enter')  # explicitly approve only the fixture's synthetic key
   until(lambda:'Do you want to use this API key?' not in capture(),'synthetic API key choice accepted')
   continue
  if 'Do you trust the files in this folder?' in view or 'Yes, I trust this folder' in view:
   (root/(stage+'-trust-screen.txt')).write_text(view)
   key('Enter')
   until(lambda:'Yes, I trust this folder' not in capture() and 'Do you trust the files in this folder?' not in capture(),'fixture project trust accepted')
   continue
  if 'Not logged in' in view:
   (root/'failure-screen.txt').write_text(view)
   raise RuntimeError('Claude rejected synthetic fixture authentication before '+stage)
  # A blank Claude input prompt is distinct from selection prompts and from
  # the Harbor launch banner or shell prompt visible while startup is pending.
  if 'Claude Code v' in view and any(line.rsplit('│',1)[-1].strip()=='❯' for line in view.splitlines()):
   (root/(stage+'-ready-screen.txt')).write_text(view)
   return
  time.sleep(.2)
 (root/'failure-screen.txt').write_text(capture())
 raise RuntimeError('timeout waiting for interactive Claude input: '+stage)

try:
 run([root/'bin/herdr','integration','install','claude'])
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+k+'='+shlex.quote(v)+'\n' for k,v in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nread answer\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('Harbor ^B a')
 managed=root/'h/herdr/bin/herdr'
 panes=json.loads(run([managed,'pane','list']).stdout)['result']['panes']
 script=root/'create.sh';script.write_text('#!/bin/sh\n'+shlex.quote(str(root/'bin/agent-harbor'))+' create --dir '+shlex.quote(str(root/'project'))+' --client claude\n');script.chmod(0o700)
 # pane.run uses the managed shell; create opens the generic plugin runner.
 run([managed,'pane','run',panes[0]['pane_id'],str(script)])
 claude_ready('claude-start')
 text('HARBOR_FIRST_NATIVE_CLAUDE');key('Enter')
 until(lambda:any('HARBOR_FIRST_NATIVE_CLAUDE' in m for m in server.observed),'first Claude turn')
 until(lambda:records()[0].get('conversation',{}).get('status')=='observed','official Claude identity')
 rec=records()[0]
 assert rec['cli']=='claude' and rec['conversation']['reference']['agent']=='claude'
 assert rec['codex_session_id']=='' if 'codex_session_id' in rec else True
 runstop=subprocess.run([root/'bin/agent-harbor','stop-session',rec['id']],env=env,input='yes\n',text=True,capture_output=True,check=False)
 assert runstop.returncode==0,runstop.stdout+runstop.stderr
 until(lambda:records()[0].get('process_group',0)==0,'exit')
 assert records()[0]['recovery']=='ready'
 script=root/'resume.sh';script.write_text('#!/bin/sh\nprintf "accept\\nyes\\n" | '+shlex.quote(str(root/'bin/agent-harbor'))+' resume --development-query '+rec['id']+'\n');script.chmod(0o700)
 panes=json.loads(run([managed,'pane','list']).stdout)['result']['panes']
 run([managed,'pane','run',panes[0]['pane_id'],str(script)])
 claude_ready('claude-resume')
 text('HARBOR_SECOND_NATIVE_CLAUDE');key('Enter')
 until(lambda:any('HARBOR_FIRST_NATIVE_CLAUDE' in m and 'HARBOR_SECOND_NATIVE_CLAUDE' in m for m in server.observed),'exact resumed Claude context')
 assert records()[0]['conversation']['reference']==rec['conversation']['reference']
 print('PASS real interactive Claude native launch official identity exit exact query recovery',platform.system(),platform.machine(),flush=True)
 until(lambda:len(records())>0,'record')
 print('diagnostics',root,flush=True)
 time.sleep(1)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 stop_codex(root,env)
 server.shutdown();discovery.shutdown()
 print('diagnostics',root,flush=True)
