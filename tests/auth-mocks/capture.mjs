// Capture mock-only GitHub, OAuth2 and npm exchanges through lminspect.
// Usage: node tests/auth-mocks/capture.mjs build/lmmock /path/to/lminspect
import assert from 'node:assert/strict';
import {createHash,randomBytes} from 'node:crypto';
import {spawn,spawnSync} from 'node:child_process';
import fs from 'node:fs';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import {once} from 'node:events';
import {fileURLToPath} from 'node:url';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const binary=path.resolve(process.argv[2]||'build/lmmock');
const inspector=path.resolve(process.argv[3]||process.env.LMINSPECT_BIN||'build/lminspect');
const output=path.join(root,'captures/auth');
fs.mkdirSync(output,{recursive:true});

async function freePort() {
  const server=net.createServer();
  await new Promise((resolve,reject)=>{server.once('error',reject);server.listen(0,'127.0.0.1',resolve);});
  const port=server.address().port;
  await new Promise((resolve,reject)=>server.close(err=>err?reject(err):resolve()));
  return port;
}
function waitForFile(child,file) {
  if(fs.existsSync(file)) return Promise.resolve();
  return new Promise((resolve,reject)=>{
    const watcher=fs.watch(path.dirname(file),()=>{
      if(fs.existsSync(file)){watcher.close();resolve();}
    });
    if(fs.existsSync(file)){watcher.close();resolve();return;}
    if(child.exitCode!==null){watcher.close();reject(Error(`lmmock exited before readiness (${child.exitCode})`));return;}
    child.once('exit',(code,signal)=>{watcher.close();reject(Error(`lmmock exited before readiness (${code??signal})`));});
  });
}
function waitForLine(child,needle) {
  return new Promise((resolve,reject)=>{
    let output='';
    if(child.exitCode!==null){reject(Error(`process exited before readiness (${child.exitCode})`));return;}
    child.stdout.setEncoding('utf8'); child.stderr.setEncoding('utf8');
    child.stdout.on('data',chunk=>{output+=chunk;if(output.includes(needle))resolve(output);});
    child.stderr.on('data',chunk=>{output+=chunk;if(output.includes(needle))resolve(output);});
    child.once('exit',(code,signal)=>reject(Error(`lminspect exited before readiness (${code??signal}): ${output}`)));
  });
}
async function waitForPort(port,child) {
  while(child.exitCode===null) {
    const listening=await new Promise(resolve=>{
      const socket=net.connect({host:'127.0.0.1',port});
      socket.once('connect',()=>{socket.destroy();resolve(true);});
      socket.once('error',()=>resolve(false));
    });
    if(listening) return;
    await new Promise(resolve=>setImmediate(resolve));
  }
  throw Error(`lminspect exited before binding 127.0.0.1:${port}`);
}
async function stop(child,expected) {
  if(child.exitCode!==null) return;
  assert.equal(path.resolve(child.spawnfile),path.resolve(expected),'only stop the exact child process started by this capture');
  child.kill('SIGTERM');
  await once(child,'exit');
}
function request(proxy,host,pathName,{method='GET',headers=[],body='',include=false}={}) {
  const args=['--silent','--show-error','--proxy',proxy,'--noproxy','','--request',method];
  if(include) args.push('--include');
  for(const header of headers) args.push('--header',header);
  if(body!=='') args.push('--data-binary',body);
  args.push('--write-out','\nWP316_STATUS:%{http_code}',`http://${host}${pathName}`);
  const result=spawnSync('curl',args,{encoding:'utf8'});
  if(result.error) throw result.error;
  assert.equal(result.status,0,`curl transport failed: ${result.stderr}`);
  const marker='\nWP316_STATUS:'; const at=result.stdout.lastIndexOf(marker);
  assert.notEqual(at,-1,'curl status marker is present');
  const status=Number(result.stdout.slice(at+marker.length).trim());
  const payload=result.stdout.slice(0,at);
  if(!include) return {status,body:payload,headers:''};
  const boundary=payload.indexOf('\r\n\r\n');
  assert.notEqual(boundary,-1,'HTTP header block is present');
  return {status,headers:payload.slice(0,boundary),body:payload.slice(boundary+4)};
}
function jsonRequest(proxy,host,pathName,body,method='POST',extraHeaders=[]) {
  const result=request(proxy,host,pathName,{method,headers:['Content-Type: application/json','Accept: application/json',...extraHeaders],body:JSON.stringify(body)});
  return {...result,json:JSON.parse(result.body)};
}
async function responseJson(response) { return JSON.parse(response.body); }
function formRequest(proxy,host,pathName,body,extraHeaders=[]) {
  return request(proxy,host,pathName,{method:'POST',headers:['Content-Type: application/x-www-form-urlencoded','Accept: application/json',...extraHeaders],body:new URLSearchParams(body).toString()});
}

