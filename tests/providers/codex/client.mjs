// Actual published lmctl adapter; the native fake CLI has no provider fallback.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {DatabaseSync} from 'node:sqlite';
export const root=path.resolve(import.meta.dirname,'../../..');
export function prepareCase(selected,{model='gpt-5.6-sol',resume=false,batch=false}={}) {
 const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-codex-'));const bin=path.join(dir,'bin');fs.mkdirSync(bin);
 const suffix=process.platform==='win32'?'.exe':'';const fake=path.join(bin,'codex'+suffix);
 fs.copyFileSync(path.resolve(process.env.CODEX_FIXTURE_BIN||'build/codex-fixture'+suffix),fake);fs.chmodSync(fake,0o700);
 assert.equal(path.dirname(fs.realpathSync(fake)),fs.realpathSync(bin),'provider escaped owned fake-bin');
 const wire=JSON.parse(fs.readFileSync(path.join(root,'fixtures/codex/wire.json')));wire.model=model;wire.cells=wire.cases[selected].cells;
 const trace=path.join(dir,'trace');fs.mkdirSync(trace);const config=path.join(dir,'config.json');fs.writeFileSync(config,JSON.stringify(wire));
 const codexHome=path.join(dir,'.codex');const sessions=path.join(codexHome,'sessions','2026','10','31');fs.mkdirSync(sessions,{recursive:true});
 if(resume||batch)fs.writeFileSync(path.join(sessions,'rollout-2026-10-31T00-00-00-'+wire.session+'.jsonl'),JSON.stringify({type:'session_meta',payload:{id:wire.session,cwd:dir,model}})+'\n');
 const member={provider:'codex',alias:'Worker',model,sessiondir:dir,...(resume||batch?{sessionid:wire.session}:{})};
 const script=path.join(dir,'drive.lms');
 fs.writeFileSync(script,'m = '+JSON.stringify(member)+';\n'+(batch?
 'h=lm_batch_open(m); assert(h != null); r=lm_batch_turn(h,"ping"); r2=lm_batch_turn(h,r.reply); lm_batch_close(h); println(json_stringify({result:r,second:r2,member:m}));\n':
 'r=lm_agent(m,"ping"); f=lm_classify_failure(str(r.error)); println(json_stringify({result:r,failure:f,member:m}));\n'));
 const env=Object.fromEntries(Object.entries(process.env).filter(([k])=>!/^LMCTL_|^LMBEE_|^CODEX_|TOKEN|SECRET|PASSWORD|API_KEY|AUTHORIZATION/i.test(k)));
 delete env.NODE_OPTIONS;
 Object.assign(env,{HOME:dir,USERPROFILE:dir,CODEX_HOME:codexHome,LMCTL_HOME:path.join(dir,'profile'),LMCTL_DB:path.join(dir,'state.db'),LMCTL_NO_UPDATE_NOTIFIER:'1',LMCTL_DEBUG:'1',XDG_CACHE_HOME:path.join(dir,'cache'),PATH:bin+path.delimiter+path.dirname(process.execPath)+path.delimiter+process.env.PATH,LMMOCK_CODEX_CONFIG:config,LMMOCK_CODEX_TRACE:trace});
 const pkg=process.env.LMCTL_PACKAGE_ROOT;if(!pkg)throw Error('LMCTL_PACKAGE_ROOT must name the privately downloaded published package');
 return {dir,trace,script,env,pkg,wire,collect(processResult){
 let result;try{result=JSON.parse(processResult.stdout.trim().split('\n').at(-1));}catch{}
 const read=name=>fs.existsSync(path.join(trace,name))?fs.readFileSync(path.join(trace,name),'utf8'):'';
 const decode=text=>text.trim()?text.trim().split('\n').map(x=>JSON.parse(x)):[];
 let rows=[];if(fs.existsSync(path.join(dir,'state.db'))){const db=new DatabaseSync(path.join(dir,'state.db'),{readOnly:true});try{rows=db.prepare('SELECT * FROM team_chat_log WHERE attempt_key IS NOT NULL ORDER BY id').all();}finally{db.close();}}
 const evidence={selected,exit:processResult.status,signal:processResult.signal,stdout:processResult.stdout,stderr:processResult.stderr,result,requests:decode(read('input.jsonl')),events:decode(read('output.jsonl')),argv:read('argv.json')?JSON.parse(read('argv.json')):[],rows,dir};
 // Teardown checks the recorded owned PID independently of protocol success.
 evidence.cleanup=()=>{const pid=Number(read('pid'));if(Number.isSafeInteger(pid)&&pid>0){try{process.kill(pid,0);process.kill(pid,'SIGKILL');throw Error('owned provider survived client exit');}catch(e){if(e.code!=='ESRCH')throw e;}}fs.rmSync(dir,{recursive:true,force:true});};
 return evidence;
 }};
}
export function runCase(selected,options={}){const c=prepareCase(selected,options);return c.collect(spawnSync(process.execPath,[path.join(c.pkg,"bin/lmscript"),"--no-preflight",c.script],{cwd:c.dir,env:c.env,encoding:"utf8"}));}

if(process.argv.includes('--probe')){
 for(const selected of ['success','malformed-usage','wrong-turn-id','app-error','rpc-error']){const e=runCase(selected);console.log(JSON.stringify(e,(k,v)=>k==='cleanup'?undefined:v));e.cleanup();}
}
