// Actual published lmctl, owned native process copies, no shell/provider traffic.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';

const repo=path.resolve(import.meta.dirname,'../../..');
const families=JSON.parse(fs.readFileSync(path.join(repo,'fixtures/providers/lane-e/wire.json'),'utf8'));
export function projectKey(cwd){let text='',separator=false;for(let i=0;i<cwd.length;i++){const ch=cwd[i];if('/\\:'.includes(ch)){if(!separator)text+='-';separator=true;}else if(/[A-Za-z0-9._-]/.test(ch)){text+=ch;separator=false;}else{text+='~'+cwd.charCodeAt(i).toString(16).toUpperCase().padStart(4,'0');separator=false;}}return '--'+(text.replace(/^-+/,'')||'root').slice(0,251)+'--';}
function clientPath(){
 const value=process.env.LMCTL_TEST_CLI;if(value)return path.resolve(value);
 const prefix=process.env.LMCTL_TEST_PREFIX;if(prefix)return path.join(prefix,'node_modules/@lmctl-ai/lmctl/dist/cli/index.js');
 throw Error('LMCTL_TEST_CLI or LMCTL_TEST_PREFIX must point to the privately installed published client');
}
export function workspace(root){
 // Native children resolve temporary-directory aliases (for example macOS /var).
 // Derive transcript paths from that same identity before computing projectKey.
 const work=path.join(root,'workspace 空间');fs.mkdirSync(work);return fs.realpathSync.native(work);
}
export function runCase(provider,variant,binary){
 assert.ok(Object.hasOwn(families.families,provider),'unknown fixture provider');
 const root=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock lane E '));
 const work=workspace(root);const bin=path.join(root,'bin');fs.mkdirSync(bin);
 const native=path.join(bin,provider+(process.platform==='win32'?'.exe':''));fs.copyFileSync(binary,native);fs.chmodSync(native,0o700);
 assert.equal(path.dirname(fs.realpathSync(native)),fs.realpathSync(bin),'fake executable escaped owned bin');
 const cfgFile=path.join(root,'config.json'),binding=path.join(root,'bindings.json'),data=path.join(root,'data');fs.mkdirSync(path.join(data,'muse'),{recursive:true});
 const family=families.families[provider],model=variant==='alternate'?family.alternate_model:family.model;assert.ok(model);
 const cfg={trace:path.join(root,'trace.jsonl'),model,text:'pong',session_root:path.join(root,'dsh','sessions',projectKey(work)),muse_home:path.join(data,'muse'),native_root:path.join(data,'muse','sessions'),cwd:work,request:{'--model':model},response:{stdout:family.stdout,stderr:'fixture diagnostic\n',exit:0}};
 if(provider==='agy'||provider==='pi')cfg.request['-p']='ping';
 if(provider==='lmplayer')cfg.request['--format']='json';
 if(variant==='nonzero'){cfg.response={stdout:'',stderr:'Authentication required fixture\n',exit:3};}
 if(variant==='malformed-exit'){cfg.response={stdout:'{malformed\n',stderr:'model is not supported fixture\n',exit:17};}
 if(variant==='empty-gap')cfg.response={stdout:provider==='lmplayer'?'{}\n':'',stderr:'',exit:0};
 if(variant==='v2')cfg.v2=true;
 if(variant==='malformed')cfg.malformed=true;
 if(variant==='boot-error')cfg.boot_error='Authentication required fixture';
 if(variant==='turn-error')cfg.turn_error='Authentication required fixture';
 if(variant==='bad-terminal')cfg.terminal='unrecognized';
 if(variant==='missing-terminal')cfg.missing_terminal=true;
 fs.writeFileSync(cfgFile,JSON.stringify(cfg));
 const profile=process.env.LANE_E_HANDLER_OVERRIDE;
 const code=name=>path.join(repo,'build/lane-e-'+name+'.lmcode');
 let command;
 if(provider==='dsh')command=['sidecar','--config',cfgFile,'--http-lmcode',profile||code('dsh'),'--boot-lmcode',process.env.LANE_E_BOOT_OVERRIDE||code('sidecar-boot'),'--'];
 else if(provider==='muse')command=['stdio','rpc','--lmcode',profile||code('muse'),'--scenario',cfgFile,'--alias','muse','--state',path.join(root,'counts'),'--'];
 else command=['stdio','once','--lmcode',profile||code('print'),'--scenario',cfgFile,'--'];
 fs.writeFileSync(binding,JSON.stringify({version:1,directory:bin,aliases:{[provider]:command}}));
 const env=Object.fromEntries(Object.entries(process.env).filter(([k])=>!k.startsWith('LMCTL_')&&!k.startsWith('LMBEE_')&&!k.startsWith('LMMOCK_')&&!/TOKEN|API_KEY|SECRET|PASSWORD/.test(k)&&!/^(DSH|AGY|MUSE|PI|LMPLAYER|ANTHROPIC|OPENAI|GOOGLE|GEMINI)_/.test(k)&&!/^(HTTP_PROXY|HTTPS_PROXY|ALL_PROXY|NODE_OPTIONS|NODE_USE_ENV_PROXY)$/i.test(k)));
 const prices=path.join(root,'prices');fs.mkdirSync(prices);fs.writeFileSync(path.join(prices,'model_list.json'),'{}');
 Object.assign(env,{HOME:root,USERPROFILE:root,DSH_HOME:path.join(root,'dsh'),XDG_DATA_HOME:data,LMCTL_HOME:path.join(root,'profile'),LMCTL_DB:path.join(root,'state.db'),LMCTL_PRICE_DATA:prices,PATH:bin+path.delimiter+process.env.PATH,LMMOCK_PROCESS_BINDINGS:binding});
 const cli=clientPath();
 function invoke(args){const r=spawnSync(process.execPath,['--import',pathToFileURL(path.join(repo,'tests/providers/lane-e/offline.mjs')).href,cli,...args],{cwd:work,env,encoding:'utf8'});if(r.error)throw r.error;return r;}
 function parsed(r){const lines=r.stdout.trim().split(/\r?\n/).filter(Boolean);return JSON.parse(lines.at(-1));}
 let nativeFacts={};
 const successful=['success','alternate','v2','resume'].includes(variant);
 try{
  const version=invoke(['--version']);assert.equal(version.status,0);assert.equal(version.stdout.trim(),'lmctl '+families.client_version,'published client pin changed');
  if(!['dsh','muse'].includes(provider)&&variant!=='empty-gap'){
   const args=provider==='lmplayer'?['run','--model',model,'--format','json','--dangerously-skip-permissions','--','ping']:['-p','ping','--model',model];
   const direct=spawnSync(native,args,{cwd:work,env,encoding:'utf8'});if(direct.error)throw direct.error;
   assert.equal(direct.status,cfg.response.exit,'native exit contract');assert.equal(direct.stdout,cfg.response.stdout,'native stdout contract');assert.equal(direct.stderr,cfg.response.stderr,'native stderr contract');
   nativeFacts={native_exit:direct.status,native_stdout:direct.stdout,native_stderr:direct.stderr};
  }
  const first=invoke(['prompt','--provider',provider,'--model',model,'--json','ping']);
  let raw=parsed(first),exit=first.status;let usage;
  if(variant==='empty-gap'){
   assert.equal(first.status,0);assert.equal(raw.status,'ok');assert.equal(raw.replyText,'');
   // This is a diagnosed immutable-client defect, not a qualified success case.
   return {exit:0,result:{status:'known-client-gap',provider,version:families.client_version,observed:raw,qualified:false}};
  }
  if(raw.status==='ok')assert.notEqual(raw.replyText,'','successful empty reply is never qualified');
  if(successful){assert.equal(first.status,0,first.stdout+first.stderr);assert.equal(raw.status,'ok');assert.equal(raw.replyText,'pong','handler reply mismatch');assert.equal(raw.requestedModel,model);}
  else{assert.equal(first.status,1,'adverse fixture was accepted as success');assert.equal(raw.status,'error');assert.ok(raw.errorClass,'real client error_class missing');}
  const traces=()=>fs.readFileSync(cfg.trace,'utf8').trim().split(/\r?\n/).filter(Boolean).map(JSON.parse);
  if(successful&&provider==='dsh'){
   const rows=traces();assert.deepEqual(rows.find(r=>r.event==='boot').argv,['web','--no-open','--port','0']);assert.equal(rows.find(r=>/selectModel$/.test(r.method??'')).payload.model,model);
  }
  if(successful&&provider==='muse'){const start=traces().find(r=>r.method==='session/start');assert.equal(start.params.modelId,model);assert.equal(start.params.sessionId,raw.sessionId);}
  if(successful&&provider!=='dsh'&&provider!=='muse'){
   const args=traces().at(-1).argv;assert.equal(args[args.indexOf('--model')+1],model);assert.equal(args.includes('--conversation')||args.includes('--session'),false,'fresh prompt unexpectedly resumed');
   if(provider==='agy'){assert.ok(args.includes('--dangerously-skip-permissions'));assert.equal(args[args.indexOf('--print-timeout')+1],'0');}
   if(provider==='lmplayer'){assert.equal(args[0],'run');assert.equal(args[args.indexOf('--format')+1],'json');assert.equal(args.at(-2),'--');assert.equal(raw.sessionId,'fixture-player-session');}
  }
  let resumed=false;
  if(successful&&(provider==='dsh'||provider==='muse')){
   const read=invoke(['export',raw.sessionId,'--provider',provider,'--json']);assert.equal(read.status,0,read.stderr);const session=JSON.parse(read.stdout);
   assert.equal(session.totalInputTokens,5,'native input token fact');assert.equal(session.totalOutputTokens,2,'native output token fact');assert.equal(session.lastTokenUsage.reasoningOutputTokens,1);assert.equal(session.cwd,work);
   usage={input_tokens:session.totalInputTokens,output_tokens:session.totalOutputTokens,reasoning_output_tokens:session.lastTokenUsage.reasoningOutputTokens};
   if(variant==='resume'){
    const firstSID=raw.sessionId;const team=path.join(root,'team.lmctl');fs.writeFileSync(team,`_MEMBER_ alias=Fixture provider=${provider} model=${model} sessionid=${firstSID} sessiondir=${JSON.stringify(work)}\n`);
    const second=invoke(['prompt',path.relative(work,team),'Fixture','ping','--json']);assert.equal(second.status,0,second.stdout+second.stderr);const reply=parsed(second);assert.equal(reply.replyText,'pong');
    const reread=invoke(['export',firstSID,'--provider',provider,'--json']);assert.equal(reread.status,0,reread.stderr);const next=JSON.parse(reread.stdout);assert.equal(next.totalInputTokens,10,'restart lost or duplicated input token facts');assert.equal(next.totalOutputTokens,4);
    if(provider==='dsh'){const boots=traces().filter(r=>r.event==='boot');assert.equal(boots.length,2);assert.notEqual(boots[0].pid,boots[1].pid,'DSH sidecar did not restart');assert.equal(traces().filter(r=>/session[./]create/.test(r.method??'')).length,1,'resume recreated the session');}
    resumed=true;
   }
  }
  return {exit,result:{status:raw.status,provider,model:raw.requestedModel,reply:raw.replyText??'',...(raw.errorClass?{error_class:raw.errorClass}:{}),...(usage?{usage}:{}),tokens_exposed:Boolean(usage),resumed,...nativeFacts,raw}};
 }finally{fs.rmSync(root,{recursive:true,force:true});}
}
if(process.argv[1]&&import.meta.url===pathToFileURL(path.resolve(process.argv[1])).href){
 try{const {exit,result}=runCase(process.argv[2],process.argv[3],path.resolve(process.env.LMMOCK_TEST_BINARY||'build/lmmock'+(process.platform==='win32'?'.exe':'')));process.stdout.write(JSON.stringify(result)+'\n');process.exitCode=exit;}
 catch(error){process.stderr.write('FAIL lane-E '+(error.stack??error)+'\n');process.exitCode=1;}
}
