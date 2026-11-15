#!/usr/bin/env python3
"""Real Herdr 0.9.5 mouse regression; owns all state and synthetic records."""
import argparse, http.server, os, pathlib, shlex, shutil, subprocess, tempfile, threading, time

p=argparse.ArgumentParser();p.add_argument('--binary',required=True);p.add_argument('--herdr',required=True);p.add_argument('--codex',required=True);p.add_argument('--no-color',action='store_true');a=p.parse_args()
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
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor05-smoke-',dir='/tmp')).resolve()
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
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'agent-harbor\nprintf \"exit %s\\n\" \"$?\"\nexec /bin/sh\n')
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
  lines=capture().splitlines(); matches=[i for i,line in enumerate(lines) if label in line]; assert matches, (label,capture()); y=matches[-1] if label in ("Retry","Known") else matches[0]
  prefix=lines[y][:lines[y].index(label)];x=sum(2 if unicodedata.east_asian_width(c) in ('W','F') else 0 if unicodedata.combining(c) else 1 for c in prefix)+1
  tm('send-keys','-t','app:0.0','-l',f'\x1b[<{button};{x};{y+1}M'+(f'\x1b[<0;{x};{y+1}m' if button==0 else ''));time.sleep(.4)
 def save(name):
  (root/(name+'.ansi')).write_text(tm('capture-pane','-e','-p','-t','app:0.0'))

 project=root/'project-日本語-🚢';project.mkdir()
 for i in range(24): (project/('folder-%02d-日本語'%i)).mkdir()
 keys('C-b');keys('a');wait('HARBOR')
 for cols,rows in [(160,45),(100,30),(60,20)]:
  tm('resize-window','-t','app:0','-x',str(cols),'-y',str(rows));time.sleep(.5)
  click('New session');wait('Project');click('Project');wait('Choose project');save('directory-known-'+str(cols))
  # Click the complete search boundary, then type a synthetic absolute root.
  click('Search known projects');keys('-l',str(project)+'/');wait('folder-00');save('directory-browse-'+str(cols))
  selected='folder-00'
  if cols==60:
   keys('Down','Down');selected='folder-02';wait(selected)
  click(selected);save('directory-select-'+str(cols))
  click('Full path / preview');save('directory-path-'+str(cols));keys('Escape')
  if cols==100: keys('Enter')
  else: click('Use this folder')
  wait('New session');save('directory-draft-'+str(cols))
  if cols==60: tm('resize-window','-t','app:0','-x','160','-y','45');time.sleep(.4)
  assert selected in capture(),capture()
  if cols==60: tm('resize-window','-t','app:0','-x','60','-y','20');time.sleep(.4)
  click('Project');wait('Choose project');keys('Escape');wait('New session')
  click('Project');wait('Choose project');click('Search known projects');keys('-l',str(root/'missing')+'/');wait('Error:');save('directory-error-'+str(cols));click('Retry');wait('Error:');click('Known');wait('Known projects');click('Cancel');wait('New session')
  keys('Escape');wait('Search sessions')
 assert not (root/'harbor/sessions.json').exists() or json.loads((root/'harbor/sessions.json').read_text())==[]
 print('PASS directory popup mouse, keyboard cancel, draft return, errors/retry, sizes',root)

finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
