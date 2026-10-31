#!/usr/bin/env python3
"""Record a local Copilot stand-in through lminspect; never contacts a vendor.
Run inside lmmock exec, with LMMOCK_URL pointing at examples/copilot.lms.
Raw output exists only in a private temporary directory and is never printed.
"""
import json,os,pathlib,socket,subprocess,sys,tempfile,urllib.request
root=pathlib.Path(__file__).resolve().parents[2]
mock=str(root/'build/lmmock');inspect=os.environ['LMINSPECT_BIN']
target=os.environ['LMMOCK_URL']
requests=[('GET','/models',None),('GET','/v1/models',None),('POST','/auto',{}),
 ('GET','/copilot_internal/team_customizations',None),('GET','/copilot_internal/managed_settings',None),
 ('POST','/mcp/readonly',{}),('POST','/telemetry',{}),
 ('POST','/responses',{'model':'gpt-5.3-codex','input':[],'stream':True}),
 ('POST','/v1/responses',{'model':'gpt-5.3-codex','input':[],'stream':True}),
 ('POST','/v1/messages',{'model':'claude-haiku-4.5','messages':[{'role':'user','content':'synthetic hello'}],'stream':True}),
 ('POST','/v1/chat/completions',{'model':'gpt-4o','messages':[{'role':'user','content':'synthetic hello'}],'stream':True}),
 ('GET','/models',None)]
with tempfile.TemporaryDirectory(prefix='lmmock-private-capture-') as temp:
 raw=pathlib.Path(temp)/'raw.jsonl'
 with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
 child=subprocess.Popen([inspect,'run','--listen',f'127.0.0.1:{port}','--mode','reverse:'+target,'--record',str(raw)],stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
 try:
  while True:
   line=child.stderr.readline()
   if b'proxy listening' in line:break
   if not line:raise RuntimeError('capture proxy failed to start')
  # Socket readiness, no replay sleeps or timing model.
  while True:
   try:
    with socket.create_connection(('127.0.0.1',port)):break
   except ConnectionRefusedError:
    if child.poll() is not None:raise RuntimeError('proxy exited before readiness')
  opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
  for i,(method,path,body) in enumerate(requests):
   headers={'Authorization':'Bearer '+('copilot-test-key' if i<len(requests)-1 else 'fake-invalid-credential'),'Content-Type':'application/json'}
   request=urllib.request.Request(f'http://127.0.0.1:{port}'+path,data=None if body is None else json.dumps(body,separators=(',',':')).encode(),headers=headers,method=method)
   try:response=opener.open(request)
   except urllib.error.HTTPError as error:response=error
   response.read();response.close()
 finally:
  child.terminate();child.wait();child.stderr.close()
 destination=pathlib.Path(sys.argv[1]);destination.mkdir(parents=True,exist_ok=True)
 subprocess.run([mock,'scenario','scrub','--input',str(raw),'--output',str(destination/'flows.jsonl')],check=True)
 subprocess.run([mock,'scenario','import','--input',str(destination/'flows.jsonl'),'--output',str(destination/'scenario.json')],check=True)
 print('PASS recorded and scrubbed',len(requests),'local Copilot wire exchanges')
