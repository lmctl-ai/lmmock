import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {fork} from 'node:child_process';
import {prepareCase,root} from './client.mjs';
const c=prepareCase('interrupt',{batch:true});
fs.writeFileSync(c.script,'m='+JSON.stringify({provider:'codex',alias:'Worker',model:'gpt-5.6-sol',sessiondir:c.dir,sessionid:c.wire.session})+';\nh=lm_batch_open(m); assert(h != null); println("OPENED"); r=lm_batch_turn(h,"cancel"); println(json_stringify(r)); lm_batch_close(h);\n');
let child,stdout='',stderr='',sent=false;
const watcher=fs.watch(c.trace,()=>{
 const file=path.join(c.trace,'output.jsonl');if(sent||!fs.existsSync(file))return;
 const text=fs.readFileSync(file,'utf8');const lines=text.split('\n');lines.pop();
 if(lines.some(line=>JSON.parse(line).method==='turn/started')){sent=true;child.send({signal:'SIGINT'});}
});
try{
 child=fork(path.join(root,'tests/providers/codex/signal-client.mjs'),[c.script,c.pkg],{cwd:c.dir,env:c.env,stdio:['ignore','pipe','pipe','ipc']});
 child.stdout.setEncoding('utf8');child.stderr.setEncoding('utf8');child.stdout.on('data',s=>stdout+=s);child.stderr.on('data',s=>stderr+=s);
 const result=await new Promise((resolve,reject)=>{child.once('error',reject);child.once('close',(status,signal)=>resolve({status,signal,stdout,stderr}));});
 const e=c.collect(result);
 try{
  assert.ok(sent,'interrupt must be triggered by the actual turn/started notification: '+JSON.stringify(result));
  const interrupts=e.requests.filter(r=>r.method==='turn/interrupt');
  assert.equal(interrupts.length,1,'one real adapter interrupt RPC');
  assert.deepEqual(interrupts[0].params,{threadId:c.wire.session,turnId:'turn-1'});
  assert.ok(e.events.some(r=>r.method==='turn/completed'&&r.params.turn.status==='interrupted'),'handler acknowledged the interrupted turn');
  assert.equal(e.rows.length,1,'one cancelled accounting/transcript attempt');
  if(process.argv.includes('--require-finished-attempt')) assert.equal(e.rows[0].turn_outcome,'cancelled','published cancellation must finish its accounting attempt');
  else if(e.rows[0].turn_outcome==='cancelled'){assert.ok(e.rows[0].ended_at,'finished cancellation needs end marker');}
  else { assert.equal(e.rows[0].status,'running','unexpected cancellation accounting status'); assert.equal(e.rows[0].turn_outcome,null); assert.equal(e.rows[0].ended_at,null); }
  assert.notEqual(e.exit,0,'published runner reports cancellation');
  assert.match(stdout,/OPENED/);
  console.log('PASS published Codex interrupt: notification rendezvous, correlated RPC, terminal acknowledgement, owned cleanup; published running-row accounting limitation observed');
 }finally{e.cleanup();}
}finally{watcher.close();if(child&&child.exitCode===null&&child.signalCode===null)child.kill('SIGKILL');}
