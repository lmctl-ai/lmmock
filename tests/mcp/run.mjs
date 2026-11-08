import assert from 'node:assert/strict';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
import {runMcpMutations} from './mutations.mjs';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const binary=path.resolve(process.argv[2]||'build/lmmock');
const compiled=path.join(root,'build/mcp/server.lmcode');
function run(scenario,source=compiled) {
 const config=scenario==='legacy-sse'?'examples/mcp/http-sse/config.json':scenario==='legacy-session'||scenario==='legacy-cancel'?'tests/mcp/session-config.json':scenario==='auth'?'tests/mcp/auth-config.json':'examples/mcp/streamable-http/config.json';
 const result=spawnSync(binary,['exec','--lmcode',source,'--config',path.join(root,config),'--',process.execPath,path.join(root,'tests/mcp/client.mjs'),scenario],{cwd:root,encoding:'utf8'});
 if(result.error)throw result.error;
 return result;
}
for(const [config,scenario] of [
 ['examples/mcp/streamable-http/config.json','modern'],
 ['examples/mcp/http-sse/config.json','legacy-sse'],
 ['tests/mcp/session-config.json','legacy-session'],
 ['tests/mcp/session-config.json','legacy-cancel'],
 ['examples/mcp/streamable-http/config.json','progress-cancel'],
 ['tests/mcp/auth-config.json','auth'],
 ['examples/mcp/streamable-http/config.json','mismatch'],
]) {
 const result=run(scenario);assert.equal(result.status,0,`${scenario} failed\n${result.stdout}\n${result.stderr}`);process.stdout.write(result.stdout);
}
const gosdk=spawnSync('go',['test','-count=1','-v','./...'],{cwd:path.join(root,'tests/mcp/gosdk'),encoding:'utf8',env:{...process.env,LMMOCK_BIN:binary}});
if(gosdk.error)throw gosdk.error;
assert.equal(gosdk.status,0,`gosdk failed\n${gosdk.stdout}\n${gosdk.stderr}`);
process.stdout.write(gosdk.stdout);
runMcpMutations({root,binary,run});
console.log('PASS MCP transport examples');
