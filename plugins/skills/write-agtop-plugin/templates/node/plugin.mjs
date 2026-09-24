// A minimal agtop plugin in Node, no dependencies.
//
// It offers Claude three tools: note and notes, which keep notes in the
// plugin's data folder (the only place it may write), and agents, which
// calls agtop back to list its agents. Rename it, change the tools, and keep
// the plumbing: agtop speaks JSON-RPC 2.0 on fd 3, each message a 4-byte
// big-endian length and then that many bytes of JSON.

import { appendFileSync, readFileSync } from "node:fs";
import net from "node:net";
import { join } from "node:path";

// --- tools: change these ---

const tools = [
  {
    name: "note",
    description: "Keep a note for later sessions.",
    inputSchema: {
      type: "object",
      properties: { text: { type: "string", description: "The note." } },
      required: ["text"],
      additionalProperties: false,
    },
  },
  {
    name: "notes",
    description: "Read every note kept so far.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
  {
    name: "agents",
    description: "List the agents running in agtop.",
    inputSchema: { type: "object", properties: {}, additionalProperties: false },
  },
];

const NOTES = join(process.env.AGTOP_PLUGIN_DATA, "notes.txt");

// callTool runs one tool. session is the agtop session that called it.
async function callTool(session, name, args) {
  if (name === "note") {
    const text = (args?.text ?? "").trim();
    if (!text) throw new Error("text is required");
    appendFileSync(NOTES, `[${session}] ${text}\n`);
    return "Noted.";
  }
  if (name === "notes") {
    try {
      return readFileSync(NOTES, "utf8") || "No notes yet.";
    } catch {
      return "No notes yet.";
    }
  }
  if (name === "agents") {
    // Calling agtop back: needs "list" in the manifest's "sessions".
    const sessions = await call("sessions.list");
    return sessions.map((s) => `${s.id} ${s.state.padEnd(8)} ${s.name ?? ""}: ${s.detail ?? ""}`).join("\n") || "No agents.";
  }
  throw new Error(`no tool named ${name}`);
}

// handle answers agtop's requests and takes its notifications.
async function handle(method, params) {
  switch (method) {
    case "initialize":
      // params: protocol, name, dataDir, sessions, workspaces, network.
      return {};
    case "tools.list":
      return { tools };
    case "tools.call":
      try {
        return toolResult(await callTool(params.session, params.name, params.arguments), false);
      } catch (e) {
        return toolResult(String(e.message ?? e), true);
      }
    case "session.event":
      // Sessions this plugin follows (sessions.subscribe) report here.
      return null;
  }
  throw Object.assign(new Error(`method not found: ${method}`), { code: -32601 });
}

const toolResult = (text, isError) => ({ content: [{ type: "text", text }], isError });

// --- plumbing: keep as is ---

const ipc = new net.Socket({ fd: 3, readable: true, writable: true });
const pending = new Map();
let nextId = 0;

// call calls agtop and resolves to its result; rejects if it refuses.
function call(method, params) {
  const id = ++nextId;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    send({ id, method, params });
  });
}

function send(msg) {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...msg }));
  const head = Buffer.alloc(4);
  head.writeUInt32BE(body.length);
  ipc.write(Buffer.concat([head, body]));
}

let buf = Buffer.alloc(0);
ipc.on("data", (chunk) => {
  buf = Buffer.concat([buf, chunk]);
  while (buf.length >= 4) {
    const n = buf.readUInt32BE(0);
    if (buf.length < 4 + n) break;
    const msg = JSON.parse(buf.subarray(4, 4 + n).toString("utf8"));
    buf = buf.subarray(4 + n);
    if (!msg.method) {
      // A reply to one of our calls.
      const p = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) p?.reject(Object.assign(new Error(msg.error.message), { code: msg.error.code }));
      else p?.resolve(msg.result);
      continue;
    }
    if (msg.id === undefined) {
      handle(msg.method, msg.params ?? {}).catch(() => {});
      continue;
    }
    handle(msg.method, msg.params ?? {}).then(
      (result) => send({ id: msg.id, result: result ?? {} }),
      (e) => send({ id: msg.id, error: { code: e.code ?? -32000, message: String(e.message ?? e) } }),
    );
  }
});
ipc.on("close", () => process.exit(0)); // agtop closed the channel: time to go
