#!/usr/bin/env python3
"""Call an approved, running agtop plugin's tools the way a session would.

    call.py <plugin> list                      tools/list
    call.py <plugin> call <tool> ['{json}']    tools/call
    call.py status                             what the broker is running

It talks to the plugin broker's socket (only you can), which passes the MCP
message to the sandboxed plugin exactly as it would for Claude Code. The
plugin sees the session id "call.py".
"""

import json
import os
import socket
import struct
import sys


def home():
    return os.environ.get("AGTOP_HOME") or os.path.expanduser("~/.config/agtop")


def rpc(method, params):
    s = socket.socket(socket.AF_UNIX)
    try:
        s.connect(os.path.join(home(), "plugins", "broker.sock"))
    except OSError as e:
        sys.exit(f"the plugin broker isn't running ({e}); approve a plugin first: agtop plugin approve <name>")
    s.settimeout(600)
    body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    s.sendall(struct.pack(">I", len(body)) + body)

    def exact(n):
        buf = b""
        while len(buf) < n:
            chunk = s.recv(n - len(buf))
            if not chunk:
                sys.exit("the broker closed the connection")
            buf += chunk
        return buf

    (n,) = struct.unpack(">I", exact(4))
    reply = json.loads(exact(n))
    if "error" in reply:
        sys.exit(f"broker: {reply['error']['message']}")
    return reply["result"]


def mcp(plugin, method, params):
    reply = rpc("mcp", {"plugin": plugin, "session": "call.py",
                        "message": {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}})
    if "error" in reply:
        sys.exit(f"{plugin}: {reply['error']['message']}")
    return reply["result"]


def main(argv):
    if argv[:1] == ["status"]:
        for p in rpc("status", None):
            print(f"{p['name']:<16} {p['state']:<8} pid={p.get('pid', '-')} restarts={p.get('restarts', 0)} {p.get('error', '')}")
        return
    if len(argv) < 2 or argv[1] not in ("list", "call") or (argv[1] == "call" and len(argv) < 3):
        sys.exit(__doc__)
    plugin = argv[0]
    mcp(plugin, "initialize", {"protocolVersion": "2025-06-18"})
    if argv[1] == "list":
        for t in mcp(plugin, "tools/list", {})["tools"]:
            print(f"{t['name']}: {t.get('description', '')}")
            print(f"  input: {json.dumps(t.get('inputSchema', {}))}")
        return
    args = json.loads(argv[3]) if len(argv) > 3 else {}
    res = mcp(plugin, "tools/call", {"name": argv[2], "arguments": args})
    for c in res.get("content", []):
        print(c.get("text", json.dumps(c)))
    if res.get("isError"):
        sys.exit(1)


if __name__ == "__main__":
    main(sys.argv[1:])
