// Real published lmctl adapter -> pinned public Miniplayer -> local lmcode HTTP.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const home=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-openai-lmctl-'));
const source=process.env.MINIPLAYER_TEST_BIN;
assert.ok(source && fs.existsSync(source),'MINIPLAYER_TEST_BIN must name pinned real Miniplayer');
const config=process.env.MOCK_CONFIG;
const original=JSON.parse(fs.readFileSync(config));
const quote=s=>"'"+s.replaceAll("'","'\\''")+"'";
try {
 const bin=path.join(home,'bin');fs.mkdirSync(bin);
 fs.writeFileSync(path.join(bin,'miniplayer'),'#!/bin/sh\nexec '+[process.execPath,source].map(quote).join(' ')+' "$@"\n',{mode:0o700});
 const env=Object.fromEntries(Object.entries(process.env).filter(([k])=>!k.startsWith('LMCTL_')&&!k.startsWith('LMBEE_')&&!/TOKEN|API_KEY|SECRET|PASSWORD/.test(k)));
 Object.assign(env,{HOME:home,USERPROFILE:home,LMCTL_HOME:path.join(home,'lmctl'),LMCTL_DB:path.join(home,'state.db'),MINIPLAYER_HOME:path.join(home,'sessions-home'),MINIPLAYER_CONFIG:path.join(home,'config.json'),MINIPLAYER_CATALOG:path.join(home,'catalog.json'),MINIPLAYER_LMPRICE_CATALOG:path.join(home,'catalog.json'),XDG_CACHE_HOME:path.join(home,'cache'),PATH:bin+path.delimiter+process.env.PATH});
 fs.writeFileSync(env.MINIPLAYER_CATALOG,JSON.stringify({schema_version:1,records:[]}));
 for(const [dialect,provider] of [['chat','deepseek'],['responses','openai']]) {
  fs.writeFileSync(env.MINIPLAYER_CONFIG,JSON.stringify({provider:{[provider]:{apiKey:original.keys[dialect],baseURL:process.env.LMMOCK_URL+'/v1'}}}));
  for(const selected of ['success','bad-usage','auth-error']) {
   fs.writeFileSync(config,JSON.stringify({...original,selected}));
   const sessions=path.join(env.MINIPLAYER_HOME,'sessions');
   const before=new Set(fs.existsSync(sessions)?fs.readdirSync(sessions):[]);
   const script=path.join(home,'case.lms');
   fs.writeFileSync(script,'r = lm_agent({provider:"miniplayer",model:'+JSON.stringify(provider+'/fixture-gpt')+'}, "ping"); println(json_stringify(r));\n');
   const r=spawnSync(process.env.LMSCRIPT_BIN||'lmscript',['--no-preflight',script],{env,cwd:home,encoding:'utf8'});
   assert.equal(r.status,0,r.stdout+r.stderr);
   const result=JSON.parse(r.stdout.trim().split('\n').at(-1));
   assert.match(r.stderr,/^agent [0-9:]+: prompting miniplayer\n$/,dialect+' expected progress stderr');
   const added=fs.readdirSync(sessions).filter(f=>!before.has(f)&&f.endsWith('.json'));assert.equal(added.length,1,'one fresh session');
   const record=JSON.parse(fs.readFileSync(path.join(sessions,added[0])));assert.equal(record.turns.length,1);
   const turn=record.turns[0];assert.equal(turn.input.text,'ping');
   if(selected==='success') {
    assert.equal(result.status,'ok');assert.equal(result.reply,'pong');assert.equal(result.error,null);
    assert.equal(turn.status,'complete');assert.equal(turn.output.text,'pong');
    assert.deepEqual(turn.usage,{inputTokens:10,outputTokens:2,totalTokens:12,reasoningTokens:1,cachedInputTokens:3});
   } else {
    assert.equal(result.status,'error');assert.equal(result.reply,'');
    assert.match(result.error,selected==='bad-usage'?/ACP RPC error: Invalid JSON response/:/ACP RPC error: fixture auth-error/);
    assert.equal(turn.status,'failed');assert.equal(turn.output,undefined);assert.equal(turn.usage,undefined,'failed parse must not fabricate token facts');
    const logdir=path.join(home,'.lmctl');
    const logs=fs.readdirSync(logdir).filter(f=>f.endsWith('.log')).flatMap(f=>fs.readFileSync(path.join(logdir,f),'utf8').trim().split('\n').filter(Boolean).map(l=>JSON.parse(l)));
    const event=logs.findLast(e=>e.pid===r.pid&&e.provider==='miniplayer'&&e.event==='transport_error');
    assert.ok(event,'provider error telemetry');assert.equal(event.error_class,'Error');assert.equal(event.reason,'provider_no_result');
   }
   console.log('PASS published lmctl '+dialect+' '+selected+' status/session/usage/error_class');
  }
 }
}finally{fs.rmSync(home,{recursive:true,force:true});}

