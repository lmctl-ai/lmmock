import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

export function runMcpMutations({root,run}) {
 const cases=[
  [String.raw`2026-07-28\",\"2025-11-25`,String.raw`2026-01-01\",\"2025-11-25`,'modern','advertises supported current protocol version'],
  [String.raw`\"destructiveHint\":true`,String.raw`\"destructiveHint\":false`,'modern','destructive tool annotation is explicit'],
  ['let value = field(args, "text", "");','let value = "mutated";','modern','echo returns the input text'],
  ['let total = a + b;','let total = a - b;','modern','add computes typed numeric sum'],
  ['isError = "true"','isError = "false"','modern','tool failure uses isError result'],
  ['error_reply(id, -32603, "synthetic crash")','error_reply(id, -32604, "synthetic crash")','modern','crash uses expected JSON-RPC code'],
  [String.raw`\"text\":\"fixture text\"`,String.raw`\"text\":\"broken text\"`,'modern','text resource content is preserved'],
  [String.raw`\"blob\":\"AAEC\"`,String.raw`\"blob\":\"AAED\"`,'modern','blob resource remains exact base64 data'],
  [String.raw`\"resourceTemplates\":[{\"uriTemplate\":\"mock://item/{id}\",\"name\":\"item\"}]`,String.raw`\"resourceTemplates\":[]`,'modern','resource template is listed'],
  [String.raw`\"prompts\":[{\"name\":\"greet\"`,String.raw`\"prompts\":[]`,'modern','prompt is listed'],
  ['json_quote("Hello " + name)','json_quote("Hello guest")','modern','prompt arguments are applied'],
  ['else if (method == "ping") {\n    mcp_reply(x, envelope(id, "{}"));\n  }','else if (method == "ping") {\n    mcp_reply(x, error_reply(id, -32601, "bad ping"));\n  }','modern','ping returns a JSON-RPC result'],
  ['origin != "" && origin != "http://" + host','false','modern','untrusted origin is refused'],
  ['allowed && token != "" && x.header("Authorization") != "Bearer " + token','false','auth','optional bearer guard challenges'],
  ['headerMethod == "" || bodyVersion != headerVersion || headerMethod != method','false','mismatch','request header/body consistency is enforced'],
  ['else if (version == "2026-07-28" && (!str_contains(x.header("Accept"), "application/json") || !str_contains(x.header("Accept"), "text/event-stream")))','else if (false)','modern','current transport requires both Accept media types'],
  ['else if (method == "initialize" && version == "2026-07-28") {\n    mcp_reply(x, error_reply(id, -32601, "method not found"));\n  }','else if (method == "initialize" && version == "2026-07-28") {\n    mcp_reply(x, envelope(id, "{}"));\n  }','modern','current stateless transport refuses initialize'],
  ['if (digits) {\n      allowed = true;\n    }','if (true) {\n      allowed = true;\n    }','modern','loopback Host port must be numeric'],
 ];
 const original=fs.readFileSync(path.join(root,'examples/mcp/server.lms'),'utf8');
 let index=0;
 for(const [anchor,replacement,scenario,diagnostic] of cases) {
  index++;
  assert.equal(original.split(anchor).length-1,1,`MCP mutation ${index} anchor must stay unique: ${anchor}`);
  const source=path.join(root,`examples/mcp/server.mutant-${process.pid}-${index}.lms`);
  const compiled=path.join(root,`build/mcp/mutant-${process.pid}-${index}.lmcode`);
  try {
   fs.writeFileSync(source,original.replace(anchor,replacement));
   const compile=spawnSync(process.execPath,[path.join(root,'scripts/capture-fixture.mjs'),source,compiled],{cwd:root,encoding:'utf8'});
   if(compile.error)throw compile.error;
   assert.equal(compile.status,0,`MCP mutant ${index} compiles\n${compile.stdout}\n${compile.stderr}`);
   const result=run(scenario,compiled);
   assert.notEqual(result.status,0,`MCP mutation ${index} survived: ${diagnostic}`);
   console.log(`PASS MCP mutation ${index} caught: ${diagnostic}`);
  } finally {
   if(fs.existsSync(source))fs.rmSync(source);
   if(fs.existsSync(compiled))fs.rmSync(compiled);
  }
 }
}
