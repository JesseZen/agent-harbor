#!/usr/bin/env python3
"""Exercise online install/update/uninstall with a local release server, no user state."""
import functools,hashlib,http.server,os,pathlib,platform,subprocess,sys,tarfile,tempfile,threading
root=pathlib.Path(tempfile.mkdtemp(prefix='harbor-distribution-'))
repo=pathlib.Path(__file__).resolve().parents[2]
server_root=root/'http';server_root.mkdir()
platform_name={'Darwin':'darwin','Linux':'linux'}[platform.system()]
arch='arm64' if platform.machine() in ('aarch64','arm64') else 'amd64'
for version in ('v0.1.0-dev.1','v0.1.0-dev.2'):
    stage=root/version;stage.mkdir();(stage/'licenses').mkdir()
    for name in ('agent-harbor','herdr','magpie','codex-auth'):
        file=stage/name;file.write_text('#!/bin/sh\nprintf "%s\\n" '+version+'\n');file.chmod(0o755)
    if len(sys.argv)>1:
        (stage/'agent-harbor').write_bytes(pathlib.Path(sys.argv[1]).read_bytes())
    (stage/'README.md').write_text('fixture')
    (stage/'install.sh').write_bytes((repo/'bridge/install.sh').read_bytes())
    (stage/'manage.sh').write_bytes((repo/'distribution/install.sh').read_bytes())
    release_dir=server_root/version;release_dir.mkdir()
    archive=release_dir/f'agent-harbor-{version}-{platform_name}-{arch}.tar.gz'
    with tarfile.open(archive,'w:gz') as tar:
        for p in stage.iterdir():tar.add(p,arcname=p.name)
    (release_dir/'SHA256SUMS').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
handler=functools.partial(http.server.SimpleHTTPRequestHandler,directory=str(server_root))
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),handler)
threading.Thread(target=server.serve_forever,daemon=True).start()
home=root/'home';home.mkdir();prefix=home/'package with spaces';bin_dir=home/'bin'
env=dict(os.environ,HOME=str(home),AGENT_HARBOR_DOWNLOAD_BASE=f'http://127.0.0.1:{server.server_port}',AGENT_HARBOR_CHANNEL_URL=f'http://127.0.0.1:{server.server_port}/latest')
(server_root/'latest').write_text('v0.1.0-dev.1\n')
def run(*args,ok=True):
    r=subprocess.run(['sh',str(repo/'distribution/install.sh'),*args,'--prefix',str(prefix),'--bin-dir',str(bin_dir)],env=env,text=True,capture_output=True)
    assert (r.returncode==0)==ok,(args,r.stdout,r.stderr)
    return r
try:
    run('update',ok=False)
    bin_dir.mkdir();(bin_dir/'ah').write_text('user command')
    run('install',ok=False);assert not prefix.exists()
    (bin_dir/'ah').unlink()
    run('install')
    assert (bin_dir/'ah').resolve()==prefix/'releases/v0.1.0-dev.1/agent-harbor'
    assert (bin_dir/'agent-harbor').resolve()==(bin_dir/'ah').resolve()
    state=home/'client-state';state.write_text('synthetic account and conversation')
    (server_root/'latest').write_text('v0.1.0-dev.2\n')
    sums=server_root/'v0.1.0-dev.2/SHA256SUMS';saved=sums.read_text();sums.write_text(saved.replace(saved[:64],'0'*64))
    run('update',ok=False)
    assert (prefix/'current').readlink()==pathlib.Path('releases/v0.1.0-dev.1')
    sums.write_text(saved)
    (prefix/'.install-lock').mkdir();run('update',ok=False);(prefix/'.install-lock').rmdir()
    if len(sys.argv)>1:
        subprocess.run([str(bin_dir/'ah'),'update'],env=env,check=True)
    else:run('update')
    assert (bin_dir/'ah').resolve()==prefix/'releases/v0.1.0-dev.2/agent-harbor'
    assert (prefix/'releases/v0.1.0-dev.1/agent-harbor').exists(),'removed running version'
    run('update')
    (prefix/'releases/v0.1.0-dev.1/added.txt').write_text('keep')
    (bin_dir/'ah').unlink();(bin_dir/'ah').write_text('replacement command')
    if len(sys.argv)>1:
        subprocess.run([str(bin_dir/'agent-harbor'),'uninstall'],env=env,check=True)
    else:run('uninstall')
    assert not (bin_dir/'agent-harbor').exists()
    assert (bin_dir/'ah').read_text()=='replacement command'
    assert not list(prefix.glob('releases/*/agent-harbor'))
    assert (prefix/'releases/v0.1.0-dev.1/added.txt').read_text()=='keep'
    assert state.read_text()=='synthetic account and conversation'
    run('uninstall',ok=False)
    (bin_dir/'ah').unlink()
    run('install')
    assert (bin_dir/'agent-harbor').exists()
    run('uninstall')
    print('PASS online install, aliases, checksum failure, lock, update, old version retention, uninstall, foreign command and state preservation')
finally:server.shutdown()
