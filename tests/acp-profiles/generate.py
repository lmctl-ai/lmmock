#!/usr/bin/env python3
"""Deterministic synthetic fixture authoring; no capture or model lookup."""
import json,pathlib
root=pathlib.Path(__file__).resolve().parents[2]
def write(path,value):
 p=root/path;p.parent.mkdir(parents=True,exist_ok=True);p.write_text(json.dumps(value,indent=2)+'\n')
profiles={
 'copilot':{'argv':['--acp','--yolo'],'configure':[],'reason':'session_totals_only','families':['gpt','claude','gemini','grok']},
 'kimi':{'argv':['acp'],'configure':[{'method':'session/set_config_option','key':'value','value':'auto','configId':'mode'},{'method':'session/set_config_option','key':'value','value':'$model','configId':'model'}],'reason':'native_record_missing','families':['kimi-k2','kimi-k3']},
 'opencode':{'argv':['acp'],'configure':[{'method':'session/set_config_option','key':'value','value':'$model','configId':'model'},{'method':'session/set_config_option','key':'value','value':'high','configId':'effort'}],'reason':'native_record_missing','families':['configured-provider-model']},
 'hermes':{'argv':['acp'],'configure':[{'method':'session/set_mode','key':'modeId','value':'dont_ask'},{'method':'session/set_model','key':'modelId','value':'$model'}],'reason':'session_totals_only','families':['deepseek','nous']},
 'gemini':{'argv':['--acp','-y','--skip-trust','--model','$model'],'configure':[],'reason':'turn_not_completed','families':['gemini','gemma']},
 'miniplayer':{'argv':['acp','-m','$model'],'configure':[],'reason':'native_record_missing','families':['anthropic','openai','google','moonshot','deepseek','ollama','openrouter']},
}
def model_id(profile,family):
 if profile=='opencode':return 'fixture-provider/fixture-'+family
 if profile=='hermes':return family+':fixture-chat'
 if profile=='miniplayer':return family+'/fixture-chat'
 return 'fixture-'+profile+'-'+family
