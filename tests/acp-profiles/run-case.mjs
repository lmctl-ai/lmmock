import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync,spawn} from 'node:child_process';
import {createInterface} from 'node:readline';
import {DatabaseSync} from 'node:sqlite';
import {fileURLToPath} from 'node:url';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
export const profiles=JSON.parse(fs.readFileSync(path.join(root,'tests/acp-profiles/profiles.json')));
const ext=process.platform==='win32'?'.exe':'';
export const binary=path.resolve(process.env.LMMOCK_TEST_BINARY||path.join(root,'build/lmmock'+ext));
const bytecode=path.resolve(process.env.LMMOCK_ACP_BYTECODE||path.join(root,'build/acp-profiles.dslb'));
function compiler(){
 if(process.env.LMMOCK_LMSCRIPT_JS)return path.resolve(process.env.LMMOCK_LMSCRIPT_JS);
 for(const dir of process.env.PATH.split(path.delimiter)){const file=path.resolve(dir,'../@lmctl-ai/lmctl/bin/lmscript');if(fs.existsSync(file))return file;}
 throw Error('Put a private published npm lmctl install on PATH or set LMMOCK_LMSCRIPT_JS');
}
export function manifest(profile){return JSON.parse(fs.readFileSync(path.join(root,'fixtures/providers/acp-'+profile+'.json')));}
export async function runCase(id,{quiet=false,variantOverride,expectedOverride}={}){
 const profile=Object.keys(profiles).find(p=>id.startsWith(p+'-'));assert.ok(profile,'unknown profile case');
 const meta=profiles[profile],decl=manifest(profile),item=decl.cases.find(c=>c.id===id);assert.ok(item,'unknown fixture case');
 const variant=variantOverride||id.slice(profile.length+1),config=JSON.parse(fs.readFileSync(path.join(root,'fixtures/acp/'+profile+'-'+variant+'.json')));
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-acp-profile-'));
 try{
  fs.writeFileSync(path.join(dir,'scenario.json'),JSON.stringify(config));
  if(variant==='cancel')return await wireCase(profile,config,dir,{cancel:true});
  const bin=path.join(dir,'bin');fs.mkdirSync(bin);const shim=path.join(bin,profile+ext);
  fs.copyFileSync(path.join(root,'build/acp-shim'+ext),shim);fs.chmodSync(shim,0o700);
  assert.equal(path.dirname(fs.realpathSync(shim)),fs.realpathSync(bin),'provider must resolve inside fake-bin');
  const team=path.join(dir,'team.lmctl');const effort=profile==='opencode'?' effort=high':'';
  fs.writeFileSync(team,`_MEMBER_ alias=Worker provider=${profile} model=${config.model} sessionid=${config.session} sessiondir="${dir.replaceAll('\\','/')}"${effort}\n`);
  const script=path.join(dir,'run.lms');fs.writeFileSync(script,`r = lm_prompt(${JSON.stringify('team.lmctl')}, "Worker", "ping"); println(json_stringify(r));\n`);
  const env=Object.fromEntries(Object.entries(process.env).filter(([k])=>!k.startsWith('LMCTL_')&&!k.startsWith('LMBEE_')&&!/^(COPILOT|KIMI|OPENCODE|HERMES|GEMINI|MINIPLAYER|CODEX|CLAUDE|AGY|QWEN|OPENAI|ANTHROPIC|GOOGLE|XDG_)/.test(k)&&!/TOKEN|API_KEY|SECRET|PASSWORD|AUTHORIZATION/.test(k)));
  Object.assign(env,{HOME:dir,USERPROFILE:dir,LMCTL_HOME:path.join(dir,'profile'),LMCTL_DB:path.join(dir,'state.db'),XDG_CACHE_HOME:path.join(dir,'cache'),XDG_DATA_HOME:path.join(dir,'data'),XDG_CONFIG_HOME:path.join(dir,'config'),LMCTL_NO_UPDATE_NOTIFIER:'1',NODE_NO_WARNINGS:'1',PATH:bin+path.delimiter+process.env.PATH,LMMOCK_ACP_CASE_DIR:dir,LMMOCK_TEST_BINARY:binary,LMMOCK_ACP_BYTECODE:bytecode});
  const child=spawnSync(process.execPath,[compiler(),'--no-preflight',script],{cwd:dir,env,encoding:'utf8'});
  assert.equal(child.status,0,child.stdout+child.stderr);
  const result=JSON.parse(child.stdout.trim());const expected=expectedOverride||item.expect;
  assert.equal(result.error_class,expected.error_class??null,id+' error_class: '+result.error);
  if(expected.error_class){assert.equal(result.status,'error');assert.equal(result.reply,'');}
  else{assert.equal(result.status,'ok',result.error);assert.equal(result.reply,variant==='permission'?'permission rejected':'pong');}
  if(expected.stderr_contains)assert.ok(child.stderr.includes(expected.stderr_contains),child.stderr);
  const traceFile=path.join(dir,'trace.jsonl');const trace=fs.existsSync(traceFile)?fs.readFileSync(traceFile,'utf8').trim().split('\n').map(JSON.parse):[];
  if(!meta.enabled){assert.equal(trace.length,0,'disabled Gemini must not spawn');assert.match(result.error,/Standalone Gemini CLI support is disabled/);await wireCase(profile,config,dir,{cancel:false});}
  else{
   const start=trace.find(t=>t.direction==='spawn');assert.ok(start);assert.deepEqual(start.argv,config.argv);
   if(profile==='hermes')assert.equal(start.hermes_yolo,'1');
   const sent=trace.filter(t=>t.direction==='client').map(t=>JSON.parse(t.line));const recv=trace.filter(t=>t.direction==='server').map(t=>JSON.parse(t.line));
   assert.equal(sent[0].method,'initialize');assert.equal(sent[0].params.protocolVersion,1);assert.equal(sent[0].params.clientInfo.name,'lmctl');
   if(variant!=='auth'){
    const load=sent.find(m=>m.method==='session/load');assert.equal(load?.params.sessionId,config.session);assert.equal(path.resolve(load.params.cwd),path.resolve(dir));assert.deepEqual(load.params.mcpServers,[]);
   }else assert.equal(sent.filter(m=>m.method==='session/load').length,0,'auth failure must stop before session open');
   if(variant==='stale')assert.ok(sent.some(m=>m.method==='session/new'),'one-shot adapter must visibly fall back after load refusal');
   if(!expected.error_class){
    const configured=sent.filter(m=>m.method?.startsWith('session/set_'));assert.equal(configured.length,config.configure.length);
    for(let i=0;i<configured.length;i++){const step=config.configure[i];assert.equal(configured[i].method,step.method);assert.equal(configured[i].params[step.key],step.value);if(step.configId)assert.equal(configured[i].params.configId,step.configId);}
    const prompt=sent.find(m=>m.method==='session/prompt');assert.ok(prompt);assert.match(prompt.params.prompt[0].text,/ping/);
    assert.ok(!result.reply.includes('old replay'),'session/load history must not leak');assert.ok(!result.reply.includes('reasoning'),'thought/tool content must not leak');
    const updates=recv.filter(m=>m.method==='session/update').map(m=>m.params.update);
    assert.ok(updates.some(u=>u.sessionUpdate==='tool_call'&&u.toolCallId==='tool-fixture'));
    assert.ok(updates.some(u=>u.sessionUpdate==='tool_call_update'&&u.toolCallId==='tool-fixture'));
    if(variant==='permission'){assert.ok(sent.some(m=>m.error?.code===-32601),'real lmctl must reject unsupported server permission request');}
    assert.equal(result.usage.reason,meta.usage_reason);
   }
   if(profile==='opencode'){const configPath=path.join(dir,'.opencode/opencode.json');assert.ok(fs.existsSync(configPath));assert.equal(JSON.parse(fs.readFileSync(configPath)).permission,'allow');}
  }
  for(const field of ['inputUncached','cacheRead','cacheWrite','output','reasoning','costUsd'])assert.equal(result.usage[field],null,'ACP metadata must not fabricate billable '+field);
  const db=new DatabaseSync(path.join(dir,'state.db'),{readOnly:true});try{const row=db.prepare('SELECT * FROM team_chat_log WHERE id=?').get(result.team_chat_log_id);assert.ok(row);assert.equal(row.provider,profile);assert.equal(row.receiver_alias,'Worker');assert.equal(row.native_session_id,config.session);assert.equal(row.model_requested,config.model);result.model=row.model_requested;assert.equal(row.response,result.reply||null);for(const k of ['tok_input_uncached','tok_output','tok_reasoning'])assert.equal(row[k],null);}finally{db.close();}
  if(!quiet){process.stdout.write(JSON.stringify({...result,fixture:id})+'\n');process.stderr.write(child.stderr);}
  return result;
 }finally{fs.rmSync(dir,{recursive:true,force:true});}
}
export async function wireCase(profile,config,dir,{cancel}){
 const file=path.join(dir,'wire.json');fs.writeFileSync(file,JSON.stringify(config));
 const child=spawn(binary,['stdio','acp','--lmcode',bytecode,'--scenario',file,'--alias','Worker','--',...config.argv],{stdio:['pipe','pipe','pipe']});
 let stderr='',next=0;const pending=new Map(),messages=[];child.stderr.on('data',b=>stderr+=b);let cancelSent=false,activeId=null;
 const closed=new Promise(resolve=>child.once('close',(code,signal)=>{for(const [,p]of pending)p.reject(Error('peer closed '+code+' '+signal+' '+stderr));pending.clear();resolve(code);}));
 const lines=createInterface({input:child.stdout});lines.on('line',line=>{const m=JSON.parse(line);messages.push(m);
  if(m.method==='session/update'&&activeId!==null&&cancel&&!cancelSent){cancelSent=true;send({jsonrpc:'2.0',method:'session/cancel',params:{sessionId:config.session}});}
  if(m.method==='session/request_permission'){send({jsonrpc:'2.0',id:m.id,error:{code:-32601,message:'Method not found: fixture client'}});}
  if(m.id!==undefined&&!m.method){const p=pending.get(m.id);if(p){pending.delete(m.id);m.error?p.reject(Object.assign(Error(m.error.message),{rpc:m.error})):p.resolve(m.result);}}
 });
 function send(m){child.stdin.write(JSON.stringify(m)+'\n');}
 function call(method,params){const id=++next;const promise=new Promise((resolve,reject)=>pending.set(id,{resolve,reject}));if(method==='session/prompt')activeId=id;send({jsonrpc:'2.0',id,method,params});return promise;}
 try{
  if(config.fault==='auth'){await assert.rejects(call('initialize',{protocolVersion:1,clientInfo:{name:'fixture'}}),/Authentication required/);child.stdin.end();assert.equal(await closed,0);return {fixture:profile+'-auth',profile_only:true};}
  const init=await call('initialize',{protocolVersion:1,clientInfo:{name:'fixture'}});assert.equal(init.agentCapabilities.loadSession,true);
  send({jsonrpc:'2.0',method:'notifications/initialized'});
  const method=config.fault==='stale'?'session/load':'session/new';
  if(config.fault==='stale'){await assert.rejects(call(method,{sessionId:config.session,cwd:dir,mcpServers:[]}),/No conversation/);child.stdin.end();assert.equal(await closed,0);return {fixture:profile+'-stale',profile_only:true};}
  const opened=await call(method,{cwd:dir,mcpServers:[]});assert.equal(opened.sessionId,config.session);
  for(const step of config.configure){const p={sessionId:config.session,[step.key]:step.value};if(step.configId)p.configId=step.configId;await call(step.method,p);}
  const args={sessionId:config.session,prompt:[{type:'text',text:'ping'}]};
  if(config.cells[0].completion==='missing'){await assert.rejects(call('session/prompt',args),/peer closed/);assert.notEqual(await closed,0);return {fixture:profile+'-missing',profile_only:true};}
  const completed=await call('session/prompt',args);assert.equal(completed.stopReason,cancel?'cancelled':'end_turn');
  if(cancel){assert.ok(cancelSent);assert.equal(messages.filter(m=>m.id===activeId&&!m.method).length,1);activeId=null;const second=await call('session/prompt',args);assert.equal(second.stopReason,'end_turn');}
  child.stdin.end();assert.equal(await closed,0,stderr);
  return {fixture:profile+'-'+(cancel?'cancel':'wire'),events:cancel?['agent_message_chunk','cancelled','end_turn']:['end_turn'],profile_only:true};
 }finally{lines.close();if(child.exitCode===null){child.kill();await closed;}}
}
if(process.argv[1]&&path.resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 const id=process.argv[2];assert.ok(id,'provide a declared case id');const result=await runCase(id);if(result.profile_only)console.log(JSON.stringify(result));
}
