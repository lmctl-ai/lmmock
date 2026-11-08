import assert from 'node:assert/strict';
import fs from 'node:fs';
import http from 'node:http';
import Ajv2020 from 'ajv/dist/2020.js';
import Ajv from 'ajv';
import addFormats from 'ajv-formats';

const base=process.env.LMMOCK_URL;
assert.ok(base,'lmmock must provide LMMOCK_URL');

function makeStrictDefs(rawSchema,defsKey,id) {
 const copy=structuredClone(rawSchema);
 copy.$id=id;
 for(const [name,def] of Object.entries(copy[defsKey]||{})) {
  if(def&&def.type==='object'&&def.properties&&def.additionalProperties===undefined&&!name.endsWith('MetaObject')) {
   def.additionalProperties=false;
  }
 }
 return copy;
}

const raw2026=JSON.parse(fs.readFileSync(new URL('./schema/2026-07-28.schema.json',import.meta.url)));
const raw2025=JSON.parse(fs.readFileSync(new URL('./schema/2025-11-25.schema.json',import.meta.url)));
const raw2024=JSON.parse(fs.readFileSync(new URL('./schema/2024-11-05.schema.json',import.meta.url)));

const current=makeStrictDefs(raw2026,'$defs','https://lmmock.invalid/schema/2026-07-28');
const sessionSchema=makeStrictDefs(raw2025,'$defs','https://lmmock.invalid/schema/2025-11-25');
const legacy=makeStrictDefs(raw2024,'definitions','https://lmmock.invalid/schema/2024-11-05');

const modernAjv=new Ajv2020({strict:false,allErrors:true}); addFormats(modernAjv); modernAjv.addSchema(current); modernAjv.addSchema(sessionSchema);
const oldAjv=new Ajv({strict:false,allErrors:true}); addFormats(oldAjv); oldAjv.addSchema(legacy);
const checkModern=modernAjv.getSchema('https://lmmock.invalid/schema/2026-07-28#/$defs/JSONRPCMessage');
const checkSession=modernAjv.getSchema('https://lmmock.invalid/schema/2025-11-25#/$defs/JSONRPCMessage');
const checkLegacy=oldAjv.getSchema('https://lmmock.invalid/schema/2024-11-05#/definitions/JSONRPCMessage');

// Self-check: validators must never be no-ops.
assert.equal(checkModern('not-an-object'),false,'modern schema validator must reject primitives');
assert.equal(checkModern({jsonrpc:'999.0',id:1,result:{resultType:'complete'}}),false,'modern schema validator must reject invalid jsonrpc version');
assert.equal(checkModern({jsonrpc:'2.0',id:1,result:{}}),false,'modern schema validator must require resultType');
assert.equal(checkSession('not-an-object'),false,'session schema validator must reject primitives');
assert.equal(checkLegacy('not-an-object'),false,'legacy schema validator must reject primitives');

const METHOD_RESULT_DEFS={
 'server/discover':'DiscoverResult',
 'initialize':'InitializeResult',
 'ping':'EmptyResult',
 'tools/list':'ListToolsResult',
 'tools/call':'CallToolResult',
 'resources/list':'ListResourcesResult',
 'resources/read':'ReadResourceResult',
 'resources/templates/list':'ListResourceTemplatesResult',
 'prompts/list':'ListPromptsResult',
 'prompts/get':'GetPromptResult',
 'subscriptions/listen':'SubscriptionsListenResult',
};

function rawReq(method,pathStr,headers={},body='') {
 return new Promise((resolve,reject)=>{
  const u=new URL(base);
  const req=http.request({hostname:u.hostname,port:u.port,path:pathStr,method,headers},res=>{
   let data='';res.setEncoding('utf8');
   res.on('data',chunk=>data+=chunk);
   res.on('error',reject);
   res.on('end',()=>resolve({status:res.statusCode,headers:res.headers,body:data,complete:res.complete}));
  });
  req.on('error',reject);
  if(body)req.write(body);
  req.end();
 });
}
function rawPost(headers,body='{}') { return rawReq('POST','/mcp',{'content-type':'application/json',...headers},body); }

const meta={'io.modelcontextprotocol/protocolVersion':'2026-07-28','io.modelcontextprotocol/clientInfo':{name:'wp351-test',version:'1'},'io.modelcontextprotocol/clientCapabilities':{}};
let serial=0;
const request=(method,params={},extra={})=>({jsonrpc:'2.0',id:++serial,method,params:{_meta:meta,...params},...extra});

