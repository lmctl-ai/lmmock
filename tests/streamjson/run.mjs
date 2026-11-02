import assert from 'node:assert/strict';
import fs from 'node:fs';import os from 'node:os';import path from 'node:path';
import {spawnSync} from 'node:child_process';import {fileURLToPath,pathToFileURL} from 'node:url';
import {loadFixture,checkExpectation} from '../provider-fixtures/run.mjs';
const ROOT=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
export function clientPackage(){
 const explicit=process.env.LMMOCK_LMCTL_PACKAGE;
 const guesses=explicit?[explicit]:[path.resolve(path.dirname(process.env.LMSCRIPT_BIN||'/nonexistent'),'..'),path.resolve(path.dirname(process.env.LMSCRIPT_BIN||'/nonexistent'),'../@lmctl-ai/lmctl'),...process.env.PATH.split(path.delimiter).map(dir=>path.resolve(dir,'../@lmctl-ai/lmctl'))];
 const pin=JSON.parse(fs.readFileSync(path.join(ROOT,'tests/streamjson/client.json')));
 for(const pkg of guesses){try{const p=JSON.parse(fs.readFileSync(path.join(pkg,'package.json')));if(p.name===pin.package){assert.equal(p.version,pin.version,'published client pin');return path.resolve(pkg);}}catch(e){if(explicit)throw e;}}
 throw Error('Set LMMOCK_LMCTL_PACKAGE to the privately installed pinned npm package');
}
function materialize(value,vars){
 if(typeof value==='string'){if(Object.hasOwn(vars,value))return structuredClone(vars[value]);return value.replace(/\$\{[A-Z_]+\}/g,k=>{assert.ok(Object.hasOwn(vars,k),'unknown placeholder '+k);return vars[k];});}
 if(Array.isArray(value))return value.map(v=>materialize(v,vars));
 if(value&&typeof value==='object')return Object.fromEntries(Object.entries(value).map(([k,v])=>[k,materialize(v,vars)]));return value;
}
function cleanEnv(dir,bin,pkg,scenario,code,prices){
 const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>! /^(LMCTL_|LMBEE_|CLAUDE|ANTHROPIC|QWEN|OPENAI|CODEX|GEMINI|LMMOCK_JSONLINES)/.test(key)&&!/TOKEN|API_KEY|SECRET|PASSWORD/.test(key)));
 return {...env,HOME:dir,USERPROFILE:dir,XDG_CONFIG_HOME:path.join(dir,'config'),XDG_CACHE_HOME:path.join(dir,'cache'),LMCTL_HOME:path.join(dir,'profile'),LMCTL_DB:path.join(dir,'state.db'),LMCTL_PRICE_DATA:prices,PATH:bin+path.delimiter+process.env.PATH,LMMOCK_JSONLINES_PROGRAM:code,LMMOCK_JSONLINES_SCENARIO:scenario};
}
function runClient(pkg,name,args,env,cwd){
 const p=spawnSync(process.execPath,[path.join(pkg,'bin',name),...args],{env,cwd,encoding:'utf8'});assert.ifError(p.error);assert.equal(p.signal,null,'unexpected client signal');return p;
}
export async function runProvider({manifestFile,binary,code=path.join(ROOT,'fixtures/streamjson/handler.lmcode'),only,mutateExpected=false}={}){
 const {manifest}=await loadFixture(manifestFile,ROOT);assert.equal(manifest.server.kind,'stdio');
 const pkg=clientPackage(),wire=JSON.parse(fs.readFileSync(path.join(ROOT,manifest.server.config)));
 const provider=manifest.server.binary;assert.ok(['claude','qwen'].includes(provider),'declared adapter');
 const scratch=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock streamjson 水 '));let passed=0;
 try{
  const bin=path.join(scratch,'bin');fs.mkdirSync(bin);const fake=path.join(bin,provider+(process.platform==='win32'?'.exe':''));fs.copyFileSync(path.resolve(binary),fake);fs.chmodSync(fake,0o700);
  assert.equal(path.dirname(fs.realpathSync(fake)),fs.realpathSync(bin),'fake executable must be owned');
  const prices=path.join(scratch,'prices');fs.mkdirSync(prices);fs.copyFileSync(path.join(pkg,'data/price-data.json'),path.join(prices,'model_list.json'));fs.copyFileSync(path.join(pkg,'data/price-aliases.json'),path.join(prices,'aliases.json'));
  for(const original of manifest.cases){
   if(only&&original.id!==only)continue;
   const dir=path.resolve(scratch,original.id);assert.ok(dir.startsWith(scratch+path.sep),'case directory must remain private');fs.mkdirSync(dir);const workspace=path.join(dir,'workspace');fs.mkdirSync(workspace);
   const model=original.kind==='model'?original.expect.model:manifest.models[0].id;
   const sid=wire.session,transcript=path.join(dir,wire.stores[provider],sid+'.jsonl');fs.mkdirSync(path.dirname(transcript),{recursive:true});
   const audit=path.join(dir,'audit.jsonl');fs.writeFileSync(audit,'');
   const vars={'${MODEL}':model,'${SESSION}':sid,'${TRANSCRIPT}':transcript};
   const profile=structuredClone(wire.profiles[original.kind==='model'?'complete':original.id]);assert.ok(profile,'declared wire profile');
   const header={type:provider==='claude'?'user':'system',subtype:'init',sessionId:sid,cwd:workspace,model,version:'fixture-cli',...(provider==='claude'?{message:{role:'user',content:'fixture seed'}}:{})};
   const config={version:wire.version,audit,seed:{...wire.seed,files:[{path:transcript,text:JSON.stringify(header)+'\n'}]},init:wire.init,turns:[{...profile,expect:'ping'}]};
   if(profile.record_usage){
    const usage=structuredClone(wire.usage),metadata=structuredClone(wire.usageMetadata);if(profile.malformed_usage){usage.input_tokens='invalid';metadata.promptTokenCount='invalid';}
    const blocks=provider==='claude'?[{type:'thinking',thinking:'fixture reasoning'},{type:'tool_use',id:'tool-1',name:'fixture_lookup',input:{query:'fixture'}},{type:'text',text:'pong'}]:[{type:'thinking',thinking:'fixture reasoning'},{functionCall:{id:'tool-1',name:'fixture_lookup',args:{query:'fixture'}}},{type:'text',text:'pong'}];
    const assistant={type:'assistant',sessionId:sid,uuid:'assistant-1',cwd:workspace,...(provider==='qwen'?{usageMetadata:metadata}:{}),message:{id:'assistant-1',role:'assistant',model,content:blocks,...(provider==='claude'?{usage}: {})}};
    const tool=provider==='claude'?{type:'user',sessionId:sid,message:{role:'user',content:[{type:'tool_result',tool_use_id:'tool-1',content:'fixture tool answer'}]}}:{type:'tool_result',sessionId:sid,message:{content:[{functionResponse:{id:'tool-1',name:'fixture_lookup',response:'fixture tool answer'}}]}};
    config.turns[0].append=[{path:transcript,text:[{type:'user',sessionId:sid,message:{role:'user',content:'ping'}},assistant,tool].map(JSON.stringify).join('\n')+'\n'}];
   }
   const scenario=path.join(dir,'scenario.json');fs.writeFileSync(scenario,JSON.stringify(materialize(config,vars)));
   const env=cleanEnv(dir,bin,pkg,scenario,path.resolve(code),prices);
   const team=path.join(dir,'team.lmctl');fs.writeFileSync(team,`_MEMBER_ alias=Worker provider=${provider} model=${model} sessiondir=${JSON.stringify(workspace.replaceAll('\\','/'))}\n`);
   const script=path.join(dir,'client.lms');fs.writeFileSync(script,`s=lm_seed(${JSON.stringify(team)});assert(s.status=="ok",json_stringify(s));r=lm_prompt(${JSON.stringify(team)},"Worker","ping");println("OBS "+json_stringify(r));m=lm_session_get({provider:${JSON.stringify(provider)},sessionid:${JSON.stringify(sid)}});println("SESSION "+json_stringify(m));`);
   const client=runClient(pkg,'lmscript',[script],env,workspace);
   assert.equal(client.status,0,'published client failed: '+client.stdout+client.stderr);
   const observed=client.stdout.split('\n').filter(l=>l.startsWith('OBS '));assert.equal(observed.length,1,'one adapter result');const result=JSON.parse(observed[0].slice(4));
   const testCase=materialize(structuredClone(original),vars);
   if(mutateExpected&&testCase.expect.error_class)testCase.expect.error_class='model_not_offered';
   let observedModel=result.usage?.modelReported??null;
   if(original.kind==='model'){const native=runClient(pkg,'lmctl',['export',sid,'--provider',provider,'--json'],env,workspace);assert.equal(native.status,0,native.stderr);observedModel=JSON.parse(native.stdout).model;}
   const normalized={...result,model:observedModel};
   await checkExpectation(testCase,{exitCode:client.status,stdout:JSON.stringify(normalized),stderr:client.stderr},{tmpdir:dir});
   const allRecords=fs.readFileSync(audit,'utf8').trim().split('\n').map(JSON.parse);const records=allRecords.filter(r=>r.argv?.[0]!=='--version');assert.equal(records.length,3,'one seed, one turn startup, one JSONL user: '+JSON.stringify(records));
   const [seed,start,user]=records;assert.equal(seed.argv[0],'-p');assert.match(seed.argv[1],/initial connection only/);assert.ok(!seed.argv.includes('--input-format'),'seed is argv prompt, not a JSONL turn');
   assert.ok(start.argv.includes('--input-format')&&start.argv.includes('--output-format'));assert.equal(start.argv[start.argv.indexOf('--resume')+1],sid);assert.equal(start.argv[start.argv.indexOf('--model')+1],model);assert.deepEqual(user,JSON.parse(testCase.request.stdin));assert.deepEqual(start.argv,testCase.request.argv,'pinned turn argv');
   if(provider==='claude')assert.ok(start.argv.includes('--verbose')&&start.argv.includes('--dangerously-skip-permissions'));else assert.equal(start.argv[start.argv.indexOf('--approval-mode')+1],'yolo');
   assert.match(fs.readFileSync(team,'utf8'),new RegExp('sessionid='+sid),'seed pins native session');
   if(profile.record_usage&&!profile.malformed_usage){
    const exported=runClient(pkg,'lmctl',['export',sid,'--provider',provider,'--json'],env,workspace);assert.equal(exported.status,0,exported.stderr);const session=JSON.parse(exported.stdout);
    assert.equal(session.totalInputTokens,15,'native input/cache total');assert.equal(session.totalCachedInputTokens,3);assert.equal(session.totalOutputTokens,11);
    if(provider==='qwen')assert.equal(session.totalReasoningOutputTokens,4,'Qwen native thinking count');else {assert.equal(session.totalCacheWriteInputTokens,5);assert.equal(session.totalCacheWrite5mInputTokens,2);assert.equal(session.totalCacheWrite1hInputTokens,3);}
    assert.ok(session.messages.some(m=>m.role==='thinking'));assert.ok(session.messages.some(m=>m.role==='tool_use'&&m.toolCallId==='tool-1'));assert.ok(session.messages.some(m=>m.role==='tool_result'&&m.toolCallId==='tool-1'));
   }
   const rawConfig=materialize(structuredClone(config),vars);rawConfig.audit=path.join(dir,'raw-audit');fs.writeFileSync(rawConfig.audit,'');delete rawConfig.turns[0].append;
   const rawScenario=path.join(dir,'raw-scenario.json');fs.writeFileSync(rawScenario,JSON.stringify(rawConfig));
   const raw=spawnSync(fake,start.argv,{env:{...env,LMMOCK_JSONLINES_SCENARIO:rawScenario},cwd:workspace,input:testCase.request.stdin,encoding:'utf8'});assert.ifError(raw.error);assert.equal(raw.signal,null);
   assert.equal(raw.status,profile.stderr?1:0,'native fake exit');assert.deepEqual(raw.stdout.trim().split('\n').filter(Boolean).map(JSON.parse),[...rawConfig.init.events,...rawConfig.turns[0].events],'exact ordered wire events');
   if(profile.stderr)assert.match(raw.stderr,/Invalid API key/);else assert.equal(raw.stderr,'','clean native stderr');
   if(original.id==='complete'){
    const verbose=runClient(pkg,'lmctl',['prompt',path.relative(workspace,team),'Worker','ping','--json','--verbose'],env,workspace);
    assert.equal(verbose.status,0,'verbose prompt: '+verbose.stdout+verbose.stderr);assert.match(verbose.stderr,/fixture_lookup/,'real adapter tool progress');assert.ok(verbose.stderr.indexOf('tool_use')<verbose.stderr.indexOf('tool_result'),'interleaved tool ordering');assert.doesNotMatch(verbose.stderr,/fixture reasoning/,'thinking text remains private');
    const first=structuredClone(config.turns[0]),second=structuredClone(config.turns[0]);first.close=false;second.close=false;second.expect='again';
    for(const event of second.events)if(event.type==='result'){event.uuid='terminal-2';event.total_cost_usd=0.002;}
    config.turns=[first,second];fs.writeFileSync(scenario,JSON.stringify(materialize(config,vars)));
    const before=fs.readFileSync(audit,'utf8').trim().split('\n').length;
    const prompts=path.join(dir,'prompts.txt');fs.writeFileSync(prompts,'ping\n\nagain\n');
    const batch=runClient(pkg,'lmctl',['batch',path.relative(workspace,team),'Worker',path.relative(workspace,prompts)],env,workspace);assert.equal(batch.status,0,'persistent batch: '+batch.stdout+batch.stderr);assert.equal((batch.stdout.match(/pong/g)||[]).length,2,'two bundle replies');
    const added=fs.readFileSync(audit,'utf8').trim().split('\n').slice(before).map(JSON.parse);assert.equal(added.length,3,'one process startup and two JSONL turns');assert.ok(added[0].argv);assert.match(added[1].message.content,/ping/);assert.match(added[2].message.content,/again/);
   }
   console.log(`PASS ${manifest.id}/${original.id}`);passed++;
  }
  assert.ok(passed,'selected fixture cases must run');return passed;
 }finally{fs.rmSync(scratch,{recursive:true,force:true});}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
 try{const binary=process.argv[2]||'build/lmmock';for(const name of ['claude','qwen'])await runProvider({manifestFile:`fixtures/providers/${name}-stream-json.json`,binary,only:process.env.STREAMJSON_CASE});}
 catch(e){console.error('FAIL stream-json client:',e.message);process.exitCode=1;}
}
