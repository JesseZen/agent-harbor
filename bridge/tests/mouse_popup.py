#!/usr/bin/env python3
"""Real Herdr 0.9.5 mouse regression; owns all state and synthetic records."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);a=p.parse_args()
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
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor03-smoke-',dir='/tmp')).resolve()
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
env['PATH']=str(root/'bin')+':'+env['PATH'];env['AGENT_HARBOR_HERDR_BIN']=str(root/'bin/herdr');env['AGENT_HARBOR_BRIDGE_HOME']=str(root/'harbor')
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
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'agent-harbor\nprintf \"exit %s\\n\" \"$?\"\nread answer\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('First run')
 wait('codex-cli 0.167.0')
 out=wait('herdr 0.9.5');(root/'onboarding.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Enter');wait('Review: local')
 tm('send-keys','-t','app:0.0','Enter')
 out=wait('Harbor ^B a')
 import json
 records=[]
 for i,name in enumerate(['Mouse First','Mouse Second','Mouse Third']):
  project=root/('project-'+str(i));project.mkdir()
  records.append(dict(version=2,id=('%032x'%(i+1)),name=name,working_directory=str(project),cli='codex',mode='local',recovery='ready',started=False,created_at='2026-11-13T00:00:00Z',notify_warning='Synthetic detail scrolling evidence. '*100))
 (root/'harbor/sessions.json').write_text(json.dumps(records))
 (root/'tabbar.txt').write_text(out)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','?')
 wait('keybinds');tm('send-keys','-t','app:0.0','/');tm('send-keys','-t','app:0.0','-l','harbor')
 out=wait('Agent Harbor sessions');(root/'help.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Escape');time.sleep(.2);tm('send-keys','-t','app:0.0','Escape');time.sleep(.3)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','?');wait('keybinds')
 tm('send-keys','-t','app:0.0','/');tm('send-keys','-t','app:0.0','-l','magpie')
 out=wait('Manage Magpie');(root/'magpie-help.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Escape');time.sleep(.2);tm('send-keys','-t','app:0.0','Escape');time.sleep(.4)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','a')
 out=wait('Search sessions');assert 'n New session' in out;(root/'popup.txt').write_text(out)
 def mouse(text, button=0, double=False):
  import unicodedata
  def wcswidth(text):return sum(2 if unicodedata.east_asian_width(c) in ("W","F") else 0 if unicodedata.combining(c) else 1 for c in text)
  out=capture();lines=out.splitlines();matches=[i for i,l in enumerate(lines) if text in l];y=next((i for i in matches if '●' in lines[i]),matches[0]);line=lines[y];x=wcswidth(line[:line.index(text)])+2
  seq=f'\x1b[<{button};{x};{y+1}M'
  tm('send-keys','-t','app:0.0','-l',seq)
  if button==0:tm('send-keys','-t','app:0.0','-l',f'\x1b[<0;{x};{y+1}m')
  if double:
   time.sleep(.08);tm('send-keys','-t','app:0.0','-l',seq);tm('send-keys','-t','app:0.0','-l',f'\x1b[<0;{x};{y+1}m')
 for cols,rows in [(60,20),(100,30),(160,45)]:
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.5)
  if 'Mouse First' not in capture():tm('send-keys','-t','app:0.0','r');wait('Mouse First')
  if cols>=100:
   mouse('Mouse Second');time.sleep(.2)
   assert 'Sessions ·' in capture() and any('› Mouse Second' in l for l in capture().splitlines()),capture()
   mouse('Mouse First');time.sleep(.2)
  mouse('Mouse First',button=65);time.sleep(.3)
  assert any('› Mouse Second' in l for l in capture().splitlines()),capture()
  mouse('Mouse Second',double=True);wait('Start session')
  mouse('Mouse Second');time.sleep(.2);assert 'Start session' in capture(),capture()
  assert all(not r['started'] for r in json.loads((root/'harbor/sessions.json').read_text()))
  (root/('mouse-review-'+str(cols)+'.txt')).write_text(capture())
  tm('send-keys','-t','app:0.0','Escape');wait('Sessions ·')
  mouse('Mouse Second',button=64);time.sleep(.2)
  assert any('› Mouse First' in l for l in capture().splitlines()),capture()
  if cols==160:
   before=capture();mouse('WORKSPACE',button=65);time.sleep(.2)
   assert capture()!=before and any('› Mouse First' in l for l in capture().splitlines()),capture()
  mouse('List:');tm('send-keys','-t','app:0.0','-l','Mouse First');wait('1/3')
  tm('send-keys','-t','app:0.0','Escape');wait('3/3')
  (root/('mouse-list-'+str(cols)+'.txt')).write_text(capture())
  tm('send-keys','-t','app:0.0','n');wait('enter create')
  mouse('Name');wait('Session name')
  tm('send-keys','-t','app:0.0','-l','mouse-draft');tm('send-keys','-t','app:0.0','Enter');wait('Custom: mouse-draft')
  mouse('Connection');wait('Local Codex')
  mouse('Local Codex',double=True);wait('Custom: mouse-draft')
  assert 'mouse-draft' in capture()
  mouse('Directory');wait('Choose a project directory')
  before=next(l for l in capture().splitlines() if '› [' in l)
  mouse('[',button=65);time.sleep(.3)
  after=next(l for l in capture().splitlines() if '› [' in l)
  assert before!=after,(before,after)
  (root/('mouse-directory-'+str(cols)+'.txt')).write_text(capture())
  tm('send-keys','-t','app:0.0','Escape');wait('Custom: mouse-draft')
  (root/('mouse-form-'+str(cols)+'.txt')).write_text(capture())
  mouse('Cancel');wait('Sessions ·')
 print('PASS isolated real Herdr mouse list/search/review/form/directory and scoped wheel assertions at three sizes',root)

finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