async function capture(provider,host,exercise) {
  const work=fs.mkdtempSync(path.join(os.tmpdir(),'wp316-auth-capture-'));
  const ready=path.join(work,'mock-ready.json');
  const mock=spawn(binary,['serve','--lmcode',path.join(root,'build/auth',provider+'.lmcode'),'--config',path.join(root,'examples/auth',provider,'config.json'),'--listen','127.0.0.1:0','--ready-file',ready],{cwd:root,stdio:['ignore','pipe','pipe']});
  let proxy;
  try {
    await waitForFile(mock,ready);
    const info=JSON.parse(fs.readFileSync(ready,'utf8')); const mockPort=new URL(info.url).port;
    const proxyPort=await freePort();
    const target=path.join(output,provider+'.jsonl');
    fs.rmSync(target,{force:true});
    const proxyAddr=`127.0.0.1:${proxyPort}`;
    proxy=spawn(inspector,['run','--listen',proxyAddr,'--route',`${host}=127.0.0.1:${mockPort}`,'--record',target,'--insecure=false'],{cwd:root,stdio:['ignore','pipe','pipe']});
    await waitForLine(proxy,'lminspect proxy listening on');
    await waitForPort(proxyPort,proxy);
    await exercise(`http://${proxyAddr}`,host);
    await stop(proxy,inspector); proxy=null;
    assert.ok(fs.statSync(target).size>0,`lminspect capture is nonempty: ${target}`);
    console.log(`CAPTURE ${provider}: ${target}`);
  } finally {
    if(proxy) await stop(proxy,inspector);
    await stop(mock,binary);
    fs.rmSync(work,{recursive:true,force:true});
  }
}

await capture('github-device-flow','github.mock.test',async(proxy,host)=>{
  const start=jsonRequest(proxy,host,'/login/device/code',{client_id:'fixture-github-app-client',scope:'repo, workflow'});
  assert.equal(start.status,200); const code=start.json;
  jsonRequest(proxy,host,'/__mock/clock',{advance_seconds:6});
  const poll=(await responseJson(formRequest(proxy,host,'/login/oauth/access_token',{client_id:'fixture-github-app-client',device_code:code.device_code,grant_type:'urn:ietf:params:oauth:grant-type:device_code'})));
  assert.equal(poll.error,'authorization_pending');
  jsonRequest(proxy,host,'/__mock/approve',{user_code:code.user_code,decision:'approve'});
  jsonRequest(proxy,host,'/__mock/clock',{advance_seconds:6});
  const token=await responseJson(formRequest(proxy,host,'/login/oauth/access_token',{client_id:'fixture-github-app-client',device_code:code.device_code,grant_type:'urn:ietf:params:oauth:grant-type:device_code'}));
  assert.ok(token.access_token.startsWith('gho_mock_'));
  request(proxy,host,'/user',{headers:[`Authorization: Bearer ${token.access_token}`,'Accept: application/vnd.github+json']});
  request(proxy,host,'/repos/fixture-org/fixture-repo',{headers:[`Authorization: Bearer ${token.access_token}`,'Accept: application/vnd.github+json']});
});

await capture('oauth2-pkce','oauth.mock.test',async(proxy,host)=>{
  const verifier=randomBytes(32).toString('base64url');
  const challenge=createHash('sha256').update(verifier).digest('base64url');
  const redirect='http://client.example.test/callback';
  const query=new URLSearchParams({response_type:'code',client_id:'fixture-public-client',redirect_uri:redirect,scope:'profile',state:'capture-state',code_challenge:challenge,code_challenge_method:'S256'});
  const auth=request(proxy,host,'/authorize?'+query.toString(),{include:true});
  assert.equal(auth.status,302);
  const locationLine=auth.headers.split('\r\n').find(line=>line.startsWith('Location: '));
  assert.ok(locationLine,'authorization response has a redirect Location');
  const location=new URL(locationLine.slice('Location: '.length));
  assert.equal(location.searchParams.get('state'),'capture-state');
  const token=await responseJson(formRequest(proxy,host,'/oauth/token',{grant_type:'authorization_code',client_id:'fixture-public-client',code:location.searchParams.get('code'),redirect_uri:redirect,code_verifier:verifier}));
  assert.ok(token.access_token.startsWith('oauth_mock_'));
  request(proxy,host,'/oauth/userinfo',{headers:[`Authorization: Bearer ${token.access_token}`]});
});

await capture('npm-web-login','npm.mock.test',async(proxy,host)=>{
  const init=jsonRequest(proxy,host,'/-/v1/login',{},'POST',['npm-auth-type: web']);
  const done=new URL(init.json.doneUrl); const login=new URL(init.json.loginUrl);
  request(proxy,host,login.pathname);
  request(proxy,host,done.pathname+done.search);
  jsonRequest(proxy,host,'/__mock/approve',{sessionId:done.searchParams.get('sessionId')});
  const token=await responseJson(request(proxy,host,done.pathname+done.search));
  assert.ok(token.token.startsWith('npm_mock_'));
  request(proxy,host,'/-/npm/v1/user',{headers:[`Authorization: Bearer ${token.token}`]});
  request(proxy,host,'/-/npm/v1/tokens',{headers:[`Authorization: Bearer ${token.token}`]});
});
