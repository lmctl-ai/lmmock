// Published lmctl/lmscript only; scratch HOME/database and a guarded fake binary.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const binary=path.resolve(process.argv[2]||'build/lmmock');
const compiler=process.env.LMSCRIPT_BIN||'lmscript';
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-lmctl-'));
const quote=s=>"'"+s.replaceAll("'","'\\''")+"'";
try{
 const bin=path.join(dir,'bin');fs.mkdirSync(bin);const fixture=path.join(dir,'scenario.json');
 fs.writeFileSync(fixture,JSON.stringify({session:'fixture-session',cells:[{alias:'Worker',turn:1,expect:'ping',text:'pong',chunks:['po','ng']},{alias:'Worker',turn:2,expect:'ping',text:'pong'}]}));
 const shim=path.join(bin,'miniplayer');fs.writeFileSync(shim,'#!/bin/sh\nexec '+[binary,'stdio','acp','--scenario',fixture,'--alias','Worker','--state',path.join(dir,'counters')].map(quote).join(' ')+' -- "$@"\n',{mode:0o700});
 // This seam guard runs before any provider call; it is deliberately duplicated
 // by the production owned-peer guard, and refuses PATH escape/symlinks.
 function guard(p){assert.equal(path.dirname(fs.realpathSync(p)),fs.realpathSync(bin),'provider executable escaped fake bin');}
 guard(shim);assert.throws(()=>guard(process.execPath),/escaped fake bin/);
 const team=path.join(dir,'team.lmctl');fs.writeFileSync(team,'_MEMBER_ alias=Worker provider=miniplayer model=fixture-model sessionid=fixture-session sessiondir='+dir+'\n');
 const script=path.join(dir,'drive.lms');fs.writeFileSync(script,
 'r = lm_agent({provider:"miniplayer", model:"fixture-model", sessionid:"fixture-session"}, "ping"); assert(r.status == "ok"); assert(r.reply == "pong");\n'+
 'r = lm_prompt('+JSON.stringify(team)+', "Worker", "ping"); assert(r.status == "ok"); assert(r.reply == "pong"); println("PASS published lm_agent and lm_prompt through ACP");\n');
 const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith('LMCTL_')&&!key.startsWith('LMBEE_')&&!/TOKEN|API_KEY|SECRET|PASSWORD/.test(key)));
 Object.assign(env,{HOME:dir,USERPROFILE:dir,LMCTL_HOME:path.join(dir,'profile'),LMCTL_DB:path.join(dir,'state.db'),XDG_CACHE_HOME:path.join(dir,'cache'),PATH:bin+path.delimiter+process.env.PATH});
 const run=spawnSync(compiler,['--no-preflight',script],{cwd:dir,env,encoding:'utf8'});assert.equal(run.status,0,run.stdout+run.stderr);assert.match(run.stdout,/PASS published/);process.stdout.write(run.stdout);
 // A third turn is unscripted. It must remain a client-visible error, not an ok
 // empty reply or the last successful value.
 fs.writeFileSync(script,'r = lm_agent({provider:"miniplayer", model:"fixture-model", sessionid:"fixture-session"}, "ping"); assert(r.status != "ok"); println("PASS unscripted ACP turn remains failure");\n');
 const bad=spawnSync(compiler,['--no-preflight',script],{cwd:dir,env,encoding:'utf8'});assert.equal(bad.status,0,bad.stdout+bad.stderr);assert.match(bad.stdout,/PASS unscripted/);process.stdout.write(bad.stdout);
 console.log('MCP service integration not claimed: npm lmctl 0.2.448 ships no lmbee/mcp service entrypoint; standalone owned MCP peer test covers protocol.');
}finally{fs.rmSync(dir,{recursive:true,force:true});}
