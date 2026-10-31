import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const binary=path.resolve(process.argv[2]||'build/lmmock');
const scenario=path.resolve('tests/stdio/scenario.json');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-stdio-'));
try {
 const fake=path.join(dir,path.basename(binary));fs.copyFileSync(binary,fake);fs.chmodSync(fake,0o700);
 const command=[fake,'stdio','mcp','--scenario',scenario,'--alias','MCP'];
 const client=spawnSync(binary,['stdio','mcp-client','--scenario',scenario,'--fake-bin',dir,'--',...command],{encoding:'utf8'});
 assert.equal(client.status,0,client.stderr);assert.equal(client.stdout,'','client must keep stdout protocol-only');
 const denied=spawnSync(binary,['stdio','mcp-client','--scenario',scenario,'--fake-bin',dir,'--',process.execPath,'-e','throw Error("should never execute")'],{encoding:'utf8'});
 assert.notEqual(denied.status,0);assert.match(denied.stderr,/outside fake-bin/);
 for(const preset of ['claude','gemini','agy']){
  const state=path.join(dir,preset);const args=['stdio','agent','--preset',preset,'--scenario',scenario,'--alias','Worker','--state',state,'--','-p','ping'];
  const first=spawnSync(binary,args,{encoding:'utf8'});assert.equal(first.status,0,first.stderr);assert.match(first.stdout,/pong/);
  const second=spawnSync(binary,args,{encoding:'utf8'});assert.equal(second.status,0,second.stderr);assert.match(second.stdout,/second/);
  const third=spawnSync(binary,args,{encoding:'utf8'});assert.notEqual(third.status,0);assert.match(third.stderr,/unscripted/);
 }
 console.log('PASS owned MCP client/server, fail-closed launch guard, agent cells and unscripted refusal');
}finally{fs.rmSync(dir,{recursive:true,force:true});}
