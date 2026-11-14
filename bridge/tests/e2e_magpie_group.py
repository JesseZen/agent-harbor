#!/usr/bin/env python3
"""Verify rotation against two mock providers in an isolated Magpie instance."""
import argparse
import http.server, json, os, pathlib, shutil, socket, subprocess, tempfile, threading, time, urllib.request

parser = argparse.ArgumentParser()
parser.add_argument("--magpie", required=True, help="Magpie 0.1.3528 executable")
args = parser.parse_args()

root = pathlib.Path(tempfile.mkdtemp(prefix="bridge-magpie-smoke-")).resolve()
for name in ("home", "config", "data", "state", "cache", "codex", "project", "bin", "runtime", "sqlite", "tmp"):
    (root / name).mkdir()
magpie = root / "bin" / "magpie"
shutil.copy(args.magpie, magpie)
events = []
class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def do_GET(self): self.reply({"data":[{"id":"test-model","object":"model"}]})
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        events.append(self.server.name)
        def chunk(delta, finish=None):
            return {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1,"model":"test-model","choices":[{"index":0,"delta":delta,"finish_reason":finish}]}
        payload = "".join("data: "+json.dumps(x)+"\n\n" for x in [chunk({"role":"assistant","content":""}), chunk({"content":"ISOLATED_REPLY"}), chunk({}, "stop")])+"data: [DONE]\n\n"
        data=payload.encode(); self.send_response(200); self.send_header("Content-Type","text/event-stream"); self.send_header("Content-Length",str(len(data))); self.end_headers(); self.wfile.write(data)
    def reply(self, obj):
        data=json.dumps(obj).encode(); self.send_response(200); self.send_header("Content-Type","application/json"); self.send_header("Content-Length",str(len(data))); self.end_headers(); self.wfile.write(data)
server=http.server.ThreadingHTTPServer(("127.0.0.1",0),Handler)
server.name="A"
other=http.server.ThreadingHTTPServer(("127.0.0.1",0),Handler)
other.name="B"
threading.Thread(target=other.serve_forever,daemon=True).start()
threading.Thread(target=server.serve_forever,daemon=True).start()
with socket.socket() as sock:
    sock.bind(("127.0.0.1",0)); port=sock.getsockname()[1]
env={"HOME":str(root/"home"),"XDG_CONFIG_HOME":str(root/"config"),"XDG_DATA_HOME":str(root/"data"),"XDG_STATE_HOME":str(root/"state"),"XDG_CACHE_HOME":str(root/"cache"),"CODEX_HOME":str(root/"codex"),"MAGPIE_NO_ACCOUNTS":"1","MAGPIE_ADDR":f"127.0.0.1:{port}","PATH":os.environ.get("PATH", "/usr/bin:/bin"),"LANG":"en_US.UTF-8","AGENT_HARBOR_MAGPIE_KEY":"magpie-agent-harbor"}
env.update(XDG_RUNTIME_DIR=str(root/'runtime'),CODEX_SQLITE_HOME=str(root/'sqlite'),TMPDIR=str(root/'tmp'))
(root/'codex/config.toml').write_text('check_for_update_on_startup = false\n')
print("diagnostics",root,flush=True)
subprocess.run([str(magpie),"provider","add","test-relay",f"url=http://127.0.0.1:{server.server_port}/v1","key=test-only","models=test-model"],env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
subprocess.run([str(magpie),"provider","add","other-relay",f"url=http://127.0.0.1:{other.server_port}/v1","key=test-only","models=test-model"],env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
subprocess.run([str(magpie),"group","add","test-group","models=test-relay/test-model,other-relay/test-model","routing=rotate","stays=off"],env=env,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
log=open(root/"magpie.log","w")
proc=subprocess.Popen([str(magpie),"serve"],env=env,stdout=log,stderr=subprocess.STDOUT)
try:
    for i in range(100):
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{port}/v1/models",timeout=1) as r:
                catalog=json.load(r)
            break
        except Exception:
            if proc.poll() is not None: raise RuntimeError("Magpie exited")
            time.sleep(.1)
    else: raise RuntimeError("Magpie did not start")
    print("catalog", [m["id"] for m in catalog["data"]],flush=True)
    for n in range(6):
        body=json.dumps({"model":"group/test-group","messages":[{"role":"user","content":"routing test"}],"stream":True}).encode()
        request=urllib.request.Request(f"http://127.0.0.1:{port}/v1/chat/completions",body,{"Content-Type":"application/json","Authorization":"Bearer magpie-agent-harbor"})
        with urllib.request.urlopen(request,timeout=30) as response: response.read()
    print("routing destinations",events,flush=True)
    if set(events) != {"A","B"}: raise RuntimeError("routing group did not rotate")
finally:
    proc.terminate()
    try: proc.wait(timeout=5)
    except subprocess.TimeoutExpired: proc.kill(); proc.wait()
    server.shutdown()
    other.shutdown()
