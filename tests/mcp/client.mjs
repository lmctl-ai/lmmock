import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import Ajv2020 from 'ajv/dist/2020.js';
import Ajv from 'ajv';
import addFormats from 'ajv-formats';

const base=process.env.LMMOCK_URL;
assert.ok(base,'lmmock must provide LMMOCK_URL');
const current=JSON.parse(fs.readFileSync(new URL('./schema/2026-07-28.schema.json',import.meta.url)));
const legacy=JSON.parse(fs.readFileSync(new URL('./schema/2024-11-05.schema.json',import.meta.url)));
const sessionSchema=JSON.parse(fs.readFileSync(new URL('./schema/2025-11-25.schema.json',import.meta.url)));
current.$id='https://lmmock.invalid/schema/2026-07-28'; sessionSchema.$id='https://lmmock.invalid/schema/2025-11-25';
current.$id='https://lmmock.invalid/schema/2026-07-28'; legacy.$id='https://lmmock.invalid/schema/2024-11-05';
const modernAjv=new Ajv2020({strict:false}); addFormats(modernAjv); modernAjv.addSchema(current); modernAjv.addSchema(sessionSchema);
const oldAjv=new Ajv({schemaId:'auto',strict:false}); addFormats(oldAjv); oldAjv.addSchema(legacy);
const checkModern=modernAjv.compile(current);
const checkSession=modernAjv.compile(sessionSchema);
const checkLegacy=oldAjv.compile(legacy);
function rawPost(headers,body='{}') { return new Promise((resolve,reject)=>{const u=new URL(base);const req=http.request({hostname:u.hostname,port:u.port,path:'/mcp',method:'POST',headers:{'content-type':'application/json',...headers}},res=>{let data='';res.setEncoding('utf8');res.on('data',chunk=>data+=chunk);res.on('end',()=>resolve({status:res.statusCode,headers:res.headers,body:data}));});req.on('error',reject);req.end(body);}); }
const meta={'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientInfo':{name:'wp351-test',version:'1'},'io.modelcontextprotocol/clientCapabilities':{}};
let serial=0;
const request=(method,params={},extra={})=>({jsonrpc:'2.0',id:++serial,method,params:{_meta:meta,...params},...extra});
async function post(body,mode='json',version='2026-07-28') {
 const headers={'content-type':'application/json',accept:mode==='sse'?'text/event-stream, application/json':'application/json, text/event-stream','MCP-Protocol-Version':version,'Mcp-Method':body.method,'X-Mock-Reply-Mode':mode};
 if(body.method==='tools/call'||body.method==='prompts/get') headers['Mcp-Name']=body.params.name;
 if(body.method==='resources/read') headers['Mcp-Name']=body.params.uri;
 const response=await fetch(base+'/mcp',{method:'POST',headers,body:JSON.stringify(body)});
 if(mode==='json') return {response,message:await response.json()};
 const reader=response.body.getReader(); let bytes='';
 while(true){const item=await reader.read();if(item.done)break;bytes+=new TextDecoder().decode(item.value);if(bytes.includes('\n\n'))break;}
 await reader.cancel();
 const line=bytes.split('\n').find(v=>v.startsWith('data: '));
 return {response,message:JSON.parse(line.slice(6)),raw:bytes};
}
function valid(message,version='modern') { const check=version==='modern'?checkModern:version==='session'?checkSession:checkLegacy; const ok=check(message); assert.ok(ok,`invalid ${version} MCP JSON-RPC message: ${JSON.stringify(check.errors)}`); }

