// Causal process cancellation: send SIGTERM only after observing an actual
// partial response from the fake ACP process. No sleep or per-case deadline.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
if(process.platform!=='linux'){console.log('Native SIGTERM qualification is Linux-only; RPC cancellation runs on all platforms.');process.exit(0);}
const root=process.cwd(),dir=fs.mkdtempSync(path.join(os.tmpdir(),'acp-cancel-client-')),bin=path.join(dir,'bin');fs.mkdirSync(bin);
let child,watcher,closed;
try{
 const cfg=JSON.parse(fs.readFileSync('fixtures/acp/miniplayer-cancel.json'));fs.writeFileSync(path.join(dir,'scenario.json'),JSON.stringify(cfg));
 const shim=path.join(bin,'miniplayer');fs.copyFileSync('build/acp-shim',shim);fs.chmodSync(shim,0o700);
 const team=path.join(dir,'team.lmctl');fs.writeFileSync(team,`_MEMBER_ alias=Worker provider=miniplayer model=${cfg.model} sessionid=${cfg.session} sessiondir=${dir}\n`);
 const script=path.join(dir,'run.lms');fs.writeFileSync(script,`r=lm_prompt(${JSON.stringify('team.lmctl')},"Worker","ping");println(json_stringify(r));\n`);
 const env=Object.fromEntries(Object.entries(process.env).filter(([k])=>!/^LMCTL_|^LMBEE_|^XDG_|^MINIPLAYER|TOKEN|API_KEY|SECRET|PASSWORD|AUTHORIZATION/.test(k)));
 Object.assign(env,{HOME:dir,USERPROFILE:dir,LMCTL_HOME:path.join(dir,'profile'),LMCTL_DB:path.join(dir,'state.db'),XDG_CACHE_HOME:path.join(dir,'cache'),XDG_DATA_HOME:path.join(dir,'data'),XDG_CONFIG_HOME:path.join(dir,'config'),LMCTL_NO_UPDATE_NOTIFIER:'1',NODE_NO_WARNINGS:'1',PATH:bin+':'+process.env.PATH,LMMOCK_ACP_CASE_DIR:dir,LMMOCK_TEST_BINARY:path.join(root,'build/lmmock'),LMMOCK_ACP_BYTECODE:path.join(root,'build/acp-profiles.lmcode')});
 const compiler=process.env.PATH.split(path.delimiter).map(d=>path.resolve(d,'../@lmctl-ai/lmctl/bin/lmscript')).find(fs.existsSync);assert.ok(compiler);
 let cancelled=false,stdout='',stderr='';
 watcher=fs.watch(dir,()=>{
  const file=path.join(dir,'trace.jsonl');if(!fs.existsSync(file)||cancelled||!child)return;
  const rows=fs.readFileSync(file,'utf8').split('\n').filter(Boolean).flatMap(line=>{try{return [JSON.parse(line)]}catch{return []}});
  const packets=rows.filter(row=>row.line).flatMap(row=>{try{return [{direction:row.direction,message:JSON.parse(row.line)}]}catch{return []}});
  const prompted=packets.some(p=>p.direction==='client'&&p.message.method==='session/prompt');
  const partial=packets.some(p=>p.direction==='server'&&p.message.method==='session/update'&&p.message.params?.update?.sessionUpdate==='agent_message_chunk'&&p.message.params.update.content?.text==='po');
  if(prompted&&partial){cancelled=true;child.kill('SIGTERM');}
 });
 child=spawn(process.execPath,[compiler,script],{cwd:dir,env,stdio:['ignore','pipe','pipe']});closed=new Promise(resolve=>child.once('close',(code,signal)=>resolve({code,signal})));child.stdout.on('data',b=>stdout+=b);child.stderr.on('data',b=>stderr+=b);
 const result=await closed;assert.ok(cancelled,'cancel only after the partial update');assert.equal(result.code,143,stdout+stderr);assert.equal(result.signal,null);assert.ok(!stdout.includes('"status":"ok"'));
 const db=new DatabaseSync(path.join(dir,'state.db'),{readOnly:true});try{const rows=db.prepare('SELECT * FROM team_chat_log').all();assert.equal(rows.length,1);assert.equal(rows[0].native_session_id,cfg.session);assert.notEqual(rows[0].status,'ok');assert.equal(rows[0].tok_output,null);}finally{db.close();}
 console.log('PASS published lmctl SIGTERM after partial ACP output: exit 143, no success, private session/token facts preserved');
}finally{watcher?.close();if(child&&child.exitCode===null&&child.signalCode===null){child.kill();await closed;}fs.rmSync(dir,{recursive:true,force:true});}
