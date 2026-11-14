"""Synthetic provider asking real Codex to call the isolated Herdr MCP server."""
import io,json
from e2e_local import MockResponses

class MCPProbe(MockResponses):
    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        body = json.loads(raw)
        inputs = body.get("input", [])
        outputs = [x for x in inputs if isinstance(x, dict) and x.get("type") == "function_call_output" and x.get("call_id") == "call_probe"]
        if outputs:
            self.server.tool_output = outputs[-1].get("output", "")
        elif "AH_MCP_PROBE" in raw.decode() and body.get("tools"):
            tools = body.get("tools", [])
            namespace = next((x for x in tools if x.get("name") == "mcp__herdr"), {})
            name = next((x.get("name") for x in namespace.get("tools", []) if x.get("name") == "herdr_pane_list"), None)
            if not name:
                self.server.tool_error = "Herdr MCP tool missing from Codex request: " + repr([(x.get("type"), x.get("name")) for x in tools])
            else:
                item = {"id":"fc_probe", "type":"function_call", "status":"completed", "call_id":"call_probe", "namespace":"mcp__herdr", "name":name, "arguments":"{}"}
                response = {"id":"resp_probe", "object":"response", "status":"completed", "model":"test-model", "output":[item], "usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}
                events = [{"type":"response.created", "response":{"id":"resp_probe","object":"response","status":"in_progress","model":"test-model","output":[]}}, {"type":"response.output_item.added","output_index":0,"item":dict(item,status="in_progress",arguments="")}, {"type":"response.function_call_arguments.delta","item_id":"fc_probe","output_index":0,"delta":"{}"}, {"type":"response.function_call_arguments.done","item_id":"fc_probe","output_index":0,"arguments":"{}"}, {"type":"response.output_item.done","output_index":0,"item":item}, {"type":"response.completed","response":response}]
                self.reply("".join("event: "+e["type"]+"\ndata: "+json.dumps(e)+"\n\n" for e in events).encode(), "text/event-stream")
                return
        self.rfile = io.BytesIO(raw)
        super().do_POST()
