#!/usr/bin/env python3
"""Real Herdr 0.9.5 discoverability smoke; creates only its own workspace."""
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
 codex_version=run([root/'bin/codex','--version']).stdout.strip()
 herdr_version=run([root/'bin/herdr','--version']).stdout.strip()
 launcher=root/'launch.sh'
 launcher.write_text('#!/bin/sh\n'+''.join('export '+key+'='+shlex.quote(value)+'\n' for key,value in env.items())+'agent-harbor\nprintf \"exit %s\\n\" \"$?\"\nread answer\n')
 launcher.chmod(0o700)
 tm('new-session','-d','-s','app','-x','160','-y','45',str(launcher))
 wait('First run')
 wait(codex_version)
 out=wait(herdr_version);(root/'onboarding.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Enter');wait('Review: local')
 tm('send-keys','-t','app:0.0','Enter')
 out=wait('Harbor ^B a');(root/'tabbar.txt').write_text(out)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','?')
 wait('keybinds');tm('send-keys','-t','app:0.0','/');tm('send-keys','-t','app:0.0','-l','harbor')
 out=wait('Agent Harbor sessions');(root/'help.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Escape');time.sleep(.2);tm('send-keys','-t','app:0.0','Escape');time.sleep(.3)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','?');wait('keybinds')
 tm('send-keys','-t','app:0.0','/');tm('send-keys','-t','app:0.0','-l','magpie')
 out=wait('Manage Magpie');(root/'magpie-help.txt').write_text(out)
 tm('send-keys','-t','app:0.0','Escape');time.sleep(.2);tm('send-keys','-t','app:0.0','Escape');time.sleep(.4)
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','a')
 out=wait('HARBOR');assert 'n new' in out;(root/'popup.txt').write_text(out)
 tm('send-keys','-t','app:0.0','q');time.sleep(.5)
 assert 'HARBOR' not in capture()
 tm('send-keys','-t','app:0.0','C-b');tm('send-keys','-t','app:0.0','m')
 out=wait('Magpie instances');(root/'magpie.txt').write_text(out)
 tm('send-keys','-t','app:0.0','q');time.sleep(.3)
 print('PASS native Codex dependency check, reviewed local onboarding, real Herdr help/tab-bar/popups',codex_version,herdr_version,root)
finally:
 run([root/'bin/agent-harbor','stop'],check=False)
 run([tmux,'-S',sock,'kill-server'],check=False)
 discovery.shutdown();discovery.server_close()