const scenario=process.argv[2]||'modern';
if(scenario==='modern') {
 const discover=await post(request('server/discover'));
 assert.equal(discover.response.status,200); valid(discover.message); assert.ok(discover.message.result.supportedVersions.includes('2026-07-28'));
 const list=await post(request('tools/list')); valid(list.message); assert.ok(list.message.result.tools.some(t=>t.name==='delete_everything'&&t.annotations.destructiveHint));
 const echo=await post(request('tools/call',{name:'echo',arguments:{text:'mcp fixture'}})); valid(echo.message); assert.equal(echo.message.result.structuredContent.text,'mcp fixture');
 const sse=await post(request('tools/call',{name:'echo',arguments:{text:'sse fixture'}}),'sse'); valid(sse.message); assert.equal(sse.message.result.structuredContent.text,'sse fixture');
 const add=await post(request('tools/call',{name:'add',arguments:{left:2,right:3}})); valid(add.message); assert.equal(add.message.result.structuredContent.sum,5);
 const failed=await post(request('tools/call',{name:'fail',arguments:{}})); valid(failed.message); assert.equal(failed.message.result.isError,true);
 const crash=await post(request('tools/call',{name:'crash',arguments:{}})); valid(crash.message); assert.equal(crash.message.error.code,-32603);
 const resources=await post(request('resources/list'));valid(resources.message);
 const text=await post(request('resources/read',{uri:'mock://text'}));valid(text.message);assert.equal(text.message.result.contents[0].text,'fixture text');
 const blob=await post(request('resources/read',{uri:'mock://blob'}));valid(blob.message);assert.equal(blob.message.result.contents[0].blob,'AAEC');
 const templates=await post(request('resources/templates/list'));valid(templates.message);assert.ok(templates.message.result.resourceTemplates.some(t=>t.name==='item'));
 const prompts=await post(request('prompts/list'));valid(prompts.message);
 assert.ok(prompts.message.result.prompts.some(p=>p.name==='greet'));
 const prompt=await post(request('prompts/get',{name:'greet',arguments:{name:'Ada'}}));valid(prompt.message);assert.match(prompt.message.result.messages[0].content.text,/Ada/);
 const ping=await post(request('ping'));valid(ping.message);
 assert.ok(ping.message.result);
 const initialize=request('initialize');const initializedCurrent=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'initialize'},body:JSON.stringify(initialize)});assert.equal((await initializedCurrent.json()).error.code,-32601,'stateless current version does not initialize a session');
 const clientResponse=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:77,result:{ok:true}})});assert.equal(clientResponse.status,202,'JSON-RPC response POST is accepted');
 const listenerBody=request('subscriptions/listen',{notifications:{toolsListChanged:true}});const listenerPromise=post(listenerBody,'sse');await fetch(base+'/__mock/list-changed',{method:'POST'});const listen=await listenerPromise;valid(listen.message);assert.equal(listen.message.method,'notifications/tools/list_changed');
 const deletion=await post(request('tools/call',{name:'delete_everything',arguments:{}}));valid(deletion.message);assert.match(deletion.message.result.content[0].text,/no deletion/);
 const state=await (await fetch(base+'/__mock/state')).json();assert.equal(state.deleteCalls,1,'destructive tool records call without affecting external state');assert.ok(state.calls.some(c=>c.method==='tools/call'&&c.name==='delete_everything'),'state control exposes recorded MCP calls');
 const reset=await fetch(base+'/__mock/reset',{method:'POST'});assert.equal(reset.status,200);const cleared=await (await fetch(base+'/__mock/state')).json();assert.equal(cleared.deleteCalls,0);assert.equal(cleared.calls.length,0,'reset clears recorded calls and counters');
 const host=await rawPost({host:'example.invalid'});assert.equal(host.status,403);const malformedHost=await rawPost({host:'127.0.0.1:attacker'});assert.equal(malformedHost.status,403,'malformed loopback Host ports are refused');
 const origin=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',origin:'http://attacker.invalid'},body:'{}'});assert.equal(origin.status,403);
 const controlled=await fetch(base+'/__mock/reply-mode',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({mode:'sse'})});assert.equal(controlled.status,200);
 const controlRequest=request('ping');const controlReply=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'ping'},body:JSON.stringify(controlRequest)});assert.equal(controlReply.headers.get('content-type'),'text/event-stream');await controlReply.body.cancel();
 await fetch(base+'/__mock/reply-mode',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({mode:'json'})});
 const noAccept=request('ping');const refusedAccept=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','Accept':'application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'ping'},body:JSON.stringify(noAccept)});assert.equal(refusedAccept.status,406,'current POST requires both JSON and SSE media types in Accept');
 const unsupported=request('ping');const unsupportedReply=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2099-01-01','Mcp-Method':'ping'},body:JSON.stringify(unsupported)});assert.equal(unsupportedReply.status,400);assert.equal((await unsupportedReply.json()).error.code,-32022);
 const auth=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',authorization:'Bearer wrong'},body:'{}'});assert.equal(auth.status,400,'protocol header mismatch is rejected');
 console.log('PASS current Streamable HTTP: JSON/SSE, tools, resources, prompts, errors, ping, schema and origin/host checks');
} else if(scenario==='legacy-sse') {
 const open=await fetch(base+'/sse',{headers:{accept:'text/event-stream'}});assert.equal(open.status,200);const reader=open.body.getReader();const first=await reader.read();const text=new TextDecoder().decode(first.value);assert.match(text,/event: endpoint/);const session=text.match(/session_id=([^\n]+)/)?.[1];assert.ok(session);
 const sent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'legacy-ping',method:'ping',params:{}})});assert.equal(sent.status,202);
 let stream='';while(!stream.includes('event: message')){const item=await reader.read();assert.equal(item.done,false);stream+=new TextDecoder().decode(item.value);}
 const data=stream.split('\n').find(v=>v.startsWith('data: '));const message=JSON.parse(data.slice(6));valid(message,'legacy');assert.equal(message.id,'legacy-ping');assert.ok(message.result);
 await reader.cancel();console.log('PASS legacy HTTP+SSE endpoint, message POST and JSON-RPC response event');
} else if(scenario==='legacy-session') {
 const init={jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2025-11-25',capabilities:{},clientInfo:{name:'wp351-test',version:'1'}}};
 const initialized=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify(init)});
 assert.equal(initialized.status,200);const sid=initialized.headers.get('mcp-session-id');assert.ok(sid);const initBody=await initialized.json();valid(initBody,'session');assert.equal(initBody.result.protocolVersion,'2025-11-25');
 const unknown=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify({jsonrpc:'2.0',id:2,method:'tools/list',params:{}})});assert.equal(unknown.status,404);
 const list=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:3,method:'tools/list',params:{}})});assert.equal(list.status,200);const listBody=await list.json();valid(listBody,'session');assert.ok(listBody.result.tools.length>0);
 const changed=await (await fetch(base+'/__mock/list-changed',{method:'POST'})).json();const resume=await fetch(base+'/mcp',{headers:{'Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25','Last-Event-ID':'previous-event'}});const resumeReader=resume.body.getReader();const resumeChunk=await resumeReader.read();const replay=new TextDecoder().decode(resumeChunk.value);assert.ok(replay.includes('id: '+changed.eventId),'Last-Event-ID replay: '+JSON.stringify(replay)+' expected '+changed.eventId);assert.ok(replay.includes('notifications/tools/list_changed'));await resumeReader.cancel();
 const expired=await fetch(base+'/__mock/expire-session',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({sessionId:sid})});assert.equal(expired.status,200);
 const gone=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:4,method:'tools/list',params:{}})});assert.equal(gone.status,404);
 const second=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify({...init,id:5})});const secondId=second.headers.get('mcp-session-id');const removed=await fetch(base+'/mcp',{method:'DELETE',headers:{'Mcp-Session-Id':secondId,'MCP-Protocol-Version':'2025-11-25'}});assert.equal(removed.status,200);assert.equal((await fetch(base+'/mcp',{method:'DELETE',headers:{'Mcp-Session-Id':secondId,'MCP-Protocol-Version':'2025-11-25'}})).status,404);
 console.log('PASS legacy Streamable HTTP session initialization, required session, expiry and DELETE');
} else if(scenario==='progress-cancel') {
 const body=request('tools/call',{name:'slow',arguments:{}});body.params._meta.progressToken='case-11';
 const response=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream, application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/call','Mcp-Name':'slow','X-Mock-Reply-Mode':'sse'},body:JSON.stringify(body)});
 const reader=response.body.getReader();const first=await reader.read();const chunk=new TextDecoder().decode(first.value);const line=chunk.split('\n').find(v=>v.startsWith('data: '));const progress=JSON.parse(line.slice(6));valid(progress);assert.equal(progress.method,'notifications/progress');assert.equal(progress.params.progressToken,'case-11');await reader.cancel();
 const state=await (await fetch(base+'/__mock/state')).json();assert.ok(state.cancelledSlow>=1,'closing current SSE cooperatively cancels the slow handler');
 console.log('PASS progress token delivery and cancellation by closing Streamable HTTP response');
} else if(scenario==='legacy-cancel') {
 const init={jsonrpc:'2.0',id:8,method:'initialize',params:{protocolVersion:'2025-11-25',capabilities:{},clientInfo:{name:'wp351-test',version:'1'}}};
 const initialized=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify(init)});const sid=initialized.headers.get('mcp-session-id');assert.ok(sid);
 const requestBody={jsonrpc:'2.0',id:55,method:'tools/call',params:{_meta:{progressToken:'legacy-progress'},name:'slow',arguments:{}}};
 const responsePromise=fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify(requestBody)});
 const cancelled=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:55}})});assert.equal(cancelled.status,202);const response=await responsePromise;assert.equal(response.status,200);const result=await response.json();valid(result,'session');assert.equal(result.id,55);
 const state=await (await fetch(base+'/__mock/state')).json();assert.ok(state.cancelledSlow>=1,'notifications/cancelled stops a running slow tool');console.log('PASS legacy notifications/cancelled aborts cooperative slow request');
} else if(scenario==='auth') {
 const response=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json'},body:'{}'});assert.equal(response.status,401);assert.equal(response.headers.get('www-authenticate'),'Bearer');console.log('PASS optional bearer returns 401 challenge');
} else if(scenario==='mismatch') {
 const body=request('server/discover');const response=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','MCP-Protocol-Version':'2026-07-28'},body:JSON.stringify(body)});assert.equal(response.status,400);assert.equal((await response.json()).error.code,-32020);console.log('PASS mismatched MCP method header returns HeaderMismatch');
}