sid='11111111-2222-4333-8444-555555555555'
events=[
 {'sessionUpdate':'agent_thought_chunk','content':{'type':'text','text':'private fixture reasoning'}},
 {'sessionUpdate':'tool_call','toolCallId':'tool-fixture','title':'Read synthetic note','kind':'read','status':'in_progress','rawInput':{'path':'fixture.txt'}},
 {'sessionUpdate':'tool_call_update','toolCallId':'tool-fixture','status':'completed','content':[{'type':'content','content':{'type':'text','text':'fixture note'}}]},
 {'sessionUpdate':'usage_update','size':99,'used':7,'cost':{'amount':0,'currency':'USD'}},
 {'sessionUpdate':'agent_message_chunk','content':{'type':'text','text':'po'}},
 {'sessionUpdate':'agent_message_chunk','content':{'type':'text','text':'ng'}},
]
for profile,meta in profiles.items():
 model=model_id(profile,meta['families'][0])
 args=[x.replace('$model',model) for x in meta['argv']]
 steps=json.loads(json.dumps(meta['configure']).replace('$model',model))
 cases=[];faults=[]
 variants=['success','auth','cancel','missing','stale','permission']+(['config'] if steps else [])+(['mode-error'] if profile in ['kimi','hermes'] else [])
 for variant in variants:
  cfg={'profile':profile,'model':model,'initial_model':'fixture-default','session':sid,'argv':args,'configure':steps,'fault':variant if variant in ['auth','config','stale','mode-error'] else '',
       'replay':[{'sessionUpdate':'agent_message_chunk','content':{'type':'text','text':'old replay must not leak'}}],
       'cells':[{'alias':'Worker','turn':1,'expect':'ping','events':events if variant not in ['cancel','missing','permission'] else [events[1]] if variant=='permission' else [events[-2]],'completion':variant if variant in ['cancel','missing','permission'] else 'end_turn'},
                {'alias':'Worker','turn':2,'expect':'ping','events':events[-2:],'completion':'end_turn'}]}
  path='fixtures/acp/'+profile+'-'+variant+'.json';write(path,cfg)
  expect={'exit_code':0,'stdout_contains':'pong','model':model,'usage':{'source':None,'inputUncached':None,'cacheRead':None,'cacheWrite':None,'output':None,'reasoning':None,'costUsd':None,'reason':meta['reason']}}
  expect['json']={('usage.'+k):v for k,v in expect.pop('usage').items()}
  if variant=='auth':expect={'exit_code':0,'error_class':'auth_required','stdout_contains':'Authentication required'}
  if variant=='cancel':expect={'exit_code':0,'json':{'events':['agent_message_chunk','cancelled','end_turn']}}
  if variant=='mode-error':expect={'exit_code':0,'error_class':'auth_required'}
  if variant=='permission':expect={'exit_code':0,'stdout_contains':'permission rejected'}
  if variant=='missing':expect={'exit_code':0,'error_class':'unknown'}
  if variant=='stale':expect={'exit_code':0,'stdout_contains':'pong','stderr_contains':'session/load failed'}
  if variant=='config':expect={'exit_code':0,**({'error_class':'model_not_offered'} if profile=='hermes' else {'stdout_contains':'pong','stderr_contains':'set_config_option'})}
  # Gemini is a wire-profile fixture, not an enabled session adapter claim.
  if profile=='gemini' and variant in ['success','auth','stale','missing','permission']:expect={'exit_code':0,'error_class':'unknown','stdout_contains':'Standalone Gemini CLI support is disabled'}
  case={'id':profile+'-'+variant,'kind':'error' if variant in ['auth','missing','stale','config','mode-error'] else 'stream','request':{'kind':'process','executable':'node','argv':['tests/acp-profiles/run-case.mjs',profile+'-'+variant],'env':{'LMMOCK_ACP_SCENARIO':path}},'expect':expect}
  if variant in ['auth','missing','stale']:
   fault={'auth':'auth-invalid','missing':'missing-terminal','stale':'session-stale'}[variant]
   case['faults']=[fault];faults.append({'id':fault,'surface':'session' if variant=='stale' else 'stream' if variant=='missing' else 'process','trigger':{'case_id':case['id']},'description':'Deterministic ACP '+variant+' fixture; no delay or vendor contact.'})
  cases.append(case)
 manifest={'schema_version':1,'id':'acp-'+profile,'dialect':{'id':'acp-'+profile,'version':'1','transport':'acp'},'source':{'kind':'synthetic','reference':'https://agentclientprotocol.com/protocol/v1/prompt-turn','version':'ACP v1; npm lmctl 0.2.448; mono 8e334dda','retrieved':'2026-10-31','note':'Synthetic model families and wire values. Gemini is profile-only: the published adapter is disabled. Usage metadata is not billing usage.'},
 'server':{'kind':'stdio','handler':'fixtures/acp/handler.lms','config':'fixtures/acp/'+profile+'-success.json','binary':'lmmock'},
 'models':[{'id':model_id(profile,family),'family':family,'capabilities':['chat','tools','stream'],'price_record':None}for family in meta['families']],
 'routes':[{'id':name.replace('/','-'),'method':'RPC','path':'/'+name,'annotation':annotation,'sam':'RPCHandler'}for name,annotation in [('initialize','initialize'),('session/new','open'),('session/load','open'),('session/prompt','prompt'),('session/cancel','cancel')]],'cases':cases,'faults':faults}
 write('fixtures/providers/acp-'+profile+'.json',manifest)
write('tests/acp-profiles/profiles.json',{name:{'model':model_id(name,m['families'][0]),'usage_reason':m['reason'],'enabled':name!='gemini'}for name,m in profiles.items()})
