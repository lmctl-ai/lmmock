// The cancellation rendezvous must observe an ordered stream, not fs.watch hints.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import {spawn, spawnSync} from 'node:child_process';
const root=path.resolve(import.meta.dirname,'../../..');
const binary=path.join(root,'build','codex-fixture'+(process.platform==='win32'?'.exe':''));
const temp=fs.mkdtempSync(path.join(os.tmpdir(),'codex-observer-'));
const env={...process.env,LMMOCK_CODEX_TRACE:temp,LMMOCK_CODEX_CONFIG:path.join(root,'fixtures/codex/wire.json')};
try{
 const bad=spawnSync(binary,['app-server','--listen','stdio://'],{env:{...env,LMMOCK_CODEX_OBSERVER_ADDR:'192.0.2.1:1'},input:'',encoding:'utf8'});
 assert.notEqual(bad.status,0,'non-loopback observer must be refused before connecting');
 assert.match(bad.stderr,/observer address must be loopback/);
 const chunks=[];
 let end;
 const ended=new Promise(resolve=>{end=resolve;});
 const server=net.createServer(socket=>{socket.on('data',chunk=>chunks.push(chunk));socket.on('end',end);});
 await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
 try{
  const child=spawn(binary,['app-server','--listen','stdio://'],{env:{...env,LMMOCK_CODEX_OBSERVER_ADDR:'127.0.0.1:'+server.address().port},stdio:['pipe','pipe','pipe']});
  let stdout='',stderr='';child.stdout.on('data',b=>stdout+=b);child.stderr.on('data',b=>stderr+=b);
  child.stdin.end(JSON.stringify({jsonrpc:'2.0',id:1,method:'initialize',params:{}})+'\n');
  const status=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('close',resolve);});
  assert.equal(status,0,stderr);await ended;
  assert.equal(Buffer.concat(chunks).toString(),stdout,'observer must see exactly the native output bytes');
  assert.equal(JSON.parse(stdout).id,1);
 }finally{await new Promise(resolve=>server.close(resolve));}
 console.log('PASS loopback-only native output observer and exact ordered bytes');
}finally{fs.rmSync(temp,{recursive:true,force:true});}