function valid(message,version='modern',resultDef='') {
 assert.equal(typeof message,'object','MCP message must be an object');
 assert.notEqual(message,null,'MCP message must not be null');
 assert.equal(message.jsonrpc,'2.0','MCP message must have jsonrpc 2.0');
 const check=version==='modern'?checkModern:version==='session'?checkSession:checkLegacy;
 const ok=check(message);
 assert.ok(ok,`invalid ${version} MCP JSON-RPC message: ${JSON.stringify(check.errors)}\npayload: ${JSON.stringify(message)}`);
 if(resultDef) {
  assert.notEqual(message.result,undefined,`expected JSON-RPC result (${resultDef}), got: ${JSON.stringify(message)}`);
  const schemaUri=version==='modern'?`https://lmmock.invalid/schema/2026-07-28#/$defs/${resultDef}`:version==='session'?`https://lmmock.invalid/schema/2025-11-25#/$defs/${resultDef}`:`https://lmmock.invalid/schema/2024-11-05#/definitions/${resultDef}`;
  const defCheck=(version==='legacy'?oldAjv:modernAjv).getSchema(schemaUri);
  assert.ok(defCheck,`missing schema definition ${schemaUri}`);
  assert.ok(defCheck(message.result),`invalid ${version} ${resultDef}: ${JSON.stringify(defCheck.errors)}\npayload: ${JSON.stringify(message.result)}`);
 }
}

async function post(body,mode='json',version='2026-07-28',headerOverrides={}) {
 const headers={'content-type':'application/json',accept:mode==='sse'?'text/event-stream, application/json':'application/json, text/event-stream','MCP-Protocol-Version':version,'Mcp-Method':body.method,'X-Mock-Reply-Mode':mode,...headerOverrides};
 if((body.method==='tools/call'||body.method==='prompts/get')&&headers['Mcp-Name']===undefined) headers['Mcp-Name']=body.params.name;
 if(body.method==='resources/read'&&headers['Mcp-Name']===undefined) headers['Mcp-Name']=body.params.uri;
 const response=await fetch(base+'/mcp',{method:'POST',headers,body:JSON.stringify(body)});
 if(mode==='json') return {response,message:await response.json(),resultDef:METHOD_RESULT_DEFS[body.method]||''};
 const reader=response.body.getReader(); let bytes='';
 while(true){const item=await reader.read();if(item.done)break;bytes+=new TextDecoder().decode(item.value);if(bytes.includes('\n\n'))break;}
 await reader.cancel();
 const line=bytes.split('\n').find(v=>v.startsWith('data: '));
 return {response,message:JSON.parse(line.slice(6)),raw:bytes,resultDef:METHOD_RESULT_DEFS[body.method]||''};
}

