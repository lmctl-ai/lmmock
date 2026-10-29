// Real process and loopback tests; lmscript owns compilation and orchestration.
import assert from 'node:assert/strict';
import {spawn, spawnSync} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import net from 'node:net';
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
 } else if(mode==='fail') {process.exit(7);} else throw Error(mode);
 console.log('PASS client '+mode);process.exit(0);
}
const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock integration '));
function run(args,extra={}) {const r=spawnSync(binary,args,{env:{...process.env,...extra},encoding:'utf8',timeout:60000});if(r.error)throw r.error;return r;}
async function closed(url){const u=new URL(url);await new Promise((resolve,reject)=>{const s=net.connect({host:u.hostname,port:Number(u.port)});s.once('connect',()=>{s.destroy();reject(Error('server still listening after client exited'))});s.once('error',resolve);s.setTimeout(2000,()=>{s.destroy();reject(Error('close check timed out'))});});}
try {
 assert.match(run(['--version']).stdout,/^lmmock /);
 assert.deepEqual(JSON.parse(run(['schema']).stdout),JSON.parse(fs.readFileSync('examples/schema.json')));
 assert.notEqual(run(['serve','--dsl','build/api.dslb','--listen','0.0.0.0:0']).status,0);
 for(const mode of ['api','faults','stateful','fail']) {
  const urlFile=path.join(scratch,'url');const ready=path.join(scratch,'ready.json');
  const source=mode==='fail'?'api':mode;
  const r=run(['exec','--dsl',`build/${source}.dslb`,'--ready-file',ready,'--',process.execPath,import.meta.filename,binary,'client',mode],{CLIENT_URL_FILE:urlFile});
  assert.equal(r.status,mode==='fail'?7:0,r.stdout+r.stderr);assert.equal(fs.existsSync(ready),false);await closed(fs.readFileSync(urlFile,'utf8'));process.stdout.write(r.stdout);
 }
 // Source mode and paths containing spaces exercise npm shim invocation on Windows too.
 const urlFile=path.join(scratch,'source-url');
 const r=run(['exec','--script','examples/api.lms','--',process.execPath,import.meta.filename,binary,'client','api'],{CLIENT_URL_FILE:urlFile});assert.equal(r.status,0,r.stdout+r.stderr);await closed(fs.readFileSync(urlFile,'utf8'));
 // Standalone readiness and graceful signal handling (Windows uses exec mode above).
 if(process.platform!=='win32') {
  const childURL=path.join(scratch,'interrupted-url');
  const wrapper=spawn(binary,['exec','--dsl','build/api.dslb','--',process.execPath,'-e',"require('fs').writeFileSync(process.env.CLIENT_URL_FILE,process.env.LMMOCK_URL);setInterval(()=>{},1000)"],{env:{...process.env,CLIENT_URL_FILE:childURL},stdio:'ignore'});
  const stopped=new Promise(resolve=>wrapper.on('exit',code=>resolve(code)));
  try {
   for(let i=0;i<100&&!fs.existsSync(childURL);i++)await new Promise(r=>setTimeout(r,50));
   assert.ok(fs.existsSync(childURL));wrapper.kill('SIGTERM');
   assert.equal(await Promise.race([stopped,new Promise((_,rej)=>setTimeout(()=>rej(Error('child shutdown timeout')),5000).unref())]),130);
   await closed(fs.readFileSync(childURL,'utf8'));
  } finally {if(wrapper.exitCode===null)wrapper.kill('SIGKILL');}
  const ready=path.join(scratch,'serve-ready.json');
  const p=spawn(binary,['serve','--dsl','build/api.dslb','--ready-file',ready],{stdio:['ignore','pipe','pipe']});
  const exited=new Promise(resolve=>p.on('exit',(code,signal)=>resolve({code,signal})));
  try {
   for(let i=0;i<100&&!fs.existsSync(ready);i++)await new Promise(r=>setTimeout(r,50));
   assert.ok(fs.existsSync(ready),'missing readiness');const info=JSON.parse(fs.readFileSync(ready));assert.equal((await fetch(info.url+'/health')).status,200);
   p.kill('SIGTERM');assert.deepEqual(await Promise.race([exited,new Promise((_,rej)=>setTimeout(()=>rej(Error('shutdown timeout')),5000).unref())]),{code:0,signal:null});assert.equal(fs.existsSync(ready),false);await closed(info.url);
  } finally {if(p.exitCode===null)p.kill('SIGKILL');}
 }
 console.log('PASS real loopback lifecycle, source compilation, client exit propagation and cleanup');
} finally {fs.rmSync(scratch,{recursive:true,force:true});}
