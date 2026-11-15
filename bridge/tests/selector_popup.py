#!/usr/bin/env python3
"""Isolated real Magpie selector creation in Herdr; no user credentials."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--magpie',required=True);p.add_argument('--no-color',action='store_true');a=p.parse_args()
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
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor06-smoke-',dir='/tmp')).resolve()
env={'PATH':'/usr/bin:/bin:/usr/sbin:/sbin','TERM':'xterm-256color','SHELL':'/bin/sh','LANG':'en_US.UTF-8','COLORTERM':'truecolor'}
if a.no_color:env['NO_COLOR']='1'
for key,leaf in [('HOME','home'),('XDG_CONFIG_HOME','config'),('XDG_DATA_HOME','data'),('XDG_STATE_HOME','state'),('XDG_CACHE_HOME','cache'),('XDG_RUNTIME_DIR','runtime'),('TMPDIR','tmp'),('CODEX_HOME','codex'),('CODEX_SQLITE_HOME','sqlite')]:
 (root/leaf).mkdir(mode=0o700);env[key]=str(root/leaf)
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
(root/'bin').mkdir();shutil.copy2(a.binary,root/'bin/agent-harbor');shutil.copy2(a.herdr,root/'bin/herdr')
native=pathlib.Path(a.codex)
assert native.read_bytes()[:4] in (b'\x7fELF',b'\xcf\xfa\xed\xfe',b'\xca\xfe\xba\xbe'), 'use a native Codex binary, never the npm wrapper'
shutil.copy2(native,root/'bin/codex');(root/'bin/codex').chmod(0o700)
(root/'bin/herdr').chmod(0o700)
(root/'bin/agent-harbor').chmod(0o700)
env['PATH']=str(root/'bin')+':'+env['PATH'];env['AGENT_HARBOR_HERDR_BIN']=str(root/'bin/herdr');env['AGENT_HARBOR_BRIDGE_HOME']=str(root/'harbor')
shutil.copy2(a.magpie,root/'bin/magpie');env['AGENT_HARBOR_MAGPIE_BIN']=str(root/'bin/magpie')
external=None;external2=None;provider=None
tmux=shutil.which('tmux');sock=str(root/'tmux.sock')
def run(args,check=True):return subprocess.run(list(map(str,args)),env=env,text=True,capture_output=True,check=check)
def tm(*args):return run([tmux,'-S',sock,*args]).stdout
def capture():return tm('capture-pane','-p','-t','app:0.0')
def wait(text):
 end=time.time()+20
 while time.time()<end:
  out=capture()
  if text in out:return out
  time.sleep(.2)
 raise RuntimeError('missing '+text+'\n'+capture())
try:
 (root/'project').mkdir()
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'cd '+shlex.quote(str(root/'project'))+'\nagent-harbor\nprintf \"exit %s\\n\" \"$?\"\nexec /bin/sh\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('First run')
 wait(run([root/'bin/codex','--version']).stdout.strip())
 out=wait('herdr 0.9.5');(root/'onboarding.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Enter');wait('Review: local')
 tm('send-keys','-t','app:0.0','Enter')
 out=wait('Harbor ^B a')

 def keys(*items):tm('send-keys','-t','app:0.0',*items);time.sleep(.5)
 import json,unicodedata
 def click(label,button=0):
  lines=capture().splitlines();y=next(i for i,line in enumerate(lines) if label in line)
  prefix=lines[y][:lines[y].index(label)];x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
  tm('send-keys','-t','app:0.0','-l',f'\x1b[<{button};{x};{y+1}M'+(f'\x1b[<0;{x};{y+1}m' if button==0 else ''));time.sleep(.4)
 def save(name):
  (root/(name+'.ansi')).write_text(tm('capture-pane','-e','-p','-t','app:0.0'))
 keys('C-b');keys('a');wait('HARBOR')
 import urllib.request, socket
 class Provider(http.server.BaseHTTPRequestHandler):
  def do_GET(self):
   data=json.dumps({'data':[{'id':'fixture-model','object':'model'}]}).encode()
   self.send_response(200);self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
  def log_message(self,*args):pass
 provider=http.server.ThreadingHTTPServer(('127.0.0.1',0),Provider)
 threading.Thread(target=provider.serve_forever,daemon=True).start()
 ext_env=dict(env)
 for key in ['HOME','XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_STATE_HOME','XDG_CACHE_HOME','CODEX_HOME','TMPDIR']:
  directory=root/('external-'+key.lower());directory.mkdir();ext_env[key]=str(directory)
 with socket.socket() as sockport:
  sockport.bind(('127.0.0.1',0));port=sockport.getsockname()[1]
 ext_env.update(MAGPIE_ADDR='127.0.0.1:'+str(port),MAGPIE_NO_ACCOUNTS='1',MAGPIE_CODEX_CALLER_ONLY='1')
 subprocess.run([root/'bin/magpie','provider','add','fixture','url=http://127.0.0.1:'+str(provider.server_port)+'/v1','key=synthetic-only','models=fixture-model'],env=ext_env,check=True,capture_output=True)
 subprocess.run([root/'bin/magpie','group','add','fixture-group','models=fixture/fixture-model','routing=rotate','stays=off'],env=ext_env,check=True,capture_output=True)
 external=subprocess.Popen([root/'bin/magpie','serve'],env=ext_env,stdout=open(root/'external.log','w'),stderr=subprocess.STDOUT)
 address='http://127.0.0.1:'+str(port)
 for attempt in range(100):
  try:
   with urllib.request.urlopen(address+'/api/hello',timeout=1) as response:hello=json.load(response)
   break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('external did not start')
 assert hello['codex_caller_only'] is True
 run([root/'bin/agent-harbor','magpie','bind','--address',address])
 registry=json.loads((root/'harbor/magpie.json').read_text())
 external_id=next(i['id'] for i in registry['instances'] if i['address']==address)
 # A second independently verified gateway proves consent is per instance.
 other_env=dict(ext_env)
 for key in ['HOME','XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_STATE_HOME','XDG_CACHE_HOME','CODEX_HOME','TMPDIR']:
  directory=root/('other-'+key.lower());directory.mkdir();other_env[key]=str(directory)
 with socket.socket() as other_port:
  other_port.bind(('127.0.0.1',0));port2=other_port.getsockname()[1]
 other_env['MAGPIE_ADDR']='127.0.0.1:'+str(port2)
 subprocess.run([root/'bin/magpie','provider','add','other','url=http://127.0.0.1:'+str(provider.server_port)+'/v1','key=synthetic-only','models=fixture-model'],env=other_env,check=True,capture_output=True)
 external2=subprocess.Popen([root/'bin/magpie','serve'],env=other_env,stdout=open(root/'other.log','w'),stderr=subprocess.STDOUT)
 address2='http://127.0.0.1:'+str(port2)
 for attempt in range(100):
  try:
   with urllib.request.urlopen(address2+'/api/hello',timeout=1) as response:assert json.load(response)['codex_caller_only'] is True
   break
  except Exception:time.sleep(.1)
 else:raise RuntimeError('second external did not start')
 run([root/'bin/agent-harbor','magpie','bind','--address',address2])
 registry=json.loads((root/'harbor/magpie.json').read_text())
 other_id=next(i['id'] for i in registry['instances'] if i['address']==address2)

 click('New session');wait('New session')
 # First selection explicitly acknowledges the concrete instance.
 click('Connection');click('Choose instance and model');click('Select');wait('Choose instance')
 click(external_id);click('Select instance');wait('Model or routing group')
 click('group/fixture-group');save('selector-first-160');click('Acknowledge & apply');wait('New session')
 click('Connection');click('Choose instance and model');click('Select');wait('Choose instance')
 click(other_id);click('Select instance');wait('Model or routing group');wait('Acknowledge & apply');save('selector-other-instance-160')
 click('Cancel');wait('New session');assert external_id in capture() and 'group/fixture-group' in capture(),capture()
 registry=json.loads((root/'harbor/magpie.json').read_text())
 assert not next(i for i in registry['instances'] if i['id']==other_id).get('auxiliary_acknowledged',False)
 click('Model');wait('Model or routing group');assert 'Apply selection' in capture()
 click('fixture/fixture-model');click('Cancel');wait('New session');assert 'group/fixture-group' in capture()
 for cols,rows in [(160,45),(100,30),(60,20)]:
  click('Model');wait('Model or routing group')
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.4);save('selector-'+str(cols))
  if cols==60:
   first=capture();assert 'other providers.' in first,first
   assert 'Cancel' in first and 'Apply selection' in first,first
   keys('PageDown');last=capture();assert 'destination.' in last,last
   save('selector-disclosure-60')
  click('Cancel');wait('New session')
 tm('resize-window','-t','app:0','-x','160','-y','45');time.sleep(.4)
 click('Create');time.sleep(3)
 records=json.loads((root/'harbor/sessions.json').read_text());assert len(records)==1,records
 assert records[0]['mode']=='magpie' and records[0]['instance_id']==external_id and records[0]['magpie_model']=='group/fixture-group',records
 (root/'verified-records.json').write_text(json.dumps(records,indent=2))
 print('PASS real Magpie group selection/ack/known-ack/cancel/unique Create/sizes',root)

finally:
 if external2 is not None:external2.terminate();external2.wait(timeout=10)
 if external is not None:external.terminate();external.wait(timeout=10)
 if provider is not None:provider.shutdown();provider.server_close()
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
