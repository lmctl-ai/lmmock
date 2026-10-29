import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
if(process.argv[3]==='client') {
 const origin=process.env.LMMOCK_URL;
 const file=process.env.MOCK_CONFIG;
 let config=JSON.parse(fs.readFileSync(file));
 const set=patch=>{config={...config,...patch};fs.writeFileSync(file,JSON.stringify(config));};
 if(process.argv[4]==='oauth') {
  const redirect='http://127.0.0.1:1/callback';
  const verifier='v'.repeat(43), challenge=createHash('sha256').update(verifier).digest('base64url');
  const auth=async(extra={})=>fetch(origin+'/authorize?'+new URLSearchParams({client_id:'test',redirect_uri:redirect,response_type:'code',state:'test-state',code_challenge:challenge,code_challenge_method:'S256',scope:'read',...extra}),{redirect:'manual'});
  const code=async()=>{const r=await auth();assert.equal(r.status,302);const u=new URL(r.headers.get('location'));assert.equal(u.searchParams.get('state'),'test-state');return u.searchParams.get('code');};
  const token=(fields,secret='fixture-secret')=>fetch(origin+'/token',{method:'POST',headers:{Authorization:'Basic '+Buffer.from('test:'+secret).toString('base64'),'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams(fields)});
  const grant=c=>({grant_type:'authorization_code',code:c,redirect_uri:redirect,code_verifier:verifier});
  assert.equal((await auth({redirect_uri:'https://invalid.example/callback'})).status,400);
  assert.equal((await auth({code_challenge:''})).status,400);
  const c=await code();const replies=await Promise.all([token(grant(c)),token(grant(c))]);assert.deepEqual(replies.map(r=>r.status).sort(),[200,400]);
  const issued=await replies.find(r=>r.status===200).json();
  assert.equal((await fetch(origin+'/api/userinfo',{headers:{Authorization:'Bearer '+issued.access_token}})).status,200);
  assert.equal((await fetch(origin+'/api/userinfo',{headers:{Authorization:'Bearer invalid'}})).status,401);
  assert.equal((await token(grant(await code()),'x'.repeat(1000))).status,401);
  assert.equal((await token({...grant(await code()),code_verifier:'wrong'.repeat(10)})).status,400);
  assert.equal((await token({...grant(await code()),redirect_uri:redirect+'/wrong'})).status,400);
  set({codeLifetimeMs:-1});assert.equal((await token(grant(await code()))).status,400);set({codeLifetimeMs:60000});
  const refreshed=await token({grant_type:'refresh_token',refresh_token:issued.refresh_token});assert.equal(refreshed.status,200);const rotated=await refreshed.json();assert.notEqual(rotated.refresh_token,issued.refresh_token);
  assert.equal((await token({grant_type:'refresh_token',refresh_token:issued.refresh_token})).status,400);
  assert.equal((await token({grant_type:'client_credentials'})).status,400);
  for(const [mode,status] of [['server-error',500],['rate-limit',429]]){set({misbehave:mode});assert.equal((await token({grant_type:'refresh_token',refresh_token:rotated.refresh_token})).status,status);}
  set({misbehave:'bad-json'});await assert.rejects(async()=> (await token({grant_type:'refresh_token',refresh_token:rotated.refresh_token})).json());
  const log=fs.readFileSync(config.logFile,'utf8');for(const secret of ['fixture-secret',verifier,issued.access_token,issued.refresh_token,rotated.access_token])assert.ok(!log.includes(secret),'audit must not expose secret');
 } else {
  const post=(body,opts={})=>fetch(origin+'/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),...opts});
  const body={model:'test',messages:[{role:'user',content:'ping'}]};
  for(const [mode,status] of [['unauthorized',401],['rate-limit',429],['server-error',503]]){set({mode});assert.equal((await post(body)).status,status);}
  set({mode:'bad-json'});await assert.rejects(async()=> (await post(body)).json());
  set({mode:'timeout'});await assert.rejects(()=>post(body,{signal:AbortSignal.timeout(30)}));
  set({mode:'disconnect'});await assert.rejects(()=>post(body));
  set({mode:''});const stream=await post({...body,stream:true,tools:[{type:'function',function:{name:'lookup'}}]});const text=await stream.text();assert.match(text,/text|tool_calls/);assert.ok(text.includes('call_1'));assert.ok(text.endsWith('data: [DONE]\n\n'));
 }
 console.log('PASS '+process.argv[4]+' protocol and adversarial cases');
} else {
 const binary=path.resolve(process.argv[2]);const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock protocols '));
 try {for(const scenario of ['oauth','llm']) {
  const config=path.join(dir,scenario+'.json');const logFile=path.join(dir,'audit.jsonl');fs.writeFileSync(logFile,'');
  fs.writeFileSync(config,JSON.stringify({clients:{test:{clientSecret:'fixture-secret',redirectUris:['http://127.0.0.1:1/callback'],authStyle:'header'}},requirePkce:true,rotateRefreshTokens:true,logFile}));
  const r=spawnSync(binary,['exec','--dsl',`build/${scenario}.dslb`,'--config',config,'--',process.execPath,import.meta.filename,binary,'client',scenario],{env:{...process.env,MOCK_CONFIG:config},encoding:'utf8',timeout:30000});assert.equal(r.status,0,r.stdout+r.stderr);process.stdout.write(r.stdout);
 }}finally{fs.rmSync(dir,{recursive:true,force:true});}
}
