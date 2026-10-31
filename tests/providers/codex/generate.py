#!/usr/bin/env python3
"""Synthetic Codex protocol data; model families share one annotated handler."""
import json
from pathlib import Path
root=Path(__file__).resolve().parents[3]
sid='11111111-1111-4111-8111-111111111111';fork='22222222-2222-4222-8222-222222222222'
def event(method,params):return {'method':method,'params':params}
def cell(thread=sid,tid='turn-1',text='pong',usage=None):
 usage=usage or {'inputTokens':100,'cachedInputTokens':30,'outputTokens':12,'reasoningOutputTokens':4,'totalTokens':112}
 p={'threadId':thread,'turnId':tid};item={'id':'message-'+tid,'type':'agentMessage','phase':'final_answer','text':text}
 return {'alias':'Worker','turn':1,'turn_id':tid,'text':text,'expect':'ping','events':[
 event('turn/started',{'threadId':thread,'turn':{'id':tid,'status':'inProgress'}}),
 event('item/started',{**p,'item':{**item,'text':''}}),
 event('item/agentMessage/delta',{**p,'itemId':item['id'],'delta':text[:2]}),
 event('item/agentMessage/delta',{**p,'itemId':item['id'],'delta':text[2:]}),
 event('item/completed',{**p,'item':item}),
 event('thread/tokenUsage/updated',{**p,'model':'gpt-5.6-sol','tokenUsage':{'last':usage,'total':usage,'modelContextWindow':128000}}),
 event('turn/completed',{'threadId':thread,'turn':{'id':tid,'status':'completed','error':None}})]}
normal=cell();malformed=cell(usage={'inputTokens':'not-an-integer','cachedInputTokens':30,'outputTokens':12,'reasoningOutputTokens':4,'totalTokens':112})
foreign=cell();foreign['events'].insert(5,event('item/agentMessage/delta',{'threadId':sid,'turnId':'foreign-turn','itemId':'foreign','delta':'MUST-NOT-APPEAR'}))
wrong=cell();wrong['events'][-1]['params']['turn']['id']='wrong-turn';wrong['close']=True
missing=cell();missing['events']=missing['events'][:-1];missing['close']=True
failed=cell();failed['events'].insert(-1,event('error',{'threadId':sid,'turnId':'turn-1','willRetry':False,'error':{'message':'fixture app-server failure'}}));failed['events'][-1]['params']['turn'].update(status='failed',error={'message':'fixture app-server failure'})
waiting=cell();waiting['events']=waiting['events'][:4];waiting['expect']='cancel';waiting['wait_for_interrupt']=True
second=cell(tid='turn-2',text='dependent answer');second['turn']=2;second['expect']='pong'
wire={'session':sid,'forkSession':fork,'model':'gpt-5.6-sol','models':['gpt-5.6-sol','o3'],'selected':'success','cases':{
'success':{'cells':[normal,second]},'fork':{'cells':[cell(thread=fork)]},'malformed-usage':{'cells':[malformed]},'foreign-event':{'cells':[foreign]},'wrong-turn-id':{'cells':[wrong]},'missing-terminal':{'cells':[missing]},'app-error':{'cells':[failed]},'rpc-error':{'cells':[{'alias':'Worker','turn':1,'error':'fixture app-server RPC failure','rpc_code':-32001}]},'interrupt':{'cells':[waiting,second]}}}
wire['cells']=wire['cases']['success']['cells']
(root/'fixtures/codex/wire.json').write_text(json.dumps(wire,indent=2)+'\n')
cases=[]
for name,kind,selected,mode,model in [
 ('success','usage','success','agent','gpt-5.6-sol'),('reasoning-model','model','success','agent','o3'),
 ('resume','success','success','resume','gpt-5.6-sol'),('persistent','success','success','batch','gpt-5.6-sol'),
 ('app-error','error','app-error','agent','gpt-5.6-sol'),('rpc-error','error','rpc-error','agent','gpt-5.6-sol'),
 ('wrong-turn-id','error','wrong-turn-id','agent','gpt-5.6-sol'),('missing-terminal','error','missing-terminal','agent','gpt-5.6-sol'),
 ('foreign-event','stream','foreign-event','agent','gpt-5.6-sol'),('malformed-usage','usage','malformed-usage','agent','gpt-5.6-sol')]:
 success=name not in ['app-error','rpc-error','wrong-turn-id','missing-terminal']
 expect={'exit_code':0,'json':{'provider.status':'ok' if success else 'error','row_count':2 if mode=='batch' else 1}}
 if success:expect['stdout_contains']='pong'
 else:expect['error_class']='unknown'
 if name=='malformed-usage':expect['json'].update({'usage_reason':'counter_not_exposed','cost_reason':'usage_unavailable'})
 elif success:expect['usage']={'input_tokens':100,'cached_input_tokens':30,'output_tokens':12,'reasoning_output_tokens':4}
 faults=[]
 if name=='wrong-turn-id':faults=['event-order']
 if name=='missing-terminal':faults=['missing-terminal']
 if name=='malformed-usage':faults=['malformed-usage']
 cases.append({'id':name,'kind':kind,'request':{'kind':'process','executable':'node','argv':['tests/providers/codex/manifest.mjs'],'env':{'LMMOCK_CODEX_CASE':selected,'LMMOCK_CODEX_MODE':mode,'LMMOCK_CODEX_MODEL':model}},'expect':expect,**({'faults':faults} if faults else {})})
manifest={'schema_version':1,'id':'codex-app-server','dialect':{'id':'codex-app-server','version':'lmctl-0.2.448','transport':'json-rpc'},'source':{'kind':'synthetic','reference':'https://developers.openai.com/codex/app-server','version':'lmctl-0.2.448; mono584fee2a adapter','retrieved':'2026-10-31','note':'Synthetic stdio only. Exact tested client is published npm0.2.448. Foreign turn events are ignored; malformed usage remains provider success with unavailable accounting. Specialized protocol/usage error classes are NOT claimed; strict client controls record this unmet requirement.'},'server':{'kind':'stdio','handler':'fixtures/codex/handler.lms','config':'fixtures/codex/wire.json','binary':'codex'},'models':[{'id':'gpt-5.6-sol','family':'codex-gpt','capabilities':['chat','reasoning','stream']},{'id':'o3','family':'codex-o-series','capabilities':['chat','reasoning','stream']}],'routes':[{'id':method.replace('/','-'),'method':'RPC','path':'/'+method,'annotation':annotation,'sam':'RPCHandler'} for method,annotation in [('initialize','initialize'),('initialized','initialized'),('thread/start','thread'),('thread/resume','thread'),('thread/fork','thread'),('turn/start','turn'),('turn/interrupt','interrupt')]],'cases':cases,'faults':[{'id':fault,'surface':'stream' if fault!='malformed-usage' else 'process','trigger':{'case_id':name},'description':description} for fault,name,description in [('event-order','wrong-turn-id','Only terminal event has a foreign turn ID; close the process without completing the active turn.'),('missing-terminal','missing-terminal','Close after the valid prefix without a terminal turn event.'),('malformed-usage','malformed-usage','Emit inputTokens as a string; current client marks accounting unavailable but does not fail the turn.')]]}
(root/'fixtures/providers/codex-app-server.json').write_text(json.dumps(manifest,indent=2)+'\n')