const scenario=process.argv[2]||'modern';
if(scenario==='modern') {
 const discover=await post(request('server/discover'));
 assert.equal(discover.response.status,200); valid(discover.message,'modern',discover.resultDef);
 assert.ok(discover.message.result.supportedVersions.includes('2026-07-28'));
 assert.equal(discover.message.result._meta?.['io.modelcontextprotocol/serverInfo']?.name,'lmmock-mcp');
 assert.equal(discover.message.result.capabilities?.resources?.subscribe,undefined);

 const list=await post(request('tools/list')); valid(list.message,'modern',list.resultDef);
 assert.ok(list.message.result.tools.some(t=>t.name==='delete_everything'&&t.annotations.destructiveHint));

 const echo=await post(request('tools/call',{name:'echo',arguments:{text:'mcp fixture'}})); valid(echo.message,'modern',echo.resultDef);
 assert.equal(echo.message.result.structuredContent.text,'mcp fixture');

 const sse=await post(request('tools/call',{name:'echo',arguments:{text:'sse fixture'}}),'sse'); valid(sse.message,'modern',sse.resultDef);
 assert.equal(sse.message.result.structuredContent.text,'sse fixture');

 // Verify slow in 2026-07-28 SSE reply mode streams all 5 notifications/progress events and the final CallToolResult, terminating cleanly
 const slowModernReq=request('tools/call',{name:'slow',arguments:{}});slowModernReq.params._meta.progressToken='modern-slow-5';
 const slowModernRaw=await rawPost({accept:'text/event-stream, application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/call','Mcp-Name':'slow','X-Mock-Reply-Mode':'sse'},JSON.stringify(slowModernReq));
 assert.equal(slowModernRaw.status,200);assert.equal(slowModernRaw.complete,true,'2026-07-28 slow SSE response must terminate cleanly with zero-length chunk');
 const slowModernEvents=slowModernRaw.body.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6)));
 assert.equal(slowModernEvents.length,6,'expected 5 progress events + 1 final result event');
 for(let i=0;i<5;i++){
  valid(slowModernEvents[i],'modern');
  assert.equal(slowModernEvents[i].method,'notifications/progress');
  assert.equal(slowModernEvents[i].params.progressToken,'modern-slow-5');
  assert.equal(slowModernEvents[i].params.progress,i+1);
  assert.equal(slowModernEvents[i].params.total,5);
 }
 valid(slowModernEvents[5],'modern','CallToolResult');
 assert.equal(slowModernEvents[5].id,slowModernReq.id);
 assert.equal(slowModernEvents[5].result.content[0].text,'slow complete');

 const add=await post(request('tools/call',{name:'add',arguments:{left:2,right:3}})); valid(add.message,'modern',add.resultDef);
 assert.equal(add.message.result.structuredContent.sum,5);

 const badAdd=await post(request('tools/call',{name:'add',arguments:{left:'bad'}}));
 assert.equal(badAdd.response.status,200); valid(badAdd.message,'modern'); assert.equal(badAdd.message.error.code,-32602);

 const unknownTool=await post(request('tools/call',{name:'no_such_tool',arguments:{}}));
 assert.equal(unknownTool.response.status,200); valid(unknownTool.message,'modern'); assert.equal(unknownTool.message.error.code,-32602);

 const failed=await post(request('tools/call',{name:'fail',arguments:{}})); valid(failed.message,'modern',failed.resultDef);
 assert.equal(failed.message.result.isError,true); assert.equal(failed.message.result.structuredContent,undefined);

 const crash=await post(request('tools/call',{name:'crash',arguments:{}})); valid(crash.message,'modern');
 assert.equal(crash.message.error.code,-32603);

 const resources=await post(request('resources/list')); valid(resources.message,'modern',resources.resultDef);
 const text=await post(request('resources/read',{uri:'mock://text'})); valid(text.message,'modern',text.resultDef);
 assert.equal(text.message.result.contents[0].text,'fixture text');

 const blob=await post(request('resources/read',{uri:'mock://blob'})); valid(blob.message,'modern',blob.resultDef);
 assert.equal(blob.message.result.contents[0].blob,'AAEC');

 const b64Item=`=?base64?${Buffer.from('mock://item/42').toString('base64')}?=`;
 const templateItem=await post(request('resources/read',{uri:'mock://item/42'}),'json','2026-07-28',{'Mcp-Name':b64Item});
 assert.equal(templateItem.response.status,200); valid(templateItem.message,'modern',templateItem.resultDef);
 assert.equal(templateItem.message.result.contents[0].uri,'mock://item/42');
 assert.equal(templateItem.message.result.contents[0].text,'item 42');
 const b64UnicodeName=`=?base64?${Buffer.from('grëët').toString('base64')}?=`;
 const unicodePrompt=await post(request('prompts/get',{name:'grëët',arguments:{}}),'json','2026-07-28',{'Mcp-Name':b64UnicodeName});
 assert.equal(unicodePrompt.response.status,200); valid(unicodePrompt.message,'modern'); assert.equal(unicodePrompt.message.error.code,-32602,'Base64-encoded UTF-8 Mcp-Name passes header validation');

 const missingRes=await post(request('resources/read',{uri:'mock://missing'}));
 assert.equal(missingRes.response.status,200); valid(missingRes.message,'modern'); assert.equal(missingRes.message.error.code,-32602);

 const templates=await post(request('resources/templates/list')); valid(templates.message,'modern',templates.resultDef);
 assert.ok(templates.message.result.resourceTemplates.some(t=>t.name==='item'));

 const prompts=await post(request('prompts/list')); valid(prompts.message,'modern',prompts.resultDef);
 assert.ok(prompts.message.result.prompts.some(p=>p.name==='greet'));

 const prompt=await post(request('prompts/get',{name:'greet',arguments:{name:'Ada'}})); valid(prompt.message,'modern',prompt.resultDef);
 assert.match(prompt.message.result.messages[0].content.text,/Ada/);

 const badPrompt=await post(request('prompts/get',{name:'unknown_prompt',arguments:{}}));
 assert.equal(badPrompt.response.status,200); valid(badPrompt.message,'modern'); assert.equal(badPrompt.message.error.code,-32602);

 // ping and initialize are removed in 2026-07-28 and must return HTTP 404 with -32601
 const pingCurrent=await post(request('ping'));
 assert.equal(pingCurrent.response.status,404); valid(pingCurrent.message,'modern'); assert.equal(pingCurrent.message.error.code,-32601);

 const initialize=request('initialize');
 const initializedCurrent=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'initialize'},body:JSON.stringify(initialize)});
 assert.equal(initializedCurrent.status,404); assert.equal((await initializedCurrent.json()).error.code,-32601,'stateless current version does not initialize a session');

 // GET /mcp and DELETE /mcp return 405 in stateless 2026-07-28
 assert.equal((await fetch(base+'/mcp',{method:'GET'})).status,405);
 assert.equal((await fetch(base+'/mcp',{method:'DELETE'})).status,405);

 // Client-sent JSON-RPC responses are rejected with 400 in stateless 2026-07-28
 const clientResponse=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28'},body:JSON.stringify({jsonrpc:'2.0',id:77,result:{ok:true}})});
 assert.equal(clientResponse.status,400,'JSON-RPC response POST is rejected in 2026-07-28');

 // Client notification in 2026-07-28 returns 202
 const notifCancel=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'notifications/cancelled'},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{_meta:meta,requestId:999}})});
 assert.equal(notifCancel.status,202);

 // subscriptions/listen flushes 200 headers immediately with notifications/subscriptions/acknowledged, then delivers notifications/tools/list_changed and SubscriptionsListenResult
 const listenerBody=request('subscriptions/listen',{notifications:{toolsListChanged:true}});
 const listenResp=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream, application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'subscriptions/listen'},body:JSON.stringify(listenerBody)});
 assert.equal(listenResp.status,200);
 const listenReader=listenResp.body.getReader();
 const ackChunk=new TextDecoder().decode((await listenReader.read()).value);
 const ackMsg=JSON.parse(ackChunk.split('\n').find(v=>v.startsWith('data: ')).slice(6));
 valid(ackMsg,'modern');
 assert.equal(ackMsg.method,'notifications/subscriptions/acknowledged');
 assert.equal(ackMsg.params._meta['io.modelcontextprotocol/subscriptionId'],listenerBody.id);

 // Also verify subscriptions/listen with toolsListChanged:false does not emit notifications/tools/list_changed
 const filteredBody=request('subscriptions/listen',{notifications:{toolsListChanged:false}});
 const filteredResp=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream, application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'subscriptions/listen'},body:JSON.stringify(filteredBody)});
 const filteredReader=filteredResp.body.getReader();
 const filteredAck=new TextDecoder().decode((await filteredReader.read()).value);
 assert.ok(filteredAck.includes('notifications/subscriptions/acknowledged'));

 await fetch(base+'/__mock/list-changed',{method:'POST'});
 let restText='';
 while(!restText.includes('"resultType":"complete"')){
  const item=await listenReader.read();if(item.done)break;
  restText+=new TextDecoder().decode(item.value);
 }
 await listenReader.cancel();
 const dataLines=restText.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6)));
 const changedNotif=dataLines.find(m=>m.method==='notifications/tools/list_changed');
 assert.ok(changedNotif,'expected notifications/tools/list_changed on subscriptions/listen');
 valid(changedNotif,'modern');
 assert.equal(changedNotif.params._meta['io.modelcontextprotocol/subscriptionId'],listenerBody.id);
 const listenFinal=dataLines.find(m=>m.result!==undefined);
 assert.ok(listenFinal,'expected SubscriptionsListenResult');
 valid(listenFinal,'modern','SubscriptionsListenResult');

 const raceFiltered=await Promise.race([filteredReader.read(),new Promise(r=>setTimeout(()=>r({filteredOut:true}),80))]);
 assert.equal(raceFiltered.filteredOut,true,'toolsListChanged:false must not emit notifications/tools/list_changed');
 await filteredReader.cancel();

 const deletion=await post(request('tools/call',{name:'delete_everything',arguments:{}}));
 valid(deletion.message,'modern',deletion.resultDef); assert.match(deletion.message.result.content[0].text,/no deletion/);
 const state=await (await fetch(base+'/__mock/state')).json();
 assert.equal(state.deleteCalls,1,'destructive tool records call without affecting external state');
 assert.ok(state.calls.some(c=>c.method==='tools/call'&&c.name==='delete_everything'),'state control exposes recorded MCP calls');

 const reset=await fetch(base+'/__mock/reset',{method:'POST'});assert.equal(reset.status,200);
 const cleared=await (await fetch(base+'/__mock/state')).json();assert.equal(cleared.deleteCalls,0);assert.equal(cleared.calls.length,0,'reset clears recorded calls and counters');

 // Malformed JSON, batch array, and missing-id request checks
 const malformedJson=await rawPost({},'{bad'); assert.equal(malformedJson.status,400); assert.equal(JSON.parse(malformedJson.body).error.code,-32700);
 const batchJson=await rawPost({},'[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]'); assert.equal(batchJson.status,400); assert.equal(JSON.parse(batchJson.body).error.code,-32600);
 const missingIdReq=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/list'},body:JSON.stringify({jsonrpc:'2.0',method:'tools/list',params:{_meta:meta}})});
 assert.equal(missingIdReq.status,400); assert.equal((await missingIdReq.json()).error.code,-32600);

 // Host and Origin guard checks on /mcp and /__mock/*
 const host=await rawPost({host:'example.invalid'});assert.equal(host.status,403);
 const malformedHost=await rawPost({host:'127.0.0.1:attacker'});assert.equal(malformedHost.status,403,'malformed loopback Host ports are refused');
 const origin=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',origin:'http://attacker.invalid'},body:'{}'});assert.equal(origin.status,403);
 const ctrlBadHost=await rawReq('GET','/__mock/state',{host:'example.invalid'});assert.equal(ctrlBadHost.status,403,'control routes refuse non-loopback Host');
 const ctrlAnyOrigin=await rawReq('GET','/__mock/state',{origin:base});assert.equal(ctrlAnyOrigin.status,403,'control routes refuse any Origin header');

 const controlled=await fetch(base+'/__mock/reply-mode',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({mode:'sse'})});assert.equal(controlled.status,200);
 const controlRequest=request('server/discover');const controlReply=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'server/discover'},body:JSON.stringify(controlRequest)});assert.equal(controlReply.headers.get('content-type'),'text/event-stream');await controlReply.body.cancel();
 await fetch(base+'/__mock/reply-mode',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({mode:'json'})});

 const noAccept=request('server/discover');const refusedAccept=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json','Accept':'application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'server/discover'},body:JSON.stringify(noAccept)});assert.equal(refusedAccept.status,406,'current POST requires both JSON and SSE media types in Accept');
 const unsupported=request('server/discover',{_meta:{...meta,'io.modelcontextprotocol/protocolVersion':'2099-01-01'}});const unsupportedReply=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2099-01-01','Mcp-Method':'server/discover'},body:JSON.stringify(unsupported)});assert.equal(unsupportedReply.status,400);assert.equal((await unsupportedReply.json()).error.code,-32022);
 const auth=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',authorization:'Bearer wrong'},body:'{}'});assert.equal(auth.status,400,'protocol header mismatch is rejected');
 console.log('PASS current Streamable HTTP: JSON/SSE, tools, resources, prompts, errors, subscriptions, schema and origin/host checks');
} else if(scenario==='legacy-sse') {
 const open=await fetch(base+'/sse',{headers:{accept:'text/event-stream'}});assert.equal(open.status,200);const reader=open.body.getReader();const first=await reader.read();const text=new TextDecoder().decode(first.value);assert.match(text,/event: endpoint/);const session=text.match(/session_id=([^\n]+)/)?.[1];assert.ok(session);
 const initSent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'legacy-init',method:'initialize',params:{protocolVersion:'2099-01-01',capabilities:{},clientInfo:{name:'legacy-c',version:'1'}}})});assert.equal(initSent.status,202);
 let initStream='';while(!initStream.includes('"id":"legacy-init"')){const item=await reader.read();assert.equal(item.done,false);initStream+=new TextDecoder().decode(item.value);}
 const initData=initStream.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6))).find(m=>m.id==='legacy-init');
 valid(initData,'legacy','InitializeResult');assert.equal(initData.result.protocolVersion,'2024-11-05','initialize negotiates server version');
 const discSent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'legacy-disc',method:'server/discover',params:{}})});assert.equal(discSent.status,202);
 let discStream='';while(!discStream.includes('"id":"legacy-disc"')){const item=await reader.read();assert.equal(item.done,false);discStream+=new TextDecoder().decode(item.value);}
 const discData=discStream.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6))).find(m=>m.id==='legacy-disc');
 valid(discData,'legacy');assert.equal(discData.error.code,-32601,'server/discover is rejected on 2024-11-05');
 const sent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:'legacy-ping',method:'ping',params:{}})});assert.equal(sent.status,202);
 let stream='';while(!stream.includes('"id":"legacy-ping"')){const item=await reader.read();assert.equal(item.done,false);stream+=new TextDecoder().decode(item.value);}
 const message=stream.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6))).find(m=>m.id==='legacy-ping');
 valid(message,'legacy','EmptyResult');assert.equal(message.id,'legacy-ping');assert.ok(message.result);
 // Test uncancelled slow delivering all 5 progress events + final CallToolResult on 2024-11-05 /sse
 const slowCompleteSent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:87,method:'tools/call',params:{_meta:{progressToken:'legacy-sse-complete'},name:'slow',arguments:{}}})});
 assert.equal(slowCompleteSent.status,202);
 let completeStream='';while(!completeStream.includes('"id":87')){const item=await reader.read();assert.equal(item.done,false);completeStream+=new TextDecoder().decode(item.value);}
 const completeMsgs=completeStream.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6)));
 const completeProgs=completeMsgs.filter(m=>m.method==='notifications/progress'&&m.params.progressToken==='legacy-sse-complete');
 assert.equal(completeProgs.length,5,'expected 5 progress events on 2024-11-05 /sse');
 for(let i=0;i<5;i++){
  valid(completeProgs[i],'legacy');
  assert.equal(completeProgs[i].params.progress,i+1);
  assert.equal(completeProgs[i].params.total,5);
 }
 const completeFinal=completeMsgs.find(m=>m.id===87);
 valid(completeFinal,'legacy','CallToolResult');assert.equal(completeFinal.result.content[0].text,'slow complete');
 // Test slow with progressToken and notifications/cancelled on 2024-11-05 stopping within one polling step
 const slowPromise=fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',id:88,method:'tools/call',params:{_meta:{progressToken:'legacy-sse-prog'},name:'slow',arguments:{}}})});
 let progStream='';while(!progStream.includes('notifications/progress')){const item=await reader.read();assert.equal(item.done,false);progStream+=new TextDecoder().decode(item.value);}
 const progMsg=progStream.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6))).find(m=>m.method==='notifications/progress');
 valid(progMsg,'legacy');assert.equal(progMsg.params.progressToken,'legacy-sse-prog');
 const cancelStart=Date.now();
 const cancelSent=await fetch(base+'/messages?session_id='+encodeURIComponent(session),{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:88}})});assert.equal(cancelSent.status,202);
 const slowSent=await slowPromise;assert.equal(slowSent.status,202);
 assert.ok(Date.now()-cancelStart<180,'cancelled 2024-11-05 slow must stop within one polling step');
 const stLegacy=await (await fetch(base+'/__mock/state')).json();
 assert.equal(stLegacy.cancelledSlow,1,'2024-11-05 notifications/cancelled cancels slow');
 assert.equal(stLegacy.cancelledByVersion?.['2024-11-05'],1);
 assert.deepEqual(stLegacy.cancelledRequestIds,[88]);
 await reader.cancel();console.log('PASS legacy HTTP+SSE endpoint, message POST and JSON-RPC response event');
} else if(scenario==='legacy-session') {
 const init={jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2099-01-01',capabilities:{},clientInfo:{name:'wp351-test',version:'1'}}};
 const initialized=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify(init)});
 assert.equal(initialized.status,200);const sid=initialized.headers.get('mcp-session-id');assert.ok(sid);const initBody=await initialized.json();valid(initBody,'session','InitializeResult');assert.equal(initBody.result.protocolVersion,'2025-11-25','initialize negotiates server version');
 const disc2025=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:9,method:'server/discover',params:{}})});
 assert.equal((await disc2025.json()).error.code,-32601,'server/discover is rejected on 2025-11-25');
 const pingResp=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:10,method:'ping',params:{}})});
 assert.equal(pingResp.status,200);const pingBody=await pingResp.json();valid(pingBody,'session','EmptyResult');
 const missingSid=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify({jsonrpc:'2.0',id:2,method:'tools/list',params:{}})});assert.equal(missingSid.status,400);
 const unknownSid=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':'unknown-sid'},body:JSON.stringify({jsonrpc:'2.0',id:2,method:'tools/list',params:{}})});assert.equal(unknownSid.status,404);
 const badPostAccept=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:2,method:'tools/list',params:{}})});assert.equal(badPostAccept.status,406);
 const list=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:3,method:'tools/list',params:{}})});assert.equal(list.status,200);const listBody=await list.json();valid(listBody,'session','ListToolsResult');assert.ok(listBody.result.tools.length>0);
 const badGetVersion=await fetch(base+'/mcp',{headers:{accept:'text/event-stream','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2024-11-05'}});assert.equal(badGetVersion.status,400);
 const badGetAccept=await fetch(base+'/mcp',{headers:{accept:'application/json','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25'}});assert.equal(badGetAccept.status,406);
 const badGetHost=await rawReq('GET','/mcp',{host:'example.invalid',accept:'text/event-stream','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25'});assert.equal(badGetHost.status,403);
 const badDeleteHost=await rawReq('DELETE','/mcp',{host:'example.invalid','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25'});assert.equal(badDeleteHost.status,403);
 // Verify GET /mcp flushes 200 headers immediately even before any event is queued, and two concurrent GET streams both receive list-changed
 const secondInit=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify({...init,id:11})});const sid2=secondInit.headers.get('mcp-session-id');assert.ok(sid2);
 const getStream1=await fetch(base+'/mcp',{headers:{accept:'text/event-stream','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25'}});assert.equal(getStream1.status,200);
 const getStream2=await fetch(base+'/mcp',{headers:{accept:'text/event-stream','Mcp-Session-Id':sid2,'MCP-Protocol-Version':'2025-11-25'}});assert.equal(getStream2.status,200);
 const r1=getStream1.body.getReader();const r2=getStream2.body.getReader();
 const changed=await (await fetch(base+'/__mock/list-changed',{method:'POST'})).json();
 const c1=new TextDecoder().decode((await r1.read()).value);const c2=new TextDecoder().decode((await r2.read()).value);
 assert.ok(c1.includes('id: '+changed.eventId)&&c1.includes('notifications/tools/list_changed'));
 assert.ok(c2.includes('id: '+changed.eventId)&&c2.includes('notifications/tools/list_changed'));
 await r1.cancel();await r2.cancel();
 const resume=await fetch(base+'/mcp',{headers:{accept:'text/event-stream','Mcp-Session-Id':sid,'MCP-Protocol-Version':'2025-11-25','Last-Event-ID':'previous-event'}});const resumeReader=resume.body.getReader();const resumeChunk=await resumeReader.read();const replay=new TextDecoder().decode(resumeChunk.value);assert.ok(replay.includes('id: '+changed.eventId),'Last-Event-ID replay: '+JSON.stringify(replay)+' expected '+changed.eventId);assert.ok(replay.includes('notifications/tools/list_changed'));await resumeReader.cancel();
 // Verify slow in 2025-11-25 SSE reply mode keeps stream open, sends all 5 progress events + final result, and terminates HTTP chunked response cleanly
 const slowSessionRaw=await rawPost({accept:'text/event-stream, application/json','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid,'X-Mock-Reply-Mode':'sse'},JSON.stringify({jsonrpc:'2.0',id:30,method:'tools/call',params:{_meta:{progressToken:'session-slow-5'},name:'slow',arguments:{}}}));
 assert.equal(slowSessionRaw.status,200);assert.equal(slowSessionRaw.complete,true,'2025-11-25 slow SSE response must terminate cleanly with zero-length chunk');
 const slowSessionEvents=slowSessionRaw.body.split('\n').filter(v=>v.startsWith('data: ')).map(v=>JSON.parse(v.slice(6)));
 assert.equal(slowSessionEvents.length,6,'expected 5 progress events + 1 final result event in 2025-11-25 SSE mode');
 for(let i=0;i<5;i++){
  valid(slowSessionEvents[i],'session');
  assert.equal(slowSessionEvents[i].method,'notifications/progress');
  assert.equal(slowSessionEvents[i].params.progressToken,'session-slow-5');
  assert.equal(slowSessionEvents[i].params.progress,i+1);
  assert.equal(slowSessionEvents[i].params.total,5);
 }
 valid(slowSessionEvents[5],'session','CallToolResult');
 assert.equal(slowSessionEvents[5].id,30);
 assert.equal(slowSessionEvents[5].result.content[0].text,'slow complete');
 const expired=await fetch(base+'/__mock/expire-session',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({sessionId:sid})});assert.equal(expired.status,200);
 const gone=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:4,method:'tools/list',params:{}})});assert.equal(gone.status,404);
 const removed=await fetch(base+'/mcp',{method:'DELETE',headers:{'Mcp-Session-Id':sid2,'MCP-Protocol-Version':'2025-11-25'}});assert.equal(removed.status,200);assert.equal((await fetch(base+'/mcp',{method:'DELETE',headers:{'Mcp-Session-Id':sid2,'MCP-Protocol-Version':'2025-11-25'}})).status,404);
 console.log('PASS legacy Streamable HTTP session initialization, required session, expiry and DELETE');
} else if(scenario==='progress-cancel') {
 const body=request('tools/call',{name:'slow',arguments:{}});body.params._meta.progressToken='case-11';
 const response=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'text/event-stream, application/json','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/call','Mcp-Name':'slow','X-Mock-Reply-Mode':'sse'},body:JSON.stringify(body)});
 const reader=response.body.getReader();const first=await reader.read();const chunk=new TextDecoder().decode(first.value);const line=chunk.split('\n').find(v=>v.startsWith('data: '));const progress=JSON.parse(line.slice(6));valid(progress,'modern');assert.equal(progress.method,'notifications/progress');assert.equal(progress.params.progressToken,'case-11');
 const cancelAt=Date.now();
 await reader.cancel();
 let state=await (await fetch(base+'/__mock/state')).json();
 while(state.cancelledSlow<1&&Date.now()-cancelAt<180){await new Promise(r=>setTimeout(r,10));state=await (await fetch(base+'/__mock/state')).json();}
 assert.ok(Date.now()-cancelAt<180,'closing 2026-07-28 SSE must stop slow within one polling step');
 assert.equal(state.cancelledSlow,1,'closing current SSE cooperatively cancels the slow handler');
 assert.equal(state.cancelledByVersion?.['2026-07-28'],1);
 assert.deepEqual(state.cancelledRequestIds,[body.id]);
 console.log('PASS progress token delivery and cancellation by closing Streamable HTTP response');
} else if(scenario==='legacy-cancel') {
 const init={jsonrpc:'2.0',id:8,method:'initialize',params:{protocolVersion:'2025-11-25',capabilities:{},clientInfo:{name:'wp351-test',version:'1'}}};
 const initialized=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25'},body:JSON.stringify(init)});const sid=initialized.headers.get('mcp-session-id');assert.ok(sid);
 // In 2025-11-25, disconnecting an SSE stream WITHOUT sending notifications/cancelled must NOT increment cancelledSlow
 const disconnectBody={jsonrpc:'2.0',id:54,method:'tools/call',params:{_meta:{progressToken:'disc-progress'},name:'slow',arguments:{}}};
 const discResp=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid,'X-Mock-Reply-Mode':'sse'},body:JSON.stringify(disconnectBody)});
 const discReader=discResp.body.getReader();await discReader.read();await discReader.cancel();
 await new Promise(r=>setTimeout(r,120));
 const stBefore=await (await fetch(base+'/__mock/state')).json();
 assert.equal(stBefore.cancelledSlow,0,'HTTP disconnect alone in 2025-11-25 must not count as notifications/cancelled');
 assert.equal(stBefore.cancelledByVersion?.['2025-11-25'],0);
 assert.deepEqual(stBefore.cancelledRequestIds,[]);
 // Cooperative cancellation while POST /mcp remains open returns 204 within one polling step
 const requestBody={jsonrpc:'2.0',id:55,method:'tools/call',params:{_meta:{progressToken:'legacy-progress'},name:'slow',arguments:{}}};
 const responsePromise=fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify(requestBody)});
 await new Promise(r=>setTimeout(r,40));
 const cancelStart=Date.now();
 const cancelled=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:55}})});assert.equal(cancelled.status,202);
 const response=await responsePromise;assert.equal(response.status,204,'cancelled request must not send a JSON-RPC response');assert.equal(await response.text(),'');
 assert.ok(Date.now()-cancelStart<180,'notifications/cancelled must stop slow within one polling step');
 const state=await (await fetch(base+'/__mock/state')).json();
 assert.equal(state.cancelledSlow,1,'notifications/cancelled stops a running slow tool');
 assert.equal(state.cancelledByVersion?.['2025-11-25'],1);
 assert.deepEqual(state.cancelledRequestIds,[55]);
 // Client abort of in-flight POST followed immediately by notifications/cancelled (go-sdk / lmmcp pattern) must also count deterministically
 const abortCtrl=new AbortController();
 const racePromise=fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:56,method:'tools/call',params:{name:'slow',arguments:{}}}),signal:abortCtrl.signal}).catch(()=>null);
 await new Promise(r=>setTimeout(r,40));
 abortCtrl.abort();
 await racePromise;
 const raceCancel=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:56}})});assert.equal(raceCancel.status,202);
 const stateAfterRace=await (await fetch(base+'/__mock/state')).json();
 assert.equal(stateAfterRace.cancelledSlow,2,'notifications/cancelled after in-flight POST abort must increment cancelledSlow');
 assert.equal(stateAfterRace.cancelledByVersion?.['2025-11-25'],2);
 assert.deepEqual(stateAfterRace.cancelledRequestIds,[55,56]);
 // Sending notifications/cancelled for an already-completed slow request or unknown requestId must not increment cancelledSlow
 const doneSlow=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',id:57,method:'tools/call',params:{name:'slow',arguments:{}}})});
 assert.equal(doneSlow.status,200);
 await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:57}})});
 await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Session-Id':sid},body:JSON.stringify({jsonrpc:'2.0',method:'notifications/cancelled',params:{requestId:9999}})});
 const stateFinal=await (await fetch(base+'/__mock/state')).json();
 assert.equal(stateFinal.cancelledSlow,2,'completed or non-slow requestId must not increment cancelledSlow');
 assert.deepEqual(stateFinal.cancelledRequestIds,[55,56]);
 console.log('PASS legacy notifications/cancelled aborts cooperative slow request without sending response');
} else if(scenario==='auth') {
 const response=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json'},body:'{}'});assert.equal(response.status,401);assert.equal(response.headers.get('www-authenticate'),'Bearer');
 const ctrlUnauth=await fetch(base+'/__mock/state');assert.equal(ctrlUnauth.status,401);
 console.log('PASS optional bearer returns 401 challenge on /mcp and /__mock/*');
} else if(scenario==='mismatch') {
 const body=request('server/discover');
 const noMethod=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28'},body:JSON.stringify(body)});assert.equal(noMethod.status,400);assert.equal((await noMethod.json()).error.code,-32020);
 const noVer=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','Mcp-Method':'server/discover'},body:JSON.stringify(body)});assert.equal(noVer.status,400);assert.equal((await noVer.json()).error.code,-32020);
 const diffVer=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2025-11-25','Mcp-Method':'server/discover'},body:JSON.stringify(body)});assert.equal(diffVer.status,400);assert.equal((await diffVer.json()).error.code,-32020);
 const callBody=request('tools/call',{name:'echo',arguments:{text:'x'}});
 const badName=await fetch(base+'/mcp',{method:'POST',headers:{'content-type':'application/json',accept:'application/json, text/event-stream','MCP-Protocol-Version':'2026-07-28','Mcp-Method':'tools/call','Mcp-Name':'wrong'},body:JSON.stringify(callBody)});assert.equal(badName.status,400);assert.equal((await badName.json()).error.code,-32020);
 console.log('PASS mismatched MCP version/method/name headers return HeaderMismatch (-32020)');
}
