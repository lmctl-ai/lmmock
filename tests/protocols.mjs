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
 } else if(process.argv[4]==='github') {
  set({apiKey:'fixture-github-token',strictHeaders:true});
  const headers={'Accept':'application/vnd.github+json','User-Agent':'lmclient','X-GitHub-Api-Version':'2022-11-28'};
  const get=(p,extra={})=>fetch(origin+p,{headers:{...headers,...extra}});
  assert.equal((await fetch(origin+'/user')).status,400);
  assert.equal((await get('/user')).status,401);
  assert.equal((await get('/user',{Authorization:'Bearer wrong'})).status,401);
  assert.deepEqual(await (await get('/user',{Authorization:'Bearer fixture-github-token'})).json(),{login:'octo',name:'Octo Cat',id:583231,public_repos:8});
  const repos=await (await get('/orgs/acme/repos?per_page=30&sort=full_name')).json();
  assert.deepEqual(repos.map(r=>r.name),['a','b']); assert.equal(repos[1].description,null);
  const page=await get('/orgs/paged/repos?per_page=3&sort=full_name');
  assert.deepEqual((await page.json()).map(r=>r.name),['r1','r2']);
  const next=new URL(page.headers.get('link').match(/<([^>]+)>/)[1],origin);
  assert.deepEqual((await (await get(next.pathname+next.search)).json()).map(r=>r.name),['r3','r4']);
  assert.equal((await get('/orgs/mike/repos')).status,404);
  assert.equal((await (await get('/users/mike/repos')).json())[0].name,'personal');
  for(const [owner,status] of [['forbidden',403],['broken',500],['nobody',404]]) assert.equal((await get('/orgs/'+owner+'/repos')).status,status);
  assert.deepEqual(await (await get('/orgs/weird/repos')).json(),{not:'a list'});
  const denied=await get('/orgs/acme/repos',{Authorization:'Bearer do-not-echo'});assert.equal(denied.status,401);assert.ok(!(await denied.text()).includes('do-not-echo'));
 } else if(process.argv[4]==='bitbucket') {
  set({username:'override-user',appPassword:'override-password'});
  const headers={Authorization:'Basic '+Buffer.from('override-user:override-password').toString('base64')};
  assert.equal((await fetch(origin+'/2.0/user')).status,401);
  assert.equal((await fetch(origin+'/2.0/user',{headers})).status,200);
  const first=await (await fetch(origin+'/2.0/repositories/fixture',{headers})).json();
  assert.equal(new URL(first.next).origin,origin);
  const last=await (await fetch(first.next,{headers})).json();
  assert.equal(last.values[0].slug,'second');assert.equal(Object.hasOwn(last,'next'),false);
 } else if(process.argv[4]==='circleci') {
  set({token:'override-token'});
  assert.equal((await fetch(origin+'/api/v2/me',{headers:{'Circle-Token':'fixture-token'}})).status,401);
  assert.equal((await fetch(origin+'/api/v2/me',{headers:{'Circle-Token':'override-token'}})).status,200);
 } else if(process.argv[4]==='paypal') {
  set({clientId:'override-client',secret:'override-secret',token:'override-access'});
  const headers={Authorization:'Bearer override-access'};
  assert.equal((await fetch(origin+'/v1/identity/oauth2/userinfo',{headers})).status,401);
  const issued=await fetch(origin+'/v1/oauth2/token',{method:'POST',headers:{Authorization:'Basic '+Buffer.from('override-client:override-secret').toString('base64'),'Content-Type':'application/x-www-form-urlencoded'},body:'grant_type=client_credentials'});
  assert.equal(issued.status,200);assert.equal((await issued.json()).access_token,'override-access');
  assert.equal((await fetch(origin+'/v1/identity/oauth2/userinfo',{headers})).status,200);
 } else {
  const post=(body,opts={})=>fetch(origin+'/v1/chat/completions',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),...opts});
  const body={model:'test',messages:[{role:'user',content:'ping'}]};
  for(const [mode,status] of [['unauthorized',401],['rate-limit',429],['server-error',503]]){set({mode});assert.equal((await post(body)).status,status);}
  set({mode:'retry'});
  const retry=await post(body);assert.equal(retry.status,429);assert.equal(retry.headers.get('retry-after'),'1');
  assert.equal((await post(body)).status,200);
  set({mode:'unknown-model'});const missing=await post(body);assert.equal(missing.status,404);assert.equal((await missing.json()).error.code,'model_not_found');
  set({mode:'',apiKey:'fixture-key'});
  for(const route of ['/chat/completions','/api/v1/chat/completions','/v1/chat/completions']) {
   const request={model:'override',max_tokens:12,messages:[{role:'system',content:'be brief'},{role:'user',content:'stdin 世界'}]};
   const response=await fetch(origin+route,{method:'POST',headers:{Authorization:'Bearer fixture-key','Content-Type':'application/json'},body:JSON.stringify(request)});
   assert.equal(response.status,200);const reply=await response.json();assert.equal(reply.model,'override');assert.deepEqual(reply.usage,{prompt_tokens:2,completion_tokens:2,total_tokens:4});
   const seen=await (await fetch(origin+'/__inspect')).json();assert.equal(seen.url,route);assert.equal(seen.headers.authorization,'Bearer fixture-key');assert.deepEqual(JSON.parse(seen.body),request);
   const overloaded=await fetch(origin+route,{method:'POST',headers:{Authorization:'Bearer fixture-key','Content-Type':'application/json'},body:JSON.stringify({...request,model:'overloaded'})});
   assert.equal(overloaded.status,429);assert.equal(overloaded.headers.get('retry-after'),'1');
  }
  set({mode:'ok',responseModel:'fixture-model'});assert.equal((await (await post(body,{headers:{Authorization:'Bearer fixture-key'}})).json()).model,'fixture-model');
  delete config.apiKey;delete config.responseModel;
  set({mode:'bad-json'});await assert.rejects(async()=> (await post(body)).json());
  set({mode:'timeout'});await assert.rejects(()=>post(body,{signal:AbortSignal.timeout(30)}));
  set({mode:'disconnect'});await assert.rejects(()=>post(body));
  set({mode:''});const stream=await post({...body,stream:true,tools:[{type:'function',function:{name:'lookup'}}]});const text=await stream.text();assert.match(text,/text|tool_calls/);assert.ok(text.includes('call_1'));assert.ok(text.endsWith('data: [DONE]\n\n'));
 }
 console.log('PASS '+process.argv[4]+' protocol and adversarial cases');
} else {
 const binary=path.resolve(process.argv[2]);const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock protocols '));
 try {for(const scenario of ['oauth','llm','github','bitbucket','circleci','paypal']) {
  const config=path.join(dir,scenario+'.json');const logFile=path.join(dir,'audit.jsonl');fs.writeFileSync(logFile,'');
  fs.writeFileSync(config,JSON.stringify({clients:{test:{clientSecret:'fixture-secret',redirectUris:['http://127.0.0.1:1/callback'],authStyle:'header'}},requirePkce:true,rotateRefreshTokens:true,logFile}));
  const r=spawnSync(binary,['exec','--dsl',`build/${scenario}.dslb`,'--config',config,'--',process.execPath,import.meta.filename,binary,'client',scenario],{env:{...process.env,MOCK_CONFIG:config},encoding:'utf8',timeout:30000});assert.equal(r.status,0,r.stdout+r.stderr);process.stdout.write(r.stdout);
 }
  // n8n connector presets: each fixture serves its own suite through `exec` + `check`.
  for(const preset of ['slack','discord','telegram','jira','stripe','notion','airtable','hubspot','gitlab','asana','trello','pagerduty','datadog','linear','twilio','sendgrid','mailchimp','bitbucket','circleci','paypal','dropbox','box','cloudflare','shopify','wordpress','woocommerce','clickup','typeform']) {
   const r=spawnSync(binary,['exec','--dsl',`build/${preset}.dslb`,'--',binary,'check','--suite',`examples/${preset}-client.json`],{encoding:'utf8',timeout:60000});
   assert.equal(r.status,0,preset+': '+r.stdout+r.stderr);const result=JSON.parse(r.stdout.trim().split('\n').pop());assert.equal(result.ok,true,preset);
   console.log(`PASS ${preset} connector preset (${result.passed} checks)`);
  }
  for (const preset of ['shopify','wordpress','woocommerce']) {
   const config=path.join(dir,preset+'.json');
   fs.writeFileSync(config,JSON.stringify({token:'override-token',username:'override-user',password:'override-secret',consumerKey:'override-user',consumerSecret:'override-secret'}));
   for (const mode of ['paging','override']) {
    const args=['exec','--dsl',`build/${preset}.dslb`,...(mode==='override'?['--config',config]:[]),'--',process.execPath,'tests/commerce.mjs',preset,mode];
    const r=spawnSync(binary,args,{encoding:'utf8',timeout:30000});
    assert.equal(r.status,0,r.stdout+r.stderr);process.stdout.write(r.stdout);
   }
  }
 }finally{fs.rmSync(dir,{recursive:true,force:true});}
}
