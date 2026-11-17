#!/usr/bin/env python3
"""Inspect and install a native archive in a fresh, isolated prefix."""
import os,pathlib,subprocess,sys,tarfile,tempfile
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor03-install-')).resolve();stage=root/'archive';stage.mkdir()
with tarfile.open(sys.argv[1]) as archive:
    assert all(n in ('agent-harbor','herdr','magpie','codex-auth','README.md','install.sh','manage.sh','licenses') or n.startswith('licenses/') for n in archive.getnames()), 'unexpected archive contents'
    archive.extractall(stage,filter='data')
env=os.environ.copy()
for key in ('HOME','XDG_CONFIG_HOME','XDG_DATA_HOME','XDG_STATE_HOME','XDG_CACHE_HOME','XDG_RUNTIME_DIR','TMPDIR','CODEX_HOME','CODEX_SQLITE_HOME'):
    path=root/key;path.mkdir(mode=0o700);env[key]=str(path)
prefix=root/'installed'
def install(action,check=True):
    return subprocess.run(['sh',str(stage/'install.sh'),action,'--prefix',str(prefix)],env=env,check=check,capture_output=True,text=True)
install('install')
assert install('install',False).returncode!=0, 'overwrote populated prefix'
for executable,args in [('agent-harbor',['version']),('herdr',['--version']),('magpie',['--version']),('codex-auth',['--version'])]:
    result=subprocess.run([str(prefix/executable),*args],env=env,check=True,capture_output=True,text=True,timeout=20)
    print(executable,result.stdout.strip())
help_text=subprocess.run([str(prefix/'agent-harbor'),'help'],env=env,check=True,capture_output=True,text=True,timeout=20).stdout
assert 'Ctrl+B a' in help_text and 'explicit confirmation' in help_text, 'first-use help missing'
(prefix/'added.txt').write_text('preserve')
account_data=pathlib.Path(env['CODEX_HOME'])/'accounts'
account_data.mkdir();(account_data/'user-owned').write_text('synthetic saved login data')
install('uninstall')
assert (prefix/'added.txt').read_text()=='preserve'
assert not (prefix/'agent-harbor').exists() and not (prefix/'codex-auth').exists()
assert (account_data/'user-owned').read_text()=='synthetic saved login data'
assert install('uninstall',False).returncode!=0, 'accepted unowned prefix'
print('PASS archive allowlist, native versions, prefix install/uninstall and preservation',root)

# Optional PATH link belongs to this install and must never overwrite a user file.
linked=root/'linked'
result=subprocess.run(['sh',str(stage/'install.sh'),'install','--prefix',str(linked),'--link'],env=env,capture_output=True,text=True)
assert result.returncode==0, result.stderr
link=pathlib.Path(env['HOME'])/'.local/bin/agent-harbor'
assert link.is_symlink() and link.resolve()==linked/'agent-harbor'
subprocess.run(['sh',str(linked/'install.sh'),'uninstall','--prefix',str(linked)],env=env,check=True)
assert not link.is_symlink(), 'owned PATH link retained'
link.write_text('user-owned')
result=subprocess.run(['sh',str(stage/'install.sh'),'install','--prefix',str(root/'refused'),'--link'],env=env,capture_output=True,text=True)
assert result.returncode!=0 and link.read_text()=='user-owned'
print('PASS optional PATH link ownership and overwrite refusal')
