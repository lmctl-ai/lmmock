import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import path from 'node:path';

const root=process.env.LMCONN_ROOT;
assert.ok(root,'LMCONN_ROOT must point to the fresh read-only lmconn clone');
const base=process.env.LMMOCK_URL;
const body={jsonrpc:'2.0',id:'lmconn-discover',method:'server/discover',params:{_meta:{'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientCapabilities':{}}}};
const result=spawnSync(process.execPath,[path.join(root,'bin/lmconn.js'),'http','request',base+'/mcp','--method','POST','--header','Accept: application/json, text/event-stream','--header','MCP-Protocol-Version: 2026-07-28','--header','Mcp-Method: server/discover','--data',JSON.stringify(body)],{encoding:'utf8'});
if(result.error)throw result.error;
assert.equal(result.status,0,`lmconn generic HTTP request failed\n${result.stdout}\n${result.stderr}`);
const envelope=JSON.parse(result.stdout);assert.equal(envelope.ok,true);assert.equal(envelope.result.status,200);assert.equal(envelope.result.bodyJson.result.supportedVersions[0],'2026-07-28');
console.log('PASS lmconn generic HTTP request reached MCP server/discover; lmconn mcp itself remains stdio-only');
