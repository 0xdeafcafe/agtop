"""A minimal agtop plugin in Python, standard library only.

It offers Claude three tools: note and notes, which keep notes in the plugin's
data folder (the only place it may write), and agents, which calls agtop back
to list its agents. Rename it, change the tools, and keep the plumbing:
agtop speaks JSON-RPC 2.0 on fd 3, each message a 4-byte big-endian length
and then that many bytes of JSON.
"""

import json
import os
import socket
import struct
import sys
import threading

# --- tools: change these ---

TOOLS = [
    {
        "name": "note",
        "description": "Keep a note for later sessions.",
        "inputSchema": {
            "type": "object",
            "properties": {"text": {"type": "string", "description": "The note."}},
            "required": ["text"],
            "additionalProperties": False,
        },
    },
    {
        "name": "notes",
        "description": "Read every note kept so far.",
        "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
    },
    {
        "name": "agents",
        "description": "List the agents running in agtop.",
        "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False},
    },
]

NOTES = os.path.join(os.environ["AGTOP_PLUGIN_DATA"], "notes.txt")


def call_tool(session, name, args):
    """Run one tool. session is the agtop session that called it."""
    if name == "note":
        text = (args or {}).get("text", "").strip()
        if not text:
            raise ValueError("text is required")
        with open(NOTES, "a") as f:
            f.write(f"[{session}] {text}\n")
        return "Noted."
    if name == "notes":
        try:
            with open(NOTES) as f:
                return f.read() or "No notes yet."
        except FileNotFoundError:
            return "No notes yet."
    if name == "agents":
        # Calling agtop back: needs "list" in the manifest's "sessions".
        sessions = call("sessions.list")
        return "\n".join(f"{s['id']} {s['state']:<8} {s.get('name', '')}: {s.get('detail', '')}" for s in sessions) or "No agents."
    raise ValueError(f"no tool named {name}")


def handle(method, params):
    """Answer agtop's requests and take its notifications."""
    if method == "initialize":
        # params: protocol, name, dataDir, sessions, workspaces, network.
        return {}
    if method == "tools.list":
        return {"tools": TOOLS}
    if method == "tools.call":
        try:
            text = call_tool(params.get("session"), params.get("name"), params.get("arguments"))
            return tool_result(text, False)
        except Exception as e:  # a tool's failure is Claude's to read, not a protocol error
            return tool_result(str(e), True)
    if method == "session.event":
        # Sessions this plugin follows (sessions.subscribe) report here.
        return None
    raise RpcError(-32601, f"method not found: {method}")


def tool_result(text, is_error):
    return {"content": [{"type": "text", "text": text}], "isError": is_error}


# --- plumbing: keep as is ---


class RpcError(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code, self.message = code, message


ipc = socket.socket(fileno=3)
wlock = threading.Lock()
plock = threading.Lock()
pending = {}  # id -> [Event, reply]
next_id = 0


def call(method, params=None):
    """Call agtop and return its result; raises RpcError if it refuses."""
    global next_id
    with plock:
        next_id += 1
        cid = next_id
        slot = pending[cid] = [threading.Event(), None]
    send({"id": cid, "method": method, "params": params})
    slot[0].wait()
    reply = slot[1]
    if "error" in reply:
        raise RpcError(reply["error"]["code"], reply["error"]["message"])
    return reply.get("result")


def send(msg):
    msg["jsonrpc"] = "2.0"
    body = json.dumps(msg).encode()
    with wlock:
        ipc.sendall(struct.pack(">I", len(body)) + body)


def read_exact(n):
    buf = b""
    while len(buf) < n:
        chunk = ipc.recv(n - len(buf))
        if not chunk:
            raise EOFError
        buf += chunk
    return buf


def serve(msg):
    try:
        result = handle(msg["method"], msg.get("params") or {})
        send({"id": msg["id"], "result": {} if result is None else result})
    except RpcError as e:
        send({"id": msg["id"], "error": {"code": e.code, "message": e.message}})
    except Exception as e:
        send({"id": msg["id"], "error": {"code": -32000, "message": str(e)}})


def main():
    while True:
        try:
            (n,) = struct.unpack(">I", read_exact(4))
            msg = json.loads(read_exact(n))
        except (EOFError, OSError):
            return  # agtop closed the channel: time to go
        if "method" not in msg:
            with plock:  # a reply to one of our calls
                slot = pending.pop(msg.get("id"), None)
            if slot:
                slot[1] = msg
                slot[0].set()
            continue
        if "id" not in msg:
            handle(msg["method"], msg.get("params") or {})  # notifications, in order
            continue
        threading.Thread(target=serve, args=(msg,), daemon=True).start()


if __name__ == "__main__":
    sys.exit(main())
