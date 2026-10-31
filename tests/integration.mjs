// Real process and loopback tests; lmscript owns compilation and orchestration.
import assert from 'node:assert/strict';
import {spawn, spawnSync} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import net from 'node:net';
import http from 'node:http';
const binary=path.resolve(process.argv[2]);
if(process.argv[3]==='client') {
 const base=process.env.LMMOCK_URL;
 const mode=process.argv[4];
 fs.writeFileSync(process.env.CLIENT_URL_FILE,base);
 const get=async (p,opts)=>fetch(base+p,opts);
 if(mode==='api') {
  let r=await get('/health');assert.equal(r.status,200);assert.deepEqual(await r.json(),{ok:true});
  r=await get('/users/42?name=Alice');assert.equal(r.status,401);
  r=await get('/users/42?name=Alice',{headers:{Authorization:'Bearer test-token'}});assert.equal(r.status,200);assert.equal(r.headers.get('x-user'),'42');assert.equal(await r.text(),'Alice');
  const bytes=Buffer.from([0,255,195,169,10]);r=await get('/echo',{method:'POST',body:bytes});assert.equal(r.headers.get('x-method'),'POST');assert.deepEqual(Buffer.from(await r.arrayBuffer()),bytes);
  assert.equal((await get('/health',{method:'POST'})).status,405);assert.equal((await get('/missing')).status,404);
 } else if(mode==='faults') {
  for(const code of [503,503,200])assert.equal((await get('/retry')).status,code);
  assert.equal((await get('/error')).status,500);
  const r=await get('/malformed');await assert.rejects(()=>r.json());
  let start=Date.now();assert.equal(await (await get('/slow')).text(),'late response');assert.ok(Date.now()-start>=450);
  await assert.rejects(()=>get('/slow',{signal:AbortSignal.timeout(40)}));
  await assert.rejects(()=>get('/disconnect'));
 } else if(mode==='stateful') {
  assert.equal((await get('/webhook',{method:'POST',body:'event-1'})).status,202);assert.equal(await (await get('/last-event')).text(),'event-1');
  const counts=await Promise.all(Array.from({length:20},async()=>Number(await (await get('/count')).text())));assert.deepEqual(counts.sort((a,b)=>a-b),Array.from({length:20},(_,i)=>i+1));
 } else if(mode==='proxy') {
  assert.equal((await get('/health')).status,200);
  let r=await get('/fallback?q=a%2Bb');assert.equal(r.status,418);assert.equal(r.headers.get('x-upstream'),'yes');assert.equal(await r.text(),'upstream\x00body');
  assert.equal((await get('/fallback',{method:'PUT',body:'denied'})).status,405);
 } else if(mode==='fail') {process.exit(7);} else throw Error(mode);
 console.log('PASS client '+mode);process.exit(0);
}
const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock integration '));
function run(args,extra={}) {const r=spawnSync(binary,args,{env:{...process.env,...extra},encoding:'utf8',timeout:60000});if(r.error)throw r.error;return r;}
async function closed(url){const u=new URL(url);await new Promise((resolve,reject)=>{const s=net.connect({host:u.hostname,port:Number(u.port)});s.once('connect',()=>{s.destroy();reject(Error('server still listening after client exited'))});s.once('error',resolve);s.setTimeout(2000,()=>{s.destroy();reject(Error('close check timed out'))});});}
try {
 assert.match(run(['--version']).stdout,/^lmmock /);
 assert.deepEqual(JSON.parse(run(['schema']).stdout),JSON.parse(fs.readFileSync('examples/schema.json')));
 assert.notEqual(run(['serve','--lmcode','build/api.dslb','--listen','0.0.0.0:0']).status,0);
 assert.notEqual(run(['serve','--lmcode','build/api.dslb','--upstream','http://user:pass@localhost']).status,0);
 // Async child keeps this in-process upstream responsive on all platforms.
 const seen=[];
 const upstream=http.createServer((req,res)=>{seen.push(req.url);res.writeHead(418,{'X-Upstream':'yes'});res.end('upstream\x00body');});
 await new Promise(resolve=>upstream.listen(0,'127.0.0.1',resolve));
 try {
  const audit=path.join(scratch,'proxy-audit.jsonl');
  const child=spawn(binary,['exec','--lmcode','build/api.dslb','--upstream',`http://127.0.0.1:${upstream.address().port}/base/`,'--upstream-read-only','--upstream-timeout','5s','--audit-log',audit,'--',process.execPath,import.meta.filename,binary,'client','proxy'],{env:{...process.env,CLIENT_URL_FILE:path.join(scratch,'proxy-url')},stdio:['ignore','pipe','pipe']});
  let output='';child.stdout.on('data',b=>{output+=b});child.stderr.on('data',b=>{output+=b});
  const timer=setTimeout(()=>child.kill('SIGKILL'),15000);
  try {assert.equal(await new Promise((resolve,reject)=>{child.once('error',reject);child.once('exit',resolve)}),0,output);} finally {clearTimeout(timer);}
  assert.deepEqual(seen,['/base/fallback?q=a%2Bb']);
  const events=fs.readFileSync(audit,'utf8').trim().split('\n').map(JSON.parse);
  assert.deepEqual(events.map(e=>[e.kind,e.status]),[['mocked',200],['forwarded',418],['forwarded',405]]);
  console.log('PASS selective proxy CLI, mock precedence, audit and read-only fallback');
 } finally {await new Promise(resolve=>upstream.close(resolve));}
 for(const mode of ['api','faults','stateful','fail']) {
  const urlFile=path.join(scratch,'url');const ready=path.join(scratch,'ready.json');
  const source=mode==='fail'?'api':mode;
  const r=run(['exec','--lmcode',`build/${source}.dslb`,'--ready-file',ready,'--',process.execPath,import.meta.filename,binary,'client',mode],{CLIENT_URL_FILE:urlFile});
  assert.equal(r.status,mode==='fail'?7:0,r.stdout+r.stderr);assert.equal(fs.existsSync(ready),false);await closed(fs.readFileSync(urlFile,'utf8'));process.stdout.write(r.stdout);
 }
 // Source mode and paths containing spaces exercise npm shim invocation on Windows too.
 const urlFile=path.join(scratch,'source-url');
 const r=run(['exec','--script','examples/api.lms','--',process.execPath,import.meta.filename,binary,'client','api'],{CLIENT_URL_FILE:urlFile});assert.equal(r.status,0,r.stdout+r.stderr);await closed(fs.readFileSync(urlFile,'utf8'));
 // Standalone readiness and graceful signal handling (Windows uses exec mode above).
 if(process.platform!=='win32') {
  const childURL=path.join(scratch,'interrupted-url');
  const wrapper=spawn(binary,['exec','--lmcode','build/api.dslb','--',process.execPath,'-e',"require('fs').writeFileSync(process.env.CLIENT_URL_FILE,process.env.LMMOCK_URL);setInterval(()=>{},1000)"],{env:{...process.env,CLIENT_URL_FILE:childURL},stdio:'ignore'});
  const stopped=new Promise(resolve=>wrapper.on('exit',code=>resolve(code)));
  try {
   for(let i=0;i<100&&!fs.existsSync(childURL);i++)await new Promise(r=>setTimeout(r,50));
   assert.ok(fs.existsSync(childURL));wrapper.kill('SIGTERM');
   assert.equal(await Promise.race([stopped,new Promise((_,rej)=>setTimeout(()=>rej(Error('child shutdown timeout')),5000).unref())]),130);
   await closed(fs.readFileSync(childURL,'utf8'));
  } finally {if(wrapper.exitCode===null)wrapper.kill('SIGKILL');}
  const ready=path.join(scratch,'serve-ready.json');
  const p=spawn(binary,['serve','--lmcode','build/api.dslb','--ready-file',ready],{stdio:['ignore','pipe','pipe']});
  const exited=new Promise(resolve=>p.on('exit',(code,signal)=>resolve({code,signal})));
  try {
   for(let i=0;i<100&&!fs.existsSync(ready);i++)await new Promise(r=>setTimeout(r,50));
   assert.ok(fs.existsSync(ready),'missing readiness');const info=JSON.parse(fs.readFileSync(ready));assert.equal((await fetch(info.url+'/health')).status,200);
   p.kill('SIGTERM');assert.deepEqual(await Promise.race([exited,new Promise((_,rej)=>setTimeout(()=>rej(Error('shutdown timeout')),5000).unref())]),{code:0,signal:null});assert.equal(fs.existsSync(ready),false);await closed(info.url);
  } finally {if(p.exitCode===null)p.kill('SIGKILL');}
 }
 console.log('PASS real loopback lifecycle, source compilation, client exit propagation and cleanup');
} finally {fs.rmSync(scratch,{recursive:true,force:true});}
